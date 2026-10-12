package update

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lumen/internal/core"
)

// Source supplies authenticated discovery. Tests use an isolated signed feed.
type Source interface {
	Check(context.Context, core.UpdateChannel, string, int) (*Candidate, error)
}

// Delivery streams runtime bytes independently of metadata discovery.
type Delivery interface {
	Download(context.Context, Candidate, string, int, func(int64)) error
}

type journal struct {
	Format    int               `json:"format"`
	Status    core.UpdateStatus `json:"status"`
	Candidate *Candidate        `json:"candidate"`
}

// How the scheduler paces itself. Discovery is daily; what stands in its way
// (a metered connection, the battery, no connection at all) is asked about
// again far less often than the minute between looks at the policy.
const (
	checkEvery  = 24 * time.Hour
	policyEvery = time.Minute
	deferredFor = 15 * time.Minute
	failedFor   = time.Hour
	followEvery = 5 * time.Second
)

// Manager is the one long-lived owner of checks, policy and requested downloads.
type Manager struct {
	mu       sync.Mutex // guards state, admission and the worker lifetime
	state    journal
	dir      string
	install  Installation
	schema   int
	source   Source
	delivery Delivery
	pins     []string
	verify   func(context.Context, string, []string) error
	safe     func(context.Context, core.UpdatePolicy) bool
	emit     func(core.UpdateStatus)
	ctx      context.Context
	stop     context.CancelFunc
	cancel   context.CancelFunc
	busy     bool
	closed   bool
	wg       sync.WaitGroup
	owner    *Lease
}

func fresh(installed string) journal {
	return journal{Format: 1, Status: core.UpdateStatus{InstalledVersion: installed, Phase: core.UpdateIdle, Policy: core.UpdatePolicy{Channel: core.UpdateStable, DeferBattery: true}}}
}

// Inert is the manager of a build that takes no part in updates: it reports
// that truthfully and refuses every operation.
func Inert(ctx context.Context, installed string, emit func(core.UpdateStatus)) *Manager {
	m := &Manager{state: fresh(installed), emit: emit, verify: VerifyPublisher, safe: automaticSafe}
	m.ctx, m.stop = context.WithCancel(context.WithoutCancel(ctx))
	return m
}

// Open returns the installation's update manager: its coordinator, an observer
// of another Lumen window's, or an inert one for a build that is not an
// installation (a source checkout, a portable copy).
func Open(ctx context.Context, executable, installed string, schema int, emit func(core.UpdateStatus)) (*Manager, error) {
	m := Inert(ctx, installed, emit)
	m.schema = schema
	install, err := DetectInstallation(executable)
	if errors.Is(err, ErrUnavailable) {
		return m, nil
	}
	if err == nil {
		m.install = install
		m.state.Status.Managed = true
		if m.dir, err = StateDirectory(install); err == nil {
			err = os.MkdirAll(m.dir, 0o700)
		}
	}
	if err == nil {
		err = m.attach(ctx, 100*time.Millisecond)
	}
	if err != nil {
		m.stop()
		return nil, err
	}
	return m, nil
}

// attach makes this process the installation's coordinator when no other
// Lumen is, and an observer of that one's journal otherwise. The caller holds
// mu, or is Open.
func (m *Manager) attach(ctx context.Context, wait time.Duration) error {
	owner := tryAcquire(ctx, m.dir, leaseFile, wait)
	if owner == nil {
		// Another window coordinates: observing it is the outcome, not a failure.
		m.observe()
		return nil
	}
	var err error
	state := fresh(m.state.Status.InstalledVersion)
	var saved journal
	switch err := readJSON(filepath.Join(m.dir, journalFile), &saved); {
	case errors.Is(err, os.ErrNotExist):
	case err != nil || saved.Format != 1 || !validPolicy(saved.Status.Policy):
		// The journal is a record of convenience: policy falls back to its
		// safe defaults and staged files are judged afresh. Trust state and
		// activation records are separate files, and are never guessed at.
		slog.Warn("update journal unreadable; starting from defaults", "err", err)
	default:
		state = saved
		state.Status.InstalledVersion = m.state.Status.InstalledVersion
	}
	// A snapshot the interface already holds must never outrank a later one.
	state.Status.Generation = max(state.Status.Generation, m.state.Status.Generation)
	state.Status.Managed, state.Status.Coordinator, state.Status.Configured = true, true, false
	m.owner, m.state = owner, state
	m.settle(ctx)
	if err := m.PruneRecovery(ctx); err != nil {
		slog.Warn("prune update recovery material", "err", err)
	}
	if err = m.configure(); err == nil {
		err = m.persistLocked(ctx)
	}
	if err != nil {
		m.owner = nil
		return errors.Join(err, owner.Close())
	}
	return nil
}

