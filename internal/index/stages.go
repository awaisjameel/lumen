package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
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
	// is retried later. Per-file failures go in the results instead.
	process func(context.Context, []store.JobItem) ([]R, error)
	save    func(context.Context, []R) error
}

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
		items, err := ix.fetch(ctx, s.name, s.batch)
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
		if err != nil {
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
		if err := s.save(ctx, results); err != nil {
			if ctx.Err() == nil {
				slog.Error("save results", "stage", s.name, "err", err)
			}
			sleep(ctx, retryDelay)
			continue
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
		items, err := ix.store.PendingJobs(ctx, stage, focus, n)
		if err != nil || len(items) > 0 {
			return items, err
		}
	}
	return ix.store.PendingJobs(ctx, stage, nil, n)
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

// doEmbeddings embeds a batch of photos and videos with SigLIP and tags
// them. Photo inputs come from the 1024 px thumbnails (upright, small,
// already decoded once), rebuilt first if they were evicted; videos are
// sampled frame by frame.
func (ix *Indexer) doEmbeddings(ctx context.Context, items []store.JobItem) ([]store.EmbedResult, error) {
	if err := ix.search.Wait(ctx); err != nil {
		return nil, fmt.Errorf("search engine: %w", err)
	}
	enc, err := ix.encoder.get()
	if err != nil {
		return nil, err
	}
	side := ix.search.Model().ImageSize
	results := make([]store.EmbedResult, len(items))
	inputs := make([][]byte, len(items))
	parallel.For(ctx, len(items), embedWorkers, func(i int) {
		j := items[i]
		r := &results[i]
		r.AssetID = j.AssetID
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

	// Run the model on full batches of whatever decoded.
	var todo []int
	for i, in := range inputs {
		if in != nil {
			todo = append(todo, i)
		}
	}
	for lo := 0; lo < len(todo); lo += enc.BatchSize() {
		chunk := todo[lo:min(lo+enc.BatchSize(), len(todo))]
		batch := make([][]byte, len(chunk))
		for k, i := range chunk {
			batch[k] = inputs[i]
		}
		vecs, err := enc.Encode(batch)
		if err != nil {
			return nil, err
		}
		for k, i := range chunk {
			if vecs[k] == nil {
				results[i].Err = errNoVector
				continue
			}
			results[i].Vec, results[i].Scale = search.Quantize(vecs[k])
			results[i].Tags = ix.search.Tags(vecs[k])
		}
	}
	for i, j := range items {
		if j.Kind == core.KindVideo {
			if err := ix.embedVideoSafely(ctx, j, enc, side, &results[i]); err != nil {
				return nil, err
			}
		}
	}
	ix.setAIState(func() { ix.aiDevice = enc.Device() }) // it may have moved to the CPU
	return results, nil
}

// errNoVector is the per-file error for an input the image model made no
// usable vector of.
const errNoVector = "the image model gave no usable result"

// embedVideoSafely is embedVideo, turning a panic (a corrupt file finding a
// decoder bug) into the video's error, as the per-file stages do.
func (ix *Indexer) embedVideoSafely(ctx context.Context, j store.JobItem, enc *ai.ImageEncoder, side int,
	r *store.EmbedResult) error {
	defer recoverInto(&r.Err, j.Path)
	return ix.embedVideo(ctx, j, enc, side, r)
}

// embedVideo keeps each sampled moment searchable and stores a normalised
// mean for the asset-level vector.
func (ix *Indexer) embedVideo(ctx context.Context, j store.JobItem, enc *ai.ImageEncoder, side int,
	r *store.EmbedResult) error {
	var inputs [][]byte
	var times []int64
	seen := map[int64]bool{}
	err := ix.cache.Decoder.Frames(ctx, j.Path, media.SampleCount(j.Duration), core.ThumbLarge, func(f media.Frame) error {
		if seen[f.At] {
			return nil
		}
		px, err := media.ModelInputPixels(f.Image, side)
		if err == nil {
			seen[f.At] = true
			inputs = append(inputs, px)
			times = append(times, f.At)
		}
		return err
	})
	switch {
	case errors.Is(err, media.ErrUnsupported):
		r.Skipped = true
		return nil
	case err != nil:
		r.Err = err.Error()
		//nolint:nilerr // per-file decoding failures are recorded in the job result
		return nil
	case len(inputs) == 0:
		r.Skipped = true
		return nil
	}
	mean := make([]float32, ix.search.Model().Dim)
	for lo := 0; lo < len(inputs); lo += enc.BatchSize() {
		vecs, err := enc.Encode(inputs[lo:min(lo+enc.BatchSize(), len(inputs))])
		if err != nil {
			return err
		}
		for k, v := range vecs {
			if v == nil {
				continue // that frame alone is left out
			}
			q, scale := search.Quantize(v)
			r.Frames = append(r.Frames, store.FrameEmbedding{At: times[lo+k], Vec: q, Scale: scale})
			for d, x := range v {
				mean[d] += x
			}
			r.Tags = search.BestTags(r.Tags, ix.search.Tags(v))
		}
	}
	if !ai.Normalise(mean) {
		r.Frames, r.Tags, r.Err = nil, nil, errNoVector
		return nil
	}
	r.Vec, r.Scale = search.Quantize(mean)
	return nil
}

// saveEmbeddings stores a batch, then makes it searchable.
func (ix *Indexer) saveEmbeddings(ctx context.Context, rs []store.EmbedResult) error {
	if err := ix.store.SaveEmbeddings(ctx, ix.search.Model().Version, ix.version[core.StageEmbedding], rs); err != nil {
		return err
	}
	for _, r := range rs {
		if r.Vec != nil {
			ix.search.Add(r.AssetID, r.Vec, r.Scale)
			if r.Frames != nil {
				ix.search.AddVideo(r.AssetID, r.Frames)
			}
		}
	}
	return nil
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
		return nil, err
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
	faces, err := ix.faces.get()
	if err != nil {
		return nil, err
	}
	parallel.For(ctx, len(run), faceWorkers, func(k int) {
		j := items[run[k]]
		r := &results[run[k]]
		defer recoverInto(&r.Err, j.Path)
		process := func(im media.Image, at int64) error {
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
				if ids[k].Vec == nil {
					continue // not recognisable: as if too small
				}
				r.Faces = append(r.Faces, store.DetectedFace{
					Frame: at,
					X:     float32(f.X / w), Y: float32(f.Y / h), W: float32(f.W / w), H: float32(f.H / h),
					Score: f.Score, Sharpness: ids[k].Sharpness, Yaw: f.Yaw(), Vec: ids[k].Vec,
				})
			}
			return nil
		}
		var err error
		if j.Kind == core.KindVideo {
			err = ix.cache.Decoder.Frames(ctx, j.Path, media.SampleCount(j.Duration), faceSide,
				func(f media.Frame) error { return process(f.Image, f.At) })
		} else {
			im, ok := ix.pixels.take(j.AssetID, j.Orientation)
			if !ok {
				im, err = ix.cache.Decoder.Decode(ctx, j.Path, j.Kind, j.Orientation, faceSide)
			}
			if err == nil {
				err = process(im, 0)
			}
		}
		if errors.Is(err, media.ErrUnsupported) {
			r.Skipped = true
		} else if err != nil {
			r.Err = err.Error()
		}
	})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return results, nil
}

// saveFaces stores a batch, then lets the people engine group them.
func (ix *Indexer) saveFaces(ctx context.Context, rs []store.FaceResult) error {
	if err := ix.store.SaveFaces(ctx, ix.people.Model().Version, ix.version[core.StageFaces], rs); err != nil {
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
