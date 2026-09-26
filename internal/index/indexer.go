// Package index runs the background pipeline: scanning, then each stage
// (metadata, thumbnails, the AI) over the durable job queue in SQLite. Work
// survives restarts and resumes where it stopped.
package index

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/scanner"
	"lumen/internal/search"
	"lumen/internal/store"
)

// Events is how the indexer reports to the UI. Implementations must not block.
type Events interface {
	Progress(core.Progress)
	LibraryChanged(visible int) // assets were added, removed or updated
}

const (
	// debounce is how long folder changes must go quiet before they are rescanned.
	debounce = 2 * time.Second
	// powerPoll is how often the power source is checked.
	powerPoll = 20 * time.Second
	// encoderIdle is how long the image model stays loaded without work.
	// Loading it on the GPU takes seconds, so short gaps shouldn't unload it.
	encoderIdle = time.Minute
)

// Indexer owns all background work.
type Indexer struct {
	store  *store.Store
	paths  core.Paths
	cache  *media.Cache
	search *search.Engine // nil when the AI can't run (see aiError)
	events Events

	scanReqs  chan scanRequest
	wakeMeta  chan struct{}
	wakeThum  chan struct{}
	wakeEmbed chan struct{}

	encMu   sync.Mutex
	enc     *ai.ImageEncoder // loaded while the AI stage has work
	encUsed time.Time

	mu          sync.Mutex
	resume      chan struct{} // closed (and replaced) whenever a hold may have lifted
	focus       *store.TimeRange
	paused      bool
	onBattery   bool
	aiOnBattery bool // the user lets AI work continue on battery
	aiDevice    string
	aiError     string
	scanning    bool
	discovered  int
	pendingFS   map[string]bool // changed paths waiting for the debounce
	fsTimer     *time.Timer
	watchStop   context.CancelFunc

	// Set by whoever changes things and cleared by reportLoop, which only then
	// recomputes progress, so an idle library costs no queries.
	changed atomic.Bool // library contents changed (implies stale)
	stale   atomic.Bool // only the progress changed (scan counts, pause)
	last    core.Progress

	cancel  context.CancelFunc
	wg      sync.WaitGroup
	rootCtx context.Context // nil until Start
}

type scanRequest struct {
	sourceID int64
	under    string // "" = the whole source
}

// New builds an indexer. engine is nil when the AI can't run; aiErr says why.
// Call Start to begin work.
func New(st *store.Store, paths core.Paths, cache *media.Cache, engine *search.Engine, aiErr error, ev Events) *Indexer {
	ix := &Indexer{
		store: st, paths: paths, cache: cache, search: engine, events: ev,
		scanReqs:  make(chan scanRequest, 256),
		wakeMeta:  make(chan struct{}, 1),
		wakeThum:  make(chan struct{}, 1),
		wakeEmbed: make(chan struct{}, 1),
		resume:    make(chan struct{}),
		pendingFS: map[string]bool{},
	}
	if aiErr != nil {
		ix.aiError = aiErr.Error()
	}
	return ix
}

// versions is the current version of every stage that runs. A finished job
// with another version re-runs.
func (ix *Indexer) versions() map[core.Stage]string {
	v := map[core.Stage]string{
		core.StageMetadata:  media.MetadataVersion,
		core.StageThumbnail: media.ThumbnailVersion,
	}
	if ix.search != nil {
		v[core.StageEmbedding] = ix.search.Model().Version
	}
	return v
}