// configure connects the coordinator to the release feed this build trusts.
// A build without one stays truthfully unconfigured.
func (m *Manager) configure() error {
	c, err := ConfiguredBuild()
	if errors.Is(err, ErrUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	transport, err := NewTransport(c.Origins)
	if err != nil {
		return err
	}
	m.source = &Feed{Root: c.Root, MetadataURL: c.MetadataURL, TargetsURL: c.TargetsURL, StateDir: m.dir, Transport: transport}
	m.delivery = transport
	m.pins = c.PublisherPins
	m.state.Status.Configured = true
	return nil
}

// observe shows the coordinator's journal without owning any of it.
func (m *Manager) observe() {
	var saved journal
	if readJSON(filepath.Join(m.dir, journalFile), &saved) == nil && saved.Format == 1 && saved.Status.Generation >= m.state.Status.Generation {
		saved.Status.InstalledVersion = m.state.Status.InstalledVersion
		m.state = saved
	}
	m.state.Status.Managed, m.state.Status.Coordinator = true, false
	m.state.Status.Error = core.UpdateFailureObserver
}

// settle makes the journal of a previous run truthful: work that was under
// way then is not under way now, and a readiness flag is not proof that the
// staged files are still there.
func (m *Manager) settle(ctx context.Context) {
	s := &m.state.Status
	switch s.Phase {
	case core.UpdateChecking, core.UpdateDownloading, core.UpdateVerifying, core.UpdateStaging:
		s.Phase, s.Error = core.UpdateFailed, core.UpdateFailureInterrupted
	case core.UpdateQuiescing, core.UpdateApplying, core.UpdateAwaitingHealth, core.UpdateRecovering:
		m.settleActivation(ctx)
	case core.UpdateReady:
		if !m.stagedIntact(ctx) {
			s.Phase, s.Error = core.UpdateFailed, core.UpdateFailureInvalid
		}
	default:
	}
	if s.Phase != core.UpdateReady {
		// No transfer is resumed, so whatever is staged now is only space.
		if err := m.removeStaging(); err != nil {
			slog.Warn("remove abandoned update staging", "err", err)
		}
	}
}

// settleActivation reads how the restart this journal last recorded ended.
func (m *Manager) settleActivation(ctx context.Context) {
	s := &m.state.Status
	var a Activation
	mine := readJSON(filepath.Join(m.dir, activationFile), &a) == nil && a.Install == m.install && a.Candidate.Release.Version == s.InstalledVersion
	switch {
	case mine && a.Phase == core.UpdateComplete:
		m.state.Candidate = nil
		s.Phase, s.Error, s.Offer, s.Bytes = core.UpdateComplete, "", core.UpdateOffer{}, 0
	case mine && a.Phase == core.UpdateAwaitingHealth:
		// This is that first launch: Completed settles it once health is durable.
		s.Phase, s.Error = core.UpdateAwaitingHealth, ""
	case m.stagedIntact(ctx):
		// The prepared runtime survived: another restart needs no download.
		s.Phase, s.Error = core.UpdateReady, core.UpdateFailureFailed
	default:
		s.Phase, s.Error = core.UpdateFailed, core.UpdateFailureFailed
	}
}

func (m *Manager) stagedIntact(ctx context.Context) bool {
	return m.state.Candidate != nil && noLinks(m.install.Root, m.staged()) == nil && VerifyLayout(ctx, m.staged(), m.state.Candidate.Release) == nil
}

// Completed records that this first launch of an update passed its health
// check, for the interface to say so.
func (m *Manager) Completed(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner == nil || m.state.Status.Phase != core.UpdateAwaitingHealth {
		return nil
	}
	m.settleActivation(ctx)
	return m.saveLocked(ctx)
}

func validPolicy(p core.UpdatePolicy) bool {
	return (p.Channel == core.UpdateStable || p.Channel == core.UpdatePreview) && p.BandwidthKB >= 0 && p.BandwidthKB <= 102400
}

func (m *Manager) staged() string { return stagedDir(m.install.Root) }

func (m *Manager) archive() string { return filepath.Join(m.install.Root, ".update", "runtime.zip") }

// persistLocked makes the next snapshot durable without announcing it.
func (m *Manager) persistLocked(ctx context.Context) error {
	m.state.Status.Generation++
	if m.dir == "" {
		return nil
	}
	return writeJSON(ctx, filepath.Join(m.dir, journalFile), m.state)
}

func (m *Manager) saveLocked(ctx context.Context) error {
	if err := m.persistLocked(ctx); err != nil {
		return err
	}
	m.announceLocked()
	return nil
}

func (m *Manager) announceLocked() {
	if m.emit != nil {
		m.emit(m.state.Status)
	}
}

// admitLocked refuses what this manager may not do: an observer waits for the
// coordinating window, and a build without delivery has nothing to operate.
func (m *Manager) admitLocked(delivery bool) error {
	switch {
	case m.closed:
		return ErrState
	case m.owner == nil && m.state.Status.Managed:
		return ErrBusy
	case m.owner == nil || (delivery && !m.state.Status.Configured):
		return ErrUnavailable
	}
	return nil
}

// Status returns a snapshot; component lifetimes never own update work.
func (m *Manager) Status() core.UpdateStatus { m.mu.Lock(); defer m.mu.Unlock(); return m.state.Status }

// SetPolicy orders durable installation policy writes without changing library settings.
func (m *Manager) SetPolicy(ctx context.Context, p core.UpdatePolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !validPolicy(p) {
		return ErrInvalid
	}
	if err := m.admitLocked(true); err != nil {
		return err
	}
	if m.busy && p.Channel != m.state.Status.Policy.Channel {
		return ErrBusy
	}
	if m.state.Status.Phase == core.UpdateReady && p.Channel != m.state.Status.Policy.Channel {
		return ErrState
	}
	before := m.state
	m.state.Status.Policy = p
	if p.Channel != before.Status.Policy.Channel {
		m.state.Candidate = nil
		m.state.Status.Offer = core.UpdateOffer{}
		m.state.Status.Phase = core.UpdateIdle
		m.state.Status.LastCheck = 0
	}
	if err := m.saveLocked(ctx); err != nil {
		m.state = before
		return err
	}
	if !p.Automatic && m.state.Status.Phase == core.UpdateChecking && m.cancel != nil {
		m.cancel()
	}
	return nil
}

// Start schedules discovery only, with no implicit runtime download or restart.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.dir == "" {
		return
	}
	m.wg.Go(m.schedule)
}

