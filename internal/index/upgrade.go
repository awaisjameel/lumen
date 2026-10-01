package index

import (
	"context"
	"strings"
	"time"

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
	photo, video, ok := strings.Cut(j.Prior, "+ffmpeg-")
	if !ok {
		return false
	}
	if j.Kind == core.KindVideo {
		return "ffmpeg-"+video == media.VideoThumbnailVersion
	}
	return photo == media.ThumbnailVersion || media.DecodesAsAt(j.Path, photo)
}

// metadataStand keeps a photo's metadata unless this upgrade rejects its
// stored capture date. Videos read every tag again when ffprobe's part changes.
func metadataStand(j store.JobItem) bool {
	photo, video, ok := strings.Cut(j.Prior, "+ffprobe-")
	if !ok {
		return false
	}
	if j.Kind == core.KindVideo {
		return "ffprobe-"+video == media.VideoMetadataVersion
	}
	if photo == media.MetadataVersion {
		return true
	}
	if photo != media.EpochDatesMetadataVersion {
		return false
	}
	unix := core.WallClock(time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC))
	quicktime := core.WallClock(time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC))
	return j.TakenAt != unix && j.TakenAt != quicktime
}

// facesBeforeAnalysis is the faces version from before ai.FaceAnalysisVersion
// was part of it, for the current face models.
func (ix *Indexer) facesBeforeAnalysis() string {
	return ix.people.Model().Version + "+video-frames-1"
}

// facesStanding returns which of items' faces, found at their Prior version,
// are what the faces stage would find now. Analysis 2's output stands where
// the detector never enlarged the photo and its colour conversion is the
// same. Earlier analysis still needs its landmarks to update yaw and
// sharpness: only photos without faces can stand. Videos always run again.
func (ix *Indexer) facesStanding(ctx context.Context, items []store.JobItem) (map[int64]bool, error) {
	before := ix.facesBeforeAnalysis()
	analysis2 := ix.people.Model().Version + "+analysis-2"
	stand := map[int64]bool{}
	var ids []int64
	for _, j := range items {
		if j.Kind != core.KindPhoto {
			continue
		}
		prior := strings.TrimSuffix(j.Prior, "+video-frames-1")
		if prior == analysis2 {
			if facePixelsStand(j.Path, j.Width, j.Height, ix.people.Model().DetectSize, media.DecodesAsAt) {
				stand[j.AssetID] = true
			}
		} else if j.Prior == before {
			ids = append(ids, j.AssetID)
		}
	}
	if len(ids) == 0 {
		return stand, nil
	}
	found, err := ix.store.WithFaces(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, j := range items {
		if j.Kind == core.KindPhoto && j.Prior == before && !found[j.AssetID] &&
			media.DecodesAsAt(j.Path, media.UnmanagedThumbnailVersion) {
			stand[j.AssetID] = true
		}
	}
	return stand, nil
}

// facePixelsStand reports whether the face detector sees a photo of width x
// height at path as it did before analysis 3: not enlarged to detectSize
// (enlarging changed), and decoded as before HEIC colour profiles were
// converted.
func facePixelsStand(path string, width, height, detectSize int, decodesAsAt func(string, string) bool) bool {
	return width > 0 && height > 0 && detectSize > 0 && min(max(width, height), faceSide) >= detectSize &&
		decodesAsAt(path, media.HEIFProfilesVersion)
}
