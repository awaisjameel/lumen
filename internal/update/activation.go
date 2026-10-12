package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"lumen/internal/core"
)

// The installation's records, beside the stable launcher.
const (
	installFile    = ".lumen-install.json"    // NSIS-owned identity
	activeFile     = ".lumen-active.json"     // the selected generation
	barrierFile    = ".lumen-activating.json" // closes launches while an activation is under way
	installingFile = ".lumen-installing"      // closes launches while Setup replaces files
	launcherName   = "Lumen.exe"
	helperName     = "lumen-update.exe"
	appName        = "lumen-app.exe"
)

// The lock files. A handle on one is the whole of the lock: Windows releases
// it when its process ends, however that happens.
const (
	leaseFile  = ".lumen-process-lock" // the runtime lease, and the coordinator's in its state folder
	helperFile = ".lumen-helper-lock"  // held by the activation helper for as long as it runs
)

// The updater's records, in its state folder.
const (
	journalFile    = "journal.json"
	activationFile = "activation.json"
)

// Active is the launcher's atomic generation selection. Pending health forbids
// ordinary launch: only the helper's transaction-bound first launch may proceed.
type Active struct {
	Format     int     `json:"format"`
	Generation string  `json:"generation"`
	Version    string  `json:"version"`
	Release    Release `json:"release"`
	Pending    string  `json:"pending"`
}

// Activation pairs a runtime selection with exactly one closed library backup.
type Activation struct {
	CompletedAt    int64            `json:"completedAt"`
	RecoveryPruned bool             `json:"recoveryPruned"`
	ParentPID      uint32           `json:"parentPID"`
	ParentCreated  uint64           `json:"parentCreated"`
	Format         int              `json:"format"`
	Token          string           `json:"token"`
	Install        Installation     `json:"install"`
	Candidate      Candidate        `json:"candidate"`
	Previous       Active           `json:"previous"`
	Baseline       Capabilities     `json:"baseline"`
	DataDir        string           `json:"dataDir"`
	Backup         string           `json:"backup"`
	Phase          core.UpdatePhase `json:"phase"`
}

// token is the barrier's and the helper's acknowledgements' only content.
type token struct {
	Token string `json:"token"`
}

func validToken(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16
}

// tryAcquire is acquire for a caller to whom a lock held elsewhere is an
// answer rather than a failure: nil when it is not free within wait.
func tryAcquire(ctx context.Context, dir, name string, wait time.Duration) *Lease {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	lease, err := acquire(ctx, dir, name, true)
	if err != nil {
		return nil
	}
	return lease
}

func stagedDir(root string) string { return filepath.Join(root, ".update", "candidate") }

func generationDir(root, generation string) string { return filepath.Join(root, "runtime", generation) }

// candidateGeneration names the generation an activation renames its staged
// runtime to: unique to the attempt, so a retry never meets an earlier one's.
func (a Activation) candidateGeneration() string { return a.Candidate.Release.Version + "-" + a.Token }

func backupFile(dir, token string) string { return filepath.Join(dir, "library-"+token+".db") }

// installed reads the installation's identity and where its updater state lives.
func installed(root string) (Installation, string, error) {
	var install Installation
	if err := readJSON(filepath.Join(root, installFile), &install); err != nil {
		return install, "", err
	}
	dir, err := StateDirectory(install)
	return install, dir, err
}

// readActive reads the launch record without touching the runtime it names.
func readActive(root string) (Active, error) {
	var active Active
	if err := readJSON(filepath.Join(root, activeFile), &active); err != nil {
		return active, err
	}
	if active.Format != 1 || !safePath(active.Generation) || filepath.Base(active.Generation) != active.Generation || active.Version != active.Release.Version {
		return active, ErrInvalid
	}
	return active, nil
}

