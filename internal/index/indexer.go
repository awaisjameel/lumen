// Package index runs the background pipeline: scanning, then each stage
// (metadata, thumbnails, the AI) over the durable job queue in SQLite. Work
// survives restarts and resumes where it stopped.
package index

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/media"
	"lumen/internal/people"
	"lumen/internal/scanner"
	"lumen/internal/search"
	"lumen/internal/store"
)

// Events is how the indexer reports to the UI. Implementations must not block.
type Events interface {
	Progress(core.Progress)
	LibraryChanged(core.Changes)
}

const (
	// debounce is how long folder changes must go quiet before they are
	// rescanned, and debounceMax the longest they wait for that (a video
	// being recorded into a folder changes it all the time).
	debounce    = 2 * time.Second
	debounceMax = 30 * time.Second
	// powerPoll is how often the power source is checked.
	powerPoll = 20 * time.Second
	// modelIdle is how long a model stays loaded without work. Loading one
	// takes a second or two, so short gaps shouldn't unload it.
	modelIdle = time.Minute
	// A watch that fails (a drive unplugged, a share gone) is retried after
	// watchRetry, doubling up to watchRetryMax.
	watchRetry    = 5 * time.Second
	watchRetryMax = 5 * time.Minute
	// retryDelay is how long a scan or stage waits after the database failed.
	retryDelay = 5 * time.Second
	// hashWorkers fingerprint files in parallel while scanning (I/O bound).
	hashWorkers = 4
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
	wakeOCR   chan struct{}
	wakeVideo chan struct{}
	wakeFS    chan struct{}

	scanTurn   chan struct{} // holds a token while a scan runs or HoldScans is in force: one at a time
	onScanTurn func()        // a test's hook, called when a scan has its turn; nil otherwise

	encoder *model[*ai.ImageEncoder] // SigLIP's image tower, while its stage has work
	faces   *model[*ai.Faces]        // YuNet and SFace, while their stage has work
	ocr     *model[textReader]       // PP-OCRv5's detector and recogniser, while their stage has work
	pixels  decoded                  // photos the thumbnail stage decoded for the faces stage
	xmp     xmpState                 // the XMP sidecar pass's lock and wake-up

	videoEmbedRetry stageBackoff // used only by the video coordinator
	videoFaceRetry  stageBackoff

	version map[core.Stage]store.StageVersion // what each running stage's results are made by

	rescanMu sync.Mutex // one RescanAll at a time, so the watchers match the sources

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
	pendingFS   map[string]bool // changed paths waiting for the debounce
	fsSince     time.Time       // when the oldest of them arrived
	fsDue       time.Time       // the next flush's deadline (mu)
	watchStop   context.CancelFunc

	// Set by whoever changes things and cleared by reportLoop, which only then
	// recomputes progress, so an idle library costs no queries.
	changes atomic.Uint32 // what changed in the library (changeBits; implies stale)
	stale   atomic.Bool   // only the progress changed (scan counts, pause)
	last    core.Progress

	cancel   context.CancelFunc
	wg       sync.WaitGroup
	watchers sync.WaitGroup  // watchLoops; Added under mu, only while rootCtx lives
	rootCtx  context.Context // nil until Start
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
		scanTurn:  make(chan struct{}, 1),
		wakeMeta:  make(chan struct{}, 1),
		wakeThum:  make(chan struct{}, 1),
		wakeEmbed: make(chan struct{}, 1),
		wakeFaces: make(chan struct{}, 1),
		wakeOCR:   make(chan struct{}, 1),
		wakeVideo: make(chan struct{}, 1),
		wakeFS:    make(chan struct{}, 1),
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
			open: func() (*ai.Faces, error) { return ai.OpenFaces(m.People.Model(), ai.GPU, faceThreads) },
			close: func(f *ai.Faces) {
				f.Close()
				ix.pixels.clear() // idle, held or failed: nothing will take them soon
			},
			report: func(f *ai.Faces, loaded bool, err error) {
				if loaded {
					slog.Info("face models ready", "device", f.Device())
				}
				ix.setAIState(func() {
					if loaded {
						ix.facesError = ""
					}
					if err != nil {
						ix.facesError = err.Error()
					}
				})
			},
		}
	}
	ix.ocr = ix.openOCR()
	ix.version = ix.versions()
	return ix
}

