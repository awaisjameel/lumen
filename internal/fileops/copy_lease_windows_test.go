package fileops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFolderLeasePreventsAnAncestorRenameUntilPublicationFinishes(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	dir := filepath.Join(parent, "copies")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	release, err := LeaseCopyFolders(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	moved := filepath.Join(root, "later")
	if err := os.Rename(parent, moved); err == nil {
		t.Fatal("a leased ancestor could be renamed")
	}
	if err := os.WriteFile(filepath.Join(dir, "new-copy.png"), []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	release()
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
}

// The folder a copy is published into is as exposed as its ancestors: moved
// aside, a junction of the same name could take its place.
func TestCopyFolderLeasePreventsRenamingOrRemovingTheFolderItself(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "copies")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	release, err := LeaseCopyFolders(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	moved := filepath.Join(root, "later")
	if err := os.Rename(dir, moved); err == nil {
		t.Fatal("the leased folder could be renamed")
	}
	if err := os.Remove(dir); err == nil {
		t.Fatal("the leased folder could be removed")
	}
	second, err := LeaseCopyFolders(t.Context(), dir)
	if err != nil {
		t.Fatalf("a second lease of the same folder: %v", err)
	}
	second()
	release()
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
}
