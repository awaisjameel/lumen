//go:build windows

package fileops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// names lists the entries of dir, to see that no temporary file is left.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWriteSidecarCreatesAMissingSidecarAndLeavesNoTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMG_1.jpg.xmp")
	got, err := WriteSidecar(t.Context(), path, []byte("v1"), nil)
	if err != nil || got != SidecarCreated {
		t.Fatalf("WriteSidecar = %v, %v; want created", got, err)
	}
	if mustRead(t, path) != "v1" {
		t.Fatal("the sidecar does not hold what was written")
	}
	if n := names(t, dir); len(n) != 1 {
		t.Fatalf("folder holds %v, want only the sidecar", n)
	}
}

func TestWriteSidecarNeverTouchesAFileLumenDidNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMG_1.jpg.xmp")
	if err := os.WriteFile(path, []byte("someone else's"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := WriteSidecar(t.Context(), path, []byte("v1"), nil)
	if err != nil || got != SidecarForeign {
		t.Fatalf("WriteSidecar = %v, %v; want foreign", got, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mustRead(t, path) != "someone else's" || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("a sidecar Lumen did not write was changed")
	}
	if n := names(t, dir); len(n) != 1 {
		t.Fatalf("folder holds %v, want only the sidecar", n)
	}
}

func TestWriteSidecarReplacesItsOwnSidecarAndSkipsOneThatWasEdited(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMG_1.jpg.xmp")
	if _, err := WriteSidecar(t.Context(), path, []byte("v1"), nil); err != nil {
		t.Fatal(err)
	}
	wrote := SidecarHash([]byte("v1"))

	got, err := WriteSidecar(t.Context(), path, []byte("v2"), wrote)
	if err != nil || got != SidecarReplaced || mustRead(t, path) != "v2" {
		t.Fatalf("own sidecar: %v, %v, %q; want replaced with v2", got, err, mustRead(t, path))
	}
	if n := names(t, dir); len(n) != 1 {
		t.Fatalf("folder holds %v, want only the sidecar", n)
	}

	// Someone edits it; Lumen's record still says v2.
	if err := os.WriteFile(path, []byte("edited by hand"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = WriteSidecar(t.Context(), path, []byte("v3"), SidecarHash([]byte("v2")))
	if err != nil || got != SidecarEdited || mustRead(t, path) != "edited by hand" {
		t.Fatalf("edited sidecar: %v, %v, %q; want edited and untouched", got, err, mustRead(t, path))
	}
}

func TestWriteSidecarSaysSameWhenTheFileAlreadyHoldsTheBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "IMG_1.jpg.xmp")
	if err := os.WriteFile(path, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, wrote := range [][]byte{nil, SidecarHash([]byte("v1")), SidecarHash([]byte("older"))} {
		if got, err := WriteSidecar(t.Context(), path, []byte("v1"), wrote); err != nil || got != SidecarSame {
			t.Fatalf("WriteSidecar = %v, %v; want same", got, err)
		}
	}
}

func TestPublishNewRefusesToReplaceAFileThatAppearedMeanwhile(t *testing.T) {
	dir := t.TempDir()
	tmp, target := filepath.Join(dir, "tmp"), filepath.Join(dir, "IMG_1.jpg.xmp")
	for path, content := range map[string]string{tmp: "ours", target: "theirs"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := publishNew(tmp, target); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("publishNew over an existing file = %v, want fs.ErrExist", err)
	}
	if mustRead(t, target) != "theirs" {
		t.Fatal("publishNew overwrote the file that was there")
	}
}

func TestWriteSidecarLeavesAReadOnlySidecarAndNoTemporaryFileWhenItCannotReplaceIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMG_1.jpg.xmp")
	if _, err := WriteSidecar(t.Context(), path, []byte("v1"), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) }) // so the temporary folder can be removed
	got, err := WriteSidecar(t.Context(), path, []byte("v2"), SidecarHash([]byte("v1")))
	if err == nil || got != SidecarFailed {
		t.Fatalf("WriteSidecar = %v, %v; want a failure", got, err)
	}
	if mustRead(t, path) != "v1" {
		t.Fatal("a failed replacement changed the sidecar")
	}
	if n := names(t, dir); len(n) != 1 {
		t.Fatalf("folder holds %v after the failure, want only the sidecar", n)
	}
}

func TestWriteSidecarDoesNothingOnceTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	dir := t.TempDir()
	if got, err := WriteSidecar(ctx, filepath.Join(dir, "a.jpg.xmp"), []byte("v1"), nil); err == nil || got != SidecarFailed {
		t.Fatalf("WriteSidecar = %v, %v; want the context's error", got, err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("folder holds %v, want nothing", n)
	}
}

func TestWriteSidecarChecksTheFileAgainJustBeforeReplacingIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMG_1.jpg.xmp")
	if _, err := WriteSidecar(t.Context(), path, []byte("v1"), nil); err != nil {
		t.Fatal(err)
	}
	// Someone edits the sidecar while the new bytes are being written.
	edit := func() {
		if err := os.WriteFile(path, []byte("edited meanwhile"), 0o600); err != nil {
			t.Error(err)
		}
	}
	got, err := writeSidecar(t.Context(), path, []byte("v2"), SidecarHash([]byte("v1")), true, edit)
	if err != nil || got != SidecarEdited {
		t.Fatalf("writeSidecar = %v, %v; want edited", got, err)
	}
	if mustRead(t, path) != "edited meanwhile" {
		t.Fatal("an edit made during the write was overwritten")
	}
	if n := names(t, dir); len(n) != 1 {
		t.Fatalf("folder holds %v, want only the sidecar (no temporary file)", n)
	}
}

func TestReviseSidecarNeverCreatesOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMG_1.jpg.xmp")
	if got, err := ReviseSidecar(t.Context(), path, []byte("v1"), SidecarHash([]byte("old"))); err != nil || got != SidecarGone {
		t.Fatalf("ReviseSidecar of a missing file = %v, %v; want gone", got, err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("folder holds %v, want nothing", n)
	}
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReviseSidecar(t.Context(), path, []byte("v1"), SidecarHash([]byte("old"))); err != nil || got != SidecarReplaced || mustRead(t, path) != "v1" {
		t.Fatalf("ReviseSidecar of its own file = %v, %v, %q; want replaced", got, err, mustRead(t, path))
	}
}

func TestOnlyLumensTemporaryNamesAreSidecarTemps(t *testing.T) {
	for path, want := range map[string]bool{
		`C:\p\IMG_1.jpg.xmp.0a1b2c3d4e5f.lumen-tmp`: true,
		`C:\p\IMG_1.JPG.XMP.0A1B.LUMEN-TMP`:         true,
		`C:\p\IMG_1.jpg.xmp`:                        false,
		`C:\p\IMG_1.jpg`:                            false,
		`C:\p\lumen-tmp`:                            false,
	} {
		if got := IsSidecarTemp(path); got != want {
			t.Errorf("IsSidecarTemp(%q) = %v, want %v", path, got, want)
		}
	}
}
