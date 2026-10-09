package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/store"
)

func TestAnExcludedFormatIsNeitherAddedNorForgotten(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("kept.cr2", "raw sensor data")
	write("photo.jpg", "a jpeg")
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	s := Scanner{Store: st, HashWorkers: 2}
	if res, err := s.Scan(t.Context(), src, "", Options{}); err != nil || res.Seen != 2 || res.NewAssets != 2 {
		t.Fatalf("first scan: %+v, %v", res, err)
	}

	// Camera RAW is turned off. The RAW file the library holds changes on disk
	// and another arrives: neither is read, and the first is not forgotten.
	write("kept.cr2", "raw sensor data, edited since")
	write("new.nef", "another camera")
	write("second.jpg", "another jpeg")
	off := Options{Excluded: []core.Family{core.FamilyRAW}}
	res, err := s.Scan(t.Context(), src, "", off)
	if err != nil || res.Seen != 2 || res.Changed != 1 || res.Removed != 0 {
		t.Fatalf("scan with RAW off: %+v, %v; want only the JPEGs seen and nothing forgotten", res, err)
	}
	known, err := st.KnownFiles(t.Context(), src.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := known[core.Fold(filepath.Join(root, "kept.cr2"))]; !ok {
		t.Errorf("the RAW file the library held was forgotten: %v", known)
	}
	if _, ok := known[core.Fold(filepath.Join(root, "new.nef"))]; ok {
		t.Errorf("a RAW file was added while the format is off: %v", known)
	}

	// A file of the format that really is gone still leaves the library.
	if err := os.Remove(filepath.Join(root, "kept.cr2")); err != nil {
		t.Fatal(err)
	}
	if res, err := s.Scan(t.Context(), src, "", off); err != nil || res.Removed != 1 {
		t.Fatalf("scan after deleting the RAW file: %+v, %v; want it forgotten", res, err)
	}

	// Turned on again, what was left out is found.
	if res, err := s.Scan(t.Context(), src, "", Options{}); err != nil || res.Seen != 3 || res.NewAssets != 1 {
		t.Fatalf("scan with RAW on again: %+v, %v; want the new RAW file added", res, err)
	}
}
