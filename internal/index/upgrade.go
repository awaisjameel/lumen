package index

import (
	"context"
	"strings"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// An upgrade that changes a stage's version re-runs the stage for every
// finished job (store.RequeueOutdated), though most files come out as they
// were: at 100,000 photos that is about an hour of thumbnails and more of
// faces. The rules here say, for the earlier versions they know, which
// output still stands; the stage then records the new version without
// redoing the work. A job whose Prior no rule knows runs in full, so a
// version change the rules don't account for is always safe.

// thumbnailsStand reports whether j's thumbnails, made at j.Prior, are what
// the thumbnail stage would make now. Photos and videos have their own
// versions (see Indexer.versions): a video's stand if its part is the same,
// a photo's if its part is, or if it decodes as it did then.
func thumbnailsStand(j store.JobItem) bool {
	photo, ok := strings.CutSuffix(j.Prior, "+"+media.VideoThumbnailVersion)
	if !ok {
		return false
	}
	if j.Kind == core.KindVideo {
		return true
	}
	return photo == media.ThumbnailVersion || media.DecodesAsAt(j.Path, photo)
}

// facesBeforeAnalysis is the faces version from before ai.FaceAnalysisVersion
// was part of it, for the current face models.
func (ix *Indexer) facesBeforeAnalysis() string {
	return ix.people.Model().Version + media.VideoFramesVersion
}

// facesStanding returns which of items' faces, found at their Prior version,
// are what the faces stage would find now. Analysis 2 works out yaw and
// sharpness differently, and needs the landmarks (not stored) to do it, so
// every file with faces runs again. One where none were found still has none
// to find: the models are the same, and so are its pixels if it decodes as
// it did then (photos) or its frames are sampled the same way (videos:
// media.VideoFramesVersion).
func (ix *Indexer) facesStanding(ctx context.Context, items []store.JobItem) (map[int64]bool, error) {
	before := ix.facesBeforeAnalysis()
	var ids []int64
	for _, j := range items {
		if j.Prior == before {
			ids = append(ids, j.AssetID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	found, err := ix.store.WithFaces(ctx, ids)
	if err != nil {
		return nil, err
	}
	stand := map[int64]bool{}
	for _, j := range items {
		if j.Prior == before && !found[j.AssetID] &&
			(j.Kind == core.KindVideo || media.DecodesAsAt(j.Path, media.UnmanagedThumbnailVersion)) {
			stand[j.AssetID] = true
		}
	}
	return stand, nil
}
