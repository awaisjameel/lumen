// Package testenv finds the native runtime that scripts/deps.sh prepares
// (deps/runtime: DLLs, FFmpeg, models), so tests can use the real thing and
// skip cleanly when it hasn't been fetched.
package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

// Root is the repository root (the folder holding go.mod).
func Root(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// Runtime returns deps/runtime, skipping the test if it is missing.
func Runtime(t testing.TB) string {
	t.Helper()
	dir := filepath.Join(Root(t), "deps", "runtime")
	if _, err := os.Stat(filepath.Join(dir, "models")); err != nil {
		t.Skip("deps/runtime missing: run scripts/deps.sh")
	}
	return dir
}

// File returns a file inside deps/runtime, skipping the test if it is missing.
func File(t testing.TB, rel ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{Runtime(t)}, rel...)...)
	if _, err := os.Stat(p); err != nil {
		t.Skipf("%s missing: run scripts/deps.sh", p)
	}
	return p
}

// HEICSamples returns the public HEIC samples the Phase 0 spikes downloaded
// (spikes/.cache/heic), skipping the test if there are none.
func HEICSamples(t testing.TB) []string {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(Root(t), "spikes", ".cache", "heic", "*.heic"))
	if len(paths) == 0 {
		t.Skip("no HEIC samples: run spikes/fetch.sh")
	}
	return paths
}