// Start launches the background goroutines and queues a reconcile scan of
// every source, which catches anything that changed while Lumen was closed.
func (ix *Indexer) Start(ctx context.Context) {
	ctx, ix.cancel = context.WithCancel(ctx)
	ix.rootCtx = ctx
	if n, err := ix.store.RequeueOutdated(ctx, ix.versions()); err != nil {
		slog.Error("requeue outdated jobs", "err", err)
	} else if n > 0 {
		slog.Info("re-running stages after an upgrade", "jobs", n)
	}
	if on, err := ix.store.Bool(ctx, core.SettingAIOnBattery, false); err == nil {
		ix.aiOnBattery = on
	}
	ix.onBattery = onBattery()

	ix.run(func() { ix.scanLoop(ctx) })
	ix.run(func() {
		runStage(ctx, ix, stage[store.MetadataResult]{
			name: core.StageMetadata, batch: 128, wake: ix.wakeMeta, next: ix.wakeThum,
			process: eachItem(4, ix.doMetadata),
			save: func(ctx context.Context, rs []store.MetadataResult) error {
				return ix.store.SaveMetadata(ctx, media.MetadataVersion, rs)
			},
		})
	})
	ix.run(func() {
		runStage(ctx, ix, stage[store.ThumbResult]{
			name: core.StageThumbnail, batch: 64, wake: ix.wakeThum, next: ix.wakeEmbed,
			process: eachItem(max(1, min(runtime.NumCPU()-1, 6)), ix.doThumbnail),
			save: func(ctx context.Context, rs []store.ThumbResult) error {
				return ix.store.SaveThumbnails(ctx, media.ThumbnailVersion, rs)
			},
		})
	})
	if ix.search != nil {
		ix.run(func() {
			runStage(ctx, ix, stage[store.EmbedResult]{
				name: core.StageEmbedding, batch: 64, wake: ix.wakeEmbed,
				process: ix.doEmbeddings,
				save:    ix.saveEmbeddings,
				idle:    ix.releaseIdleEncoder,
			})
		})
	}
	ix.run(func() { ix.reportLoop(ctx) })
	ix.run(func() { ix.powerLoop(ctx) })
	ix.RescanAll()
}

// Stop cancels all work and waits for it to wind down.
func (ix *Indexer) Stop() {
	if ix.cancel == nil {
		return
	}
	ix.cancel()
	ix.mu.Lock()
	if ix.fsTimer != nil {
		ix.fsTimer.Stop()
	}
	ix.mu.Unlock()
	ix.wg.Wait()
	ix.releaseEncoder()
}

// encoder returns the image model, loading it on first use.
func (ix *Indexer) encoder() (*ai.ImageEncoder, error) {
	ix.encMu.Lock()
	defer ix.encMu.Unlock()
	ix.encUsed = time.Now()
	if ix.enc != nil {
		return ix.enc, nil
	}
	t := time.Now()
	enc, err := ai.OpenImageEncoder(ix.search.Model(), ai.GPU)
	ix.mu.Lock()
	if err != nil {
		ix.aiError = err.Error()
	} else {
		ix.aiError, ix.aiDevice = "", string(enc.Device())
	}
	ix.mu.Unlock()
	ix.stale.Store(true)
	if err != nil {
		return nil, err
	}
	slog.Info("image model loaded", "device", enc.Device(), "took", time.Since(t).Round(time.Millisecond))
	ix.enc = enc
	return enc, nil
}

// releaseIdleEncoder frees the image model once it has had no work for a
// while. The AI stage calls it whenever its queue is empty.
func (ix *Indexer) releaseIdleEncoder() {
	ix.encMu.Lock()
	idle := time.Since(ix.encUsed) >= encoderIdle
	ix.encMu.Unlock()
	if idle {
		ix.releaseEncoder()
	}
}

// releaseEncoder frees the image model (hundreds of MB).
func (ix *Indexer) releaseEncoder() {
	ix.encMu.Lock()
	defer ix.encMu.Unlock()
	if ix.enc == nil {
		return
	}
	ix.enc.Close()
	ix.enc = nil
	ix.mu.Lock()
	ix.aiDevice = ""
	ix.mu.Unlock()
	ix.stale.Store(true)
	slog.Info("image model unloaded (no work)")
}

func (ix *Indexer) run(fn func()) {
	ix.wg.Add(1)
	go func() { defer ix.wg.Done(); fn() }()
}

// RescanAll queues a scan of every enabled source and restarts the watchers.
func (ix *Indexer) RescanAll() {
	if ix.rootCtx == nil {
		return // not started yet; Start scans everything anyway
	}
	srcs, err := ix.store.ListSources(ix.rootCtx)
	if err != nil {
		slog.Error("list sources", "err", err)
		return
	}
	for _, s := range srcs {
		if s.Enabled {
			ix.queueScan(scanRequest{sourceID: s.ID})
		}
	}
	ix.restartWatchers(srcs)
}

// SetFocus tells the indexer which days the user is looking at (YYYY-MM-DD,
// inclusive), so every stage does them first.
func (ix *Indexer) SetFocus(from, to string) {
	a, err1 := time.Parse(time.DateOnly, from)
	b, err2 := time.Parse(time.DateOnly, to)
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if err1 != nil || err2 != nil {
		ix.focus = nil
		return
	}
	if a.After(b) {
		a, b = b, a
	}
	ix.focus = &store.TimeRange{From: a.Unix(), To: b.AddDate(0, 0, 1).Unix()}
	for _, ch := range []chan struct{}{ix.wakeMeta, ix.wakeThum, ix.wakeEmbed} {
		nudge(ch)
	}
}

