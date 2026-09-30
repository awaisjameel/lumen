package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sync/atomic"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/parallel"
	"lumen/internal/search"
	"lumen/internal/store"
)

// stage describes one step of the pipeline for runStage.
type stage[R any] struct {
	name  core.Stage
	batch int             // jobs fetched per round
	wake  chan struct{}   // nudged when new work may exist
	next  []chan struct{} // nudged after a save: the stages that wait for this one
	model releaser        // the model the stage runs, freed when idle or held; may be nil
	// changes is what a saved batch changes in the library, for the UI.
	changes core.Changes
	// unchanged, if set, reports a result that only records the new version
	// (see upgrade.go): a batch of nothing else changes nothing to show.
	unchanged func(R) bool
	// fail, if set, shows why the stage can't run ("" once it runs again).
	fail func(msg string)
	// process handles a batch. An error means the stage itself can't run
	// right now (e.g. the model won't load): nothing is saved and the batch
	// is retried later, with the model freed; a storeError (the database
	// failed) is retried soon, keeping it. Per-file failures go in the
	// results instead.
	process func(context.Context, []store.JobItem) ([]R, error)
	save    func(context.Context, []R) error
	fetch   func(context.Context, int) ([]store.JobItem, error)
}

// storeError is a process error from the database rather than the stage's
// model: nothing the AI state should show, and no reason to free the model.
type storeError struct{ error }

func (e storeError) Unwrap() error { return e.error }

// modelError is a model's failure while processing one file: it fails the
// whole batch (see stage.process), since every other file would fail too.
type modelError struct{ error }

func (e modelError) Unwrap() error { return e.error }

// saveAttempts is how many times a batch's results are saved before they are
// given up and the batch is fetched (and processed) again.
const saveAttempts = 3

// releaser frees a model (see model).
type releaser interface {
	releaseIdle()
	release()
}

// A stage that can't run (its model fails) tries again after stageRetry,
// doubling up to stageRetryMax. idlePoll is how often an idle stage looks
// for work it wasn't woken for.
const (
	stageRetry    = 30 * time.Second
	stageRetryMax = 10 * time.Minute
	idlePoll      = 30 * time.Second
)

// runStage drains one stage's job queue: fetch a batch (what the user is
// looking at first, then newest first), process it, save the results in one
// transaction, repeat. With nothing to do it sleeps until woken. Batches are
// small so the UI sees results stream in and focus changes apply fast.
func runStage[R any](ctx context.Context, ix *Indexer, s stage[R]) {
	retry := stageRetry
	failing := false
	for {
		if !ix.waitTurn(ctx, s.name, s.model) {
			return
		}
		fetch := s.fetch
		if fetch == nil {
			fetch = func(ctx context.Context, n int) ([]store.JobItem, error) { return ix.fetch(ctx, s.name, n) }
		}
		items, err := fetch(ctx, s.batch)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("fetch jobs", "stage", s.name, "err", err)
			sleep(ctx, retryDelay)
			continue
		}
		if len(items) == 0 {
			if s.model != nil {
				s.model.releaseIdle()
			}
			select {
			case <-ctx.Done():
				return
			case <-s.wake:
			case <-time.After(idlePoll):
			}
			continue
		}
		results, err := s.process(ctx, items)
		if ctx.Err() != nil {
			return // don't save half-cancelled work; it simply re-runs next time
		}
		var serr storeError
		if errors.As(err, &serr) {
			slog.Error("stage can't read the library", "stage", s.name, "err", err)
			sleep(ctx, retryDelay)
			continue
		}
		if err != nil {
			if len(results) > 0 && saveResults(ctx, s, results) {
				ix.markChanged(s.changes)
			}
			slog.Error("stage can't run", "stage", s.name, "err", err, "retry in", retry)
			if s.model != nil {
				s.model.release() // a model that failed is loaded afresh next time
			}
			if s.fail != nil {
				s.fail(err.Error())
			}
			failing = true
			sleep(ctx, retry)
			retry = min(2*retry, stageRetryMax)
			continue
		}
		if !saveResults(ctx, s, results) {
			continue // fetched and processed again
		}
		if failing {
			failing, retry = false, stageRetry
			if s.fail != nil {
				s.fail("")
			}
		}
		if s.unchanged == nil || !all(results, s.unchanged) {
			ix.markChanged(s.changes)
		}
		for _, ch := range s.next {
			nudge(ch)
		}
	}
}

