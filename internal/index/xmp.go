package index

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
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
// A sidecar is Lumen's only while the store has a record of it (its path and
// the hash of what was written). Anything else found at the path, and any
// sidecar that was edited since, is left alone and remembered as such, so a
// background pass does not read it again until the file changes.
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

// xmpRereads is how many times an asset is read again, because the revision
// moved since its batch was read, before Lumen writes what it has.
const xmpRereads = 1

// xmpTiming is how the background pass paces itself.
type xmpTiming struct {
	cooldown time.Duration // the least between two passes, so indexing's writes don't start one each
	poll     time.Duration // how often the revision is looked at, as nothing wakes the pass for a tag or a rename
	retry    time.Duration // the wait after the database failed
}

var defaultXMPTiming = xmpTiming{cooldown: 15 * time.Second, poll: 15 * time.Second, retry: retryDelay}

// xmpState is the exporter's share of the Indexer. The zero value is ready.
type xmpState struct {
	once sync.Once
	wake chan struct{} // nudged when the setting changes
	turn chan struct{} // holds a token while a batch is being exported: one at a time
}

func (x *xmpState) init() {
	x.once.Do(func() {
		x.wake = make(chan struct{}, 1)
		x.turn = make(chan struct{}, 1)
	})
}

// wakeCh is the wake channel.
func (x *xmpState) wakeCh() chan struct{} {
	x.init()
	return x.wake
}