func (m *Manager) schedule() {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	var retry time.Time
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-timer.C:
		}
		if m.follow() {
			timer.Reset(followEvery)
			continue
		}
		if now := time.Now(); m.due(now) && !now.Before(retry) {
			retry = now.Add(m.discover())
		}
		timer.Reset(policyEvery)
	}
}

// follow keeps an observer's snapshot current and takes over coordination
// once the coordinating window has closed. It reports whether this manager is
// still an observer.
func (m *Manager) follow() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.owner != nil || m.closed {
		return false
	}
	before := m.state.Status
	if _, err := os.Lstat(filepath.Join(m.install.Root, barrierFile)); err == nil {
		// A restart is under way and its helper owns the outcome.
		m.observe()
	} else if err := m.attach(m.ctx, 0); err != nil {
		slog.Warn("take over update coordination", "err", err)
		m.observe()
	}
	if m.state.Status != before {
		m.announceLocked()
	}
	return m.owner == nil
}

// due reports whether the daily automatic check may run now. A staged
// candidate or a restart under way is never displaced by a poll.
func (m *Manager) due(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.state.Status
	switch s.Phase {
	case core.UpdateIdle, core.UpdateAvailable, core.UpdateComplete, core.UpdateFailed, core.UpdateCancelled:
	default:
		return false
	}
	return m.owner != nil && !m.busy && s.Configured && s.Policy.Automatic && now.Sub(time.Unix(s.LastCheck, 0)) >= checkEvery
}

