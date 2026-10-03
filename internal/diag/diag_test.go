package diag

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// crashEnv makes the test binary, started again as a child, take crash
// reports in the folder it names and then crash (TestCrashForAParent).
const crashEnv = "LUMEN_TEST_CRASH_DIR"

func TestCrashForAParent(t *testing.T) {
	dir := os.Getenv(crashEnv)
	if dir == "" {
		t.Skip("only run as TestACrashOfTheLastRunBecomesAReport's child")
	}
	if _, err := New(dir, "9.9.9").Start(time.Now()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		panic("deliberate crash") // unrecovered in a goroutine: it ends the process
	}()
	<-done
}

func TestACrashOfTheLastRunBecomesAReport(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestCrashForAParent$")
	cmd.Env = append(os.Environ(), crashEnv+"="+dir)
	if err := cmd.Run(); err == nil {
		t.Fatal("the child did not crash")
	}

	r := New(dir, "9.9.9")
	stop, err := r.Start(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	name := r.LastCrash()
	if !strings.HasPrefix(name, "crash-") {
		t.Fatalf("last crash = %q, want a crash report", name)
	}
	b, err := os.ReadFile(filepath.Join(r.Dir(), name))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Lumen 9.9.9", "panic: deliberate crash", "TestCrashForAParent"} {
		if !bytes.Contains(b, []byte(want)) {
			t.Errorf("report lacks %q:\n%s", want, b)
		}
	}
}

func TestAnAbruptEndWithoutACrashLeavesNoReport(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, "1.0.0")
	check(t, os.MkdirAll(r.Dir(), 0o750))
	pending := filepath.Join(r.Dir(), pendingName)
	check(t, os.WriteFile(pending, []byte(r.headerLocked()+headerEnd), 0o600))
	stop, err := r.Start(time.Now())
	check(t, err)
	defer stop()
	if r.LastCrash() != "" {
		t.Fatalf("a power cut was reported as crash %q", r.LastCrash())
	}
	if names, err := r.Files(); err != nil || len(names) != 0 {
		t.Fatalf("reports = %v (%v), want none", names, err)
	}
}

func TestACleanExitLeavesNothingBehind(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	stop, err := r.Start(time.Now())
	check(t, err)
	pending := filepath.Join(r.Dir(), pendingName)
	b, err := os.ReadFile(pending)
	check(t, err)
	if !bytes.HasPrefix(b, []byte("Lumen 1.0.0\n")) || !bytes.HasSuffix(b, []byte(headerEnd)) {
		t.Fatalf("pending file = %q, want the header alone", b)
	}
	stop()
	if _, err := os.Stat(pending); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a clean exit left its crash output behind: %v", err)
	}
	// The runtime has let go of it: taking reports again works.
	stop, err = r.Start(time.Now())
	check(t, err)
	stop()
}

func TestOneRunTakesCrashReportsOnce(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	stop, err := r.Start(time.Now())
	check(t, err)
	defer stop()
	if _, err := r.Start(time.Now()); !errors.Is(err, ErrArmed) {
		t.Fatalf("second start = %v, want ErrArmed", err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir(), pendingName)); err != nil {
		t.Fatalf("second start disturbed the active crash output: %v", err)
	}
}

func TestAnIncompleteCrashHeaderIsKept(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	check(t, os.MkdirAll(r.Dir(), 0o750))
	check(t, os.WriteFile(filepath.Join(r.Dir(), pendingName), []byte("panic: incomplete header\n"), 0o600))
	stop, err := r.Start(time.Now())
	check(t, err)
	defer stop()
	if r.LastCrash() == "" {
		t.Fatal("incomplete crash output was discarded")
	}
}

func TestACrashThatCannotBeKeptIsNeverTruncated(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	check(t, os.MkdirAll(r.Dir(), 0o750))
	pending := filepath.Join(r.Dir(), pendingName)
	content := []byte(r.headerLocked() + headerEnd + "panic: keep this\n")
	check(t, os.WriteFile(pending, content, 0o600))
	fi, err := os.Stat(pending)
	check(t, err)
	check(t, os.Mkdir(filepath.Join(r.Dir(), reportName("crash", fi.ModTime())), 0o750))
	if _, err := r.Start(time.Now()); err == nil {
		t.Fatal("a crash report could be renamed over a directory")
	}
	b, err := os.ReadFile(pending)
	check(t, err)
	if !bytes.Equal(b, content) {
		t.Fatalf("failed start truncated crash output: %q", b)
	}
}