// SetPaused pauses or resumes the stage workers (scanning keeps going; it is cheap).
func (ix *Indexer) SetPaused(p bool) { ix.setHold(func() { ix.paused = p }) }

// SetAIOnBattery lets the AI keep working on battery power, or not.
func (ix *Indexer) SetAIOnBattery(on bool) { ix.setHold(func() { ix.aiOnBattery = on }) }

// setHold changes what holds work back, then wakes every waiting worker to
// re-check.
func (ix *Indexer) setHold(change func()) {
	ix.mu.Lock()
	change()
	close(ix.resume)
	ix.resume = make(chan struct{})
	ix.mu.Unlock()
	ix.stale.Store(true)
}

// held reports whether a stage must wait: everything waits while paused, and
// the AI waits on battery unless the user allows it. Call with mu held.
func (ix *Indexer) held(stage core.Stage) bool {
	return ix.paused || (stage == core.StageEmbedding && ix.onBattery && !ix.aiOnBattery)
}

// waitTurn blocks while stage is held. It returns false if ctx ends.
func (ix *Indexer) waitTurn(ctx context.Context, stage core.Stage) bool {
	for {
		ix.mu.Lock()
		held, resume := ix.held(stage), ix.resume
		ix.mu.Unlock()
		if !held {
			return true
		}
		if stage == core.StageEmbedding {
			ix.releaseEncoder() // free the model's memory while waiting
		}
		select {
		case <-resume:
		case <-ctx.Done():
			return false
		}
	}
}

// powerLoop follows the power source, so AI work waits while on battery.
func (ix *Indexer) powerLoop(ctx context.Context) {
	t := time.NewTicker(powerPoll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := onBattery()
		ix.mu.Lock()
		was := ix.onBattery
		ix.mu.Unlock()
		if now != was {
			slog.Info("power source changed", "battery", now)
			ix.setHold(func() { ix.onBattery = now })
		}
	}
}

func (ix *Indexer) queueScan(r scanRequest) {
	select {
	case ix.scanReqs <- r:
	default:
		slog.Warn("scan queue full; dropping request", "source", r.sourceID, "under", r.under)
	}
}

