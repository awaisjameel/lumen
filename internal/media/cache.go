package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lumen/internal/core"
	"lumen/internal/media/vips"
)

// Original is the file an asset's thumbnails are made from.
type Original struct {
	Path        string
	Kind        core.Kind
	Orientation int
}

// Cache is the on-disk thumbnail cache. Thumbnails can disappear (the user
// clears the folder; the size cap evicts them), so readers call Ensure, which
// rebuilds a missing pair from the original.
//
// It also keeps what the size cap needs (see evict.go): a running total of
// the bytes the thumbnails and sprites take, and which assets were shown
// since the database last heard of it.
type Cache struct {
	Dir     string
	Decoder Decoder

	rebuilding stripes        // one rebuild per asset at a time; waiting for it can be cancelled
	writing    [64]sync.Mutex // one writer per asset at a time: Windows refuses two renames onto one file

	size  sizeLedger
	views viewLog
}

// NewCache returns the cache in dir, rebuilding with d.
func NewCache(dir string, d Decoder) *Cache {
	c := &Cache{Dir: dir, Decoder: d}
	for i := range c.rebuilding {
		c.rebuilding[i] = make(chan struct{}, 1)
	}
	c.size.wake = make(chan struct{}, 1)
	return c
}

// stripes are locks shared by asset id, each a one-slot channel so taking
// one can be abandoned when its context ends.
type stripes [64]chan struct{}

