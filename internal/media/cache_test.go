package media

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
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