// Bits of Indexer.changes, one per field of core.Changes.
const (
	changedAssets uint32 = 1 << iota
	changedThumbs
	changedTags
	changedPeople
)

// LibraryChanged records a change made outside the indexer (a block), so the
// UI hears of it with the indexer's own, at most once a second.
func (ix *Indexer) LibraryChanged(c core.Changes) { ix.markChanged(c) }

// markChanged records that the library changed, for reportLoop to tell the UI.
func (ix *Indexer) markChanged(c core.Changes) {
	var bits uint32
	if c.Assets {
		bits |= changedAssets
	}
	if c.Thumbs {
		bits |= changedThumbs
	}
	if c.Tags {
		bits |= changedTags
	}
	if c.People {
		bits |= changedPeople
	}
	ix.changes.Or(bits)
}

// setAIState changes the AI state the progress reports.
func (ix *Indexer) setAIState(change func()) {
	ix.mu.Lock()
	change()
	ix.mu.Unlock()
	ix.stale.Store(true)
}

// versions is the current version of every stage that runs, built once in
// New: every job a stage finishes records it, and a finished job with
// another version re-runs.
func (ix *Indexer) versions() map[core.Stage]store.StageVersion {
	metadata := media.MetadataVersion + "+" + media.VideoMetadataVersion
	thumbnail := media.ThumbnailVersion + "+" + media.VideoThumbnailVersion
	v := map[core.Stage]store.StageVersion{
		core.StageMetadata:  {Photo: metadata, Video: metadata},
		core.StageThumbnail: {Photo: thumbnail, Video: thumbnail},
		// Text is only read from photos, but a stage's version has both.
		core.StageOCR: {Photo: ai.OCRVersion, Video: ai.OCRVersion},
	}
	if ix.search != nil {
		photo := ix.search.Model().Version
		v[core.StageEmbedding] = store.StageVersion{Photo: photo, Video: photo + media.VideoFramesVersion, LegacyPhoto: photo + "+video-frames-1"}
	}
	if ix.people != nil {
		photo := ix.people.Model().Version + "+analysis-" + ai.FaceAnalysisVersion
		v[core.StageFaces] = store.StageVersion{Photo: photo, Video: photo + media.VideoFramesVersion}
	}
	return v
}

// Pipeline tuning: jobs per batch, and workers for the per-file stages.
const (
	metadataBatch   = 128
	metadataWorkers = 4
	thumbnailBatch  = 64
	embeddingBatch  = 64
	facesBatch      = 32
)

