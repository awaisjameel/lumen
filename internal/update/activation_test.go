package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
)

// installationFixture builds a scratch installation whose selected generation
// is the fixture release, with its updater state under the test's own folder
// instead of the user's.
func installationFixture(t *testing.T) (root, dir string, install Installation, active Active) {
	t.Helper()
	t.Setenv("LocalAppData", t.TempDir())
	root = t.TempDir()
	release, data := releaseFixture(t)
	archive := filepath.Join(t.TempDir(), "runtime.zip")
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Stage(t.Context(), archive, generationDir(root, release.Version), release); err != nil {
		t.Fatal(err)
	}
	active = Active{Format: 1, Generation: release.Version, Version: release.Version, Release: release}
	install = Installation{Format: 1, ID: strings.Repeat("0", 32), Root: root}
	for name, value := range map[string]any{activeFile: active, installFile: install} {
		if err := writeJSON(t.Context(), filepath.Join(root, name), value); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := StateDirectory(install)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return root, dir, install, active
}

// stageCandidate stages the fixture runtime as the next version's candidate.
func stageCandidate(t *testing.T, root string) Candidate {
	t.Helper()
	release, data := releaseFixture(t)
	release.Version = "0.13.0"
	archive := filepath.Join(t.TempDir(), "runtime.zip")
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(stagedDir(root)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Stage(t.Context(), archive, stagedDir(root), release); err != nil {
		t.Fatal(err)
	}
	return Candidate{Release: release, Length: int64(len(data))}
}

func activationFixture(t *testing.T, dir string, install Installation, previous Active, c Candidate, phase core.UpdatePhase) Activation {
	t.Helper()
	id := strings.Repeat("ab", 16)
	a := Activation{Format: 1, Token: id, Install: install, Candidate: c, Previous: previous, DataDir: t.TempDir(), Backup: backupFile(dir, id), Phase: phase}
	if err := writeJSON(t.Context(), filepath.Join(dir, activationFile), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func readActivation(t *testing.T, dir string) Activation {
	t.Helper()
	var a Activation
	if err := readJSON(filepath.Join(dir, activationFile), &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestALaunchBarrierWhoseHelperDiedIsLiftedAndThePreparedRuntimeKept(t *testing.T) {
	root, dir, install, active := installationFixture(t)
	c := stageCandidate(t, root)
	a := activationFixture(t, dir, install, active, c, core.UpdateApplying)
	// The helper stopped after renaming the staged runtime, before selecting it.
	if err := os.Rename(stagedDir(root), generationDir(root, a.candidateGeneration())); err != nil {
		t.Fatal(err)
	}
	barrier := filepath.Join(root, barrierFile)
	if err := writeJSON(t.Context(), barrier, token{a.Token}); err != nil {
		t.Fatal(err)
	}

	helper, err := acquire(t.Context(), dir, helperFile, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := RecoverPending(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(barrier); err != nil {
		t.Fatal("a launch lifted the barrier of an activation whose helper is still at work", err)
	}
	if err := helper.Close(); err != nil {
		t.Fatal(err)
	}

	if err := RecoverPending(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(barrier); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the dead helper's barrier still closes every launch", err)
	}
	if got := readActivation(t, dir).Phase; got != core.UpdateFailed {
		t.Fatalf("abandoned activation recorded as %q", got)
	}
	if err := VerifyGeneration(t.Context(), stagedDir(root), c.Release); err != nil {
		t.Fatal("the prepared runtime was not returned to staging", err)
	}
	if selected, err := readActive(root); err != nil || selected.Generation != active.Generation || selected.Pending != "" {
		t.Fatalf("selection changed: %+v %v", selected, err)
	}
}

func TestRecoveryNeverReplacesALibraryAfterTheHealthCommit(t *testing.T) {
	root, dir, install, active := installationFixture(t)
	a := activationFixture(t, dir, install, active, Candidate{Release: active.Release}, core.UpdateAwaitingHealth)
	pending := active
	pending.Pending = a.Token
	if err := writeJSON(t.Context(), filepath.Join(root, activeFile), pending); err != nil {
		t.Fatal(err)
	}
	// Before the commit recovery must restore the paired library, which this
	// build (no trust configuration) cannot do: it refuses, and nothing launches.
	if err := RecoverPending(t.Context(), root); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("recovery without a restorable pair: %v", err)
	}
	if selected, err := readActive(root); err != nil || selected.Pending != a.Token {
		t.Fatalf("an unrecovered selection was admitted: %+v %v", selected, err)
	}

	// The new application committed its health and stopped before the
	// selection was cleared: its library now holds work worth keeping.
	a.Phase = core.UpdateComplete
	if err := writeJSON(t.Context(), filepath.Join(dir, activationFile), a); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(t.Context(), filepath.Join(root, barrierFile), token{a.Token}); err != nil {
		t.Fatal(err)
	}
	if err := RecoverPending(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if selected, err := readActive(root); err != nil || selected.Pending != "" {
		t.Fatalf("committed selection still pending: %+v %v", selected, err)
	}
	if _, err := os.Stat(filepath.Join(root, barrierFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a committed update still closes every launch", err)
	}
	if got := readActivation(t, dir).Phase; got != core.UpdateComplete {
		t.Fatalf("committed activation rewritten as %q", got)
	}
}

func TestAnObservingWindowCannotRestartDismissOrDiscard(t *testing.T) {
	m := managerFixture(t)
	if err := m.owner.Close(); err != nil {
		t.Fatal(err)
	}
	m.owner = nil
	// What an observer shows is the coordinator's journal, a ready update included.
	m.state.Status.Phase = core.UpdateReady
	m.state.Candidate = &Candidate{}
	for name, err := range map[string]error{
		"restart": m.RequestRestart(t.Context(), t.TempDir(), Capabilities{}),
		"dismiss": m.Dismiss(t.Context()),
		"discard": m.Discard(t.Context()),
	} {
		if !errors.Is(err, ErrBusy) {
			t.Errorf("%s by an observer: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(m.dir, journalFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an observer wrote the coordinator's journal", err)
	}
}

func TestARestartThatFailedBeforeTheSwitchIsReadyAgainWithoutADownload(t *testing.T) {
	root, dir, install, active := installationFixture(t)
	c := stageCandidate(t, root)
	activationFixture(t, dir, install, active, c, core.UpdateFailed)
	saved := fresh(active.Version)
	saved.Status.Phase = core.UpdateQuiescing
	saved.Status.Policy.Automatic = true
	saved.Candidate = &c
	if err := writeJSON(t.Context(), filepath.Join(dir, journalFile), saved); err != nil {
		t.Fatal(err)
	}
	open := func() *Manager {
		m := Inert(t.Context(), active.Version, nil)
		m.install, m.dir = install, dir
		if err := m.attach(t.Context(), 0); err != nil {
			t.Fatal(err)
		}
		return m
	}
	m := open()
	status := m.Status()
	if status.Phase != core.UpdateReady || status.Error != core.UpdateFailureFailed || !status.Coordinator {
		t.Fatalf("status after a failed restart: %+v", status)
	}
	if err := VerifyGeneration(t.Context(), stagedDir(root), c.Release); err != nil {
		t.Fatal("the staged runtime was removed", err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}

	// An unreadable journal costs the policy its opt-in, never the launch.
	if err := os.WriteFile(filepath.Join(dir, journalFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	m = open()
	defer func() { _ = m.Close() }()
	status = m.Status()
	if status.Phase != core.UpdateIdle || status.Policy.Automatic || !status.Policy.DeferBattery {
		t.Fatalf("status from an unreadable journal: %+v", status)
	}
	if _, err := os.Stat(stagedDir(root)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging that no journal accounts for was kept", err)
	}
}

type fileDelivery struct {
	data []byte
	err  error
}

func (d fileDelivery) Download(_ context.Context, _ Candidate, name string, _ int, progress func(int64)) error {
	if err := os.WriteFile(name, d.data, 0o600); err != nil {
		return err
	}
	progress(int64(len(d.data)))
	return d.err
}

func TestPreparationKeepsOnlyTheStagedRuntimeAndAFailureKeepsNothing(t *testing.T) {
	m := managerFixture(t)
	release, data := releaseFixture(t)
	m.verify = func(context.Context, string, []string) error { return nil }
	var announced []core.UpdateStatus
	m.emit = func(s core.UpdateStatus) { announced = append(announced, s) }
	prepare := func(d Delivery) core.UpdateStatus {
		m.mu.Lock()
		m.delivery = d
		m.state.Candidate = &Candidate{Release: release, Length: int64(len(data))}
		m.state.Status.Phase = core.UpdateAvailable
		m.mu.Unlock()
		if err := m.Download(); err != nil {
			t.Fatal(err)
		}
		m.wg.Wait()
		return m.Status()
	}

	if status := prepare(fileDelivery{data: data}); status.Phase != core.UpdateReady {
		t.Fatalf("preparation: %+v", status)
	}
	if _, err := os.Stat(m.archive()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the downloaded archive outlived staging", err)
	}
	if err := VerifyGeneration(t.Context(), m.staged(), release); err != nil {
		t.Fatal(err)
	}
	progressed := false
	for i, s := range announced {
		if i > 0 && s.Generation <= announced[i-1].Generation {
			t.Fatalf("snapshot %d does not follow its predecessor", i)
		}
		progressed = progressed || (s.Phase == core.UpdateDownloading && s.Bytes == int64(len(data)))
	}
	if !progressed {
		t.Fatal("download progress was never announced")
	}

	if err := m.Discard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if status := prepare(fileDelivery{data: data, err: ErrInvalid}); status.Phase != core.UpdateFailed || status.Error != core.UpdateFailureInvalid {
		t.Fatalf("failed preparation: %+v", status)
	}
	for _, name := range []string{m.archive(), m.staged()} {
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("a failed download left %s behind: %v", filepath.Base(name), err)
		}
	}
}

type answeringSource struct {
	calls int
	err   error
}

func (s *answeringSource) Check(context.Context, core.UpdateChannel, string, int) (*Candidate, error) {
	s.calls++
	return nil, s.err
}

func TestScheduledDiscoveryBacksOffAndCountsOnlyAnsweredChecks(t *testing.T) {
	m := managerFixture(t)
	m.state.Status.Policy.Automatic = true
	s := &answeringSource{err: errors.New("offline")}
	m.source = s
	if !m.due(time.Now()) {
		t.Fatal("an enabled daily check is not due")
	}
	m.safe = func(context.Context, core.UpdatePolicy) bool { return false }
	if wait := m.discover(); wait != deferredFor || s.calls != 0 {
		t.Fatalf("a metered or battery-powered PC was not deferred: %v after %d requests", wait, s.calls)
	}
	m.safe = func(context.Context, core.UpdatePolicy) bool { return true }
	if wait := m.discover(); wait != failedFor || s.calls != 1 {
		t.Fatalf("a failed check: %v after %d requests", wait, s.calls)
	}
	if status := m.Status(); status.LastCheck != 0 || !m.due(time.Now()) {
		t.Fatalf("an unanswered check counted as checked: %+v", status)
	}
	s.err = nil
	if wait := m.discover(); wait != 0 || m.Status().LastCheck == 0 || m.due(time.Now()) {
		t.Fatalf("an answered check: %v, %+v", wait, m.Status())
	}
	// A staged candidate is never displaced by a poll.
	m.state.Status.LastCheck = 0
	m.state.Status.Phase = core.UpdateReady
	if m.due(time.Now()) {
		t.Fatal("a scheduled check would replace a ready update")
	}
}

func TestAnOrdinaryLaunchCheckCatchesABrokenGenerationWithoutReadingIt(t *testing.T) {
	root, _, _, active := installationFixture(t)
	generation := generationDir(root, active.Generation)
	if err := VerifyLayout(t.Context(), generation, active.Release); err != nil {
		t.Fatal(err)
	}
	model := filepath.Join(generation, "models", "model.bin")
	data, err := os.ReadFile(model)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func() error{
		"a truncated file":  func() error { return os.WriteFile(model, data[:len(data)-1], 0o600) },
		"a missing file":    func() error { return os.Remove(model) },
		"an unrelated file": func() error { return os.WriteFile(filepath.Join(generation, "other.dll"), nil, 0o600) },
	} {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		if err := VerifyLayout(t.Context(), generation, active.Release); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s was accepted: %v", name, err)
		}
		_ = os.Remove(filepath.Join(generation, "other.dll"))
		if err := os.WriteFile(model, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Changed bytes of the same length are what the full check is for.
	data[0] ^= 1
	if err := os.WriteFile(model, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadActive(t.Context(), root); !errors.Is(err, ErrInvalid) {
		t.Fatalf("altered bytes passed the full check: %v", err)
	}
}

func TestSetupGivesAMovedFolderItsOwnIdentityAndReopensLaunches(t *testing.T) {
	root, _, install, _ := installationFixture(t)
	moved := install
	moved.Root = filepath.Join(filepath.Dir(root), "elsewhere")
	for name, value := range map[string]any{installFile: moved, barrierFile: token{strings.Repeat("cd", 16)}, installingFile: struct{}{}} {
		if err := writeJSON(t.Context(), filepath.Join(root, name), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := Bootstrap(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	got, _, err := installed(root)
	if err != nil || got.Root != root || got.ID == moved.ID || len(got.ID) != 32 {
		t.Fatalf("identity after Setup: %+v %v", got, err)
	}
	for _, barrier := range []string{barrierFile, installingFile} {
		if _, err := os.Stat(filepath.Join(root, barrier)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still closes launches: %v", barrier, err)
		}
	}
	if err := Bootstrap(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if again, _, err := installed(root); err != nil || again != got {
		t.Fatalf("a repeated Setup changed the identity: %+v %v", again, err)
	}
}

func TestRemovingAnInstallationTakesItsStagingAndKeepsUnrelatedFiles(t *testing.T) {
	root, _, _, _ := installationFixture(t)
	stageCandidate(t, root)
	if err := os.WriteFile(filepath.Join(root, ".update", "runtime.zip"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(root, "runtime", "notes.txt")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveGenerations(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".update")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("staging was left in the installation folder", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("an unrelated file was removed", err)
	}
}

func TestAnUpdateMustKeepWhatThePreviousReleaseCouldDo(t *testing.T) {
	all := Capabilities{Search: true, People: true, Video: true}
	for _, c := range []struct {
		name          string
		now, baseline Capabilities
		covers        bool
	}{
		{"nothing lost", all, all, true},
		{"a PC that never had AI", Capabilities{Video: true}, Capabilities{Video: true}, true},
		{"more than before", all, Capabilities{}, true},
		{"search lost", Capabilities{People: true, Video: true}, all, false},
		{"video lost", Capabilities{Search: true, People: true}, all, false},
	} {
		if c.now.Covers(c.baseline) != c.covers {
			t.Errorf("%s: covers = %v", c.name, !c.covers)
		}
	}
}

func TestADownloadThatStopsArrivingFailsInsteadOfWaiting(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write(make([]byte, 1024)); err != nil {
			t.Error(err)
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	transport, err := NewTransport([]string{server.URL})
	if err != nil {
		t.Fatal(err)
	}
	transport.client.Transport = server.Client().Transport
	transport.stall = 200 * time.Millisecond
	c := Candidate{Length: 1 << 20, SHA256: strings.Repeat("0", 64), URL: server.URL + "/runtime.zip"}
	err = transport.Download(t.Context(), c, filepath.Join(t.TempDir(), "runtime.zip"), 0, func(int64) {})
	if !errors.Is(err, errStalled) {
		t.Fatalf("stalled download: %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("a stalled download reads as cancelled by the user")
	}
}
