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
	DataDir        string           `json:"dataDir"`
	Backup         string           `json:"backup"`
	Phase          core.UpdatePhase `json:"phase"`
}

// LoadActive validates the stable launch record before any replaceable code runs.
func LoadActive(ctx context.Context, root string) (Active, error) {
	var active Active
	if err := readJSON(filepath.Join(root, ".lumen-active.json"), &active); err != nil {
		return active, err
	}
	if active.Format != 1 || !safePath(active.Generation) || filepath.Base(active.Generation) != active.Generation || active.Version != active.Release.Version {
		return active, ErrInvalid
	}
	if err := VerifyGeneration(ctx, filepath.Join(root, "runtime", active.Generation), active.Release); err != nil {
		return active, err
	}
	return active, nil
}

// RecoverPending checks the launch selection without hashing a healthy runtime
// twice. Launch owns complete inventory verification under the shared lease.
func RecoverPending(ctx context.Context, root string) error {
	var active Active
	if err := readJSON(filepath.Join(root, ".lumen-active.json"), &active); err != nil {
		return err
	}
	if active.Format != 1 || !safePath(active.Generation) || filepath.Base(active.Generation) != active.Generation {
		return ErrInvalid
	}
	if active.Pending == "" {
		return nil
	}
	return Recover(ctx, root)
}

