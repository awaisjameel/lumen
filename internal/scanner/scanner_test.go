package scanner

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/store"
)

func TestUnreadablePathsAreNotDeletionCandidates(t *testing.T) {
	known := map[string]store.FileStat{
		`c:\photos\private\a.jpg`:      {ID: 1},
		`c:\photos\private\trip\b.jpg`: {ID: 2},
		`c:\photos\private2\c.jpg`:     {ID: 3},
		`c:\photos\locked.jpg`:         {ID: 4},
	}
	preserveKnown(known, `C:\Photos\Private`)
	preserveKnown(known, `C:\Photos\Locked.jpg`)
	if len(known) != 1 || known[`c:\photos\private2\c.jpg`].ID != 3 {
		t.Fatalf("wrong deletion candidates after unreadable paths: %+v", known)
	}
}

func TestFingerprintIdentifiesContent(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	sum := func(p string) []byte {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		h, err := Fingerprint(p, fi.Size())
		if err != nil {
			t.Fatal(err)
		}
		return h
	}

	a := sum(write("a.jpg", []byte("same bytes")))
	b := sum(write("renamed.jpg", []byte("same bytes")))
	c := sum(write("c.jpg", []byte("other bytes")))
	if len(a) != 16 || !bytes.Equal(a, b) || bytes.Equal(a, c) {
		t.Fatalf("hashes: a=%x b=%x c=%x", a, b, c)
	}

	// Past the full-hash limit only three windows are read, so a change inside
	// one of them must still change the hash.
	big := make([]byte, fullHashLimit+1)
	h1 := sum(write("big1.mp4", big))
	big[len(big)/2] = 1
	h2 := sum(write("big2.mp4", big))
	if bytes.Equal(h1, h2) {
		t.Fatal("sampled hash missed a change in the middle window")
	}
}
