package index

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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

// runStage drains one stage's job queue: fetch a batch (what the user is
// looking at first, then newest first), process it, save the results in one
// transaction, repeat. With nothing to do it sleeps until woken. Batches are
// small so the UI sees results stream in and focus changes apply fast.
func runStage[R any](ctx context.Context, ix *Indexer, s stage[R]) {
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
			sleep(ctx, 5*time.Second)
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
			case <-time.After(30 * time.Second):
			}
			continue
		}
		results, err := s.process(ctx, items)
		if ctx.Err() != nil {
			return // don't save half-cancelled work; it simply re-runs next time
		}
		if err != nil {
			slog.Error("stage can't run", "stage", s.name, "err", err)
			sleep(ctx, time.Minute)
			continue
		}
		if err := s.save(ctx, results); err != nil {
			slog.Error("save results", "stage", s.name, "err", err)
			sleep(ctx, 5*time.Second)
			continue
		}
		ix.changed.Store(true)
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
	r.Width, r.Height, r.Orientation, r.Camera, r.Lat, r.Lon = m.Width, m.Height, m.Orientation, m.Camera, m.Lat, m.Lon
	r.Duration, r.VideoCodec, r.AudioCodec = m.Duration, m.VideoCodec, m.AudioCodec
	return r
}

func (ix *Indexer) doThumbnail(ctx context.Context, j store.JobItem) (r store.ThumbResult) {
	r.AssetID = j.AssetID
	defer recoverInto(&r.Err, j.Path)
	t, err := ix.cache.Decoder.Thumbnail(ctx, j.Path, j.Kind, j.Orientation)
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

// embedWorkers is how many photos have their image model input (read from
// the thumbnail) prepared at once.
const embedWorkers = 4

// doEmbeddings embeds a batch of photos with SigLIP and tags them. Inputs
// come from the 1024 px thumbnails (upright, small, already decoded once),
// rebuilt first if they were evicted.
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
			results[i].Vec, results[i].Scale = search.Quantize(vecs[k])
			results[i].Tags = ix.search.Tags(vecs[k])
		}
	}
	for i, j := range items {
		if j.Kind == core.KindVideo {
			if err := ix.embedVideo(ctx, j, enc, side, &results[i]); err != nil {
				return nil, err
			}
		}
	}
	ix.setAIState(func() { ix.aiDevice = enc.Device() }) // it may have moved to the CPU
	return results, nil
}

// embedVideo keeps each sampled moment searchable and stores a normalised
// mean for the existing asset-level tag and search paths.
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
			q, scale := search.Quantize(v)
			r.Frames = append(r.Frames, store.FrameEmbedding{At: times[lo+k], Vec: q, Scale: scale})
			for d, x := range v {
				mean[d] += x
			}
			r.Tags = search.BestTags(r.Tags, ix.search.Tags(v))
		}
	}
	var norm float64
	for _, x := range mean {
		norm += float64(x) * float64(x)
	}
	if norm > 0 {
		for d := range mean {
			mean[d] /= float32(math.Sqrt(norm))
		}
	}
	r.Vec, r.Scale = search.Quantize(mean)
	return nil
}

// saveEmbeddings stores a batch, then makes it searchable.
func (ix *Indexer) saveEmbeddings(ctx context.Context, rs []store.EmbedResult) error {
	if err := ix.store.SaveEmbeddings(ctx, ix.search.Model().Version, rs); err != nil {
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

// doFaces finds, aligns and recognises the faces in a batch of photos.
func (ix *Indexer) doFaces(ctx context.Context, items []store.JobItem) ([]store.FaceResult, error) {
	faces, err := ix.faces.get()
	if err != nil {
		return nil, err
	}
	results := make([]store.FaceResult, len(items))
	parallel.For(ctx, len(items), faceWorkers, func(i int) {
		j := items[i]
		r := &results[i]
		r.AssetID = j.AssetID
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
			var im media.Image
			im, err = ix.cache.Decoder.Decode(ctx, j.Path, j.Kind, j.Orientation, faceSide)
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
	if err := ix.store.SaveFaces(ctx, ix.people.Model().Version, rs); err != nil {
		return err
	}
	ix.people.Nudge()
	return nil
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
