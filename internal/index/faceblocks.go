package index

import (
	"context"

	"lumen/internal/core"
	"lumen/internal/store"
)

// faceBlockJobs returns up to n runnable faces jobs of the photos that hold a
// blocked face's anchor. A face model upgrade re-runs the stage over the whole
// library, and a block's look-alikes are only covered at the new model once its
// anchor has been found again with a vector of that model (see
// store.SaveFaceVersions), so these few photos go before the rest: the sooner
// they are done, the sooner every face saved after them is judged by the block.
// Videos are left to their own stage.
func (ix *Indexer) faceBlockJobs(ctx context.Context, n int) ([]store.JobItem, error) {
	ids, err := ix.store.FaceBlockAssets(ctx)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	items, err := ix.store.PendingJobsOf(ctx, core.StageFaces, ids)
	if err != nil {
		return nil, err
	}
	photos := items[:0]
	for _, j := range items {
		if j.Kind == core.KindPhoto && len(photos) < n {
			photos = append(photos, j)
		}
	}
	return photos, nil
}
