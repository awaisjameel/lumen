package scanner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
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

func TestFingerprintStopsReadingAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pix := bytes.NewReader([]byte("unchanged"))
	if _, err := io.ReadAll(hashReader{ctx: ctx, r: pix}); !errors.Is(err, context.Canceled) || pix.Len() != 9 {
		t.Fatalf("cancelled hash read: %v, %d bytes remain", err, pix.Len())
	}
	if _, err := Fingerprint(ctx, filepath.Join(t.TempDir(), "missing.jpg"), 9); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fingerprint opened its file: %v", err)
	}
}

func TestCancelledScanKeepsUnvisitedFiles(t *testing.T) {
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
	for i := range 600 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.jpg", i)), []byte("same bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	s := Scanner{Store: st, HashWorkers: 2}
	if res, err := s.Scan(t.Context(), src, "", Options{}); err != nil || res.Seen != 600 {
		t.Fatalf("initial scan: %+v, %v", res, err)
	}
	for i := range 600 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d.jpg", i)), []byte("changed bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.OnProgress = func(int) { cancel() }
	if _, err := s.Scan(ctx, src, "", Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted scan: %v", err)
	}
	known, err := st.KnownFiles(t.Context(), src.ID, "")
	if err != nil || len(known) != 600 {
		t.Fatalf("files after interrupted scan: %d, %v", len(known), err)
	}
	s.OnProgress = nil
	if res, err := s.Scan(t.Context(), src, "", Options{}); err != nil || res.Seen != 600 {
		t.Fatalf("scan after cancellation: %+v, %v", res, err)
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
		h, err := Fingerprint(t.Context(), p, fi.Size())
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

func TestTypeScriptModulesAreNotVideos(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	packets := func(size, sync int) []byte {
		b := make([]byte, 3*size)
		for i := range 3 {
			b[i*size+sync] = 0x47
		}
		return b
	}
	root := t.TempDir()
	files := map[string][]byte{
		"clip.mts":          packets(188, 0), // MPEG-TS
		"camera.MTS":        packets(192, 4), // AVCHD: a time code before each packet
		"film.m2ts":         packets(192, 4),
		"recording.ts":      packets(188, 0), // a broadcast recording
		"main.ts":           []byte("export const answer: number = 42;\n"),
		"component.d.ts":    bytes.Repeat([]byte("declare const x: number;\n"), 100),
		"film.mp4":          []byte("not sniffed"),
		"index.d.mts":       []byte("export declare function f(): void;\n"),
		"types.mts":         bytes.Repeat([]byte("export {};\n"), 100),
		"truncated.m2ts":    packets(188, 0)[:200],
		"not-a-video.jpg.x": []byte("ignored"),
	}
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(root, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	// An older scan took a module for a video: the next one forgets it.
	if _, err := st.Ingest(t.Context(), []store.IngestItem{{SourceID: src.ID, Path: filepath.Join(root, "index.d.mts"),
		Size: 1, Hash: []byte("0123456789abcdef"), Kind: core.KindVideo, Ext: ".mts"}}); err != nil {
		t.Fatal(err)
	}
	s := Scanner{Store: st, HashWorkers: 2}
	res, err := s.Scan(t.Context(), src, "", Options{Videos: true})
	if err != nil || res.Seen != 5 || res.Removed != 1 {
		t.Fatalf("scan: %+v, %v; want the 5 videos seen and the module forgotten", res, err)
	}
}

func TestUnavailableSourceKeepsItsFilesUntilAReadableScanConfirmsDeletion(t *testing.T) {
	for _, mode := range []string{"missing", "file", "disconnected during scan"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			root := filepath.Join(t.TempDir(), "photos")
			if err := os.Mkdir(root, 0o750); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "one.jpg")
			if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			src, err := st.AddSource(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			sc := Scanner{Store: st}
			if _, err := sc.Scan(ctx, src, "", Options{}); err != nil {
				t.Fatal(err)
			}
			disconnect := func() {
				if err := os.RemoveAll(root); err != nil {
					t.Fatal(err)
				}
				if mode == "file" {
					if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "disconnected during scan" {
				sc.OnProgress = func(int) { disconnect() }
			} else {
				disconnect()
			}
			if res, err := sc.Scan(ctx, src, "", Options{}); err == nil || res.Removed != 0 {
				t.Fatalf("unavailable scan: %+v, %v", res, err)
			}
			known, err := st.KnownFiles(ctx, src.ID, "")
			if err != nil || len(known) != 1 {
				t.Fatalf("unavailable root lost files: %v, %v", known, err)
			}
			if mode == "file" {
				if err := os.Remove(root); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(root, 0o750); err != nil {
				t.Fatal(err)
			}
			sc.OnProgress = nil
			if res, err := sc.Scan(ctx, src, "", Options{}); err != nil || res.Removed != 1 {
				t.Fatalf("readable empty source did not reconcile: %+v, %v", res, err)
			}
		})
	}
}
