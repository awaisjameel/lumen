package index

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/parallel"
	"lumen/internal/store"
)

// The OCR stage reads the text in screenshots and documents (the photos the
// image model took for one, see store.ocrEligible) when the user has turned
// it on.
//
// It decodes the original, not the 1024 px thumbnail: the small text of a
// phone screen or a scanned page is 10 to 20 px high at full size and
// unreadable once a 2400 px screenshot is shrunk to 1024. ocrSide is where
// Phase 0 measured it (2560, "accurate, including small UI text"); the
// detector shrinks what it looks at to 960 itself and the recogniser reads
// each line from these pixels. Screenshots are PNGs, which decode quickly.
//
// A batch is ocrBatch photos: about half a second each on the CPU, so a
// batch lasts a few seconds. That is how soon the stage notices a pause, the
// battery, a changed focus or the setting turned off, and how much a
// cancelled batch costs. Two workers overlap one photo's decoding and
// finding of lines with another's recognition; the models' own threads
// (ocrThreads) do the arithmetic.
const (
	ocrBatch   = 8
	ocrWorkers = 2
	ocrSide    = 2560
)

// ocrThreads is the CPU threads of each OCR model run: a quarter of the
// machine's, at least two. The text models are background work that has no
// GPU to wait for, and the UI, the decoder and the other stages need the
// rest.
var ocrThreads = max(2, runtime.NumCPU()/4)

// textReader reads the text in an image: *ai.OCR, or a test's.
type textReader interface {
	Read(ctx context.Context, pix []byte, w, h int) ([]ai.OCRLine, error)
	Close()
}

// openOCR is the OCR model holder, which loads the models only while the
// stage has work (see model).
func (ix *Indexer) openOCR() *model[textReader] {
	return &model[textReader]{
		name: "text models", idle: modelIdle,
		open:   func() (textReader, error) { return ai.OpenOCR(ix.paths.OCR(), ocrThreads) },
		close:  textReader.Close,
		report: func(textReader, bool, error) {}, // the models' failure is logged by the stage; the AI state has no place for it
	}
}

// SetOCR turns reading the text in screenshots and documents on or off, and
// wakes the stage to start or stop at once.
func (ix *Indexer) SetOCR(ctx context.Context, on bool) error {
	if err := ix.store.SetOCR(ctx, on); err != nil {
		return err
	}
	nudge(ix.wakeOCR)
	ix.stale.Store(true) // the progress counts it, or no longer does
	return nil
}

// fetchOCR is the OCR stage's queue: nothing while the user has it off, the
// queue's own leftovers (an upgrade or a retry can queue while it is off)
// waiting for it to be turned on.
func (ix *Indexer) fetchOCR(ctx context.Context, n int) ([]store.JobItem, error) {
	on, err := ix.store.Bool(ctx, core.SettingOCR, false)
	if err != nil || !on {
		return nil, err
	}
	for {
		items, err := ix.fetch(ctx, core.StageOCR, n)
		if err != nil || len(items) == 0 {
			return nil, err
		}
		// A queued photo may have stopped being a screenshot or a document
		// since: those jobs are dropped, and the next ones fetched.
		if kept, err := ix.store.KeepEligibleOCR(ctx, items); err != nil || len(kept) > 0 {
			return kept, err
		}
	}
}

// QueueOCR queues the photos that have just become screenshots or documents
// (the user changed the tag confidence, or tagged a photo), and wakes the
// stage. It does nothing while reading text is off.
func (ix *Indexer) QueueOCR(ctx context.Context) error {
	n, err := ix.store.QueueOCR(ctx)
	if n > 0 {
		nudge(ix.wakeOCR)
		ix.stale.Store(true)
	}
	return err
}

// doOCR reads the text of a batch of photos.
func (ix *Indexer) doOCR(ctx context.Context, items []store.JobItem) ([]store.OCRResult, error) {
	reader, done, err := ix.ocr.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	results := make([]store.OCRResult, len(items))
	var failed atomic.Pointer[modelError] // the models failed: the batch can't be done
	parallel.For(ctx, len(items), ocrWorkers, func(i int) {
		j := items[i]
		r := &results[i]
		r.AssetID = j.AssetID
		defer recoverInto(&r.Err, j.Path)
		im, err := ix.cache.Decoder.Decode(ctx, j.Path, j.Kind, j.Orientation, ocrSide)
		switch {
		case errors.Is(err, media.ErrUnsupported):
			r.Skipped = true
			return
		case err != nil:
			r.Err = err.Error()
			return
		}
		lines, err := reader.Read(ctx, im.Pix, im.W, im.H)
		switch {
		case ctx.Err() != nil:
		case errors.Is(err, ai.ErrBadImage), errors.Is(err, ai.ErrBadOutput):
			r.Err = err.Error() // this photo's problem; the next may read
		case err != nil:
			failed.Store(&modelError{err})
		default:
			r.Text = joinLines(lines)
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

// joinLines is the text of lines, one to a line.
func joinLines(lines []ai.OCRLine) string {
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.Text
	}
	return strings.Join(texts, "\n")
}

// saveOCR stores a batch of text. The photos with text are searchable at
// once, so the progress is refreshed with the library.
func (ix *Indexer) saveOCR(ctx context.Context, rs []store.OCRResult) error {
	if err := ix.store.SaveOCR(ctx, ix.version[core.StageOCR].Photo, rs); err != nil {
		return err
	}
	ix.stale.Store(true)
	return nil
}
