package services

import (
	"context"
	"log/slog"
	"time"

	"lumen/internal/media"
)

// viewFlushEvery is how often the thumbnails shown since the last write reach
// the database. The grid shows thousands a second, so each is only noted in
// memory (media.Cache.Viewed) and the lot goes in one statement.
const viewFlushEvery = 30 * time.Second

// viewFlusher writes the cache's pending views to the database on a timer and
// once more when it stops.
type viewFlusher struct {
	cache *media.Cache
	sink  media.ViewSink
	every time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

// start begins flushing until stop. ctx is only the parent of the flusher's own
// context: the final write at stop must not be cut short by it ending.
func (f *viewFlusher) start(ctx context.Context) {
	ctx, f.cancel = context.WithCancel(context.WithoutCancel(ctx))
	f.done = make(chan struct{})
	go func() {
		defer close(f.done)
		t := time.NewTicker(f.every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				f.flush(ctx)
			}
		}
	}()
}

// stop ends the timer, waits for a write in progress and writes what is left.
func (f *viewFlusher) stop() {
	if f.cancel == nil {
		return
	}
	f.cancel()
	<-f.done
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f.flush(ctx)
}

func (f *viewFlusher) flush(ctx context.Context) {
	if err := f.cache.FlushViews(ctx, f.sink); err != nil && ctx.Err() == nil {
		slog.Warn("save thumbnail views", "err", err) // kept in memory for the next try
	}
}
