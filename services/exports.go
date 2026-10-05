package services

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/media"
	"lumen/internal/store"
)

// PlanExport previews original copies or rendered exports at names nothing
// else has.
func (s *LibraryService) PlanExport(ctx context.Context, ids []int64, directory string, options core.ExportOptions) ([]core.ExportJob, error) {
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) == 0 || !filepath.IsAbs(directory) {
		return nil, store.ErrNotFound
	}
	if !slices.Contains([]core.ExportFormat{core.ExportOriginal, core.ExportJPEG, core.ExportPNG}, options.Format) {
		return nil, media.ErrUnsupported
	}
	rendered := options.Format != core.ExportOriginal
	options.Side = min(max(media.RenderSideMin, options.Side), media.RenderSide)
	options.Quality = min(max(1, options.Quality), 100)
	jobs := []core.ExportJob{}
	destinations := make(map[string]bool, len(ids))
	for _, id := range ids {
		f, err := s.store.AssetFile(ctx, id, false)
		if err != nil {
			return nil, err
		}
		path, ok := readablePath(f)
		if !ok {
			return nil, store.ErrNotFound
		}
		// A rendered export of a video is one frame, at a time inside it.
		frame := rendered && f.Kind == core.KindVideo
		if frame && (options.Frame < 0 || (f.Duration > 0 && options.Frame >= f.Duration)) {
			return nil, media.ErrUnsupported
		}
		fi, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(path)
		if rendered {
			name = strings.TrimSuffix(name, filepath.Ext(name))
			if frame {
				name += "-frame"
			}
			if options.Format == core.ExportJPEG {
				name += ".jpg"
			} else {
				name += ".png"
			}
		}
		// A name another selected file or the folder already has takes the
		// planner's " (2)", so one shared name never refuses the whole batch.
		to, err := fileops.FreeName(directory, name, destinations)
		if err != nil {
			return nil, err
		}
		if err := s.store.ExportAllowed(ctx, id, to); err != nil {
			return nil, err
		}
		destinations[core.Fold(to)] = true
		jobs = append(jobs, core.ExportJob{AssetID: id, From: path, To: to, Size: fi.Size(), MTime: fi.ModTime().UnixNano(), Options: options})
	}
	batch, err := s.store.SaveExportPlan(ctx, jobs)
	if err != nil {
		return nil, err
	}
	return s.store.VisibleExportJobs(ctx, batch)
}

// ExportJobs exposes visible per-file outcomes and bounded recent history.
func (s *LibraryService) ExportJobs(ctx context.Context, batch int64) ([]core.ExportJob, error) {
	return s.store.VisibleExportJobs(ctx, batch)
}

// RunExport uses the file-operation lifetime and admission owner, so exports
// cannot overlap moves or outlive shutdown. An interrupted batch can retry.
func (s *LibraryService) RunExport(ctx context.Context, batch int64) ([]core.ExportJob, error) {
	if batch < 1 {
		return nil, store.ErrNotFound
	}
	if err := s.organise.claim(-batch, core.OrganiseRunning); err != nil {
		return nil, err
	}
	defer s.organise.release(nil)
	done, ok := s.organise.track()
	if !ok {
		return nil, ErrOrganiseStopping
	}
	defer done()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.exports.Lock()
	s.exports.batch, s.exports.cancel = batch, cancel
	s.exports.Unlock()
	defer func() { s.exports.Lock(); s.exports.batch, s.exports.cancel = 0, nil; s.exports.Unlock() }()
	stop := context.AfterFunc(s.organise.lifetime(), cancel)
	defer stop()
	unhold, err := s.holdScans(ctx)
	if err != nil {
		return nil, err
	}
	defer unhold()
	runner := s.exportRunner()
	if err := runner.Run(ctx, batch); err != nil {
		return nil, err
	}
	return s.store.VisibleExportJobs(context.WithoutCancel(ctx), batch)
}

func (s *LibraryService) exportRunner() *fileops.ExportRunner {
	return &fileops.ExportRunner{Store: s.store, Render: s.exportPixels}
}

// CancelExport stops the admitted batch without cancelling other operations.
func (s *LibraryService) CancelExport(batch int64) {
	s.exports.Lock()
	defer s.exports.Unlock()
	if s.exports.batch == batch && s.exports.cancel != nil {
		s.exports.cancel()
	}
}

func (s *LibraryService) exportPixels(ctx context.Context, job core.ExportJob) ([]byte, error) {
	if s.media == nil {
		return nil, media.ErrUnsupported
	}
	f, err := s.store.AssetFile(ctx, job.AssetID, false)
	if err != nil {
		return nil, err
	}
	// The pixels are encoded once, after any adjustment: every further JPEG
	// generation would lose detail the chosen quality did not ask to lose.
	var im image.Image
	if f.Kind == core.KindVideo {
		frame, err := s.media.Decoder.FrameAt(ctx, job.From, job.Options.Frame, job.Options.Side)
		if err != nil {
			return nil, err
		}
		im = frame.RGBA()
	} else {
		path, _, err := s.media.RenderFile(ctx, job.AssetID, job.From, f.Orientation, job.Options.Side)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		im, err = jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
	}
	if job.Options.Adjust {
		a, err := s.store.Adjustment(ctx, job.AssetID, false)
		if err != nil {
			return nil, err
		}
		im = media.Adjust(im, a)
	}
	var out bytes.Buffer
	if job.Options.Format == core.ExportPNG {
		err = png.Encode(&out, im)
	} else {
		err = jpeg.Encode(&out, im, &jpeg.Options{Quality: job.Options.Quality})
	}
	return out.Bytes(), err
}
