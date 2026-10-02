package index

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
	"lumen/internal/xmp"
)

// The XMP pass writes a sidecar (IMG_1.jpg.xmp) beside each file of the
// photos that have something to say: their Things, the people named in them,
// and a favourite mark. It is opt-in, runs in the background while the user
// has it on, and never opens an original: every write goes through fileops,
// to the sidecar's own path.
//
// What marks a photo for export is the store's XMP revision (migration 0027):
// a counter that moves whenever a tag, a person, a favourite, a file, the tag
// confidence or a block can have changed what some sidecar should say. A
// revision rather than a table of dirty photos because blocks and renames
// reach many photos at once, and a pass that compares hashes is right however
// the change came about: it works out each photo's packet and writes only
// those whose hash differs from the one recorded when Lumen last wrote it.
// The comparison needs no file reads, so a pass over a library where
// nothing changed costs queries only.
//
// Like the thumbnail stage it is not AI work, so a battery does not hold it;
// pausing does.

// ErrXMPOff means XMP sidecars were asked for while the setting is off.
var ErrXMPOff = errors.New("XMP sidecars are off")

// xmpBatch is the photos a pass reads and records at a time. Records are
// saved after each batch, so a crash leaves at most this many sidecars
// written but unrecorded (which later read as someone else's and are left
// alone).
const xmpBatch = 100

// xmpTiming is how the background pass paces itself.
type xmpTiming struct {
	cooldown time.Duration // the least between two passes, so indexing's writes don't start one each
	poll     time.Duration // how often the revision is looked at, as nothing wakes the pass for a tag or a rename
	retry    time.Duration // the wait after the database failed
}

var defaultXMPTiming = xmpTiming{cooldown: 15 * time.Second, poll: 15 * time.Second, retry: retryDelay}

// xmpState is the exporter's share of the Indexer. The zero value is ready.
type xmpState struct {
	mu       sync.Mutex // one pass at a time, background or asked for
	wakeOnce sync.Once
	wake     chan struct{} // nudged when the setting changes
}

// wakeCh is the wake channel, made on first use.
func (x *xmpState) wakeCh() chan struct{} {
	x.wakeOnce.Do(func() { x.wake = make(chan struct{}, 1) })
	return x.wake
}

// SetXMP turns writing XMP sidecars on or off and wakes the pass. Turning it
// off stops a pass that is running; sidecars already written stay.
func (ix *Indexer) SetXMP(ctx context.Context, on bool) error {
	if err := ix.store.SetXMP(ctx, on); err != nil {
		return err
	}
	nudge(ix.xmp.wakeCh())
	return nil
}

// ExportXMPNow runs a full pass at once: every sidecar is checked against the
// disk, not only the ones whose photos changed, so one the user deleted is
// made again. It runs even while indexing is paused, because the user asked
// for it. It is ErrXMPOff while the setting is off.
func (ix *Indexer) ExportXMPNow(ctx context.Context) (core.XMPExport, error) {
	on, err := ix.store.Bool(ctx, core.SettingXMP, false)
	if err != nil {
		return core.XMPExport{}, err
	}
	if !on {
		return core.XMPExport{}, ErrXMPOff
	}
	return ix.exportXMP(ctx, true, false)
}

// xmpLoop runs the background pass until ctx ends.
func (ix *Indexer) xmpLoop(ctx context.Context) { ix.runXMP(ctx, defaultXMPTiming) }

// runXMP exports when the setting is on and the revision has moved since the
// last pass, then waits to be woken or for the next look at the revision.
func (ix *Indexer) runXMP(ctx context.Context, tm xmpTiming) {
	var done store.XMPRevision
	haveDone := false // the first look after the start always exports: Lumen may have been closed mid-change
	for ctx.Err() == nil {
		switch on, err := ix.store.Bool(ctx, core.SettingXMP, false); {
		case err != nil:
			ix.xmpFailed(ctx, tm, "read the XMP setting", err)
			continue
		case !on:
			haveDone = false // turning it on again starts with a full look
		default:
			rev, err := ix.store.XMPRevision(ctx)
			if err != nil {
				ix.xmpFailed(ctx, tm, "read the XMP revision", err)
				continue
			}
			if haveDone && rev == done {
				break
			}
			if !ix.waitTurn(ctx, core.StageThumbnail, nil) {
				return
			}
			// rev was read before the pass, so a change made during it leaves
			// the revision ahead of done and starts the next pass.
			res, err := ix.exportXMP(ctx, false, true)
			if err != nil {
				ix.xmpFailed(ctx, tm, "export XMP sidecars", err)
				continue
			}
			done, haveDone = rev, true
			if res.Written > 0 || res.Skipped > 0 {
				slog.Info("xmp sidecars", "written", res.Written, "unchanged", res.Unchanged, "skipped", res.Skipped)
			}
			sleep(ctx, tm.cooldown)
		}
		select {
		case <-ix.xmp.wakeCh():
		case <-time.After(tm.poll):
		case <-ctx.Done():
		}
	}
}

