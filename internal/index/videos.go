package index

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

type videoModels struct {
	encoder *model[*ai.ImageEncoder]
	faces   *model[*ai.Faces]
}

func (m videoModels) releaseIdle() {
	if m.encoder != nil {
		m.encoder.releaseIdle()
	}
	if m.faces != nil {
		m.faces.releaseIdle()
	}
}

func (m videoModels) release() {
	if m.encoder != nil {
		m.encoder.release()
	}
	if m.faces != nil {
		m.faces.release()
	}
}

type videoResults struct {
	embeddings []store.EmbedResult
	faces      []store.FaceResult
}

func (ix *Indexer) fetchVideos(ctx context.Context, n int) ([]store.JobItem, error) {
	ix.mu.Lock()
	focus := ix.focus
	ix.mu.Unlock()
	fetch := func(within *store.TimeRange) ([]store.JobItem, error) {
		var items []store.JobItem
		for _, stage := range []core.Stage{core.StageEmbedding, core.StageFaces} {
			if (stage == core.StageEmbedding && ix.search == nil) || (stage == core.StageFaces && ix.people == nil) {
				continue
			}
			jobs, err := ix.store.PendingJobsKind(ctx, stage, core.KindVideo, within, n)
			if err != nil {
				return nil, err
			}
			items = append(items, jobs...)
		}
		slices.SortFunc(items, func(a, b store.JobItem) int {
			if taken := cmp.Compare(b.TakenAt, a.TakenAt); taken != 0 {
				return taken
			}
			return cmp.Compare(b.AssetID, a.AssetID)
		})
		items = slices.CompactFunc(items, func(a, b store.JobItem) bool { return a.AssetID == b.AssetID })
		return items[:min(n, len(items))], nil
	}
	if focus != nil {
		items, err := fetch(focus)
		if err != nil || len(items) > 0 {
			return items, err
		}
	}
	return fetch(nil)
}

func (ix *Indexer) doVideos(ctx context.Context, items []store.JobItem) ([]videoResults, error) {
	return ix.processVideos(ctx, items, ix.cache.Decoder.Frames)
}

type frameSampler func(context.Context, string, int, int, func(media.Frame) error) error

func (ix *Indexer) processVideos(ctx context.Context, items []store.JobItem, sample frameSampler) ([]videoResults, error) {
	ids := make([]int64, len(items))
	for i, j := range items {
		ids[i] = j.AssetID
	}
	embed, err := ix.store.Unfinished(ctx, core.StageEmbedding, ids)
	if err != nil {
		return nil, storeError{err}
	}
	face, err := ix.store.Unfinished(ctx, core.StageFaces, ids)
	if err != nil {
		return nil, storeError{err}
	}
	var enc *ai.ImageEncoder
	var faces *ai.Faces
	var embedErr, faceErr error
	if ix.search != nil && len(embed) > 0 {
		embedErr = ix.search.Wait(ctx)
		if embedErr == nil {
			var done func()
			enc, done, embedErr = ix.encoder.acquire()
			defer done()
		}
	}
	if ix.people != nil && len(face) > 0 {
		var done func()
		faces, done, faceErr = ix.faces.acquire()
		defer done()
	}
	var rs videoResults
	batch := &modelBatch{enc: enc}
	var videos []*videoEmbedding
	generation := uint64(0)
	if enc != nil {
		generation = ix.search.LabelsGeneration()
	}
	for _, j := range items {
		var v *videoEmbedding
		var r *store.FaceResult
		if enc != nil && embed[j.AssetID] {
			v = &videoEmbedding{r: &store.EmbedResult{AssetID: j.AssetID, LabelsGeneration: generation}, tags: ix.search.Tags, mean: make([]float32, ix.search.Model().Dim)}
			videos = append(videos, v)
		}
		if faces != nil && face[j.AssetID] {
			r = &store.FaceResult{AssetID: j.AssetID}
		}
		if v == nil && r == nil {
			continue
		}
		faceErr = ix.analyseVideo(ctx, j, sample, batch, v, faces, r, faceErr)
		if r != nil {
			rs.faces = append(rs.faces, *r)
		}
	}
	if enc != nil {
		if err := batch.flush(); err != nil {
			embedErr = err
		}
		for _, v := range videos {
			v.finish()
			rs.embeddings = append(rs.embeddings, *v.r)
		}
	}
	if embedErr != nil {
		rs.embeddings = nil
	}
	if faceErr != nil {
		rs.faces = nil
	}
	ix.setAIState(func() {
		if embedErr != nil {
			ix.aiError = embedErr.Error()
		} else if enc != nil {
			ix.aiError = ""
			ix.aiDevice = enc.Device()
		}
		if faceErr != nil {
			ix.facesError = faceErr.Error()
		} else if faces != nil {
			ix.facesError = ""
		}
	})
	if len(rs.embeddings) == 0 && len(rs.faces) == 0 {
		return nil, errors.Join(embedErr, faceErr)
	}
	return []videoResults{rs}, errors.Join(embedErr, faceErr)
}

func (ix *Indexer) analyseVideo(ctx context.Context, j store.JobItem, sample frameSampler, batch *modelBatch, v *videoEmbedding, faces *ai.Faces, r *store.FaceResult, faceErr error) error {
	var decodeErr string
	func() {
		defer recoverInto(&decodeErr, j.Path)
		seen := map[int64]bool{}
		err := sample(ctx, j.Path, media.SampleCount(j.Duration), faceSide, func(f media.Frame) error {
			if seen[f.At] {
				return nil
			}
			seen[f.At] = true
			if v != nil && batch.err == nil && v.r.Err == "" {
				px, err := media.ModelInputPixels(f.Image, ix.search.Model().ImageSize)
				if err != nil {
					v.r.Err = err.Error()
				} else {
					v.sampled++
					at := f.At
					_ = batch.add(px, func(vec []float32) { v.frame(at, vec) })
				}
			}
			if r != nil && faceErr == nil && r.Err == "" {
				if err := findFaces(faces, f.Image, f.At, r); err != nil {
					if errors.Is(err, ai.ErrBadImage) {
						r.Err = err.Error()
					} else {
						faceErr = err
					}
				}
			}
			return ctx.Err()
		})
		if errors.Is(err, media.ErrUnsupported) {
			if v != nil {
				v.r.Skipped = true
			}
			if r != nil {
				r.Skipped = true
			}
		} else if err != nil {
			decodeErr = err.Error()
		}
		if len(seen) == 0 && r != nil {
			r.Skipped = true
		}
	}()
	if decodeErr != "" {
		if v != nil {
			v.r.Err = decodeErr
		}
		if r != nil {
			r.Err = decodeErr
		}
	}
	return faceErr
}

func (ix *Indexer) saveVideos(ctx context.Context, batches []videoResults) error {
	for _, rs := range batches {
		if err := ix.saveEmbeddings(ctx, rs.embeddings); err != nil {
			return err
		}
		if len(rs.faces) > 0 {
			if err := ix.saveFaces(ctx, rs.faces); err != nil {
				return err
			}
		}
	}
	return nil
}