// Start launches the background goroutines and queues a reconcile scan of
// every source, which catches anything that changed while Lumen was closed.
// It returns at once: the window must not wait for the disks.
func (ix *Indexer) Start(ctx context.Context) {
	ctx, ix.cancel = context.WithCancel(ctx)
	ix.rootCtx = ctx
	on, err := ix.store.Bool(ctx, core.SettingAIOnBattery, false)
	if err != nil {
		slog.Error("read settings", "err", err)
	}
	ix.aiOnBattery, ix.onBattery = on, onBattery()
	ix.run(func() {
		for ctx.Err() == nil {
			n, err := ix.store.BackfillPlaces(ctx)
			if n > 0 {
				ix.markChanged(core.Changes{Assets: true})
			}
			if err == nil || ctx.Err() != nil {
				return
			}
			slog.Error("resolve existing GPS locations", "err", err)
			sleep(ctx, retryDelay)
		}
	})

	ix.run(func() {
		if n, err := ix.store.RequeueVersions(ctx, ix.version); err != nil {
			slog.Error("requeue outdated jobs", "err", err)
		} else if n > 0 {
			slog.Info("re-running stages after an upgrade", "jobs", n)
			ix.wakeStages()
		}
		// Photos that became screenshots or documents while nothing was
		// watching (a changed tag confidence, a tag the user added) still get
		// their text read.
		if n, err := ix.store.QueueOCR(ctx); err != nil {
			slog.Error("queue text reading", "err", err)
		} else if n > 0 {
			slog.Info("queued photos for text reading", "photos", n)
			nudge(ix.wakeOCR)
		}
		ix.RescanAll() // opening watchers can take seconds for a network share
		ix.scanLoop(ctx)
	})
	ix.run(func() {
		runStage(ctx, ix, stage[store.MetadataResult]{
			name: core.StageMetadata, batch: metadataBatch, wake: ix.wakeMeta, next: []chan struct{}{ix.wakeThum},
			changes:   core.Changes{Assets: true},
			unchanged: func(r store.MetadataResult) bool { return r.Unchanged },
			process:   eachItem(metadataWorkers, ix.doMetadata),
			save: func(ctx context.Context, rs []store.MetadataResult) error {
				return ix.store.SaveMetadata(ctx, ix.version[core.StageMetadata].Photo, rs)
			},
		})
	})
	ix.run(func() {
		runStage(ctx, ix, stage[store.ThumbResult]{
			name: core.StageThumbnail, batch: thumbnailBatch, wake: ix.wakeThum, next: []chan struct{}{ix.wakeEmbed, ix.wakeFaces, ix.wakeVideo},
			changes:   core.Changes{Thumbs: true}, // the timeline's days and counts stay as they are
			unchanged: func(r store.ThumbResult) bool { return r.Unchanged },
			process:   ix.doThumbnails,
			save: func(ctx context.Context, rs []store.ThumbResult) error {
				return ix.store.SaveThumbnails(ctx, ix.version[core.StageThumbnail].Photo, rs)
			},
		})
	})
	if ix.search != nil {
		ix.run(func() {
			runStage(ctx, ix, stage[store.EmbedResult]{
				name: core.StageEmbedding, batch: embeddingBatch, wake: ix.wakeEmbed, model: ix.encoder,
				next:    []chan struct{}{ix.wakeOCR}, // the tags may make a photo a screenshot
				changes: core.Changes{Tags: true},
				process: ix.doEmbeddings,
				save:    ix.saveEmbeddings,
				fail:    func(msg string) { ix.setAIState(func() { ix.aiError = msg }) },
			})
		})
	}
	if ix.people != nil {
		ix.run(func() {
			runStage(ctx, ix, stage[store.FaceResult]{
				name: core.StageFaces, batch: facesBatch, wake: ix.wakeFaces, model: ix.faces,
				changes:   core.Changes{People: true},
				unchanged: facesUnchanged,
				process:   ix.doFaces,
				save:      ix.saveFaces,
				fail:      func(msg string) { ix.setAIState(func() { ix.facesError = msg }) },
			})
		})
		ix.run(func() { ix.people.Run(ctx, func() { ix.markChanged(core.Changes{People: true}) }) })
	}
	ix.run(func() {
		runStage(ctx, ix, stage[store.OCRResult]{
			name: core.StageOCR, batch: ocrBatch, wake: ix.wakeOCR, model: ix.ocr,
			fetch: ix.fetchOCR, process: ix.doOCR, save: ix.saveOCR,
		})
	})
	if ix.search != nil || ix.people != nil {
		ix.run(func() {
			name := core.StageEmbedding
			if ix.search == nil {
				name = core.StageFaces
			}
			runStage(ctx, ix, stage[videoResults]{
				name: name, batch: 4, wake: ix.wakeVideo,
				model: videoModels{ix.encoder, ix.faces},
				fetch: ix.fetchVideos, process: ix.doVideos, save: ix.saveVideos,
				changes: core.Changes{Tags: true, People: true},
			})
		})
	}
	ix.run(func() { ix.reportLoop(ctx) })
	ix.run(func() { ix.powerLoop(ctx) })
	ix.run(func() { ix.thumbCacheLoop(ctx) })
	ix.run(func() { ix.xmpLoop(ctx) })
	ix.run(func() { ix.fsLoop(ctx, ix.flushFSChanges) })
}

// wakeStages nudges every stage to look for work.
func (ix *Indexer) wakeStages() {
	for _, ch := range []chan struct{}{ix.wakeMeta, ix.wakeThum, ix.wakeEmbed, ix.wakeFaces, ix.wakeOCR, ix.wakeVideo} {
		nudge(ch)
	}
}