// LoadActive reads the launch record and proves every byte of the runtime it
// selects: for the moments that decide which generation runs (installation,
// activation, recovery). An ordinary launch uses VerifyLayout instead.
func LoadActive(ctx context.Context, root string) (Active, error) {
	active, err := readActive(root)
	if err != nil {
		return active, err
	}
	return active, VerifyGeneration(ctx, generationDir(root, active.Generation), active.Release)
}

// RecoverPending settles what an interrupted activation left behind before a
// launch: a selection still awaiting its health check is recovered, and a
// launch barrier whose helper is gone is lifted. While the helper runs it owns
// both, and the launch is refused instead.
func RecoverPending(ctx context.Context, root string) error {
	active, err := readActive(root)
	if err != nil {
		return err
	}
	var barrier token
	err = readJSON(filepath.Join(root, barrierFile), &barrier)
	if errors.Is(err, os.ErrNotExist) && active.Pending == "" {
		return nil
	}
	_, dir, err := installed(root)
	if err != nil {
		return err
	}
	// A live helper holds the lock: it settles the activation itself, and
	// Launch refuses until it has.
	helper := tryAcquire(ctx, dir, helperFile, 300*time.Millisecond)
	if helper == nil {
		return nil
	}
	defer func() { _ = helper.Close() }()
	if active.Pending != "" {
		if err := Recover(ctx, root); err != nil {
			return err
		}
	}
	return abandon(ctx, root, dir, barrier.Token)
}

