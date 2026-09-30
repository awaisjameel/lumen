package media

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"lumen/internal/core"
)

// The indexer and a thumbnail rebuild can write the same asset at once; each
// file must end up whole (one writer's bytes), with no temporary files left.
func TestCacheWritesAreAtomic(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			data := bytes.Repeat([]byte{byte(i)}, 64<<10)
			if err := c.Write(7, Thumbs{Small: data, Large: data}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, err := os.ReadFile(c.Path(7, 256))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 64<<10 || !bytes.Equal(got, bytes.Repeat(got[:1], len(got))) {
		t.Errorf("thumbnail is a mix of writers (%d bytes)", len(got))
	}
	left, _ := filepath.Glob(filepath.Join(c.tmpDir(), "*"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// A crash can leave half-written files and frames of older versions; Sweep
// clears them, and an empty cached file counts as missing.
func TestSweepAndEmptyFiles(t *testing.T) {
	d := &countingDecoder{}
	c := NewCache(t.TempDir(), d)
	for _, p := range []string{filepath.Join(c.tmpDir(), "1.jpg.123.tmp"), filepath.Join(c.Dir, "frame-old", "00", "1", "0.jpg")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(c.SweepTemp(), c.SweepFrames()); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(c.Dir, "*")); len(left) != 0 {
		t.Errorf("Sweep left %v", left)
	}

	thumb := c.Path(1, core.ThumbSmall)
	if err := os.MkdirAll(filepath.Dir(thumb), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(thumb, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Ensure(t.Context(), 1, core.ThumbSmall, Original{Path: "photo.jpg", Kind: core.KindPhoto}); err != nil || d.calls != 1 {
		t.Fatalf("Ensure over an empty file: %v, %d decodes; want it rebuilt", err, d.calls)
	}
}

type countingDecoder struct {
	Decoder
	calls int
}

func (d *countingDecoder) Thumbnail(context.Context, string, core.Kind, int) (Thumbs, error) {
	d.calls++
	return Thumbs{Small: []byte("small"), Large: []byte("large")}, nil
}

func TestCancelledRebuildDoesNotStartDecoder(t *testing.T) {
	d := &countingDecoder{}
	c := NewCache(t.TempDir(), d)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	orig := Original{Path: "photo.jpg", Kind: core.KindPhoto, Orientation: 1}
	if err := c.Ensure(ctx, 1, core.ThumbSmall, orig); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled rebuild: %v", err)
	}
	if d.calls != 0 {
		t.Fatalf("cancelled decoder calls = %d", d.calls)
	}
	if err := c.Ensure(t.Context(), 1, core.ThumbSmall, orig); err != nil {
		t.Fatal(err)
	}
	if d.calls != 1 {
		t.Fatalf("retry decoder calls = %d", d.calls)
	}
}