// Stop cancels all work and waits for it to wind down.
func (ix *Indexer) Stop() {
	if ix.cancel == nil {
		return
	}
	ix.cancel()
	ix.mu.Lock() // after cancel: restartWatchers starts no more watchers
	if ix.watchStop != nil {
		ix.watchStop()
	}
	ix.mu.Unlock()
	ix.wg.Wait()
	ix.watchers.Wait()
	if ix.encoder != nil {
		ix.encoder.release()
	}
	if ix.faces != nil {
		ix.faces.release()
	}
	ix.ocr.release()
}

func (ix *Indexer) run(fn func()) {
	ix.wg.Add(1)
	go func() { defer ix.wg.Done(); fn() }()
}

// HoldScans waits for a running scan to finish and keeps new ones from
// starting until release is called: organising moves files the scanner must
// not see half-moved. Changes the watchers report meanwhile are queued as
// usual and scanned after release, when the database already matches the
// disk. A scan and a hold take turns, so a second hold waits for the first.
// release may be called more than once.
func (ix *Indexer) HoldScans(ctx context.Context) (release func(), err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case ix.scanTurn <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var once sync.Once
	return func() { once.Do(func() { <-ix.scanTurn }) }, nil
}

// RescanAll queues a scan of every enabled source and restarts the watchers.
func (ix *Indexer) RescanAll() {
	if ix.rootCtx == nil {
		return // not started yet; Start scans everything anyway
	}
	// Two calls overlapping (a folder added while another is removed) must
	// not leave watchers for the older list.
	ix.rescanMu.Lock()
	defer ix.rescanMu.Unlock()
	srcs, err := ix.store.ListSources(ix.rootCtx)
	if err != nil {
		if ix.rootCtx.Err() == nil {
			slog.Error("list sources", "err", err)
		}
		return
	}
	for _, s := range srcs {
		if s.Enabled {
			ix.queueScan(scanRequest{sourceID: s.ID})
		}
	}
	ix.restartWatchers(srcs)
	ix.markChanged(core.Changes{Assets: true}) // source removal can change visibility without a scan
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
	ix.wakeStages()
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
		if ctx.Err() != nil {
			return false
		}
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
		case core.IsWithin(r.under, pending.under):
		case core.IsWithin(pending.under, r.under):
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
		HashWorkers: hashWorkers,
		OnIngested: func(int) {
			ix.markChanged(core.Changes{Assets: true}) // new assets, moves, revived files
			nudge(ix.wakeMeta)
		},
	}
	for {
		req, ok := ix.nextScan(ctx)
		if !ok {
			return
		}
		src, videos, err := ix.scanSettings(ctx, req)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			// Scanning with a wrong guess (videos off) would drop every video.
			slog.Error("can't read what to scan; retrying", "err", err)
			ix.queueScan(req)
			sleep(ctx, retryDelay)
			continue
		case src.ID == 0:
			continue // removed since it was queued
		}

		// A hold (organising moves files) has the turn until it lets go; the
		// request waits, and the changes made meanwhile are found as usual.
		select {
		case ix.scanTurn <- struct{}{}:
		case <-ctx.Done():
			return
		}
		if ix.onScanTurn != nil {
			ix.onScanTurn()
		}
		ix.mu.Lock()
		ix.scanning = true
		ix.mu.Unlock()
		ix.stale.Store(true)

		start := time.Now()
		res, err := sc.Scan(ctx, src, req.under, scanner.Options{
			Videos:   videos,
			SkipDirs: ix.skipDirs(),
		})
		<-ix.scanTurn
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			slog.Error("scan failed", "source", src.Path, "under", req.under, "err", err)
		default:
			slog.Info("scan done", "source", src.Path, "under", req.under, "seen", res.Seen,
				"changed", res.Changed, "new", res.NewAssets, "removed", res.Removed, "took", time.Since(start).Round(time.Millisecond))
			if req.under == "" {
				// Every file was readable just now: failures may have passed.
				if n, err := ix.store.RetryFailed(ctx, src.ID); err != nil {
					slog.Error("retry failed jobs", "source", src.Path, "err", err)
				} else if n > 0 {
					slog.Info("retrying failed jobs", "source", src.Path, "jobs", n)
					ix.wakeStages()
				}
			}
		}

		ix.mu.Lock()
		ix.scanning = len(ix.scanReqs) > 0
		ix.mu.Unlock()
		if res.Removed > 0 {
			ix.markChanged(core.Changes{Assets: true})
		}
		ix.stale.Store(true)
		nudge(ix.wakeMeta)
	}
}

