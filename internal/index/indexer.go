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
	"lumen/internal/people"
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
	// modelIdle is how long a model stays loaded without work. Loading one
	// takes a second or two, so short gaps shouldn't unload it.
	modelIdle = time.Minute
)

// Models are the AI parts the indexer runs. A nil one (with its error saying
// why) turns its stage off; the rest of Lumen works without it.
type Models struct {
	Search    *search.Engine // image embeddings, search and Things
	SearchErr error
	People    *people.Engine // faces and people
	PeopleErr error
}

// Indexer owns all background work.
type Indexer struct {
	store  *store.Store
	paths  core.Paths
	cache  *media.Cache
	search *search.Engine // nil when search can't run (see aiError)
	people *people.Engine // nil when faces can't be recognised (see facesError)
	events Events

	scanWake  chan struct{}
	scanReqs  []scanRequest // protected by mu, at most one pending request per source
	wakeMeta  chan struct{}
	wakeThum  chan struct{}
	wakeEmbed chan struct{}
	wakeFaces chan struct{}

	encoder *model[*ai.ImageEncoder] // SigLIP's image tower, while its stage has work
	faces   *model[*ai.Faces]        // YuNet and SFace, while their stage has work

	mu          sync.Mutex
	resume      chan struct{} // closed (and replaced) whenever a hold may have lifted
	focus       *store.TimeRange
	paused      bool
	onBattery   bool
	aiOnBattery bool // the user lets AI work continue on battery
	aiDevice    core.Device
	aiError     string
	facesError  string
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

// New builds an indexer. Call Start to begin work.
func New(st *store.Store, paths core.Paths, cache *media.Cache, m Models, ev Events) *Indexer {
	ix := &Indexer{
		store: st, paths: paths, cache: cache, search: m.Search, people: m.People, events: ev,
		scanWake:  make(chan struct{}, 1),
		wakeMeta:  make(chan struct{}, 1),
		wakeThum:  make(chan struct{}, 1),
		wakeEmbed: make(chan struct{}, 1),
		wakeFaces: make(chan struct{}, 1),
		resume:    make(chan struct{}),
		pendingFS: map[string]bool{},
	}
	if m.SearchErr != nil {
		ix.aiError = m.SearchErr.Error()
	}
	if m.PeopleErr != nil {
		ix.facesError = m.PeopleErr.Error()
	}
	if m.Search != nil {
		ix.encoder = &model[*ai.ImageEncoder]{
			name: "image model", idle: modelIdle,
			open:  func() (*ai.ImageEncoder, error) { return ai.OpenImageEncoder(m.Search.Model(), ai.GPU) },
			close: (*ai.ImageEncoder).Close,
			report: func(enc *ai.ImageEncoder, loaded bool, err error) {
				ix.setAIState(func() {
					switch {
					case err != nil:
						ix.aiError = err.Error()
					case loaded:
						ix.aiError, ix.aiDevice = "", enc.Device()
					default:
						ix.aiDevice = ""
					}
				})
			},
		}
	}
	if m.People != nil {
		ix.faces = &model[*ai.Faces]{
			name: "face models", idle: modelIdle,
			open:  func() (*ai.Faces, error) { return ai.OpenFaces(m.People.Model(), faceThreads) },
			close: (*ai.Faces).Close,
			report: func(_ *ai.Faces, _ bool, err error) {
				ix.setAIState(func() {
					ix.facesError = ""
					if err != nil {
						ix.facesError = err.Error()
					}
				})
			},
		}
	}
	return ix
}

// setAIState changes the AI state the progress reports.
func (ix *Indexer) setAIState(change func()) {
	ix.mu.Lock()
	change()
	ix.mu.Unlock()
	ix.stale.Store(true)
}

// versions is the current version of every stage that runs. A finished job
// with another version re-runs.
func (ix *Indexer) versions() map[core.Stage]string {
	v := map[core.Stage]string{
		core.StageMetadata:  media.MetadataVersion + "+" + media.VideoMetadataVersion,
		core.StageThumbnail: media.ThumbnailVersion + "+" + media.VideoThumbnailVersion,
	}
	if ix.search != nil {
		v[core.StageEmbedding] = ix.search.Model().Version + core.VideoFramesVersion
	}
	if ix.people != nil {
		v[core.StageFaces] = ix.people.Model().Version + core.VideoFramesVersion
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
			name: core.StageMetadata, batch: 128, wake: ix.wakeMeta, next: []chan struct{}{ix.wakeThum},
			process: eachItem(4, ix.doMetadata),
			save: func(ctx context.Context, rs []store.MetadataResult) error {
				return ix.store.SaveMetadata(ctx, media.MetadataVersion+"+"+media.VideoMetadataVersion, rs)
			},
		})
	})
	ix.run(func() {
		runStage(ctx, ix, stage[store.ThumbResult]{
			name: core.StageThumbnail, batch: 64, wake: ix.wakeThum, next: []chan struct{}{ix.wakeEmbed, ix.wakeFaces},
			process: eachItem(max(1, min(runtime.NumCPU()-1, 6)), ix.doThumbnail),
			save: func(ctx context.Context, rs []store.ThumbResult) error {
				return ix.store.SaveThumbnails(ctx, media.ThumbnailVersion+"+"+media.VideoThumbnailVersion, rs)
			},
		})
	})
	if ix.search != nil {
		ix.run(func() {
			runStage(ctx, ix, stage[store.EmbedResult]{
				name: core.StageEmbedding, batch: 64, wake: ix.wakeEmbed, model: ix.encoder,
				process: ix.doEmbeddings,
				save:    ix.saveEmbeddings,
			})
		})
	}
	if ix.people != nil {
		ix.run(func() {
			runStage(ctx, ix, stage[store.FaceResult]{
				name: core.StageFaces, batch: 32, wake: ix.wakeFaces, model: ix.faces,
				process: ix.doFaces,
				save:    ix.saveFaces,
			})
		})
		ix.run(func() { ix.people.Run(ctx, func() { ix.changed.Store(true) }) })
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
	if ix.encoder != nil {
		ix.encoder.release()
	}
	if ix.faces != nil {
		ix.faces.release()
	}
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
	ix.changed.Store(true) // source removal can change visibility without a scan
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
	for _, ch := range []chan struct{}{ix.wakeMeta, ix.wakeThum, ix.wakeEmbed, ix.wakeFaces} {
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
	return ix.paused || (stage.AI() && ix.onBattery && !ix.aiOnBattery)
}

// waitTurn blocks while stage is held, meanwhile freeing its model (if
// any). It returns false if ctx ends.
func (ix *Indexer) waitTurn(ctx context.Context, stage core.Stage, m releaser) bool {
	for {
		ix.mu.Lock()
		held, resume := ix.held(stage), ix.resume
		ix.mu.Unlock()
		if !held {
			return true
		}
		if m != nil {
			m.release()
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
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for i, pending := range ix.scanReqs {
		if pending.sourceID != r.sourceID {
			continue
		}
		// A full scan covers every subtree. Disjoint changes widen to the
		// whole source instead of losing work when a bounded queue fills.
		switch {
		case pending.under == "", pending.under == r.under:
		case r.under == "":
			ix.scanReqs[i] = r
		case store.IsWithin(r.under, pending.under):
		case store.IsWithin(pending.under, r.under):
			ix.scanReqs[i] = r
		default:
			ix.scanReqs[i].under = ""
		}
		nudge(ix.scanWake)
		return
	}
	ix.scanReqs = append(ix.scanReqs, r)
	nudge(ix.scanWake)
}

func (ix *Indexer) nextScan(ctx context.Context) (scanRequest, bool) {
	for ctx.Err() == nil {
		ix.mu.Lock()
		if len(ix.scanReqs) > 0 {
			r := ix.scanReqs[0]
			ix.scanReqs = ix.scanReqs[1:]
			ix.mu.Unlock()
			return r, true
		}
		ix.mu.Unlock()
		select {
		case <-ix.scanWake:
		case <-ctx.Done():
		}
	}
	return scanRequest{}, false
}

func (ix *Indexer) pendingScans() int {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return len(ix.scanReqs)
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
		req, ok := ix.nextScan(ctx)
		if !ok {
			return
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
		if fi, err := os.Stat(path); (err == nil && !fi.IsDir()) || (err != nil && !os.IsNotExist(err)) {
			return // not media, not a folder
		}
		// A deleted dotted folder cannot be distinguished from a non-media
		// file here. Reconcile its parent so its photos leave the library.
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
		OnBattery: (ix.search != nil || ix.people != nil) && ix.onBattery && !ix.aiOnBattery,
		AI:        core.AI{Device: ix.aiDevice, Error: ix.aiError, FacesError: ix.facesError},
	}
	ix.mu.Unlock()
	if pending, failed, skipped, err := ix.store.JobCounts(ctx); err == nil {
		p.Metadata, p.Thumbnail, p.Errors, p.Skipped = pending[core.StageMetadata], pending[core.StageThumbnail], failed, skipped
		if ix.search != nil {
			p.Embedding = pending[core.StageEmbedding]
		}
		if ix.people != nil {
			p.Faces = pending[core.StageFaces]
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
