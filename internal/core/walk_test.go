package core

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWalkDirVisitsWhatFilepathWalkDirVisitsInTheSameOrder(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"b", "a/skip/deep", "a/keep", "c/stop"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"z.jpg", "a/1.jpg", "a/skip/2.jpg", "a/keep/3.jpg", "b/4.jpg", "c/5.jpg", "c/stop/6.jpg", "c/x.jpg"} {
		if err := os.WriteFile(filepath.Join(root, file), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	walk := func(w func(string, fs.WalkDirFunc) error) []string {
		var seen []string
		err := w(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			name := filepath.ToSlash(rel)
			if d.IsDir() {
				name += "/"
			}
			seen = append(seen, name)
			switch rel {
			case filepath.FromSlash("a/skip"):
				return fs.SkipDir // a folder: skip what is inside it
			case filepath.FromSlash("c/5.jpg"):
				return fs.SkipDir // a file: skip the rest of its folder
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return seen
	}
	want, got := walk(filepath.WalkDir), walk(WalkDir)
	if !slices.Equal(got, want) {
		t.Fatalf("WalkDir visited\n%v\nfilepath.WalkDir visited\n%v", got, want)
	}
	if slices.Contains(got, "a/skip/2.jpg") || slices.Contains(got, "c/x.jpg") || !slices.Contains(got, "z.jpg") {
		t.Fatalf("SkipDir was not honoured: %v", got)
	}
}

func TestWalkDirReportsAMissingRootToItsCallback(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	var got error
	err := WalkDir(missing, func(path string, d fs.DirEntry, err error) error {
		if path != missing || d != nil {
			t.Errorf("callback(%q, %v)", path, d)
		}
		got = err
		return err
	})
	if !os.IsNotExist(got) || !os.IsNotExist(err) {
		t.Fatalf("callback saw %v and WalkDir returned %v, want not-exist for both", got, err)
	}
}
