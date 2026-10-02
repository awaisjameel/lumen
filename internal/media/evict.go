package media

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/core"
)

// sizeLedger is the running total of the bytes the cache's thumbnails and
// sprites take (video frames have their own sweep) and the cap it is held to.
//
// The total is approximate by design: Write adds what it wrote less what it
// replaced, eviction subtracts what it removed, and Measure walks the folders
// to correct it. A file the user deletes by hand goes unnoticed until the
// next walk, which is why it runs at every start.
type sizeLedger struct {
	bytes atomic.Int64  // the total; read without a lock, changed only through add and set
	limit atomic.Int64  // the cap in bytes; 0 means none
	wake  chan struct{} // nudged (one slot) when the total passes the cap or the cap changes

	mu    sync.Mutex // guards drift, and keeps add and set from interleaving
	drift int64      // net change by add since the current walk began
}

// add changes the total by delta, which may be negative.
func (l *sizeLedger) add(delta int64) {
	l.mu.Lock()
	l.bytes.Add(delta)
	l.drift += delta
	l.mu.Unlock()
}

// set replaces the total with a walk's count, plus what Write and eviction
// changed while it walked (a file made during the walk may be counted twice;
// the next walk puts that right).
func (l *sizeLedger) set(walked int64) {
	l.mu.Lock()
	l.bytes.Store(walked + l.drift)
	l.mu.Unlock()
}

// beginWalk starts counting the changes a walk will not see.
func (l *sizeLedger) beginWalk() {
	l.mu.Lock()
	l.drift = 0
	l.mu.Unlock()
}

// wakeIfOver nudges the eviction loop if the total is past the cap.
func (l *sizeLedger) wakeIfOver() {
	if limit := l.limit.Load(); limit > 0 && l.bytes.Load() > limit {
		l.nudge()
	}
}

func (l *sizeLedger) nudge() {
	select {
	case l.wake <- struct{}{}:
	default: // already nudged
	}
}

// Bytes is the size of the cached thumbnails and sprites on disk, as far as
// the cache knows: exact once Measure has finished, approximate before.
func (c *Cache) Bytes() int64 { return max(0, c.size.bytes.Load()) }

// Cap is the size the cache is held to, in bytes; 0 until SetCap.
func (c *Cache) Cap() int64 { return c.size.limit.Load() }

// SetCap changes the cap, which takes effect at once: it wakes whoever waits
// on Wake. Zero or less removes the cap.
func (c *Cache) SetCap(bytes int64) {
	c.size.limit.Store(max(0, bytes))
	c.size.nudge()
}

// Wake is signalled when the cache grows past its cap and when the cap
// changes, so eviction need not poll.
func (c *Cache) Wake() <-chan struct{} { return c.size.wake }

// Measure walks the thumbnail and sprite folders and sets the size total from
// what it finds. A big library has hundreds of thousands of files, so it
// stops as soon as ctx ends (shutdown waits for it) and leaves the total as
// it was. Call it once at startup, alongside everything else.
func (c *Cache) Measure(ctx context.Context) error {
	c.size.beginWalk()
	var walked int64
	for _, dir := range []string{fmt.Sprint(core.ThumbSmall), fmt.Sprint(core.ThumbLarge), "sprite"} {
		err := filepath.WalkDir(filepath.Join(c.Dir, dir), func(_ string, d fs.DirEntry, err error) error {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return nil // removed under us, or never made
			case err != nil:
				return err
			case d.IsDir():
				return nil
			}
			info, err := d.Info()
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return nil
			case err != nil:
				return err
			}
			walked += info.Size()
			return nil
		})
		if err != nil {
			return fmt.Errorf("measure thumbnail cache: %w", err)
		}
	}
	c.size.set(walked)
	c.size.wakeIfOver()
	return nil
}

// Evict removes the thumbnails and sprite of each asset in ids, one asset at
// a time under its write lock, so it never races a rebuild writing them. It
// returns the bytes freed. Assets shown since the database last heard of it
// are left alone (they are the opposite of what the cap drops). A file that
// can't be removed doesn't stop the rest; the errors come back together. The
// files are Lumen's own cache, not the user's, so a plain remove is right.
func (c *Cache) Evict(ctx context.Context, ids []int64) (int64, error) {
	var freed int64
	var errs []error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return freed, err
		}
		if c.views.pending(id) {
			continue
		}
		errs = append(errs, c.lockedWrite(id, func() error {
			var errs []error
			for _, path := range []string{c.Path(id, core.ThumbSmall), c.Path(id, core.ThumbLarge), c.SpritePath(id)} {
				size, err := removeFile(path)
				c.size.add(-size)
				freed += size
				errs = append(errs, err)
			}
			return errors.Join(errs...)
		}))
	}
	return freed, errors.Join(errs...)
}

// removeFile removes a cached file and reports the bytes it freed. A file
// that is not there frees nothing and is no error.
func removeFile(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	return fi.Size(), nil
}

// viewLog remembers which assets' thumbnails were shown, and when, until the
// database has them. Showing happens thousands of times a second while the
// grid scrolls, so it costs a map write under a mutex; a flusher saves the lot
// in one statement every half minute. Eviction consults it too, because the
// database lags the screen by up to that long.
type viewLog struct {
	mu   sync.Mutex // guards seen
	seen map[int64]int64
}

// Viewed records that id's thumbnail or sprite was just shown.
func (c *Cache) Viewed(id int64) {
	c.views.mu.Lock()
	if c.views.seen == nil {
		c.views.seen = map[int64]int64{}
	}
	c.views.seen[id] = time.Now().Unix()
	c.views.mu.Unlock()
}

// PendingViews copies the views the database has not been given (asset id to
// unix seconds). Pass the same copy to ViewsSaved once it has been written.
func (c *Cache) PendingViews() map[int64]int64 {
	c.views.mu.Lock()
	defer c.views.mu.Unlock()
	out := make(map[int64]int64, len(c.views.seen))
	for id, at := range c.views.seen {
		out[id] = at
	}
	return out
}

// ViewsSaved forgets views the database now holds. One shown again after the
// copy was taken is kept.
func (c *Cache) ViewsSaved(saved map[int64]int64) {
	c.views.mu.Lock()
	defer c.views.mu.Unlock()
	for id, at := range saved {
		if c.views.seen[id] <= at {
			delete(c.views.seen, id)
		}
	}
}

// ViewSink is where views are saved: the library's database.
type ViewSink interface {
	TouchThumbs(ctx context.Context, views map[int64]int64) error
}

// FlushViews saves the pending views to sink in one call, and forgets them once
// it succeeds. On an error they stay for the next try. It is a no-op when
// nothing was shown.
func (c *Cache) FlushViews(ctx context.Context, sink ViewSink) error {
	views := c.PendingViews()
	if len(views) == 0 {
		return nil
	}
	if err := sink.TouchThumbs(ctx, views); err != nil {
		return err
	}
	c.ViewsSaved(views)
	return nil
}

func (v *viewLog) pending(id int64) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, ok := v.seen[id]
	return ok
}