// discover runs one scheduled check and returns how long to leave the feed
// alone afterwards, whatever the policy's clock says.
func (m *Manager) discover() time.Duration {
	if !m.safe(m.ctx, m.Status().Policy) {
		return deferredFor
	}
	if err := m.Check(m.ctx); err != nil {
		if !errors.Is(err, ErrBusy) && !errors.Is(err, ErrState) {
			slog.Warn("scheduled update check", "err", err)
		}
		return failedFor
	}
	return 0
}

func (m *Manager) begin(phase core.UpdatePhase) (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.beginLocked(phase)
}

func (m *Manager) beginLocked(phase core.UpdatePhase) (context.Context, error) {
	if err := m.admitLocked(true); err != nil {
		return nil, err
	}
	if m.busy {
		return nil, ErrBusy
	}
	switch m.state.Status.Phase {
	case core.UpdateReady, core.UpdateQuiescing, core.UpdateAwaitingHealth:
		return nil, ErrState
	default:
	}
	ctx, cancel := context.WithCancel(m.ctx)
	before := m.state
	m.cancel = cancel
	m.busy = true
	m.state.Status.Phase = phase
	m.state.Status.Error = ""
	m.state.Status.Bytes = 0
	if err := m.saveLocked(ctx); err != nil {
		cancel()
		m.cancel = nil
		m.busy = false
		m.state = before
		return nil, err
	}
	m.wg.Add(1)
	return ctx, nil
}

func failure(err error) core.UpdateFailure {
	switch {
	case errors.Is(err, ErrInvalid):
		return core.UpdateFailureInvalid
	case errors.Is(err, ErrPublisher):
		return core.UpdateFailurePublisher
	case errors.Is(err, ErrSpace):
		return core.UpdateFailureSpace
	}
	return core.UpdateFailureFailed
}

func (m *Manager) finish(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.busy = false
	if err != nil {
		m.state.Status.Phase, m.state.Status.Error = core.UpdateFailed, failure(err)
		if errors.Is(err, context.Canceled) {
			m.state.Status.Phase, m.state.Status.Error = core.UpdateCancelled, ""
		}
		slog.Warn("update operation failed", "err", err)
	}
	if persistErr := m.saveLocked(context.Background()); persistErr != nil {
		slog.Error("persist update result", "err", persistErr)
	}
	m.wg.Done()
}

