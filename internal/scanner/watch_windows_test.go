//go:build windows

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchReportsChangesAndReleasesHandle(t *testing.T) {
	dir := t.TempDir()
	got := make(chan string, 16)
	ctx, cancel := context.WithCancel(context.Background())
	done, err := watch(ctx, dir, func(p string) { got <- p })
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(dir, "new.jpg")
	if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for seen := false; !seen; {
		select {
		case p := <-got:
			seen = p == want
		case <-timeout:
			t.Fatal("no change reported for", want)
		}
	}

	// Cancelling must interrupt the blocked read and close the handle.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("cancelled watch returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
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