// lock takes id's stripe, unless ctx ends first. On an error the stripe is
// not held.
func (s *stripes) lock(ctx context.Context, id int64) error {
	select {
	case s[uint64(id)%uint64(len(s))] <- struct{}{}:
		if err := ctx.Err(); err != nil {
			s.unlock(id) // taken as the request went away: don't work for it
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *stripes) unlock(id int64) { <-s[uint64(id)%uint64(len(s))] }

// lockedWrite takes id's writing lock around fn.
func (c *Cache) lockedWrite(id int64, fn func() error) error {
	mu := &c.writing[uint64(id)%uint64(len(c.writing))]
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

// Path is where a thumbnail lives, fanned out over 256 sub-folders so no
// folder holds hundreds of thousands of files.
func (c *Cache) Path(id int64, size int) string {
	return filepath.Join(c.Dir, fmt.Sprint(size), fmt.Sprintf("%02x", id%256), fmt.Sprintf("%d.jpg", id))
}

// SpritePath is the hover preview beside an asset's thumbnails.
func (c *Cache) SpritePath(id int64) string {
	return filepath.Join(c.Dir, "sprite", fmt.Sprintf("%02x", id%256), fmt.Sprintf("%d.jpg", id))
}

// framesDir holds decoded video frames, per version of the code that makes
// them, so a new version never serves an old frame.
var framesDir = "frame-" + strings.NewReplacer("+", "_", ".", "_").Replace(VideoThumbnailVersion)

// FramePath is a decoded video frame used for face crops.
func (c *Cache) FramePath(id, at int64) string {
	return filepath.Join(c.Dir, framesDir, fmt.Sprintf("%02x", id%256), fmt.Sprint(id), fmt.Sprintf("%d.jpg", at))
}

// tmpDir is where files are written before they are renamed into place.
func (c *Cache) tmpDir() string { return filepath.Join(c.Dir, "tmp") }

// SweepTemp removes temporary files a crash cut short. Call it once at
// startup, before anything writes to the cache: it removes the folder
// writes go through.
func (c *Cache) SweepTemp() error { return os.RemoveAll(c.tmpDir()) }

// SweepFrames removes video frames made by older versions of the code.
// Call it once at startup; it may run alongside everything else. A version
// bump leaves one old folder per video, so it stops between them when ctx
// ends: shutdown waits for it.
func (c *Cache) SweepFrames(ctx context.Context) error {
	var errs []error
	entries, err := os.ReadDir(c.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if name := entry.Name(); strings.HasPrefix(name, "frame") && name != framesDir {
			if err := removeTree(ctx, filepath.Join(c.Dir, name)); err != nil {
				if ctx.Err() != nil {
					return err
				}
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// removeTree is os.RemoveAll of dir, one child at a time, stopping when ctx
// ends; the rest is removed by the next sweep.
func removeTree(ctx context.Context, dir string) error {
	children, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return os.RemoveAll(dir) // not a folder, or unreadable: one attempt
	}
	var errs []error
	for _, child := range children {
		if err := ctx.Err(); err != nil {
			return err
		}
		errs = append(errs, os.RemoveAll(filepath.Join(dir, child.Name())))
	}
	errs = append(errs, os.RemoveAll(dir))
	return errors.Join(errs...)
}

// EnsureFrame makes an upright JPEG of a video frame for the people UI.
func (c *Cache) EnsureFrame(ctx context.Context, id, at int64, path string) (string, error) {
	dst := c.FramePath(id, at)
	if ok, err := present(dst); ok || err != nil {
		return dst, err
	}
	// Decode under the rebuild lock (so one request decodes it), not the
	// write lock, which the indexer's thumbnail writes wait on.
	if err := c.rebuilding.lock(ctx, id); err != nil {
		return "", err
	}
	defer c.rebuilding.unlock(id)
	if ok, err := present(dst); ok || err != nil {
		return dst, err
	}
	im, err := c.Decoder.FrameAt(ctx, path, at, core.ThumbLarge)
	if err != nil {
		return "", err
	}
	jpeg, err := vips.JPEG(im.Pix, im.W, im.H, jpegQuality)
	if err != nil {
		return "", err
	}
	return dst, c.lockedWrite(id, func() error { return c.writeAtomic(dst, jpeg) })
}

// Write saves both thumbnails for an asset, atomically per file: readers
// see the old file or the new one, never half of one. Writers of one asset
// (the indexer, a rebuild) take turns. The size total grows by what was
// written, less what it replaced.
func (c *Cache) Write(id int64, t Thumbs) error {
	files := []cacheFile{{c.Path(id, core.ThumbSmall), t.Small}, {c.Path(id, core.ThumbLarge), t.Large}}
	if len(t.Sprite) > 0 { // photos have none
		files = append(files, cacheFile{c.SpritePath(id), t.Sprite})
	}
	err := c.lockedWrite(id, func() error {
		for _, f := range files {
			if err := c.writeCounted(f.path, f.data); err != nil {
				return err
			}
		}
		return nil
	})
	c.size.wakeIfOver()
	return err
}

// cacheFile is one file Write puts in the cache.
type cacheFile struct {
	path string
	data []byte
}

// writeCounted is writeAtomic, keeping the size total: the file's new size
// less the old one it replaces. Call it with the asset's write lock held, so
// nothing else changes the file between the two sizes.
func (c *Cache) writeCounted(path string, data []byte) error {
	var before int64
	if fi, err := os.Stat(path); err == nil {
		before = fi.Size()
	}
	if err := c.writeAtomic(path, data); err != nil {
		return err
	}
	c.size.add(int64(len(data)) - before)
	return nil
}

// writeAtomic writes data to a temporary file, then renames it into place.
func (c *Cache) writeAtomic(path string, data []byte) error {
	for _, dir := range []string{c.tmpDir(), filepath.Dir(path)} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(c.tmpDir(), filepath.Base(path)+".*.tmp")
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
		_ = os.Remove(f.Name()) // best effort: the write's failure is the one to report
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

// present reports whether a cached file is there and whole. An empty one
// (power lost before the data reached the disk) counts as missing.
func present(path string) (bool, error) {
	fi, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	}
	return fi.Size() > 0, nil
}

// Ensure makes sure the thumbnail of the given size exists, rebuilding the
// pair from the original if it is gone. Concurrent calls for one asset
// rebuild it once.
func (c *Cache) Ensure(ctx context.Context, id int64, size int, orig Original) error {
	return c.ensure(ctx, id, c.Path(id, size), orig)
}

// EnsureSprite rebuilds a video sprite if it is missing while its poster
// thumbnails are there.
func (c *Cache) EnsureSprite(ctx context.Context, id int64, orig Original) error {
	return c.ensure(ctx, id, c.SpritePath(id), orig)
}

func (c *Cache) ensure(ctx context.Context, id int64, path string, orig Original) error {
	if ok, err := present(path); ok || err != nil {
		return err
	}
	if err := c.rebuilding.lock(ctx, id); err != nil {
		return err
	}
	defer c.rebuilding.unlock(id)
	if ok, err := present(path); ok || err != nil {
		return err // rebuilt by another caller while we waited
	}
	t, err := c.Decoder.Thumbnail(ctx, orig.Path, orig.Kind, orig.Orientation)
	if err != nil {
		return err
	}
	if err := c.Write(id, t); err != nil {
		return err
	}
	// A rebuilt asset may carry the "evicted" marker in the database, which
	// hides it from the cap. Recording a view lets the next flush clear it,
	// whichever stage or request made the files.
	c.Viewed(id)
	return nil
}
