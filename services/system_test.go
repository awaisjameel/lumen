package services

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/diag"
	"lumen/internal/index"
	"lumen/internal/store"
)

// newSystemService is a system service over a library of one photo, its data
// folder a test's own.
func newSystemService(t *testing.T) (*SystemService, core.Paths) {
	t.Helper()
	l := newShownLibrary(t, 1)
	paths := core.PathsIn(t.TempDir())
	check(t, paths.Ensure())
	ix := index.New(l.st, paths, l.cache, index.Models{}, quietEvents{})
	return NewSystemService(NewLibraryService(l.st, ix, nil, nil), diag.New(paths.DataDir, "1.2.3"), paths, "1.2.3"), paths
}

func TestAboutSaysWhichLumenThisIsAndWhereItKeepsItsData(t *testing.T) {
	s, paths := newSystemService(t)
	a := s.About()
	if a.Version != "1.2.3" || a.DataDir != paths.DataDir || a.ReleasesURL != ReleasesURL || a.LastRunCrashed {
		t.Fatalf("about = %+v", a)
	}
}

func TestAnUnknownPageIsNotOpened(t *testing.T) {
	s, _ := newSystemService(t)
	if err := s.OpenLink("https://example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("open an unknown link = %v, want not found", err)
	}
}

func TestMissingNoticesAreNotFound(t *testing.T) {
	s, _ := newSystemService(t) // a test's paths have no runtime folder
	if err := s.OpenNotices(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("open notices = %v, want not found", err)
	}
}

func TestAProblemReportDescribesThePCAndLibraryButNamesNoFolder(t *testing.T) {
	ctx := t.Context()
	s, paths := newSystemService(t)
	check(t, os.WriteFile(paths.Log, []byte("level=INFO msg=\"lumen starting\"\n"), 0o600))
	sources, err := s.lib.Sources(ctx)
	check(t, err)
	out := filepath.Join(t.TempDir(), "report.zip")
	check(t, os.WriteFile(out, []byte("previous report"), 0o600))
	check(t, s.writeProblemReport(ctx, out))

	files := readZip(t, out)
	about := files["system.txt"]
	for _, want := range []string{"Lumen 1.2.3", "Processor: ", "Library schema version: ", "Items: 1", `"pauseBusy": true`} {
		if !strings.Contains(about, want) {
			t.Errorf("system.txt lacks %q:\n%s", want, about)
		}
	}
	if strings.Contains(about, sources[0].Path) || strings.Contains(about, paths.DataDir) {
		t.Errorf("system.txt names a folder:\n%s", about)
	}
	if !strings.Contains(files["logs/lumen.log"], "lumen starting") {
		t.Errorf("report lacks the log: %v", files)
	}
}

func TestAFailedProblemReportLeavesNothingBehind(t *testing.T) {
	s, _ := newSystemService(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "report.zip")
	check(t, os.Mkdir(filepath.Join(dir, "logs-are-unreadable"), 0o750))
	s.paths.Log = filepath.Join(dir, "logs-are-unreadable") // a folder can't be read as a file
	if err := s.writeProblemReport(t.Context(), out); err == nil {
		t.Fatal("writing a report from an unreadable log succeeded")
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a failed report left %s behind (%v)", out, err)
	}
}

func TestAFailedProblemReportPreservesAnExistingDestination(t *testing.T) {
	s, paths := newSystemService(t)
	out := filepath.Join(t.TempDir(), "report.zip")
	check(t, os.WriteFile(out, []byte("keep this report"), 0o600))
	s.paths.Log = paths.DataDir // a directory cannot be included as a log
	if err := s.writeProblemReport(t.Context(), out); err == nil {
		t.Fatal("writing an unreadable log succeeded")
	}
	b, err := os.ReadFile(out)
	check(t, err)
	if string(b) != "keep this report" {
		t.Fatalf("failed save changed the destination: %q", b)
	}
	entries, err := os.ReadDir(filepath.Dir(out))
	check(t, err)
	if len(entries) != 1 {
		t.Fatalf("failed save left temporary files: %v", entries)
	}
}

func TestACancelledProblemReportPreservesItsDestination(t *testing.T) {
	s, _ := newSystemService(t)
	out := filepath.Join(t.TempDir(), "report.zip")
	check(t, os.WriteFile(out, []byte("keep"), 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.writeProblemReport(ctx, out); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled save = %v, want cancellation", err)
	}
	b, err := os.ReadFile(out)
	check(t, err)
	if string(b) != "keep" {
		t.Fatalf("cancelled save changed the destination: %q", b)
	}
}

func readZip(t *testing.T, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	check(t, err)
	defer func() { _ = z.Close() }()
	files := map[string]string{}
	for _, f := range z.File {
		rc, err := f.Open()
		check(t, err)
		b, err := io.ReadAll(rc)
		check(t, err)
		check(t, rc.Close())
		files[f.Name] = string(b)
	}
	return files
}

func TestUIFailuresAreLoggedButCannotFloodTheLog(t *testing.T) {
	s, _ := newSystemService(t)
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return at }

	for range uiLogPerMinute + 5 {
		s.LogUIError(core.UIError{Message: "TypeError: x is undefined", Stack: "at render", Where: "vue"})
	}
	if n := strings.Count(logs.String(), "ui failure"); n != uiLogPerMinute {
		t.Fatalf("%d failures logged in a minute, want %d", n, uiLogPerMinute)
	}
	at = at.Add(time.Minute)
	s.LogUIError(core.UIError{Message: "later", Where: "window"})
	if !strings.Contains(logs.String(), "count=5") || !strings.Contains(logs.String(), "err=later") {
		t.Fatalf("the next minute did not report the dropped ones and log its own:\n%s", logs.String())
	}
}

func TestLongUIFieldsAreClippedOnWholeCharacters(t *testing.T) {
	long := strings.Repeat("é", uiLogMaxField) // two bytes each
	got := clip(long)
	if len(got) > uiLogMaxField+len("…") || !strings.HasSuffix(got, "…") {
		t.Fatalf("clipped to %d bytes", len(got))
	}
	if trimmed := strings.TrimSuffix(got, "…"); strings.ToValidUTF8(trimmed, "?") != trimmed {
		t.Fatal("clipping split a character")
	}
	if clip("short") != "short" {
		t.Fatal("a short field was changed")
	}
}
