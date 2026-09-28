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
	left, _ := filepath.Glob(filepath.Join(filepath.Dir(c.Path(7, 256)), "*.tmp"))
	if len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
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