// scanSettings returns the source a scan request is for (ID 0 if it is gone
// or disabled) and whether videos are included.
func (ix *Indexer) scanSettings(ctx context.Context, req scanRequest) (core.Source, bool, error) {
	srcs, err := ix.store.ListSources(ctx)
	if err != nil {
		return core.Source{}, false, err
	}
	i := slices.IndexFunc(srcs, func(s core.Source) bool { return s.ID == req.sourceID && s.Enabled })
	if i < 0 {
		return core.Source{}, false, nil
	}
	videos, err := ix.store.Bool(ctx, core.SettingVideos, false)
	return srcs[i], videos, err
}

// skipDirs are folders a scan leaves out: Lumen's own data.
func (ix *Indexer) skipDirs() []string { return []string{ix.paths.DataDir} }

// restartWatchers watches every enabled source for changes.
func (ix *Indexer) restartWatchers(srcs []core.Source) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.watchStop != nil {
		ix.watchStop()
	}
	if ix.rootCtx.Err() != nil {
		return // stopping
	}
	ctx, cancel := context.WithCancel(ix.rootCtx)
	ix.watchStop = cancel
	for _, s := range srcs {
		if s.Enabled {
			ix.watchers.Add(1)
			go func() {
				defer ix.watchers.Done()
				ix.watchLoop(ctx, s)
			}()
		}
	}
}

// watchLoop watches one source until ctx ends. A watch that can't start or
// stops (a drive unplugged, a share gone) is retried with backoff; once it
// runs again the source is rescanned, since changes went unseen meanwhile.
func (ix *Indexer) watchLoop(ctx context.Context, src core.Source) {
	wait := watchRetry
	failed := false
	for {
		err := scanner.Watch(ctx, src.Path, func() {
			if failed {
				slog.Info("watching folder again", "dir", src.Path)
				ix.queueScan(scanRequest{sourceID: src.ID})
				failed, wait = false, watchRetry
			}
		}, ix.onFSChange)
		if ctx.Err() != nil {
			return
		}
		if !failed {
			slog.Warn("folder watch stopped; retrying", "dir", src.Path, "err", err)
		}
		failed = true
		sleep(ctx, wait)
		wait = min(2*wait, watchRetryMax)
	}
}

// onFSChange collects changed paths and turns them into scans once things go
// quiet for a moment, so copying 500 photos in causes one scan, not 500.
func (ix *Indexer) onFSChange(path string) {
	if core.IsWithin(path, ix.paths.DataDir) {
		return
	}
	if fileops.IsSidecarTemp(path) {
		// Lumen's own sidecar on its way into place: it is made, then renamed
		// away, and the vanished name would look like a deleted folder.
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
		return // stopping
	}
	if len(ix.pendingFS) == 0 {
		ix.fsSince = time.Now()
	}
	ix.pendingFS[path] = true
	ix.fsDue = time.Now().Add(debounce)
	if latest := ix.fsSince.Add(debounceMax); latest.Before(ix.fsDue) {
		ix.fsDue = latest
	}
	nudge(ix.wakeFS)
}

// fsLoop owns the debounce timer so Stop also waits for a running flush.
func (ix *Indexer) fsLoop(ctx context.Context, flush func()) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ix.wakeFS:
			ix.mu.Lock()
			due := ix.fsDue
			pending := len(ix.pendingFS) > 0
			ix.mu.Unlock()
			if pending {
				timer.Reset(time.Until(due))
			} else {
				timer.Stop()
			}
		case <-timer.C:
			if ctx.Err() != nil {
				return
			}
			flush()
		}
	}
}

