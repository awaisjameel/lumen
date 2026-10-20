//go:build windows

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/testenv"
)

// watchWait is how long a test waits for the watcher. A change arrives in
// milliseconds, but on a loaded machine (the whole race suite in parallel)
// it took over 5 s twice; waiting longer costs nothing when it passes.
const watchWait = 30 * time.Second

func TestWatchReportsChangesAndReleasesHandle(t *testing.T) {
	checkWatchReportsChangesAndReleasesHandle(t, t.TempDir())
}

func TestWatchPreservesASourcesShortPathSpelling(t *testing.T) {
	checkWatchReportsChangesAndReleasesHandle(t, testenv.ShortPath(t))
}

func checkWatchReportsChangesAndReleasesHandle(t *testing.T, dir string) {
	t.Helper()
	got := make(chan string, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done, err := watch(ctx, dir, func(p string) { got <- p })
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"new.jpg", "next.jpg"} {
		want := filepath.Join(dir, name)
		if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		timeout := time.After(watchWait)
		for seen := false; !seen; {
			select {
			case p := <-got:
				seen = p == want
			case <-timeout:
				t.Fatal("no change reported for", want)
			}
		}
	}

	// Cancelling must interrupt the blocked read and close the handle.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancelled watch returned %v, want nil", err)
		}
	case <-time.After(watchWait):
		t.Fatal("watch did not release its handle after cancel")
	}
}

func TestWatchReportsWhenItCannotStart(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	ready := false
	err := Watch(t.Context(), missing, func() { ready = true }, func(string) {})
	if err == nil || ready {
		t.Fatalf("Watch(missing folder) = %v, ready %v; want an error before ready", err, ready)
	}
}

func TestWatchRefusesAFileBeforeAnnouncingReadiness(t *testing.T) {
	path := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	ready := false
	err := Watch(t.Context(), path, func() { ready = true }, func(string) {})
	if err == nil || ready {
		t.Fatalf("Watch(file) = %v, ready %v; want an error before ready", err, ready)
	}
}