// abandon closes an activation that stopped before its generation was
// selected, or after recovery reselected the previous one: the prepared
// runtime goes back to staging for another attempt, the record says failed,
// and launches are admitted again. It never runs while a selection is pending.
func abandon(ctx context.Context, root, dir, id string) error {
	var a Activation
	err := readJSON(filepath.Join(dir, activationFile), &a)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil && validToken(id) && a.Token == id && a.Phase != core.UpdateComplete && a.Phase != core.UpdateFailed {
		// Renamed, but never selected: without this the next attempt would
		// have to download the whole runtime again.
		if _, err := os.Lstat(generationDir(root, a.candidateGeneration())); err == nil {
			if _, err := os.Lstat(stagedDir(root)); errors.Is(err, os.ErrNotExist) {
				if err := os.Rename(generationDir(root, a.candidateGeneration()), stagedDir(root)); err != nil {
					return err
				}
			}
		}
		a.Phase = core.UpdateFailed
		if err := writeJSON(ctx, filepath.Join(dir, activationFile), a); err != nil {
			return err
		}
	}
	if err := os.Remove(filepath.Join(root, barrierFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// RequestRestart revalidates availability and launches the independent helper
// before asking the application to close. Normal exit never calls this method.
// baseline is what this release can do on this PC, for the new one to match.
func (m *Manager) RequestRestart(ctx context.Context, dataDir string, baseline Capabilities) error {
	m.mu.Lock()
	if err := m.admitLocked(true); err != nil {
		m.mu.Unlock()
		return err
	}
	if m.busy || m.state.Status.Phase != core.UpdateReady || m.state.Candidate == nil {
		m.mu.Unlock()
		return ErrState
	}
	c := *m.state.Candidate
	m.busy = true
	m.wg.Add(1)
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.busy = false; m.mu.Unlock(); m.wg.Done() }()
	if !filepath.IsAbs(dataDir) {
		return ErrInvalid
	}
	// A feed that stops answering must not hold the restart for ever.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var backupBytes int64
	for _, suffix := range []string{"", "-wal"} {
		fi, err := os.Stat(filepath.Join(dataDir, "library.db") + suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			backupBytes += fi.Size()
		}
	}
	space, err := freeSpace(m.dir)
	if err != nil {
		return err
	}
	if space < uint64(backupBytes+(64<<20)) {
		return ErrSpace
	}
	fresh, err := m.source.Check(ctx, c.Release.Channel, m.Status().InstalledVersion, m.schema)
	if err != nil {
		return err
	}
	if fresh == nil || fresh.Target != c.Target || fresh.SHA256 != c.SHA256 || !reflect.DeepEqual(fresh.Release, c.Release) {
		return ErrInvalid
	}
	// The helper proves both generations' bytes under the exclusive lease;
	// here a cheap look avoids closing Lumen for an update that cannot apply.
	if err := VerifyLayout(ctx, stagedDir(m.install.Root), c.Release); err != nil {
		return err
	}
	helper := filepath.Join(m.install.Root, helperName)
	if err := m.verify(ctx, helper, m.pins); err != nil {
		return err
	}
	previous, err := readActive(m.install.Root)
	if err != nil {
		return err
	}
	// One activation at a time: a barrier is another one's, still under way.
	barrier := filepath.Join(m.install.Root, barrierFile)
	if _, err := os.Lstat(barrier); !errors.Is(err, os.ErrNotExist) || previous.Pending != "" {
		return ErrState
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(bytes[:])
	a := Activation{Format: 1, Token: id, Install: m.install, Candidate: c, Previous: previous, Baseline: baseline, DataDir: dataDir, Backup: backupFile(m.dir, id), Phase: core.UpdateQuiescing}
	a.ParentPID, a.ParentCreated, err = processIdentity()
	if err != nil {
		return err
	}
	// The record being replaced may still owe its recovery period.
	var prior Activation
	if readJSON(filepath.Join(m.dir, activationFile), &prior) == nil && prior.Install == m.install && prior.Phase == core.UpdateComplete && !prior.RecoveryPruned {
		if !validToken(prior.Token) {
			return ErrInvalid
		}
		if err := writeJSON(ctx, filepath.Join(m.dir, "recoveries", prior.Token+".json"), prior); err != nil {
			return err
		}
	}
	if err := writeJSON(ctx, filepath.Join(m.dir, activationFile), a); err != nil {
		return err
	}
	if err := writeJSON(ctx, barrier, token{id}); err != nil {
		return err
	}
	if err := startHelper(ctx, helper, m.install.Root, m.dir, id); err != nil {
		_ = os.Remove(barrier)
		return err
	}
	if err := m.transition(ctx, core.UpdateQuiescing); err != nil {
		_ = os.Remove(barrier)
		return err
	}
	return nil
}

// PendingClose returns only an explicitly approved activation for this library
// that its helper is still waiting on.
func (m *Manager) PendingClose(dataDir string) (Activation, bool, error) {
	if m.Status().Phase != core.UpdateQuiescing {
		return Activation{}, false, nil
	}
	var a Activation
	if err := readJSON(filepath.Join(m.dir, activationFile), &a); err != nil {
		return a, false, err
	}
	if a.Install != m.install || a.DataDir != dataDir {
		return a, false, ErrInvalid
	}
	// The helper gave up (it waits only so long for Lumen to close): there
	// is nothing left to hand over, and the next launch says so.
	return a, a.Phase == core.UpdateQuiescing, nil
}

// Closed acknowledges a completed SQLite backup and database close to the helper.
func (m *Manager) Closed(ctx context.Context, a Activation) error {
	return writeJSON(ctx, filepath.Join(m.dir, "closed-"+a.Token+".json"), token{a.Token})
}

// Apply waits for the old process lease, then commits one complete generation.
// The helper writes no SQL; matching library restoration belongs to the old app.
// However it ends, Lumen can be launched afterwards: with the new release, or
// with the previous one and its own library.
func Apply(ctx context.Context, root, id string) error {
	install, dir, err := installed(root)
	if err != nil {
		return err
	}
	// The launcher tells a helper at work from one that died by this lock.
	alive, err := acquire(ctx, dir, helperFile, true)
	if err != nil {
		return err
	}
	defer func() { _ = alive.Close() }()
	var a Activation
	if err := readJSON(filepath.Join(dir, activationFile), &a); err != nil {
		return err
	}
	if a.Format != 1 || a.Token != id || !validToken(id) || a.Install != install || !filepath.IsAbs(a.DataDir) || a.Backup != backupFile(dir, id) {
		return ErrInvalid
	}
	if a.Phase != core.UpdateQuiescing {
		return ErrState
	}
	err = apply(ctx, root, dir, a)
	if err == nil {
		return nil
	}
	// A selection still pending belongs to recovery at the next launch.
	// Otherwise the previous release is the selected one: reopen it.
	settle := context.WithoutCancel(ctx)
	if active, activeErr := readActive(root); activeErr != nil || active.Pending != "" {
		return err
	}
	if abandonErr := abandon(settle, root, dir, id); abandonErr != nil {
		return errors.Join(err, abandonErr)
	}
	return errors.Join(err, relaunch(settle, root, a.DataDir))
}

func apply(ctx context.Context, root, dir string, a Activation) error {
	if err := writeJSON(ctx, filepath.Join(dir, "helper-ready-"+a.Token+".json"), token{a.Token}); err != nil {
		return err
	}
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		var closed token
		if err := readJSON(filepath.Join(dir, "closed-"+a.Token+".json"), &closed); err == nil && closed.Token == a.Token {
			break
		}
		select {
		case <-deadline.Done():
			return fmt.Errorf("waiting for Lumen to close its library: %w", deadline.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := waitParent(deadline, a.ParentPID, a.ParentCreated); err != nil {
		return err
	}
	lease, err := AcquireLease(deadline, root, true)
	if err != nil {
		return fmt.Errorf("waiting for other Lumen libraries to close: %w", err)
	}
	defer func() {
		if lease != nil {
			_ = lease.Close()
		}
	}()
	config, err := ConfiguredBuild()
	if err != nil {
		return err
	}
	staged := stagedDir(root)
	if err := noLinks(root, staged); err != nil {
		return err
	}
	if err := VerifyGeneration(ctx, staged, a.Candidate.Release); err != nil {
		return err
	}
	if err := VerifyPublisher(ctx, filepath.Join(staged, appName), config.PublisherPins); err != nil {
		return err
	}
	// The previous generation is the way back: prove it before leaving it.
	current, err := LoadActive(ctx, root)
	if err != nil {
		return err
	}
	if current.Generation != a.Previous.Generation || current.Version != a.Previous.Version || current.Pending != "" {
		return ErrState
	}
	a.Phase = core.UpdateApplying
	if err := writeJSON(ctx, filepath.Join(dir, activationFile), a); err != nil {
		return err
	}
	if err := os.Rename(staged, generationDir(root, a.candidateGeneration())); err != nil {
		return err
	}
	active := Active{Format: 1, Generation: a.candidateGeneration(), Version: a.Candidate.Release.Version, Release: a.Candidate.Release, Pending: a.Token}
	if err := writeJSON(ctx, filepath.Join(root, activeFile), active); err != nil {
		return err
	}
	a.Phase = core.UpdateAwaitingHealth
	if err := writeJSON(ctx, filepath.Join(dir, activationFile), a); err != nil {
		return err
	}
	// Release the exclusive lease only after the launch record is coherent.
	if err := lease.Close(); err != nil {
		return err
	}
	lease = nil
	return launchHealth(ctx, root, dir, a)
}

// AcknowledgeHealth commits runtime health before admitting any library writes.
func AcknowledgeHealth(ctx context.Context, root, id string) error {
	_, dir, err := installed(root)
	if err != nil {
		return err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, activationFile), &a); err != nil {
		return err
	}
	if a.Token != id || a.Phase != core.UpdateAwaitingHealth {
		return ErrState
	}
	active, err := readActive(root)
	if err != nil {
		return err
	}
	if active.Pending != id || active.Version != a.Candidate.Release.Version {
		return ErrInvalid
	}
	// The durable complete checkpoint forbids automatic database restoration.
	a.Phase = core.UpdateComplete
	a.CompletedAt = time.Now().Unix()
	if err := writeJSON(ctx, filepath.Join(dir, activationFile), a); err != nil {
		return err
	}
	active.Pending = ""
	if err := writeJSON(ctx, filepath.Join(root, activeFile), active); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, barrierFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// What Windows lists as installed follows the committed selection. A
	// healthy update is not undone over a line in Apps & features.
	register(root, active.Version)
	return nil
}

// Recover restores only before the durable health commit. After acceptance,
// newer library writes must survive even if a subsequent launch fails.
func Recover(ctx context.Context, root string) error {
	install, dir, err := installed(root)
	if err != nil {
		return err
	}
	lease, err := AcquireLease(ctx, root, true)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	// Read under the lease: the new application may have passed its health
	// check and been closed again while this waited for it.
	var a Activation
	if err := readJSON(filepath.Join(dir, activationFile), &a); err != nil {
		return err
	}
	if a.Install != install || a.Format != 1 || !validToken(a.Token) {
		return ErrInvalid
	}
	if a.Phase == core.UpdateComplete {
		active, err := readActive(root)
		if err != nil {
			return err
		}
		if active.Pending == a.Token {
			active.Pending = ""
			return writeJSON(ctx, filepath.Join(root, activeFile), active)
		}
		return nil
	}
	if a.Phase != core.UpdateApplying && a.Phase != core.UpdateAwaitingHealth && a.Phase != core.UpdateRecovering {
		return ErrState
	}
	a.Phase = core.UpdateRecovering
	if err := writeJSON(ctx, filepath.Join(dir, activationFile), a); err != nil {
		return err
	}
	if err := VerifyGeneration(ctx, generationDir(root, a.Previous.Generation), a.Previous.Release); err != nil {
		return err
	}
	if err := restoreLibrary(ctx, root, a); err != nil {
		return err
	}
	if err := writeJSON(ctx, filepath.Join(root, activeFile), a.Previous); err != nil {
		return err
	}
	a.Phase = core.UpdateFailed
	if err := writeJSON(ctx, filepath.Join(dir, activationFile), a); err != nil {
		return err
	}
	// The refused generation is Lumen's own and nothing selects it now. What
	// cannot be removed here is removed with the installation.
	_ = removeOwnedGeneration(ctx, root, a.candidateGeneration(), a.Candidate.Release)
	register(root, a.Previous.Version)
	return nil
}

// ValidateHealth ties a first launch to an approved activation and library path.
func ValidateHealth(install Installation, id, dataDir, version string) (Activation, error) {
	dir, err := StateDirectory(install)
	if err != nil {
		return Activation{}, err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, activationFile), &a); err != nil {
		return a, err
	}
	if a.Token != id || a.Phase != core.UpdateAwaitingHealth || a.DataDir != dataDir || a.Install != install || a.Candidate.Release.Version != version {
		return a, ErrInvalid
	}
	active, err := readActive(install.Root)
	if err != nil {
		return a, err
	}
	if active.Pending != id {
		return a, ErrInvalid
	}
	return a, nil
}

// HealthToken parses only the one approved bootstrap argument.
func HealthToken(args []string) string {
	if len(args) == 2 && args[0] == "--update-health" && validToken(args[1]) {
		return args[1]
	}
	return ""
}

// RestoreRequest authorises the old application to restore before opening SQLite.
func RestoreRequest(ctx context.Context, root, id string) (Activation, error) {
	install, dir, err := installed(root)
	if err != nil {
		return Activation{}, err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, activationFile), &a); err != nil {
		return a, err
	}
	if a.Install != install || a.Phase != core.UpdateRecovering || a.Token != id || a.Backup != backupFile(dir, id) {
		return a, ErrInvalid
	}
	if _, err := os.Stat(a.Backup); errors.Is(err, os.ErrNotExist) {
		return a, fmt.Errorf("missing paired library backup: %w", err)
	}
	return a, ctx.Err()
}