// acquire waits for the turn to export a batch, or for ctx to end. A pass holds
// it for one batch at a time and never while it waits for a pause to lift, so
// Export now is never stuck behind a paused background pass.
func (x *xmpState) acquire(ctx context.Context) error {
	x.init()
	select {
	case x.turn <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (x *xmpState) release() { <-x.turn }

// xmpOptions say how a pass runs.
type xmpOptions struct {
	full  bool          // read each sidecar from the disk and ignore what was left alone
	held  bool          // wait while indexing is paused
	batch int           // photos at a time (xmpBatch if 0)
	write sidecarWriter // nil: fileops.WriteSidecar; a test's otherwise
}

// sidecarWriter is fileops.WriteSidecar.
type sidecarWriter func(ctx context.Context, path string, data, wrote []byte) (fileops.Sidecar, error)

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
// made again, and ones that were left alone are looked at again. It runs even
// while indexing is paused, because the user asked for it. It is ErrXMPOff
// while the setting is off. Stop cancels it and waits for it.
func (ix *Indexer) ExportXMPNow(ctx context.Context) (core.XMPExport, error) {
	return ix.exportNow(ctx, xmpOptions{full: true})
}

func (ix *Indexer) exportNow(ctx context.Context, opts xmpOptions) (core.XMPExport, error) {
	on, err := ix.store.Bool(ctx, core.SettingXMP, false)
	if err != nil {
		return core.XMPExport{}, err
	}
	if !on {
		return core.XMPExport{}, ErrXMPOff
	}
	ctx, done, err := ix.joinLifetime(ctx)
	if err != nil {
		return core.XMPExport{}, err
	}
	defer done()
	return ix.exportXMP(ctx, opts)
}

// Recycler is fileops.Recycle: it moves files to the Recycle Bin and says what
// happened to each.
type Recycler func(ctx context.Context, paths []string) ([]fileops.Outcome, error)

// DeleteXMP turns XMP sidecars off and moves every sidecar Lumen wrote to the
// Recycle Bin (recycle; nil means fileops.Recycle), so the user can take back
// everything Lumen put in their folders. Only a file Lumen has a record of,
// and that is still byte for byte what it wrote, goes: one edited since, and
// one the bin would not take, stays where it is and is counted as kept. It
// runs even while indexing is paused, because the user asked for it. Stop
// cancels it and waits for it; what went to the bin by then is forgotten
// either way.
func (ix *Indexer) DeleteXMP(ctx context.Context, recycle Recycler) (core.XMPDelete, error) {
	if recycle == nil {
		recycle = fileops.Recycle
	}
	// Off first: a pass that is running stops at its next batch, and none
	// starts to write the sidecars again.
	if err := ix.SetXMP(ctx, false); err != nil {
		return core.XMPDelete{}, err
	}
	ctx, done, err := ix.joinLifetime(ctx)
	if err != nil {
		return core.XMPDelete{}, err
	}
	defer done()
	var res core.XMPDelete
	for after := int64(0); ; {
		next, more, err := ix.deleteXMPBatch(ctx, recycle, after, &res)
		if err != nil || !more {
			return res, err
		}
		after = next
	}
}

// deleteXMPBatch recycles one batch of the sidecars recorded after the row
// after, holding the exporter's turn so that no pass writes meanwhile. more
// is false when there are none further.
func (ix *Indexer) deleteXMPBatch(ctx context.Context, recycle Recycler, after int64, res *core.XMPDelete) (next int64, more bool, err error) {
	if err := ix.xmp.acquire(ctx); err != nil {
		return 0, false, err
	}
	defer ix.xmp.release()
	owned, err := ix.store.XMPOwned(ctx, after, xmpBatch)
	if err != nil {
		return 0, false, fmt.Errorf("read the sidecars to delete: %w", err)
	}
	if len(owned) == 0 {
		return 0, false, nil
	}
	var changes store.XMPChanges
	var paths []string
	logged := 0
	for _, o := range owned {
		switch state, err := fileops.CheckSidecar(o.Path, o.Hash); state {
		case fileops.SidecarSame:
			paths = append(paths, o.Path)
		case fileops.SidecarGone:
			changes.Forget = append(changes.Forget, o.Path)
		default: // edited since, or unreadable: it stays, and so does its record
			res.Kept++
			if err != nil && logged < 10 {
				logged++
				slog.Warn("check xmp sidecar", "path", o.Path, "err", err)
			}
		}
	}
	var recycleErr error
	if len(paths) > 0 {
		var outcomes []fileops.Outcome
		outcomes, recycleErr = recycle(ctx, paths)
		for _, out := range outcomes {
			switch out.Status {
			case fileops.Recycled:
				res.Deleted++
				changes.Forget = append(changes.Forget, out.Path)
			case fileops.Gone:
				changes.Forget = append(changes.Forget, out.Path)
			case fileops.Kept, fileops.Failed:
				res.Kept++
				if out.Err != nil && logged < 10 {
					logged++
					slog.Warn("recycle xmp sidecar", "path", out.Path, "err", out.Err)
				}
			}
		}
	}
	// What went to the bin is forgotten even when shutting down, or its record
	// would outlive the file.
	if err := ix.store.SaveXMP(context.WithoutCancel(ctx), changes); err != nil {
		return 0, false, fmt.Errorf("forget the sidecars deleted: %w", err)
	}
	if recycleErr != nil {
		return 0, false, fmt.Errorf("move the sidecars to the Recycle Bin: %w", recycleErr)
	}
	return owned[len(owned)-1].Row, true, nil
}

// joinLifetime makes work a caller asked for part of the indexer's: ctx also
// ends when the indexer is stopped, and Stop waits until done is called. An
// indexer that has not been started (a test's) leaves ctx as it is.
func (ix *Indexer) joinLifetime(ctx context.Context) (context.Context, func(), error) {
	ix.mu.Lock()
	root := ix.rootCtx
	if root == nil {
		ix.mu.Unlock()
		return ctx, func() {}, nil
	}
	if err := root.Err(); err != nil {
		ix.mu.Unlock()
		return ctx, nil, err
	}
	ix.wg.Add(1) // under mu, while root lives: Stop cancels it, then takes mu, then waits
	ix.mu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(root, cancel)
	return ctx, func() { stop(); cancel(); ix.wg.Done() }, nil
}

// xmpLoop runs the background pass until ctx ends.
func (ix *Indexer) xmpLoop(ctx context.Context) { ix.runXMP(ctx, defaultXMPTiming) }

// runXMP exports when the setting is on and the revision has moved since the
// last pass, then waits to be woken or for the next look at the revision.
func (ix *Indexer) runXMP(ctx context.Context, tm xmpTiming) {
	var done store.XMPRevision
	haveDone := false // the first look after the start always exports: Lumen may have been closed mid-change
	for ctx.Err() == nil {
		on, err := ix.store.Bool(ctx, core.SettingXMP, false)
		if err != nil {
			ix.xmpFailed(ctx, tm, "read the XMP setting", err)
			continue
		}
		if !on {
			haveDone = false // turning it on again starts with a full look
		} else if rev, err := ix.store.XMPRevision(ctx); err != nil {
			ix.xmpFailed(ctx, tm, "read the XMP revision", err)
			continue
		} else if !haveDone || rev != done {
			// rev was read before the pass, so a change made during it leaves
			// the revision ahead of done and starts the next pass.
			res, err := ix.exportXMP(ctx, xmpOptions{held: true})
			if err != nil {
				ix.xmpFailed(ctx, tm, "export XMP sidecars", err)
				continue
			}
			done, haveDone = rev, true
			if res.Written > 0 {
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

// xmpPass is one pass over the library.
type xmpPass struct {
	ix      *Indexer
	opts    xmpOptions
	res     core.XMPExport
	changes store.XMPChanges // of the batch in hand, saved when it is done
	rev     store.XMPRevision
	logged  int // failures logged in this pass, so an unwritable folder is not a flood
}

// exportXMP writes the sidecars that are out of date and says what it did. A
// pass stops, with what it did so far, when the setting is turned off or ctx
// ends; what it had written by then is recorded either way.
func (ix *Indexer) exportXMP(ctx context.Context, opts xmpOptions) (core.XMPExport, error) {
	if opts.batch <= 0 {
		opts.batch = xmpBatch
	}
	if opts.write == nil {
		opts.write = fileops.WriteSidecar
	}
	p := &xmpPass{ix: ix, opts: opts}
	for after := int64(0); ; {
		next, more, err := p.assets(ctx, after)
		if err != nil || !more {
			return p.res, err
		}
		after = next
	}
}

// turn runs fn holding the exporter's turn, after any wait for a pause to lift
// (which is done without it). It reports false, with no error, when XMP export
// has been turned off.
func (p *xmpPass) turn(ctx context.Context, fn func() error) (on bool, err error) {
	if p.opts.held && !p.ix.waitTurn(ctx, core.StageThumbnail, nil) {
		return false, ctx.Err()
	}
	if err := p.ix.xmp.acquire(ctx); err != nil {
		return false, err
	}
	defer p.ix.xmp.release()
	switch on, err := p.ix.store.Bool(ctx, core.SettingXMP, false); {
	case err != nil:
		return false, err
	case !on:
		return false, nil
	}
	p.changes = store.XMPChanges{}
	err = fn()
	// What was done is recorded even when shutting down, or the next run would
	// find Lumen's own sidecars and leave them alone as someone else's.
	if serr := p.ix.store.SaveXMP(context.WithoutCancel(ctx), p.changes); serr != nil && err == nil {
		err = fmt.Errorf("record the sidecars written: %w", serr)
	}
	return err == nil, err
}

// assets exports one batch of photos after the id after, then (when the
// photos are done) the sidecars left behind. more is false when there is
// nothing further.
func (p *xmpPass) assets(ctx context.Context, after int64) (next int64, more bool, err error) {
	var count int
	on, err := p.turn(ctx, func() error {
		batch, err := p.ix.store.XMPAssets(ctx, after, p.opts.batch)
		if err != nil {
			return fmt.Errorf("read the photos to export: %w", err)
		}
		count = len(batch.Assets)
		if count == 0 {
			return nil
		}
		next, p.rev = batch.Assets[count-1].ID, batch.Revision
		for _, a := range batch.Assets {
			if err := ctx.Err(); err != nil {
				return err
			}
			p.exportAsset(ctx, a)
		}
		return nil
	})
	if err != nil || !on {
		return 0, false, err
	}
	if count > 0 {
		return next, true, nil
	}
	return 0, false, p.orphans(ctx)
}

// exportAsset brings the sidecars beside a's files up to date.
func (p *xmpPass) exportAsset(ctx context.Context, a store.XMPAsset) {
	paths := make([]string, len(a.Files))
	for i, f := range a.Files {
		paths[i] = f.Path
	}
	for _, path := range paths {
		if ctx.Err() != nil {
			return
		}
		p.exportFile(ctx, &a, path)
	}
}

// exportFile brings the sidecar beside one file of a up to date. Before it
// writes, it checks that nothing has changed since a was read (a block, a
// rename, a tag), and reads a again if so.
func (p *xmpPass) exportFile(ctx context.Context, a *store.XMPAsset, path string) {
	sidecar := path + store.XMPSidecarSuffix
	for reread := 0; ; reread++ {
		i := slices.IndexFunc(a.Files, func(f store.XMPFile) bool { return f.Path == path })
		if i < 0 {
			return // the file is no longer the asset's
		}
		file := a.Files[i]
		doc := xmpDoc(*a)
		data := xmp.Marshal(doc)
		hash := fileops.SidecarHash(data)
		switch {
		case doc.Empty() && file.Wrote == nil:
			// A photo with nothing to say gets no sidecar; one that has one of
			// Lumen's is brought back to the marker, so what it said is gone.
			return
		case !p.opts.full && bytes.Equal(file.Wrote, hash):
			p.res.Unchanged++
			return
		case !p.opts.full && file.Skip != nil && sidecarStamp(sidecar) == *file.Skip:
			p.res.Skipped++ // looked at before, and the file is as it was
			return
		}

		if reread < xmpRereads {
			cur, err := p.ix.store.XMPRevision(ctx)
			if err != nil {
				p.failed(ctx, "read the XMP revision", sidecar, err)
				return
			}
			if cur != p.rev {
				fresh, ok, err := p.ix.store.XMPAsset(ctx, a.ID)
				if err != nil {
					p.failed(ctx, "read a photo again", sidecar, err)
					return
				}
				if !ok {
					return // nothing to say any more, and no sidecar of Lumen's
				}
				*a = fresh
				continue
			}
		}

		outcome, err := p.opts.write(ctx, sidecar, data, file.Wrote)
		if err != nil {
			p.failed(ctx, "write xmp sidecar", sidecar, err)
			return
		}
		p.outcome(sidecar, outcome, file, a.ID, data, hash)
		return
	}
}

// outcome records what writing a sidecar did.
func (p *xmpPass) outcome(sidecar string, outcome fileops.Sidecar, file store.XMPFile, asset int64, data, hash []byte) {
	record := store.XMPRecord{AssetID: asset, Path: sidecar, Hash: hash}
	switch outcome {
	case fileops.SidecarCreated, fileops.SidecarReplaced:
		p.res.Written++
		p.changes.Records = append(p.changes.Records, record)
	case fileops.SidecarSame:
		p.res.Unchanged++
		// A file that is byte for byte what Lumen would write, marker and all,
		// is Lumen's: its own sidecar that lost its record (a crash, or a move
		// the user made in Explorer), or one that someone made equal to the new
		// packet. Nothing else is ever claimed.
		if xmp.IsLumens(data) && !bytes.Equal(file.Wrote, hash) {
			p.changes.Records = append(p.changes.Records, record)
		}
	case fileops.SidecarForeign, fileops.SidecarEdited, fileops.SidecarFailed, fileops.SidecarGone:
		p.res.Skipped++
		p.skip(sidecar)
		return
	}
	if file.Skip != nil {
		p.changes.Unskip = append(p.changes.Unskip, sidecar)
	}
}

// skip remembers that a sidecar was left alone, and how the file looks.
func (p *xmpPass) skip(sidecar string) {
	if st := sidecarStamp(sidecar); st.Size >= -1 {
		p.changes.Skips = append(p.changes.Skips, store.XMPSkip{Path: sidecar, XMPStamp: st})
	}
}

// failed counts a sidecar that could not be written as left alone, and logs
// it, unless the pass is ending. The sidecar is remembered like any other left
// alone: it is not tried again until the file changes or Export now is pressed.
func (p *xmpPass) failed(ctx context.Context, what, sidecar string, err error) {
	if ctx.Err() != nil {
		return
	}
	p.res.Skipped++
	if p.logged < 10 {
		p.logged++
		slog.Warn(what, "path", sidecar, "err", err)
	}
	p.skip(sidecar)
}

// orphans brings back to the marker the sidecars Lumen wrote that are now
// beside no file of the library, once their photo is gone or may not be shown
// (see store.XMPOrphans), and forgets them. A sidecar that has been edited
// since, or is gone, is only forgotten.
func (p *xmpPass) orphans(ctx context.Context) error {
	marker := xmp.Marshal(xmp.Doc{})
	markerHash := fileops.SidecarHash(marker)
	for after := int64(0); ; {
		var last int64
		var n int
		on, err := p.turn(ctx, func() error {
			list, err := p.ix.store.XMPOrphans(ctx, after, p.opts.batch)
			if err != nil {
				return fmt.Errorf("read the sidecars left behind: %w", err)
			}
			n = len(list)
			for _, o := range list {
				if err := ctx.Err(); err != nil {
					return err
				}
				last = o.Row
				p.orphan(ctx, o, marker, markerHash)
			}
			return nil
		})
		if err != nil || !on || n == 0 {
			return err
		}
		after = last
	}
}

func (p *xmpPass) orphan(ctx context.Context, o store.XMPOrphan, marker, markerHash []byte) {
	if !bytes.Equal(o.Hash, markerHash) {
		outcome, err := fileops.ReviseSidecar(ctx, o.Path, marker, o.Hash)
		if err != nil {
			p.failed(ctx, "clear xmp sidecar", o.Path, err)
			return // the record stays, to try again
		}
		if outcome == fileops.SidecarReplaced {
			p.res.Written++
		}
	}
	p.changes.Forget = append(p.changes.Forget, o.Path)
}

// sidecarStamp is how the file at path looks: its size and modification time,
// or size -1 if there is no file, or -2 if it cannot be looked at (which never
// matches a remembered one).
func sidecarStamp(path string) store.XMPStamp {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return store.XMPStamp{Size: -1}
	case err != nil:
		return store.XMPStamp{Size: -2}
	}
	return store.XMPStamp{Size: info.Size(), MTime: info.ModTime().UnixNano()}
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