// saveResults saves a processed batch, retrying a failed save a few times
// before giving the results up: processing them again costs far more.
func saveResults[R any](ctx context.Context, s stage[R], results []R) bool {
	for attempt := 1; ; attempt++ {
		err := s.save(ctx, results)
		if err == nil {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		slog.Error("save results", "stage", s.name, "attempt", attempt, "err", err)
		sleep(ctx, retryDelay)
		if attempt == saveAttempts {
			return false
		}
	}
}

// eachItem turns a per-file function into a batch processor that runs it on
// a pool of workers. On shutdown it only finishes the files already started.
func eachItem[R any](workers int, fn func(context.Context, store.JobItem) R) func(context.Context, []store.JobItem) ([]R, error) {
	return func(ctx context.Context, items []store.JobItem) ([]R, error) {
		results := make([]R, len(items))
		parallel.For(ctx, len(items), workers, func(i int) { results[i] = fn(ctx, items[i]) })
		return results, nil
	}
}

// all reports whether f holds for every one of rs.
func all[R any](rs []R, f func(R) bool) bool {
	for _, r := range rs {
		if !f(r) {
			return false
		}
	}
	return true
}

func (ix *Indexer) fetch(ctx context.Context, stage core.Stage, n int) ([]store.JobItem, error) {
	if stage == core.StageFaces {
		// Photos the thumbnail stage decoded for faces go first, before
		// they are dropped.
		if ids := ix.pixels.ids(); len(ids) > 0 {
			items, err := ix.store.PendingJobsOf(ctx, stage, ids[:min(len(ids), n)])
			if err != nil || len(items) > 0 {
				return items, err
			}
		}
	}
	ix.mu.Lock()
	focus := ix.focus
	ix.mu.Unlock()
	if focus != nil {
		items, err := ix.pendingJobs(ctx, stage, focus, n)
		if err != nil || len(items) > 0 {
			return items, err
		}
	}
	return ix.pendingJobs(ctx, stage, nil, n)
}

func (ix *Indexer) pendingJobs(ctx context.Context, stage core.Stage, within *store.TimeRange, n int) ([]store.JobItem, error) {
	if stage == core.StageEmbedding || stage == core.StageFaces {
		return ix.store.PendingJobsKind(ctx, stage, core.KindPhoto, within, n)
	}
	return ix.store.PendingJobs(ctx, stage, within, n)
}

func (ix *Indexer) doMetadata(ctx context.Context, j store.JobItem) (r store.MetadataResult) {
	r.AssetID = j.AssetID
	defer recoverInto(&r.Err, j.Path)
	m, err := ix.cache.Decoder.Metadata(ctx, j.Path, j.Kind)
	if errors.Is(err, media.ErrUnsupported) {
		r.Skipped = true
		return r
	}
	if err != nil {
		r.Err = err.Error()
		return r
	}
	if !m.TakenAt.IsZero() {
		r.TakenAt = core.WallClock(m.TakenAt)
	}
	if !m.Modified.IsZero() {
		r.Modified = core.WallClock(m.Modified)
	}
	r.Width, r.Height, r.Orientation, r.Camera, r.Lat, r.Lon = m.Width, m.Height, m.Orientation, m.Camera, m.Lat, m.Lon
	r.Duration, r.VideoCodec, r.AudioCodec = m.Duration, m.VideoCodec, m.AudioCodec
	return r
}

// thumbnailWorkers is how many files the thumbnail stage decodes at once:
// half the CPU's threads (libvips runs one per file), at least 6 where there
// are more, leaving one for the UI. Six on 20 threads left most of them idle.
var thumbnailWorkers = max(1, min(runtime.NumCPU()-1, max(6, runtime.NumCPU()/2)))

// doThumbnails makes a batch's thumbnails. Photos the faces stage will want
// next are decoded at faceSide and kept for it (see decoded), with the
// thumbnails made from those pixels.
func (ix *Indexer) doThumbnails(ctx context.Context, items []store.JobItem) ([]store.ThumbResult, error) {
	keep := ix.keepForFaces(ctx, items)
	return eachItem(thumbnailWorkers, func(ctx context.Context, j store.JobItem) store.ThumbResult {
		return ix.doThumbnail(ctx, j, keep[j.AssetID])
	})(ctx, items)
}

// keepForFaces returns the photos of items whose faces are still to find,
// while the faces stage can run.
func (ix *Indexer) keepForFaces(ctx context.Context, items []store.JobItem) map[int64]bool {
	ix.mu.Lock()
	off := ix.people == nil || ix.facesError != "" || ix.held(core.StageFaces)
	ix.mu.Unlock()
	if off {
		return nil
	}
	var ids []int64
	for _, j := range items {
		if j.Kind == core.KindPhoto {
			ids = append(ids, j.AssetID)
		}
	}
	keep, err := ix.store.Unfinished(ctx, core.StageFaces, ids)
	if err != nil {
		slog.Error("find photos waiting for faces", "err", err)
		return nil
	}
	return keep
}

func (ix *Indexer) doThumbnail(ctx context.Context, j store.JobItem, keep bool) (r store.ThumbResult) {
	r.AssetID = j.AssetID
	defer recoverInto(&r.Err, j.Path)
	if j.Prior != "" && thumbnailsStand(j) {
		r.Unchanged = true
		return r
	}
	var t media.Thumbs
	var err error
	if keep && ix.pixels.room() {
		t, _ = ix.thumbnailForFaces(ctx, j)
	}
	if t.Large == nil {
		// Not kept, or that failed: the usual way, whose result or error
		// is the file's.
		t, err = ix.cache.Decoder.Thumbnail(ctx, j.Path, j.Kind, j.Orientation)
	}
	switch {
	case errors.Is(err, media.ErrUnsupported):
		r.Skipped = true
		return r
	case err != nil:
		r.Err = err.Error()
		return r
	}
	if err := ix.cache.Write(j.AssetID, t); err != nil {
		r.Err = "write thumbnail: " + err.Error()
		return r
	}
	r.PHash = &t.PHash
	return r
}

// thumbnailForFaces decodes a photo at faceSide, keeps it for the faces
// stage and makes the thumbnails from it.
func (ix *Indexer) thumbnailForFaces(ctx context.Context, j store.JobItem) (media.Thumbs, error) {
	im, err := ix.cache.Decoder.Decode(ctx, j.Path, j.Kind, j.Orientation, faceSide)
	if err != nil {
		return media.Thumbs{}, err
	}
	t, err := media.ThumbsFromImage(im)
	if err == nil {
		ix.pixels.put(j.AssetID, j.Orientation, im)
	}
	return t, err
}

// embedWorkers is how many photos have their image model input (read from
// the thumbnail) prepared at once.
const embedWorkers = 4

// doEmbeddings embeds and tags photos from their upright 1024 px thumbnails,
// rebuilt first if missing. The video coordinator batches sampled frames separately.
func (ix *Indexer) doEmbeddings(ctx context.Context, items []store.JobItem) ([]store.EmbedResult, error) {
	if err := ix.search.Wait(ctx); err != nil {
		return nil, fmt.Errorf("search engine: %w", err)
	}
	enc, done, err := ix.encoder.acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	generation := ix.search.LabelsGeneration()
	side := ix.search.Model().ImageSize
	results := make([]store.EmbedResult, len(items))
	inputs := make([][]byte, len(items))
	parallel.For(ctx, len(items), embedWorkers, func(i int) {
		j := items[i]
		r := &results[i]
		r.AssetID = j.AssetID
		r.LabelsGeneration = generation
		defer recoverInto(&r.Err, j.Path)
		if j.Kind != core.KindPhoto {
			return
		}
		orig := media.Original{Path: j.Path, Kind: j.Kind, Orientation: j.Orientation}
		switch err := ix.cache.Ensure(ctx, j.AssetID, core.ThumbLarge, orig); {
		case errors.Is(err, media.ErrUnsupported):
			r.Skipped = true
			return
		case err != nil:
			r.Err = "thumbnail: " + err.Error()
			return
		}
		px, err := media.ModelInput(ix.cache.Path(j.AssetID, core.ThumbLarge), side)
		if err != nil {
			r.Err = err.Error()
			return
		}
		inputs[i] = px
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	b := &modelBatch{enc: enc}
	for i, in := range inputs {
		if in == nil {
			continue
		}
		r := &results[i]
		if err := b.add(in, func(v []float32) {
			if v == nil {
				r.Err = errNoVector
				return
			}
			r.Vec, r.Scale = search.Quantize(v)
			r.Tags = ix.search.Tags(v)
		}); err != nil {
			return nil, err
		}
	}
	if err := b.flush(); err != nil {
		return nil, err
	}
	ix.setAIState(func() { ix.aiDevice = enc.Device() }) // it may have moved to the CPU
	return results, nil
}

// errNoVector is the per-file error for an input the image model made no
// usable vector of.
const errNoVector = "the image model gave no usable result"

// modelBatch gathers image model inputs from any number of files and runs
// the model whenever a full batch is waiting, handing each vector to the
// input's owner. At most one batch of inputs is held at a time.
type modelBatch struct {
	enc    *ai.ImageEncoder
	inputs [][]byte
	owners []func(vec []float32) // vec is nil where the model made no usable one
	err    error                 // the model failed: the stage can't run
}

func (b *modelBatch) add(px []byte, owner func(vec []float32)) error {
	if b.err != nil {
		return b.err
	}
	b.inputs, b.owners = append(b.inputs, px), append(b.owners, owner)
	if len(b.inputs) < b.enc.BatchSize() {
		return nil
	}
	return b.flush()
}

func (b *modelBatch) flush() error {
	if b.err != nil || len(b.inputs) == 0 {
		return b.err
	}
	vecs, err := b.enc.Encode(b.inputs)
	if err != nil {
		b.err = err
		return err
	}
	for k, owner := range b.owners {
		owner(vecs[k])
	}
	b.inputs, b.owners = b.inputs[:0], b.owners[:0]
	return nil
}

// videoEmbedding collects one video's frame vectors as the model makes them.
type videoEmbedding struct {
	r       *store.EmbedResult
	tags    func(vec []float32) []store.TagScore // search.Engine.Tags
	mean    []float32
	sampled int // frames sent to the model
}

// frame keeps one sampled moment searchable, unless the video already failed
// or the model made nothing of that frame (it alone is left out).
func (v *videoEmbedding) frame(at int64, vec []float32) {
	if vec == nil || v.r.Err != "" || v.r.Skipped {
		return
	}
	q, scale := search.Quantize(vec)
	v.r.Frames = append(v.r.Frames, store.FrameEmbedding{At: at, Vec: q, Scale: scale})
	for d, x := range vec {
		v.mean[d] += x
	}
	v.r.Tags = search.BestTags(v.r.Tags, v.tags(vec)) // the best of its frames' tags
}

// finish stores the normalised mean of the frames as the asset's vector.
func (v *videoEmbedding) finish() {
	switch {
	case v.r.Err != "" || v.r.Skipped:
		v.r.Frames, v.r.Tags = nil, nil
	case v.sampled == 0:
		v.r.Skipped = true // no picture to look at
	case !ai.Normalise(v.mean):
		v.r.Frames, v.r.Tags, v.r.Err = nil, nil, errNoVector
	default:
		v.r.Vec, v.r.Scale = search.Quantize(v.mean)
	}
}

// saveEmbeddings stores a batch, then makes it searchable.
func (ix *Indexer) saveEmbeddings(ctx context.Context, rs []store.EmbedResult) error {
	if len(rs) == 0 {
		return nil
	}
	return ix.search.Commit(rs[0].LabelsGeneration, rs, func(rs []store.EmbedResult) error {
		return ix.store.SaveEmbeddingVersions(ctx, ix.search.Model().Version, ix.version[core.StageEmbedding], rs)
	})
}

// Face finding. Photos are decoded at faceSide for it: the detector sees the
// whole photo at 640 px, which finds faces down to a few percent of the
// frame, and at faceSide nearly all of them are big enough to recognise
// (Phase 3 measured 99.7% at 40 px or more on a folder of group photos).
const (
	faceSide     = 2048
	faceMinScore = 0.8 // detection confidence to keep a face
	faceMinSide  = 40  // px at faceSide; smaller faces are too blurry to recognise
	faceWorkers  = 4
	faceThreads  = 2 // CPU threads per face model run
)

// doFaces finds, aligns and recognises the faces in a batch of photos,
// except where those found before an upgrade stand (see facesStanding).
func (ix *Indexer) doFaces(ctx context.Context, items []store.JobItem) ([]store.FaceResult, error) {
	stand, err := ix.facesStanding(ctx, items)
	if err != nil {
		return nil, storeError{err}
	}
	results := make([]store.FaceResult, len(items))
	var run []int
	for i, j := range items {
		results[i].AssetID = j.AssetID
		if stand[j.AssetID] {
			results[i].Unchanged = true
			ix.pixels.take(j.AssetID, j.Orientation) // if the thumbnail stage kept it, it isn't wanted
		} else {
			run = append(run, i)
		}
	}
	if len(run) == 0 {
		return results, nil // without loading the models
	}
	faces, done, err := ix.faces.acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	var failed atomic.Pointer[modelError] // the models failed: the batch can't be done
	parallel.For(ctx, len(run), faceWorkers, func(k int) {
		j := items[run[k]]
		r := &results[run[k]]
		defer recoverInto(&r.Err, j.Path)
		// A bad image is this file's problem; any other failure of the
		// models is every file's.
		modelErr := func(err error) error {
			if err == nil {
				return nil
			}
			if errors.Is(err, ai.ErrBadImage) {
				return err
			}
			return modelError{err}
		}
		im, ok := ix.pixels.take(j.AssetID, j.Orientation)
		var err error
		if !ok {
			im, err = ix.cache.Decoder.Decode(ctx, j.Path, j.Kind, j.Orientation, faceSide)
		}
		if err == nil {
			err = modelErr(findFaces(faces, im, 0, r))
		}
		var merr modelError
		switch {
		case errors.As(err, &merr):
			failed.Store(&merr)
		case errors.Is(err, media.ErrUnsupported):
			r.Skipped = true
		case err != nil:
			r.Err = err.Error()
		}
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if merr := failed.Load(); merr != nil {
		return nil, merr.error
	}
	return results, nil
}

func findFaces(faces *ai.Faces, im media.Image, at int64, r *store.FaceResult) error {
	found, err := faces.Detect(im.Pix, im.W, im.H, faceMinScore)
	if err != nil {
		return err
	}
	found = slices.DeleteFunc(found, func(f ai.Face) bool { return min(f.W, f.H) < faceMinSide })
	ids, err := faces.Embed(im.Pix, im.W, im.H, found)
	if err != nil {
		return err
	}
	w, h := float64(im.W), float64(im.H)
	for k, f := range found {
		if ids[k].Vec != nil {
			r.Faces = append(r.Faces, store.DetectedFace{
				Frame: at, X: float32(f.X / w), Y: float32(f.Y / h), W: float32(f.W / w), H: float32(f.H / h),
				Score: f.Score, Sharpness: ids[k].Sharpness, Yaw: f.Yaw(), Vec: ids[k].Vec,
			})
		}
	}
	return nil
}

// saveFaces stores a batch, then lets the people engine group them.
func (ix *Indexer) saveFaces(ctx context.Context, rs []store.FaceResult) error {
	if err := ix.store.SaveFaceVersions(ctx, ix.people.Model().Version, ix.version[core.StageFaces], rs); err != nil {
		return err
	}
	if !all(rs, facesUnchanged) {
		ix.people.Nudge()
	}
	return nil
}

func facesUnchanged(r store.FaceResult) bool { return r.Unchanged }

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