func (ix *Indexer) flushFSChanges() {
	ix.mu.Lock()
	changed := ix.pendingFS
	ix.pendingFS = map[string]bool{}
	ix.mu.Unlock()
	if ix.rootCtx.Err() != nil {
		return
	}

	srcs, err := ix.store.ListSources(ix.rootCtx)
	if err != nil {
		if ix.rootCtx.Err() == nil {
			// Keep the changes for the next try rather than lose them.
			slog.Error("list sources", "err", err)
			for p := range changed {
				ix.onFSChange(p)
			}
		}
		return
	}
	// Map each path to the folder to rescan, then drop folders inside others.
	dirs := map[int64]map[string]bool{}
	for p := range changed {
		dir := p
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			dir = filepath.Dir(p)
		}
		for _, s := range srcs {
			if !s.Enabled || !core.IsWithin(dir, s.Path) {
				continue
			}
			if at, byAttribute, excluded := scanner.Exclusion(s.Path, p, ix.skipDirs()); excluded {
				if !byAttribute {
					continue // e.g. the recycle bin, which scans always leave out
				}
				// Inside a folder hidden now: what the library has of it goes,
				// by a scan of the folder around it.
				dir = filepath.Dir(at)
			}
			if dirs[s.ID] == nil {
				dirs[s.ID] = map[string]bool{}
			}
			dirs[s.ID][dir] = true
		}
	}
	for _, s := range srcs {
		list := slices.Collect(maps.Keys(dirs[s.ID]))
		if len(list) == 0 {
			continue
		}
		// Shortest first, so a parent is kept before any folder inside it.
		slices.SortFunc(list, func(a, b string) int { return len(a) - len(b) })
		var keep []string
	outer:
		for _, d := range list {
			for _, k := range keep {
				if core.IsWithin(d, k) {
					continue outer // inside a folder we'll rescan anyway
				}
			}
			keep = append(keep, d)
		}
		for _, d := range keep {
			under := d
			if core.IsWithin(s.Path, d) {
				under = "" // the source root itself
			}
			ix.queueScan(scanRequest{sourceID: s.ID, under: under})
		}
	}
}

// reportLoop emits index:progress when it changes and library:changed at most
// once a second, so the UI stays live without being flooded. While nothing
// changes it does not touch the database. Counting jobs takes a while in a
// big library, so it spends at most a fifth of the time doing it.
func (ix *Indexer) reportLoop(ctx context.Context) {
	wait := time.Second
	visibility, err := ix.store.VisibilityRevision(ctx) // 0 on failure: at worst one extra reload
	if err != nil && ctx.Err() == nil {
		slog.Error("read visibility revision", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		bits := ix.changes.Swap(0)
		if stale := ix.stale.Swap(false); !stale && bits == 0 {
			continue
		}
		if bits != 0 {
			// New tags or faces can put photos under a block: then which
			// assets are visible changed too, which every timeline shows.
			if rev, err := ix.store.VisibilityRevision(ctx); err == nil {
				if rev != visibility {
					bits |= changedAssets
				}
				visibility = rev
			} else if ctx.Err() == nil {
				slog.Error("read visibility revision", "err", err)
			}
		}
		start := time.Now()
		p := ix.Progress(ctx)
		wait = max(time.Second, 4*time.Since(start))
		if p != ix.last {
			ix.last = p
			ix.events.Progress(p)
		}
		if bits != 0 {
			ix.events.LibraryChanged(core.Changes{
				Assets: bits&changedAssets != 0, Thumbs: bits&changedThumbs != 0,
				Tags: bits&changedTags != 0, People: bits&changedPeople != 0,
			})
		}
	}
}

// Progress is a snapshot of indexing state.
func (ix *Indexer) Progress(ctx context.Context) core.Progress {
	ix.mu.Lock()
	p := core.Progress{
		Scanning: ix.scanning, Paused: ix.paused,
		OnBattery: ix.onBattery && !ix.aiOnBattery, // narrowed below to the AI that is there
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
		// Only while it is on: what an upgrade queued while it was off waits
		// for the user to turn it on.
		if on, err := ix.store.Bool(ctx, core.SettingOCR, false); err == nil && on {
			p.OCR = pending[core.StageOCR]
		}
		p.OnBattery = p.OnBattery && (ix.search != nil || ix.people != nil || p.OCR > 0)
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