func TestOnlyTheNewestReportsAreKept(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	start := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for i := range keepReports + 5 {
		r.RecordPanic(errors.New("boom"), "stack", start.Add(time.Duration(i)*time.Minute))
	}
	names, err := r.Files()
	check(t, err)
	if len(names) != keepReports {
		t.Fatalf("%d reports kept, want %d", len(names), keepReports)
	}
	if want := strings.TrimSuffix(reportName("panic", start.Add((keepReports+4)*time.Minute)), ".txt") + "-"; !strings.HasPrefix(names[0], want) {
		t.Fatalf("newest report = %s, want %s", names[0], want)
	}
	if oldest := strings.TrimSuffix(reportName("panic", start.Add(5*time.Minute)), ".txt") + "-"; !strings.HasPrefix(names[len(names)-1], oldest) {
		t.Fatalf("oldest report kept = %s, want %s", names[len(names)-1], oldest)
	}
}

func TestPanicsAtTheSameTimeKeepSeparateReports(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	now := time.Now()
	for range 5 {
		r.RecordPanic(errors.New("boom"), "stack", now)
	}
	names, err := r.Files()
	check(t, err)
	if len(names) != 5 {
		t.Fatalf("simultaneous panics kept %d reports, want 5", len(names))
	}
}

func TestAnOldStopCannotDisarmANewerRun(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	stop, err := r.Start(time.Now())
	check(t, err)
	stop()
	stopNew, err := r.Start(time.Now())
	check(t, err)
	defer stopNew()
	stop()
	if _, err := os.Stat(filepath.Join(r.Dir(), pendingName)); err != nil {
		t.Fatalf("old stop disturbed the new crash output: %v", err)
	}
}

type cancelWriter struct {
	bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelWriter) Write(p []byte) (int, error) {
	w.cancel()
	return w.Buffer.Write(p)
}

func TestAProblemReportStopsWhenItsContextIsCancelled(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w := &cancelWriter{cancel: cancel}
	if err := r.WriteReport(ctx, w, Report{About: strings.Repeat("system", 5000)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("report interrupted while writing = %v, want cancellation", err)
	}
}

func TestReportsOfBothKindsSortByTime(t *testing.T) {
	r := New(t.TempDir(), "1.0.0")
	check(t, os.MkdirAll(r.Dir(), 0o750))
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	for _, n := range []string{reportName("panic", at), reportName("crash", at.Add(time.Hour)), "notes.txt"} {
		check(t, os.WriteFile(filepath.Join(r.Dir(), n), nil, 0o600))
	}
	names, err := r.Files()
	check(t, err)
	if want := []string{reportName("crash", at.Add(time.Hour)), reportName("panic", at)}; !slices.Equal(names, want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
}

func TestAProblemReportHoldsTheLogsAndReportsAndSkipsWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, "1.0.0")
	r.RecordPanic(errors.New("boom"), "goroutine 1", time.Now())
	log := filepath.Join(dir, "lumen.log")
	check(t, os.WriteFile(log, []byte("level=INFO msg=hello\n"), 0o600))
	var buf bytes.Buffer
	check(t, r.WriteReport(t.Context(), &buf, Report{About: "Lumen 1.0.0\n", Logs: []string{filepath.Join(dir, "lumen.old.log"), log}}))

	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	check(t, err)
	got := map[string]string{}
	for _, f := range z.File {
		rc, err := f.Open()
		check(t, err)
		b, err := io.ReadAll(rc)
		check(t, err)
		check(t, rc.Close())
		got[f.Name] = string(b)
	}
	if len(got) != 3 || got["system.txt"] != "Lumen 1.0.0\n" || got["logs/lumen.log"] != "level=INFO msg=hello\n" {
		t.Fatalf("report = %v", got)
	}
	names, err := r.Files()
	check(t, err)
	if !strings.Contains(got["crashes/"+names[0]], "panic: boom") {
		t.Fatalf("report lacks the panic: %v", got)
	}
}

func TestVersionsReadAsPeopleWriteThem(t *testing.T) {
	if got := versionString(0, 9, 0, 0); got != "0.9.0" {
		t.Errorf("0.9.0.0 = %q", got)
	}
	if got := versionString(1, 2, 3, 4); got != "1.2.3.4" {
		t.Errorf("1.2.3.4 = %q", got)
	}
	if got := Version(); got != devVersion {
		t.Errorf("a test binary's version = %q, want %q", got, devVersion) // it has no version resource
	}
}

func TestThePCIsDescribed(t *testing.T) {
	pc := System()
	if pc.Threads < 1 {
		t.Fatalf("threads = %d", pc.Threads)
	}
	if s := pc.String(); !strings.Contains(s, "Processor: ") || !strings.Contains(s, "Memory: ") {
		t.Fatalf("description = %q", s)
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