// RequestRestart revalidates availability and launches the independent helper
// before asking the application to close. Normal exit never calls this method.
func (m *Manager) RequestRestart(ctx context.Context, dataDir string) error {
	m.mu.Lock()
	if m.busy || m.closed || m.state.Status.Phase != core.UpdateReady || m.state.Candidate == nil {
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
	if err := VerifyGeneration(ctx, m.staged(), c.Release); err != nil {
		return err
	}
	helper := filepath.Join(m.install.Root, "lumen-update.exe")
	if err := m.verify(ctx, helper, m.pins); err != nil {
		return err
	}
	previous, err := LoadActive(ctx, m.install.Root)
	if err != nil {
		return err
	}
	if previous.Pending != "" {
		return ErrState
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return err
	}
	token := hex.EncodeToString(bytes[:])
	a := Activation{Format: 1, Token: token, Install: m.install, Candidate: c, Previous: previous, DataDir: dataDir, Backup: filepath.Join(m.dir, "library-"+token+".db"), Phase: core.UpdateQuiescing}
	a.ParentPID, a.ParentCreated, err = processIdentity()
	if err != nil {
		return err
	}
	var prior Activation
	if readJSON(filepath.Join(m.dir, "activation.json"), &prior) == nil && prior.Install == m.install && prior.Phase == core.UpdateComplete && !prior.RecoveryPruned {
		if len(prior.Token) != 32 {
			return ErrInvalid
		}
		if err := writeJSON(ctx, filepath.Join(m.dir, "recoveries", prior.Token+".json"), prior); err != nil {
			return err
		}
	}
	if err := writeJSON(ctx, filepath.Join(m.dir, "activation.json"), a); err != nil {
		return err
	}
	if err := writeJSON(ctx, filepath.Join(m.install.Root, ".lumen-activating.json"), struct {
		Token string `json:"token"`
	}{token}); err != nil {
		return err
	}
	if err := startHelper(ctx, helper, m.install.Root, token); err != nil {
		_ = os.Remove(filepath.Join(m.install.Root, ".lumen-activating.json"))
		return err
	}
	if err := m.transition(ctx, core.UpdateQuiescing); err != nil {
		_ = os.Remove(filepath.Join(m.install.Root, ".lumen-activating.json"))
		return err
	}
	return nil
}

// PendingClose returns only an explicitly approved activation for this library.
func (m *Manager) PendingClose(dataDir string) (Activation, bool, error) {
	if m.Status().Phase != core.UpdateQuiescing {
		return Activation{}, false, nil
	}
	var a Activation
	if err := readJSON(filepath.Join(m.dir, "activation.json"), &a); err != nil {
		return a, false, err
	}
	if a.DataDir != dataDir || a.Install != m.install || a.Phase != core.UpdateQuiescing {
		return a, false, ErrInvalid
	}
	return a, true, nil
}

// Closed acknowledges a completed SQLite backup and database close to the helper.
func (m *Manager) Closed(ctx context.Context, a Activation) error {
	return writeJSON(ctx, filepath.Join(m.dir, "closed-"+a.Token+".json"), struct {
		Token string `json:"token"`
	}{a.Token})
}

// Apply waits for the old process lease, then commits one complete generation.
// The helper writes no SQL; matching library restoration belongs to the old app.
func Apply(ctx context.Context, root, token string) error {
	// A pre-commit failure reopens ordinary launches. Pending health retains the
	// admission barrier until recovery or the durable acceptance checkpoint.
	defer func() {
		var selected Active
		if readJSON(filepath.Join(root, ".lumen-active.json"), &selected) == nil && selected.Pending == "" {
			var barrier struct {
				Token string `json:"token"`
			}
			if readJSON(filepath.Join(root, ".lumen-activating.json"), &barrier) == nil && barrier.Token == token {
				_ = os.Remove(filepath.Join(root, ".lumen-activating.json"))
			}
			var install Installation
			if readJSON(filepath.Join(root, ".lumen-install.json"), &install) == nil {
				if dir, err := StateDirectory(install); err == nil {
					var a Activation
					if readJSON(filepath.Join(dir, "activation.json"), &a) == nil && a.Token == token && a.Phase != core.UpdateComplete {
						a.Phase = core.UpdateFailed
						_ = writeJSON(context.WithoutCancel(ctx), filepath.Join(dir, "activation.json"), a)
					}
				}
			}
		}
	}()
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		return err
	}
	dir, err := StateDirectory(install)
	if err != nil {
		return err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, "activation.json"), &a); err != nil {
		return err
	}
	if a.Format != 1 || a.Token != token || len(token) != 32 || a.Install != install || !filepath.IsAbs(a.DataDir) || a.Backup != filepath.Join(dir, "library-"+token+".db") {
		return ErrInvalid
	}
	if err := writeJSON(ctx, filepath.Join(dir, "helper-ready-"+token+".json"), struct {
		Token string `json:"token"`
	}{token}); err != nil {
		return err
	}
	deadline, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	for {
		var closed struct {
			Token string `json:"token"`
		}
		if err := readJSON(filepath.Join(dir, "closed-"+token+".json"), &closed); err == nil && closed.Token == token {
			break
		}
		select {
		case <-deadline.Done():
			return deadline.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	if err := waitParent(deadline, a.ParentPID, a.ParentCreated); err != nil {
		return err
	}
	lease, err := AcquireLease(deadline, root, true)
	if err != nil {
		return err
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
	staged := filepath.Join(root, ".update", "candidate")
	if err := VerifyGeneration(ctx, staged, a.Candidate.Release); err != nil {
		return err
	}
	if err := VerifyPublisher(ctx, filepath.Join(staged, "lumen-app.exe"), config.PublisherPins); err != nil {
		return err
	}
	current, err := LoadActive(ctx, root)
	if err != nil {
		return err
	}
	if current.Generation != a.Previous.Generation || current.Version != a.Previous.Version || current.Pending != "" {
		return ErrState
	}
	a.Phase = core.UpdateApplying
	if err := writeJSON(ctx, filepath.Join(dir, "activation.json"), a); err != nil {
		return err
	}
	generation := a.Candidate.Release.Version + "-" + token
	destination := filepath.Join(root, "runtime", generation)
	if err := os.Rename(staged, destination); err != nil {
		return err
	}
	active := Active{Format: 1, Generation: generation, Version: a.Candidate.Release.Version, Release: a.Candidate.Release, Pending: token}
	if err := writeJSON(ctx, filepath.Join(root, ".lumen-active.json"), active); err != nil {
		return err
	}
	a.Phase = core.UpdateAwaitingHealth
	if err := writeJSON(ctx, filepath.Join(dir, "activation.json"), a); err != nil {
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
func AcknowledgeHealth(ctx context.Context, root, token string) error {
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		return err
	}
	dir, err := StateDirectory(install)
	if err != nil {
		return err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, "activation.json"), &a); err != nil {
		return err
	}
	if a.Token != token || a.Phase != core.UpdateAwaitingHealth {
		return ErrState
	}
	active, err := LoadActive(ctx, root)
	if err != nil {
		return err
	}
	if active.Pending != token || active.Version != a.Candidate.Release.Version {
		return ErrInvalid
	}
	if err := updateRegistration(root, active.Version); err != nil {
		return err
	}
	// The durable complete checkpoint forbids automatic database restoration.
	a.Phase = core.UpdateComplete
	a.CompletedAt = time.Now().Unix()
	if err := writeJSON(ctx, filepath.Join(dir, "activation.json"), a); err != nil {
		return err
	}
	active.Pending = ""
	if err := writeJSON(ctx, filepath.Join(root, ".lumen-active.json"), active); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, ".lumen-activating.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Recover restores only before the durable health commit. After acceptance,
// newer library writes must survive even if a subsequent launch fails.
func Recover(ctx context.Context, root string) error {
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		return err
	}
	dir, err := StateDirectory(install)
	if err != nil {
		return err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, "activation.json"), &a); err != nil {
		return err
	}
	if a.Install != install || a.Format != 1 || len(a.Token) != 32 {
		return ErrInvalid
	}
	if a.Phase == core.UpdateComplete {
		active, err := LoadActive(ctx, root)
		if err != nil {
			return err
		}
		if active.Pending == a.Token {
			active.Pending = ""
			return writeJSON(ctx, filepath.Join(root, ".lumen-active.json"), active)
		}
		return nil
	}
	if a.Phase != core.UpdateApplying && a.Phase != core.UpdateAwaitingHealth && a.Phase != core.UpdateRecovering {
		return ErrState
	}
	lease, err := AcquireLease(ctx, root, true)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	a.Phase = core.UpdateRecovering
	if err := writeJSON(ctx, filepath.Join(dir, "activation.json"), a); err != nil {
		return err
	}
	if err := VerifyGeneration(ctx, filepath.Join(root, "runtime", a.Previous.Generation), a.Previous.Release); err != nil {
		return err
	}
	if err := restoreLibrary(ctx, root, a); err != nil {
		return err
	}
	if err := writeJSON(ctx, filepath.Join(root, ".lumen-active.json"), a.Previous); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, ".lumen-activating.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	a.Phase = core.UpdateFailed
	if err := writeJSON(ctx, filepath.Join(dir, "activation.json"), a); err != nil {
		return err
	}
	return updateRegistration(root, a.Previous.Version)
}

// ValidateHealth ties a first launch to an approved activation and library path.
func ValidateHealth(ctx context.Context, install Installation, token, dataDir, version string) (Activation, error) {
	dir, err := StateDirectory(install)
	if err != nil {
		return Activation{}, err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, "activation.json"), &a); err != nil {
		return a, err
	}
	if a.Token != token || a.Phase != core.UpdateAwaitingHealth || a.DataDir != dataDir || a.Install != install || a.Candidate.Release.Version != version {
		return a, ErrInvalid
	}
	active, err := LoadActive(ctx, install.Root)
	if err != nil {
		return a, err
	}
	if active.Pending != token {
		return a, ErrInvalid
	}
	return a, nil
}

// HealthToken parses only the one approved bootstrap argument.
func HealthToken(args []string) string {
	if len(args) == 2 && args[0] == "--update-health" && len(args[1]) == 32 {
		if _, err := hex.DecodeString(args[1]); err == nil {
			return args[1]
		}
	}
	return ""
}

// RestoreRequest authorises the old application to restore before opening SQLite.
func RestoreRequest(ctx context.Context, root, token string) (Activation, error) {
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		return Activation{}, err
	}
	dir, err := StateDirectory(install)
	if err != nil {
		return Activation{}, err
	}
	var a Activation
	if err := readJSON(filepath.Join(dir, "activation.json"), &a); err != nil {
		return a, err
	}
	if a.Install != install || a.Phase != core.UpdateRecovering || a.Token != token || a.Backup != filepath.Join(dir, "library-"+token+".db") {
		return a, ErrInvalid
	}
	if _, err := os.Stat(a.Backup); errors.Is(err, os.ErrNotExist) {
		return a, fmt.Errorf("missing paired library backup: %w", err)
	}
	return a, ctx.Err()
}
