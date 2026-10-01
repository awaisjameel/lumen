package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// A folder hidden in Explorer is left out for its attribute (which the user
// can change back), the recycle bin for its name.
func TestExclusionSaysWhyAndWhere(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, "Private")
	photo := filepath.Join(hidden, "2024", "a.jpg")
	if err := os.MkdirAll(filepath.Dir(photo), 0o750); err != nil {
		t.Fatal(err)
	}
	if at, _, excluded := Exclusion(root, photo, nil); excluded {
		t.Fatalf("visible folder excluded at %s", at)
	}
	p, err := windows.UTF16PtrFromString(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(p, windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
	if at, byAttribute, excluded := Exclusion(root, photo, nil); !excluded || !byAttribute || at != hidden {
		t.Errorf("hidden folder: at %q, by attribute %v, excluded %v", at, byAttribute, excluded)
	}
	bin := filepath.Join(root, "$RECYCLE.BIN", "x.jpg")
	if at, byAttribute, excluded := Exclusion(root, bin, nil); !excluded || byAttribute || at != filepath.Dir(bin) {
		t.Errorf("recycle bin: at %q, by attribute %v, excluded %v", at, byAttribute, excluded)
	}
}

func TestExclusionRecognisesDotDotPrefixedFoldersInsideSource(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "..Private")
	if at, byAttribute, excluded := Exclusion(root, filepath.Join(folder, "a.jpg"), nil); !excluded || byAttribute || at != folder {
		t.Fatalf("dot folder: at %q, by attribute %v, excluded %v", at, byAttribute, excluded)
	}
}
