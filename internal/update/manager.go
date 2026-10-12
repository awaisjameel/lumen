package update

import (
	"context"
	"errors"
	"fmt"
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
	emit     func(core.UpdateStatus)
	ctx      context.Context
	stop     context.CancelFunc
	cancel   context.CancelFunc
	busy     bool
	closed   bool
	wg       sync.WaitGroup
	owner    *Lease
}

// Open creates an offline manager for unsupported builds, with truthful capabilities.
func Open(ctx context.Context, executable, installed string, schema int, emit func(core.UpdateStatus)) (*Manager, error) {
	m := &Manager{state: journal{Format: 1, Status: core.UpdateStatus{InstalledVersion: installed, Phase: core.UpdateIdle, Policy: core.UpdatePolicy{Channel: core.UpdateStable, DeferBattery: true}}}, schema: schema, emit: emit, verify: VerifyPublisher}
	m.ctx, m.stop = context.WithCancel(context.WithoutCancel(ctx))
	install, err := DetectInstallation(executable)
	if errors.Is(err, ErrUnavailable) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	m.install = install
	m.state.Status.Managed = true
	m.dir, err = StateDirectory(install)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return nil, err
	}
	leaseCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	m.owner, err = AcquireLease(leaseCtx, m.dir, true)
	if err != nil {
		var saved journal
		if readJSON(filepath.Join(m.dir, "journal.json"), &saved) == nil && saved.Format == 1 {
			m.state = saved
			m.state.Status.InstalledVersion = installed
			m.state.Status.Coordinator = false
		}
		m.state.Status.Error = "update_busy"
		return m, nil
	}
	if err := readJSON(filepath.Join(m.dir, "journal.json"), &m.state); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = m.owner.Close()
		return nil, err
	}
	if m.state.Format != 1 || !validPolicy(m.state.Status.Policy) {
		_ = m.owner.Close()
		return nil, ErrInvalid
	}
	m.state.Status.InstalledVersion = installed
	m.state.Status.Managed = true
	m.state.Status.Coordinator = true
	if err := m.PruneRecovery(ctx); err != nil {
		slog.Warn("prune update recovery material", "err", err)
	}
	var activation Activation
	if err := readJSON(filepath.Join(m.dir, "activation.json"), &activation); err == nil && activation.Install == install {
		if activation.Phase == core.UpdateComplete && activation.Candidate.Release.Version == installed {
			m.state.Status.Phase = core.UpdateComplete
			m.state.Status.Error = ""
			m.state.Candidate = nil
			m.state.Status.Offer = core.UpdateOffer{Version: installed}
		} else if activation.Phase == core.UpdateFailed {
			m.state.Status.Phase = core.UpdateFailed
			m.state.Status.Error = "update_failed"
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = m.owner.Close()
		return nil, err
	}
	c, err := ConfiguredBuild()
	if errors.Is(err, ErrUnavailable) {
		m.state.Status.Configured = false
		return m, nil
	}
	if err != nil {
		_ = m.owner.Close()
		return nil, err
	}
	transport, err := NewTransport(c.Origins)
	if err != nil {
		_ = m.owner.Close()
		return nil, err
	}
	m.source = &Feed{Root: c.Root, MetadataURL: c.MetadataURL, TargetsURL: c.TargetsURL, StateDir: m.dir, Transport: transport}
	m.delivery = transport
	m.pins = c.PublisherPins
	m.state.Status.Configured = true
	switch m.state.Status.Phase {
	case core.UpdateDownloading, core.UpdateVerifying, core.UpdateStaging, core.UpdateChecking:
		m.state.Status.Phase = core.UpdateFailed
		m.state.Status.Error = "update_interrupted"
	case core.UpdateReady:
		if m.state.Candidate == nil || VerifyGeneration(ctx, m.staged(), m.state.Candidate.Release) != nil {
			m.state.Status.Phase = core.UpdateFailed
			m.state.Status.Error = "update_invalid"
		}
	default:
	}
	err = m.saveLocked(ctx)
	return m, err
}

func validPolicy(p core.UpdatePolicy) bool {
	return (p.Channel == core.UpdateStable || p.Channel == core.UpdatePreview) && p.BandwidthKB >= 0 && p.BandwidthKB <= 102400
}

func (m *Manager) staged() string { return filepath.Join(m.install.Root, ".update", "candidate") }

func (m *Manager) saveLocked(ctx context.Context) error {
	m.state.Status.Generation++
	if m.dir != "" {
		if err := writeJSON(ctx, filepath.Join(m.dir, "journal.json"), m.state); err != nil {
			return err
		}
	}
	if m.emit != nil {
		m.emit(m.state.Status)
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
	if m.closed {
		return ErrState
	}
	if !m.state.Status.Configured || m.owner == nil {
		if m.state.Status.Managed && m.owner == nil {
			return ErrBusy
		}
		return ErrUnavailable
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
	if m.closed {
		return
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-m.ctx.Done():
				return
			case <-timer.C:
				if m.owner == nil && m.dir != "" {
					var saved journal
					if err := readJSON(filepath.Join(m.dir, "journal.json"), &saved); err == nil && saved.Format == 1 {
						m.mu.Lock()
						saved.Status.Coordinator = false
						saved.Status.Error = "update_busy"
						m.state = saved
						if m.emit != nil {
							m.emit(saved.Status)
						}
						m.mu.Unlock()
					}
					timer.Reset(time.Second)
					continue
				}
				status := m.Status()
				if status.Configured && status.Policy.Automatic && time.Now().Unix()-status.LastCheck >= 86400 && automaticSafe(m.ctx, status.Policy) {
					if err := m.Check(m.ctx); err != nil && !errors.Is(err, ErrBusy) && !errors.Is(err, ErrState) {
						slog.Warn("scheduled update check", "err", err)
					}
				}
				timer.Reset(time.Minute)
			}
		}
	}()
}

