package index

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"lumen/internal/core"
)

// The thumbnail cache holds at most the size the user chose. Past it, the
// least recently viewed thumbnails go until the cache is at evictTarget of the
// cap, leaving room to grow before the next pass. Nothing is lost: the media
// handler rebuilds a missing thumbnail from the original when it is asked for.

// evictTarget is the share of the cap a pass trims down to, in percent.
const evictTarget = 90

// thumbCacheTiming is how the eviction loop paces itself.
type thumbCacheTiming struct {
	cooldown time.Duration // the least between two passes, so indexing's writes don't start one each
	poll     time.Duration // the fallback wake, in case a nudge was missed
	retry    time.Duration // the wait after the database failed
	batch    int           // candidates fetched at a time
}

var defaultThumbCacheTiming = thumbCacheTiming{cooldown: 10 * time.Second, poll: 10 * time.Minute, retry: retryDelay, batch: 200}

// SetThumbCacheMB tells the eviction loop the cap changed (the setting is
// stored already), so it applies without a restart.
func (ix *Indexer) SetThumbCacheMB(mb int) {
	if ix == nil || ix.cache == nil {
		return
	}
	ix.cache.SetCap(int64(mb) << 20)
}

// thumbCacheLoop measures the cache once, then keeps it under its cap until
// ctx ends: a pass runs when the cache passes the cap or the cap changes, and
// every so often in case a wake was missed.
func (ix *Indexer) thumbCacheLoop(ctx context.Context) {
	if ix.cache == nil {
		return // no thumbnails kept
	}
	ix.runThumbCache(ctx, defaultThumbCacheTiming)
}

func (ix *Indexer) runThumbCache(ctx context.Context, tm thumbCacheTiming) {
	mb, err := ix.store.ThumbCacheMB(ctx)
	if err != nil && ctx.Err() == nil {
		slog.Error("read thumbnail cache cap", "err", err)
	}
	ix.cache.SetCap(int64(mb) << 20)
	if err := ix.cache.Measure(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("measure thumbnail cache", "err", err)
	}
	for ctx.Err() == nil {
		if err := ix.evictThumbs(ctx, tm.batch); err != nil && ctx.Err() == nil {
			slog.Error("trim thumbnail cache", "err", err)
			sleep(ctx, tm.retry)
			continue
		}
		sleep(ctx, tm.cooldown)
		select {
		case <-ix.cache.Wake():
		case <-time.After(tm.poll):
		case <-ctx.Done():
		}
	}
}

// evictThumbs trims the cache to evictTarget of its cap, if it is over the
// cap. It waits while indexing is paused, as the thumbnail stage does (it is
// not AI work, so battery power doesn't hold it). It stops early, without an
// error, when nothing more may be evicted: every candidate is still wanted by
// an AI stage.
func (ix *Indexer) evictThumbs(ctx context.Context, batch int) error {
	if !ix.overThumbCap(100) {
		return nil
	}
	// Views reach the database every half minute: bring it up to date so the
	// order is the user's, not 30 seconds stale. Anything shown from here on
	// (at or after began) is kept, even if a candidate.
	began := time.Now().Unix()
	if err := ix.cache.FlushViews(ctx, ix.store); err != nil {
		return fmt.Errorf("save thumbnail views: %w", err)
	}
	start, freedTotal := ix.cache.Bytes(), int64(0)
	for ix.overThumbCap(evictTarget) {
		if !ix.waitTurn(ctx, core.StageThumbnail, nil) {
			return nil
		}
		ids, err := ix.store.ThumbEvictionCandidates(ctx, batch)
		if err != nil {
			return fmt.Errorf("list thumbnails to evict: %w", err)
		}
		if len(ids) == 0 {
			break // everything left is still needed
		}
		// One asset at a time, so the pass stops at the target, not a batch past it.
		var done []int64
		var freed int64
		for _, id := range ids {
			if !ix.overThumbCap(evictTarget) {
				break
			}
			n, err := ix.cache.Evict(ctx, []int64{id})
			if cerr := ctx.Err(); cerr != nil {
				return cerr // shutting down: the caller expects that
			}
			if err != nil {
				slog.Warn("evict thumbnail", "asset", id, "err", err)
			}
			freed += n
			done = append(done, id)
		}
		freedTotal += freed
		marked, err := ix.store.MarkThumbsEvicted(ctx, done, began)
		if err != nil {
			return fmt.Errorf("record evicted thumbnails: %w", err)
		}
		if marked == 0 && freed == 0 {
			break // each was shown again meanwhile: nothing to gain by asking again
		}
	}
	if freedTotal > 0 {
		slog.Info("thumbnail cache trimmed", "freed", freedTotal, "from", start, "to", ix.cache.Bytes(), "cap", ix.cache.Cap())
	}
	return nil
}

// overThumbCap reports whether the cache holds more than percent of its cap.
// With no cap set it never does.
func (ix *Indexer) overThumbCap(percent int64) bool {
	limit := ix.cache.Cap()
	return limit > 0 && ix.cache.Bytes() > limit*percent/100
}
