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
func Root(tb testing.TB) string {
	tb.Helper()
	dir, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// Runtime returns deps/runtime, skipping the test if it is missing.
func Runtime(tb testing.TB) string {
	tb.Helper()
	dir := filepath.Join(Root(tb), "deps", "runtime")
	if _, err := os.Stat(filepath.Join(dir, "models")); err != nil {
		tb.Skip("deps/runtime missing: run scripts/deps.sh")
	}
	return dir
}

// File returns a file inside deps/runtime, skipping the test if it is missing.
func File(tb testing.TB, rel ...string) string {
	tb.Helper()
	p := filepath.Join(append([]string{Runtime(tb)}, rel...)...)
	if _, err := os.Stat(p); err != nil {
		tb.Skipf("%s missing: run scripts/deps.sh", p)
	}
	return p
}

// HEICSamples returns the public HEIC samples the Phase 0 spikes downloaded
// (spikes/.cache/heic), skipping the test if there are none.
func HEICSamples(tb testing.TB) []string {
	tb.Helper()
	paths, _ := filepath.Glob(filepath.Join(Root(tb), "spikes", ".cache", "heic", "*.heic"))
	if len(paths) == 0 {
		tb.Skip("no HEIC samples: run spikes/fetch.sh")
	}
	return paths
}

// FacePhotos returns the photos in the folder LUMEN_FACE_PHOTOS names (real
// photos of people, kept out of the repo), skipping the test if it is unset.
func FacePhotos(tb testing.TB, n int) []string {
	tb.Helper()
	dir := os.Getenv("LUMEN_FACE_PHOTOS")
	if dir == "" {
		tb.Skip("LUMEN_FACE_PHOTOS not set: no photos of people to test on")
	}
	paths, _ := filepath.Glob(filepath.Join(dir, "*.[jJ][pP][gG]"))
	if len(paths) == 0 {
		tb.Skipf("no JPEGs in %s", dir)
	}
	return paths[:min(n, len(paths))]
}