func (m *Manager) begin(phase core.UpdatePhase) (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.beginLocked(phase)
}

func (m *Manager) beginLocked(phase core.UpdatePhase) (context.Context, error) {
	if m.closed {
		return nil, ErrState
	}
	if m.busy {
		return nil, ErrBusy
	}
	if !m.state.Status.Configured || m.owner == nil {
		if m.state.Status.Managed && m.owner == nil {
			return nil, ErrBusy
		}
		return nil, ErrUnavailable
	}
	if m.state.Status.Phase == core.UpdateReady || m.state.Status.Phase == core.UpdateQuiescing {
		return nil, ErrState
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
		m.busy = false
		m.state = before
		return nil, err
	}
	m.wg.Add(1)
	return ctx, nil
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
		m.state.Status.Phase = core.UpdateFailed
		m.state.Status.Error = "update_failed"
		if errors.Is(err, context.Canceled) {
			m.state.Status.Phase = core.UpdateCancelled
			m.state.Status.Error = ""
		}
		if errors.Is(err, ErrInvalid) {
			m.state.Status.Error = "update_invalid"
		}
		if errors.Is(err, ErrPublisher) {
			m.state.Status.Error = "update_publisher"
		}
		if errors.Is(err, ErrSpace) {
			m.state.Status.Error = "update_space"
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
	policy := m.Status().Policy
	candidate, checkErr := m.source.Check(work, policy.Channel, m.Status().InstalledVersion, m.schema)
	err = checkErr
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Status.LastCheck = time.Now().Unix()
	if err != nil {
		return err
	}
	m.state.Candidate = candidate
	m.state.Status.Offer = core.UpdateOffer{}
	m.state.Status.Phase = core.UpdateIdle
	if candidate != nil {
		size, validErr := candidate.Release.validate()
		if validErr != nil {
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
	go func() { err := m.prepare(ctx, *c); m.finish(err) }()
	return nil
}

func (m *Manager) prepare(ctx context.Context, c Candidate) error {
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
	if err := noLinks(dir); err != nil {
		return err
	}
	size, err := c.Release.validate()
	if err != nil {
		return err
	}
	available, err := freeSpace(dir)
	if err != nil {
		return err
	}
	if available < uint64(c.Length+size+(256<<20)) {
		return ErrSpace
	}
	// These exact paths belong to the coordinator. Never remove a runtime or
	// installation directory, even when a failed download needs another attempt.
	archive := filepath.Join(dir, "runtime.zip")
	if err := m.removeStaging(); err != nil {
		return err
	}
	last := time.Time{}
	err = m.delivery.Download(ctx, c, archive, bandwidth, func(n int64) {
		if time.Since(last) < 200*time.Millisecond {
			return
		}
		last = time.Now()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.state.Status.Bytes = n
		if err := m.saveLocked(ctx); err != nil {
			slog.Warn("persist download progress", "err", err)
		}
	})
	if err != nil {
		return err
	}
	if err := m.transition(ctx, core.UpdateStaging); err != nil {
		return err
	}
	if err := Stage(ctx, archive, m.staged(), c.Release); err != nil {
		return err
	}
	if err := m.transition(ctx, core.UpdateVerifying); err != nil {
		return err
	}
	if err := m.verify(ctx, filepath.Join(m.staged(), "lumen-app.exe"), m.pins); err != nil {
		return err
	}
	return m.transition(ctx, core.UpdateReady)
}

func (m *Manager) transition(ctx context.Context, phase core.UpdatePhase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := m.state
	m.state.Status.Phase = phase
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

func (m *Manager) removeStaging() error {
	for _, name := range []string{m.staged(), filepath.Join(m.install.Root, ".update", "runtime.zip")} {
		if _, err := os.Lstat(name); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		if err := noLinks(name); err != nil {
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
	if m.busy {
		return ErrBusy
	}
	if m.closed || m.owner == nil {
		return ErrUnavailable
	}
	if m.state.Status.Phase == core.UpdateQuiescing || m.state.Status.Phase == core.UpdateApplying || m.state.Status.Phase == core.UpdateAwaitingHealth {
		return ErrState
	}
	if err := m.removeStaging(); err != nil {
		return err
	}
	m.state.Candidate = nil
	m.state.Status.Offer = core.UpdateOffer{}
	m.state.Status.Phase = core.UpdateIdle
	return m.saveLocked(ctx)
}

// Dismiss suppresses availability notices for the selected release only.
func (m *Manager) Dismiss(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Status.Dismissed = m.state.Status.Offer.Version
	return m.saveLocked(ctx)
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

func (m *Manager) String() string {
	return fmt.Sprintf("Lumen update coordinator (%s)", m.Status().InstalledVersion)
}