// xmpFailed logs a failure of the background pass and waits before the next
// try. A cancelled context is shutdown, not a failure.
func (ix *Indexer) xmpFailed(ctx context.Context, tm xmpTiming, what string, err error) {
	if ctx.Err() != nil {
		return
	}
	slog.Error(what, "err", err)
	sleep(ctx, tm.retry)
}

// exportXMP writes the sidecars that are out of date and says what it did.
// With full it also reads each sidecar Lumen wrote from the disk, so one that
// went missing is made again; without it, a sidecar whose recorded hash is
// what it would write is taken as up to date. A background pass (held) waits
// while indexing is paused. A pass stops, with what it did so far, when the
// setting is turned off.
func (ix *Indexer) exportXMP(ctx context.Context, full, held bool) (core.XMPExport, error) {
	ix.xmp.mu.Lock()
	defer ix.xmp.mu.Unlock()
	var res core.XMPExport
	logged := 0 // failures logged in this pass, so an unwritable folder is not a flood
	for after := int64(0); ; {
		if held && !ix.waitTurn(ctx, core.StageThumbnail, nil) {
			return res, ctx.Err()
		}
		switch on, err := ix.store.Bool(ctx, core.SettingXMP, false); {
		case err != nil:
			return res, err
		case !on:
			return res, nil
		}
		assets, err := ix.store.XMPAssets(ctx, after, xmpBatch)
		if err != nil {
			return res, fmt.Errorf("read the photos to export: %w", err)
		}
		if len(assets) == 0 {
			return res, nil
		}
		after = assets[len(assets)-1].ID
		var records []store.XMPRecord
		var stop error
		for _, a := range assets {
			if stop = ctx.Err(); stop != nil {
				break
			}
			exportXMPAsset(ctx, a, full, &res, &records, &logged)
		}
		// What was written is recorded even when shutting down, or the next
		// run would find Lumen's own sidecars and leave them alone as foreign.
		if err := ix.store.SaveXMPExports(context.WithoutCancel(ctx), records); err != nil {
			return res, fmt.Errorf("record the sidecars written: %w", err)
		}
		if stop != nil {
			return res, stop
		}
	}
}

// exportXMPAsset brings the sidecars beside a's files up to date, adding to
// res what happened and to records what Lumen now holds.
func exportXMPAsset(ctx context.Context, a store.XMPAsset, full bool, res *core.XMPExport, records *[]store.XMPRecord, logged *int) {
	doc := xmpDoc(a)
	data := xmp.Marshal(doc)
	hash := fileops.SidecarHash(data)
	for _, file := range a.Files {
		// A photo with nothing to say gets no sidecar; one that has one of
		// Lumen's is brought back to the marker, so what it said is gone.
		if doc.Empty() && file.Wrote == nil {
			continue
		}
		if !full && bytes.Equal(file.Wrote, hash) {
			res.Unchanged++
			continue
		}
		path := file.Path + store.XMPSidecarSuffix
		outcome, err := fileops.WriteSidecar(ctx, path, data, file.Wrote)
		switch {
		case ctx.Err() != nil:
			return
		case err != nil:
			res.Skipped++
			if *logged < 10 {
				*logged++
				slog.Warn("write xmp sidecar", "path", path, "err", err)
			}
			continue
		}
		switch outcome {
		case fileops.SidecarCreated, fileops.SidecarReplaced:
			res.Written++
			*records = append(*records, store.XMPRecord{AssetID: a.ID, Path: path, Hash: hash})
		case fileops.SidecarSame:
			res.Unchanged++
			// Lumen's own sidecar that someone made equal to the new packet is
			// Lumen's again; one it never wrote stays unrecorded.
			if file.Wrote != nil && !bytes.Equal(file.Wrote, hash) {
				*records = append(*records, store.XMPRecord{AssetID: a.ID, Path: path, Hash: hash})
			}
		case fileops.SidecarForeign, fileops.SidecarEdited:
			res.Skipped++
		case fileops.SidecarFailed:
			res.Skipped++ // not returned without an error; counted rather than lost
		}
	}
}

// xmpDoc is the packet content for a. An asset that may no longer be exported
// gets the empty one.
func xmpDoc(a store.XMPAsset) xmp.Doc {
	if !a.Exportable {
		return xmp.Doc{}
	}
	d := xmp.Doc{Subjects: a.Tags, Favourite: a.Favourite, Width: a.Width, Height: a.Height}
	for _, f := range a.Faces {
		if a.Video {
			// A video's face is in one frame of it: the name is kept, the box is not.
			d.Names = append(d.Names, f.Name)
			continue
		}
		d.Faces = append(d.Faces, xmp.Face{Name: f.Name, X: f.X, Y: f.Y, W: f.W, H: f.H})
	}
	return d
}
