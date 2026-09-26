package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lumen/internal/core"
)

// Original is the file an asset's thumbnails are made from.
type Original struct {
	Path        string
	Kind        core.Kind
	Orientation int
}

// Cache is the on-disk thumbnail cache. Thumbnails can disappear (the user
// clears the folder; later, the size cap evicts them), so readers call
// Ensure, which rebuilds a missing pair from the original.
type Cache struct {
	Dir     string
	Decoder Decoder

	rebuilding sync.Map       // asset id -> *sync.Mutex while its thumbnails are rebuilt
	writing    [64]sync.Mutex // striped by asset id: one writer per asset at a time
}

// NewCache returns the cache in dir, rebuilding with d.
func NewCache(dir string, d Decoder) *Cache { return &Cache{Dir: dir, Decoder: d} }

// Path is where a thumbnail lives, fanned out over 256 sub-folders so no
// folder holds hundreds of thousands of files.
func (c *Cache) Path(id int64, size int) string {
	return filepath.Join(c.Dir, fmt.Sprint(size), fmt.Sprintf("%02x", id%256), fmt.Sprintf("%d.jpg", id))
}

// Write saves both thumbnails for an asset, atomically per file: readers
// see the old file or the new one, never half of one. Writers of one asset
// (the indexer, a rebuild) take turns: Windows refuses two renames onto one
// file at once.
func (c *Cache) Write(id int64, t Thumbs) error {
	mu := &c.writing[uint64(id)%uint64(len(c.writing))]
	mu.Lock()
	defer mu.Unlock()
	for _, v := range []struct {
		size int
		data []byte
	}{{core.ThumbSmall, t.Small}, {core.ThumbLarge, t.Large}} {
		if err := writeAtomic(c.Path(id, v.size), v.data); err != nil {
			return err
		}
	}
	return nil
}

// writeAtomic writes data to a temporary file beside path, then renames it
// into place.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = renameRetry(f.Name(), path)
	}
	if werr != nil {
		os.Remove(f.Name())
	}
	return werr
}

// renameRetry renames, retrying briefly: on Windows a file that is open for
// reading (the WebView fetching that thumbnail) can't be replaced until it
// is closed, which takes milliseconds.
func renameRetry(from, to string) error {
	var err error
	for try := range 10 {
		if err = os.Rename(from, to); err == nil || !errors.Is(err, fs.ErrPermission) {
			return err
		}
		time.Sleep(time.Duration(try+1) * 10 * time.Millisecond)
	}
	return err
}

// Ensure makes sure the thumbnail of the given size exists, rebuilding the
// pair from the original if it is gone. Concurrent calls for one asset
// rebuild it once.
func (c *Cache) Ensure(ctx context.Context, id int64, size int, orig Original) error {
	path := c.Path(id, size)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	v, _ := c.rebuilding.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer func() {
		c.rebuilding.Delete(id) // later callers find the file (or retry afresh)
		mu.Unlock()
	}()
	if _, err := os.Stat(path); err == nil {
		return nil // rebuilt by another caller while we waited
	}
	t, err := c.Decoder.Thumbnail(ctx, orig.Path, orig.Kind, orig.Orientation)
	if err != nil {
		return err
	}
	return c.Write(id, t)
}
