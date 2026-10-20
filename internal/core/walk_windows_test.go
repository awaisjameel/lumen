//go:build windows

package core

import (
	"os"
	"path/filepath"
	"testing"
)

// os.Open does not share deletion, so a folder Lumen was listing could not be
// deleted or renamed in Explorer meanwhile.
func TestAFolderOpenForListingCanStillBeDeletedOrRenamed(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "photos")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	f, err := OpenDirShared(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := os.Rename(dir, filepath.Join(parent, "renamed")); err != nil {
		t.Fatalf("rename while listed: %v", err)
	}
	if err := os.Remove(filepath.Join(parent, "renamed")); err != nil {
		t.Fatalf("delete while listed: %v", err)
	}
}

func TestOpenDirSharedListsTheFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.jpg"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadDirShared(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "a.jpg" {
		t.Fatalf("ReadDirShared = %v, %v", entries, err)
	}
}