func (ix *Indexer) scanLoop(ctx context.Context) {
	sc := &scanner.Scanner{
		Store:       ix.store,
		HashWorkers: 4,
		OnProgress: func(seen int) {
			ix.mu.Lock()
			ix.discovered = seen
			ix.mu.Unlock()
			ix.stale.Store(true)
		},
		OnIngested: func(int) {
			ix.changed.Store(true) // new assets, moves, revived files
			nudge(ix.wakeMeta)
		},
	}
	for {
		var req scanRequest
		select {
		case <-ctx.Done():
			return
		case req = <-ix.scanReqs:
		}
		srcs, err := ix.store.ListSources(ctx)
		if err != nil {
			slog.Error("list sources", "err", err)
			continue
		}
		i := slices.IndexFunc(srcs, func(s core.Source) bool { return s.ID == req.sourceID && s.Enabled })
		if i < 0 {
			continue // removed since it was queued
		}
		src := srcs[i]
		videos, err := ix.store.Bool(ctx, core.SettingVideos, false)
		if err != nil {
			slog.Error("read settings", "err", err)
		}

		ix.mu.Lock()
		ix.scanning, ix.discovered = true, 0
		ix.mu.Unlock()
		ix.stale.Store(true)

		start := time.Now()
		res, err := sc.Scan(ctx, src, req.under, scanner.Options{
			Videos:   videos,
			SkipDirs: []string{ix.paths.DataDir},
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("scan failed", "source", src.Path, "under", req.under, "err", err)
		}
		slog.Info("scan done", "source", src.Path, "under", req.under, "seen", res.Seen,
			"changed", res.Changed, "new", res.NewAssets, "removed", res.Removed, "took", time.Since(start).Round(time.Millisecond))

		ix.mu.Lock()
		ix.scanning = len(ix.scanReqs) > 0
		ix.mu.Unlock()
		if res.Removed > 0 {
			ix.changed.Store(true)
		}
		ix.stale.Store(true)
		nudge(ix.wakeMeta)
	}
}

// restartWatchers watches every enabled source for changes.
func (ix *Indexer) restartWatchers(srcs []core.Source) {
	ix.mu.Lock()
	if ix.watchStop != nil {
		ix.watchStop()
	}
	ctx, cancel := context.WithCancel(ix.rootCtx)
	ix.watchStop = cancel
	ix.mu.Unlock()
	for _, s := range srcs {
		if !s.Enabled {
			continue
		}
		if err := scanner.Watch(ctx, s.Path, ix.onFSChange); err != nil {
			slog.Warn("cannot watch folder", "dir", s.Path, "err", err)
		}
	}
}

// onFSChange collects changed paths and turns them into scans two seconds
// after things go quiet, so copying 500 photos in causes one scan, not 500.
func (ix *Indexer) onFSChange(path string) {
	if store.IsWithin(path, ix.paths.DataDir) {
		return
	}
	if ext := filepath.Ext(path); ext != "" && core.KindOf(path) == "" {
		if fi, err := os.Stat(path); err != nil || !fi.IsDir() {
			return // not media, not a folder
		}
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.rootCtx.Err() != nil {
		return // stopping; Stop cancels the timer
	}
	ix.pendingFS[path] = true
	if ix.fsTimer == nil {
		ix.fsTimer = time.AfterFunc(debounce, ix.flushFSChanges)
	} else {
		ix.fsTimer.Reset(debounce)
	}
}

func (ix *Indexer) flushFSChanges() {
	ix.mu.Lock()
	changed := ix.pendingFS
	ix.pendingFS = map[string]bool{}
	ix.mu.Unlock()

	srcs, err := ix.store.ListSources(ix.rootCtx)
	if err != nil {
		return
	}
	// Map each path to the folder to rescan, then drop folders inside others.
	dirs := map[int64][]string{}
	for p := range changed {
		dir := p
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			dir = filepath.Dir(p)
		}
		for _, s := range srcs {
			if s.Enabled && store.IsWithin(dir, s.Path) {
				dirs[s.ID] = append(dirs[s.ID], dir)
			}
		}
	}
	for _, s := range srcs {
		list := dirs[s.ID]
		if len(list) == 0 {
			continue
		}
		// Shortest first, so a parent is kept before any folder inside it.
		slices.SortFunc(list, func(a, b string) int { return len(a) - len(b) })
		var keep []string
	outer:
		for _, d := range list {
			for _, k := range keep {
				if store.IsWithin(d, k) {
					continue outer // inside a folder we'll rescan anyway
				}
			}
			keep = append(keep, d)
		}
		for _, d := range keep {
			under := d
			if store.IsWithin(s.Path, d) {
				under = "" // the source root itself
			}
			ix.queueScan(scanRequest{sourceID: s.ID, under: under})
		}
	}
}

// reportLoop emits index:progress when it changes and library:changed at most
// once a second, so the UI stays live without being flooded. While nothing
// changes it does not touch the database.
func (ix *Indexer) reportLoop(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		changed := ix.changed.Swap(false)
		if stale := ix.stale.Swap(false); !stale && !changed {
			continue
		}
		p := ix.Progress(ctx)
		if p != ix.last {
			ix.last = p
			ix.events.Progress(p)
		}
		if changed {
			ix.events.LibraryChanged(p.Total)
		}
	}
}

// Progress is a snapshot of indexing state.
func (ix *Indexer) Progress(ctx context.Context) core.Progress {
	ix.mu.Lock()
	p := core.Progress{
		Scanning: ix.scanning, Discovered: ix.discovered, Paused: ix.paused,
		OnBattery: ix.search != nil && ix.onBattery && !ix.aiOnBattery,
		AI:        core.AI{Device: ix.aiDevice, Error: ix.aiError},
	}
	ix.mu.Unlock()
	if pending, failed, skipped, err := ix.store.JobCounts(ctx); err == nil {
		p.Metadata, p.Thumbnail, p.Errors, p.Skipped = pending[core.StageMetadata], pending[core.StageThumbnail], failed, skipped
		if ix.search != nil {
			p.Embedding = pending[core.StageEmbedding]
		}
	} else if ctx.Err() == nil {
		slog.Error("count jobs", "err", err)
	}
	if n, err := ix.store.CountVisible(ctx); err == nil {
		p.Total = n
	} else if ctx.Err() == nil {
		slog.Error("count assets", "err", err)
	}
	return p
}

func nudge(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
