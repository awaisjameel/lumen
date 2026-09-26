package media

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
	Dir    string
	Thumbs Thumbnailer

	rebuilding sync.Map // asset id -> *sync.Mutex while its thumbnails are rebuilt
}

// NewCache returns the cache in dir, rebuilding with t.
func NewCache(dir string, t Thumbnailer) *Cache { return &Cache{Dir: dir, Thumbs: t} }

// Path is where a thumbnail lives, fanned out over 256 sub-folders so no
// folder holds hundreds of thousands of files.
func (c *Cache) Path(id int64, size int) string {
	return filepath.Join(c.Dir, fmt.Sprint(size), fmt.Sprintf("%02x", id%256), fmt.Sprintf("%d.jpg", id))
}

// Write saves both thumbnails for an asset, atomically per file.
func (c *Cache) Write(id int64, t Thumbs) error {
	for _, v := range []struct {
		size int
		data []byte
	}{{core.ThumbSmall, t.Small}, {core.ThumbLarge, t.Large}} {
		p := c.Path(id, v.size)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		tmp := p + ".tmp"
		if err := os.WriteFile(tmp, v.data, 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, p); err != nil {
			return err
		}
	}
	return nil
}

// Ensure makes sure the thumbnail of the given size exists, rebuilding the
// pair from the original if it is gone. Concurrent calls for one asset
// rebuild it once.
func (c *Cache) Ensure(id int64, size int, orig Original) error {
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
	t, err := c.Thumbs.Thumbnail(orig.Path, orig.Kind, orig.Orientation)
	if err != nil {
		return err
	}
	return c.Write(id, t)
}
