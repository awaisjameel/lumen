package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// runStage drains one stage's job queue: fetch a batch (what the user is
// looking at first, then newest first), process it on a worker pool, save the
// results in one transaction, repeat. With nothing to do it sleeps until woken.
// Batches are small so the UI sees results stream in and focus changes apply fast.
func runStage[R any](ctx context.Context, ix *Indexer, stage core.Stage, workers, batch int,
	wake <-chan struct{}, process func(store.JobItem) R, save func(context.Context, []R) error, next chan struct{}) {
	for {
		if !ix.waitIfPaused(ctx) {
			return
		}
		items, err := ix.fetch(ctx, stage, batch)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("fetch jobs", "stage", stage, "err", err)
			sleep(ctx, 5*time.Second)
			continue
		}
		if len(items) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-wake:
			case <-time.After(30 * time.Second):
			}
			continue
		}

		results := make([]R, len(items))
		idx := make(chan int)
		var wg sync.WaitGroup
		for range min(workers, len(items)) {
			wg.Go(func() {
				for i := range idx {
					results[i] = process(items[i])
				}
			})
		}
	feed:
		for i := range items {
			select {
			case idx <- i:
			case <-ctx.Done():
				break feed // on shutdown, only finish the files already started
			}
		}
		close(idx)
		wg.Wait()
		if ctx.Err() != nil {
			return // don't save half-cancelled work; it simply re-runs next time
		}
		if err := save(ctx, results); err != nil {
			slog.Error("save results", "stage", stage, "err", err)
			sleep(ctx, 5*time.Second)
			continue
		}
		ix.changed.Store(true)
		if next != nil {
			nudge(next)
		}
	}
}

func (ix *Indexer) fetch(ctx context.Context, stage core.Stage, n int) ([]store.JobItem, error) {
	ix.mu.Lock()
	focus := ix.focus
	ix.mu.Unlock()
	if focus != nil {
		items, err := ix.store.PendingJobs(ctx, stage, focus, n)
		if err != nil || len(items) > 0 {
			return items, err
		}
	}
	return ix.store.PendingJobs(ctx, stage, nil, n)
}

func (ix *Indexer) doMetadata(j store.JobItem) (r store.MetadataResult) {
	r.AssetID = j.AssetID
	defer recoverInto(&r.Err, j.Path)
	m, err := media.ReadMetadata(j.Path, j.Kind)
	if err != nil {
		r.Err = err.Error()
		return r
	}
	if !m.TakenAt.IsZero() {
		r.TakenAt = core.WallClock(m.TakenAt)
	}
	r.Width, r.Height, r.Orientation, r.Camera, r.Lat, r.Lon = m.Width, m.Height, m.Orientation, m.Camera, m.Lat, m.Lon
	return r
}

func (ix *Indexer) doThumbnail(j store.JobItem) (r store.ThumbResult) {
	r.AssetID = j.AssetID
	defer recoverInto(&r.Err, j.Path)
	t, err := ix.thumbs.Thumbnail(j.Path, j.Kind, j.Orientation)
	switch {
	case errors.Is(err, media.ErrUnsupported):
		r.Skipped = true
		return r
	case err != nil:
		r.Err = err.Error()
		return r
	}
	if err := media.WriteThumbs(ix.paths.ThumbDir, j.AssetID, t); err != nil {
		r.Err = "write thumbnail: " + err.Error()
		return r
	}
	r.PHash = &t.PHash
	return r
}

// recoverInto turns a panic in a decoder (corrupt files find bugs) into a
// per-file error instead of taking the whole app down.
func recoverInto(errMsg *string, path string) {
	if p := recover(); p != nil {
		slog.Error("panic while processing file", "path", path, "panic", p)
		*errMsg = fmt.Sprint("internal error: ", p)
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}