// Check is a manual one-off request even when automatic discovery is disabled.
func (m *Manager) Check(ctx context.Context) error {
	work, err := m.begin(core.UpdateChecking)
	if err != nil {
		return err
	}
	defer func() { m.finish(err) }()
	work, cancel := context.WithTimeout(work, 45*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	status := m.Status()
	candidate, checkErr := m.source.Check(work, status.Policy.Channel, status.InstalledVersion, m.schema)
	err = checkErr
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Only an authenticated answer counts as having checked: a failure is
	// retried by the scheduler's own, shorter clock.
	m.state.Status.LastCheck = time.Now().Unix()
	m.state.Candidate = candidate
	m.state.Status.Offer = core.UpdateOffer{}
	m.state.Status.Phase = core.UpdateIdle
	if candidate != nil {
		size, validErr := candidate.Release.validate()
		if validErr != nil {
			m.state.Candidate = nil
			err = validErr
			return err
		}
		m.state.Status.Offer = core.UpdateOffer{Version: candidate.Release.Version, Notes: candidate.Release.Notes, DownloadBytes: candidate.Length, ExpandedBytes: size}
		m.state.Status.Phase = core.UpdateAvailable
	}
	return nil
}

// Download starts the explicitly requested background preparation.
func (m *Manager) Download() error {
	m.mu.Lock()
	c := m.state.Candidate
	phase := m.state.Status.Phase
	if c == nil || (phase != core.UpdateAvailable && phase != core.UpdateFailed && phase != core.UpdateCancelled) {
		m.mu.Unlock()
		return ErrState
	}
	ctx, err := m.beginLocked(core.UpdateDownloading)
	m.mu.Unlock()
	if err != nil {
		return err
	}
	go func() { m.finish(m.prepare(ctx, *c)) }()
	return nil
}

func (m *Manager) prepare(ctx context.Context, c Candidate) (err error) {
	bandwidth := m.Status().Policy.BandwidthKB
	deadline := time.Hour
	if bandwidth > 0 {
		deadline = min(24*time.Hour, time.Duration(c.Length/int64(bandwidth*1024))*time.Second+30*time.Minute)
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	dir := filepath.Join(m.install.Root, ".update")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := noLinks(m.install.Root, dir); err != nil {
		return err
	}
	size, err := c.Release.validate()
	if err != nil {
		return err
	}
	// These exact paths belong to the coordinator. Never remove a runtime or
	// installation directory, even when a failed download needs another attempt.
	if err := m.removeStaging(); err != nil {
		return err
	}
	defer func() {
		// No transfer is resumed: what a failed attempt leaves is only space.
		if err != nil {
			if removeErr := m.removeStaging(); removeErr != nil {
				slog.Warn("remove failed update staging", "err", removeErr)
			}
		}
	}()
	available, err := freeSpace(dir)
	if err != nil {
		return err
	}
	if available < uint64(c.Length+size+(256<<20)) {
		return ErrSpace
	}
	last := time.Time{}
	err = m.delivery.Download(ctx, c, m.archive(), bandwidth, func(n int64) {
		if time.Since(last) < 200*time.Millisecond {
			return
		}
		last = time.Now()
		m.progress(n)
	})
	if err != nil {
		return err
	}
	if err := m.transition(ctx, core.UpdateStaging); err != nil {
		return err
	}
	if err := Stage(ctx, m.archive(), m.staged(), c.Release); err != nil {
		return err
	}
	// The archive has served its purpose, and is most of a gigabyte.
	if err := os.Remove(m.archive()); err != nil {
		return err
	}
	if err := m.transition(ctx, core.UpdateVerifying); err != nil {
		return err
	}
	if err := m.verify(ctx, filepath.Join(m.staged(), appName), m.pins); err != nil {
		return err
	}
	return m.transition(ctx, core.UpdateReady)
}

// progress announces how far a download has come. It is not written down:
// a download that stops starts again from nothing.
func (m *Manager) progress(n int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Status.Bytes = n
	m.state.Status.Generation++
	m.announceLocked()
}

func (m *Manager) transition(ctx context.Context, phase core.UpdatePhase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := m.state
	// Moving on makes an earlier attempt's failure history.
	m.state.Status.Phase, m.state.Status.Error = phase, ""
	if err := m.saveLocked(ctx); err != nil {
		m.state = before
		return err
	}
	return nil
}

// Cancel cancels checks/downloads, never an activation commit.
func (m *Manager) Cancel() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel == nil {
		return ErrState
	}
	m.cancel()
	return nil
}

// removeStaging removes exactly the coordinator's two staging paths.
func (m *Manager) removeStaging() error { return removeStaging(m.install.Root) }

func removeStaging(root string) error {
	for _, name := range []string{stagedDir(root), filepath.Join(root, ".update", "runtime.zip")} {
		if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := noLinks(root, name); err != nil {
			return err
		}
		if err := os.RemoveAll(name); err != nil {
			return err
		}
	}
	return nil
}

// Discard removes only the coordinator's candidate, keeping the installed runtime.
func (m *Manager) Discard(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.admitLocked(false); err != nil {
		return err
	}
	if m.busy {
		return ErrBusy
	}
	switch m.state.Status.Phase {
	case core.UpdateQuiescing, core.UpdateApplying, core.UpdateAwaitingHealth:
		return ErrState
	default:
	}
	if err := m.removeStaging(); err != nil {
		return err
	}
	m.state.Candidate = nil
	m.state.Status.Offer = core.UpdateOffer{}
	m.state.Status.Phase = core.UpdateIdle
	m.state.Status.Error = ""
	return m.saveLocked(ctx)
}

// Dismiss suppresses notices for the offered release only, or for the
// completed update to the installed one.
func (m *Manager) Dismiss(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.admitLocked(false); err != nil {
		return err
	}
	before := m.state
	m.state.Status.Dismissed = m.state.Status.Offer.Version
	if m.state.Status.Phase == core.UpdateComplete {
		m.state.Status.Dismissed = m.state.Status.InstalledVersion
	}
	if err := m.saveLocked(ctx); err != nil {
		m.state = before
		return err
	}
	return nil
}

// Close stops discovery and joins preparation before runtime shutdown.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.stop()
	m.mu.Unlock()
	m.wg.Wait()
	if m.owner != nil {
		return m.owner.Close()
	}
	return nil
}
