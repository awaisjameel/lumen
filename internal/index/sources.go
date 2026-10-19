package index

import (
	"context"
	"log/slog"
	"time"

	"lumen/internal/core"
)

const sourceRetry = 15 * time.Second

// SQLite can reuse a removed source's ID. Its old watcher or slow probe must
// never supply availability for a different folder that now has that ID.
type sourceKey struct {
	id   int64
	path string
}

func sourceIdentity(src core.Source) sourceKey {
	return sourceKey{id: src.ID, path: core.Fold(src.Path)}
}

// SourceAvailability overlays cached checks without touching a source's disk.
// A source not checked yet stays distinct from one known to be unavailable.
func (ix *Indexer) SourceAvailability(srcs []core.Source) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for i := range srcs {
		state, ok := ix.sourceState[sourceIdentity(srcs[i])]
		if !ok {
			state = core.SourceChecking
		}
		srcs[i].Availability = state
	}
}

// RecheckSources coalesces requests without waiting for filesystem calls.
func (ix *Indexer) RecheckSources() { nudge(ix.sourceWake) }

func (ix *Indexer) setSourceAvailability(src core.Source, state core.SourceAvailability) {
	ix.mu.Lock()
	key := sourceIdentity(src)
	old := ix.sourceState[key]
	ix.sourceState[key] = state
	ix.mu.Unlock()
	if old == state {
		return
	}
	ix.markChanged(core.Changes{Sources: true})
	if old == core.SourceUnavailable && state == core.SourceAvailable {
		ix.queueScan(scanRequest{sourceID: src.ID})
	}
}

// sourceLoop probes roots cheaply between watcher retries. Healthy roots
// are checked too: renaming a watched root need not stop its open handle. Each
// source has at most one probe, so a slow share cannot stall other sources or
// accumulate goroutines when Try again is pressed repeatedly.
func (ix *Indexer) sourceLoop(ctx context.Context) {
	tick := time.NewTicker(sourceRetry)
	defer tick.Stop()
	ix.sourceChecks(ctx, tick.C)
}

func (ix *Indexer) sourceChecks(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ix.sourceWake:
		case <-ticks:
		}
		ix.checkSources(ctx)
	}
}

func (ix *Indexer) checkSources(ctx context.Context) {
	srcs, err := ix.store.ListSources(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("list sources for availability", "err", err)
		}
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	current := make(map[sourceKey]bool, len(srcs))
	for _, src := range srcs {
		current[sourceIdentity(src)] = true
	}
	for id := range ix.sourceState {
		if !current[id] {
			delete(ix.sourceState, id)
		}
	}
	for _, src := range srcs {
		key := sourceIdentity(src)
		if ctx.Err() != nil {
			return
		}
		if !src.Enabled || ix.sourceProbes[key] {
			continue
		}
		ix.sourceProbes[key] = true
		// Stop waits for the loop before waiting for these workers. Adding under
		// mu also follows the watchers' shutdown ordering.
		ix.watchers.Add(1)
		go func() {
			defer ix.watchers.Done()
			err := ix.checkSource(ctx, src.Path)
			if ctx.Err() == nil {
				state := core.SourceAvailable
				if err != nil {
					state = core.SourceUnavailable
				}
				ix.setSourceAvailability(src, state)
			}
			ix.mu.Lock()
			delete(ix.sourceProbes, key)
			ix.mu.Unlock()
		}()
	}
}
