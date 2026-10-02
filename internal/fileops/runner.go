package fileops

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"lumen/internal/core"
	"lumen/internal/store"
)

// A run's moves are made one at a time, and each is journaled around the
// file's move, so that a crash at any point leaves a row that says what to
// check:
//
//	planned -> moving   committed before the file is touched
//	MoveFileExW         without replace: it cannot overwrite
//	moving  -> moved    in the transaction that points files.path at the new place
//
// Undo does the same the other way (moved -> undoing -> undone). Recover
// reads the disk to settle a row a crash left at moving or undoing.

// progressEvery is how often progress is reported while a run moves files.
const progressEvery = 100 * time.Millisecond

// crashPoint is a place the process could die at, for tests to stop the
// runner at.
type crashPoint int

const (
	afterJournal crashPoint = iota + 1 // the row says moving, the file has not moved
	afterMove                          // the file has moved, the library has not heard (nor has its sidecar moved)
	afterSidecar                       // the sidecar has moved too; the commit has not been made
)

// errCrashed is what the runner returns when a test's hook stops it: it has
// written nothing since, as a dead process would not have.
var errCrashed = errors.New("stopped by a test")

// Runner makes the moves of organise runs and puts them back.
type Runner struct {
	Store *store.Store
	// Progress, when set, hears of a run's progress, at most ten times a
	// second and at its start and end. It must not block.
	Progress func(core.OrganiseProgress)

	// crash is a test hook, called where a process could die. When it returns
	// true the runner stops at once, leaving the journal as it is.
	crash func(point crashPoint, seq int) bool
	// mkdir makes a folder (os.Mkdir when nil): a test's way to look at the
	// journal at the moment a folder is made.
	mkdir func(path string) error
}

// stopsAt reports whether a test asked the runner to stop at point.
func (r *Runner) stopsAt(point crashPoint, seq int) bool {
	return r.crash != nil && r.crash(point, seq)
}

// reporter turns a run's steps into progress events.
type reporter struct {
	fn    func(core.OrganiseProgress)
	runID int64
	total int
	done  int
	last  time.Time
}

func (p *reporter) emit(state core.OrganiseRunState) {
	if p.fn != nil {
		p.fn(core.OrganiseProgress{RunID: p.runID, State: state, Done: p.done, Total: p.total})
		p.last = time.Now()
	}
}

// step counts one handled move, reporting now and then.
func (p *reporter) step(state core.OrganiseRunState) {
	p.done++
	if p.fn != nil && time.Since(p.last) >= progressEvery {
		p.emit(state)
	}
}

// Run makes the planned moves of the run, in order. It first settles a move a
// crash left in doubt, so a run that failed part-way is resumed by running it
// again. A move whose file changed, or whose photo is no longer visible, is
// skipped; the others are made. The run ends done, or failed with the error
// if it could not go on (ctx ended, the database failed): it can be resumed.
func (r *Runner) Run(ctx context.Context, runID int64) error {
	if err := r.Recover(ctx, runID); err != nil {
		return err
	}
	moves, err := r.Store.OrganiseMoves(ctx, runID, false, core.MovePlanned)
	if err != nil {
		return err
	}
	if err := r.Store.SetOrganiseRunState(ctx, runID, core.OrganiseRunning); err != nil {
		return err
	}
	rep := &reporter{fn: r.Progress, runID: runID, total: len(moves)}
	rep.emit(core.OrganiseRunning)
	dirs := &dirMaker{store: r.Store, runID: runID, journal: true, mkdir: r.mkdir, known: map[string]bool{}}
	for _, m := range moves {
		if err := ctx.Err(); err != nil {
			return r.end(ctx, rep, core.OrganiseFailed, err)
		}
		if err := r.moveForward(ctx, runID, m, dirs); err != nil {
			if errors.Is(err, errCrashed) {
				return err
			}
			return r.end(ctx, rep, core.OrganiseFailed, err)
		}
		rep.step(core.OrganiseRunning)
	}
	// Nothing is left to go into a folder that was made and stayed empty.
	r.removeEmptyDirs(ctx, runID)
	return r.end(ctx, rep, core.OrganiseDone, nil)
}

// end records where the run stopped, even when ctx has ended, and reports it.
func (r *Runner) end(ctx context.Context, rep *reporter, state core.OrganiseRunState, cause error) error {
	if err := r.Store.SetOrganiseRunState(context.WithoutCancel(ctx), rep.runID, state); err != nil && cause == nil {
		cause = err
	}
	rep.emit(state)
	return cause
}

// moveForward makes one planned move. What happens to the file is recorded
// in its row; the error is for what stops the run.
func (r *Runner) moveForward(ctx context.Context, runID int64, m store.OrganiseMove, dirs *dirMaker) error {
	refusal, err := r.Store.OrganiseMovable(ctx, m)
	if err != nil {
		return err
	}
	if refusal != "" {
		return r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveSkipped, refusal)
	}
	switch info, err := os.Lstat(m.From); {
	case errors.Is(err, fs.ErrNotExist):
		return r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveSkipped, ReasonMissing)
	case err != nil:
		return r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveFailed, reasonOf(err))
	case info.Size() != m.Size || info.ModTime().UnixNano() != m.MTime:
		return r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveSkipped, ReasonChanged)
	}
	reason, fatal := dirs.ensure(ctx, filepath.Dir(m.To), m.Root)
	if fatal != nil {
		return fatal
	}
	if reason != nil {
		return r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveFailed, reasonOf(reason))
	}

	// A sidecar beside the photo goes with it, as part of this one move; the
	// journal says so before anything is touched.
	sidecar := ""
	if present, _ := exists(sidecarOf(m.From)); present {
		sidecar = store.SidecarPlanned
	}
	if err := r.Store.JournalOrganiseMove(ctx, runID, m.Seq, core.MovePlanned, core.MoveMoving, sidecar); err != nil {
		return err
	}
	if r.stopsAt(afterJournal, m.Seq) {
		return errCrashed
	}
	moveErr := Move(m.From, m.To)
	// From here the file has moved or not, and the row must say so whatever
	// ctx does: a row left at moving is only settled at the next start.
	settle := context.WithoutCancel(ctx)
	switch {
	case errors.Is(moveErr, ErrExists):
		return r.Store.SetOrganiseMove(settle, runID, m.Seq, core.MoveSkipped, ReasonTaken)
	case moveErr != nil:
		slog.Warn("organise: move a file", "kind", kindOf(moveErr), "errno", errnoOf(moveErr), "move", describe(m))
		return r.Store.SetOrganiseMove(settle, runID, m.Seq, core.MoveFailed, reasonOf(moveErr))
	}
	if r.stopsAt(afterMove, m.Seq) {
		return errCrashed
	}
	side := store.OrganiseSidecar{State: sidecar}
	if sidecar != "" {
		side = carry(sidecarOf(m.From), sidecarOf(m.To), store.SidecarMoved)
	}
	if r.stopsAt(afterSidecar, m.Seq) {
		return errCrashed
	}
	return r.Store.FinishOrganiseMove(settle, runID, m, m.From, m.To, core.MoveMoved, side)
}

// carry moves a sidecar from src to dst after its photo has moved, and says
// what became of it. It never replaces: a sidecar whose new name is taken
// stays where it is (kept, with the reason), and the photo's move stands.
// done is the state of a sidecar that moved (moved, or undone when it goes
// back); no sidecar at src gives no state.
func carry(src, dst, done string) store.OrganiseSidecar {
	present, err := exists(src)
	switch {
	case err != nil:
		return store.OrganiseSidecar{State: store.SidecarKept, Reason: reasonOf(err)}
	case !present:
		return store.OrganiseSidecar{}
	}
	switch err := Move(src, dst); {
	case err == nil:
		return store.OrganiseSidecar{State: done, From: src, To: dst}
	case errors.Is(err, ErrExists):
		return store.OrganiseSidecar{State: store.SidecarKept, Reason: ReasonSidecarTaken}
	default:
		return store.OrganiseSidecar{State: store.SidecarKept, Reason: reasonOf(err)}
	}
}

// settleSidecar finishes the sidecar of a move a crash cut short, from the
// disk: still at src, it goes now; already at dst, it went before the crash;
// at both, one is not ours and nothing is replaced (carry sees to it); at
// neither, there is none.
func settleSidecar(src, dst, done string) (store.OrganiseSidecar, error) {
	atSrc, err := exists(src)
	if err != nil {
		return store.OrganiseSidecar{}, err
	}
	atDst, err := exists(dst)
	if err != nil {
		return store.OrganiseSidecar{}, err
	}
	switch {
	case atSrc: // with one at dst too, carry leaves both: it never replaces
		return carry(src, dst, done), nil
	case atDst:
		return store.OrganiseSidecar{State: done, From: src, To: dst}, nil
	}
	return store.OrganiseSidecar{}, nil
}

// Undo moves the files of the run's moved rows back, last first, never
// replacing anything: a file whose old place is taken (or that is no longer
// where it was put) is left where it is, its row keeps saying moved with the
// reason, and the run stays done so that it can be tried again. When every
// file is back the run is undone, and the folders the run created are
// removed if they are empty (only those).
func (r *Runner) Undo(ctx context.Context, runID int64) error {
	if err := r.Recover(ctx, runID); err != nil {
		return err
	}
	moves, err := r.Store.OrganiseMoves(ctx, runID, true, core.MoveMoved)
	if err != nil {
		return err
	}
	if err := r.Store.SetOrganiseRunState(ctx, runID, core.OrganiseUndoing); err != nil {
		return err
	}
	rep := &reporter{fn: r.Progress, runID: runID, total: len(moves)}
	rep.emit(core.OrganiseUndoing)
	dirs := &dirMaker{store: r.Store, runID: runID, mkdir: r.mkdir, known: map[string]bool{}}
	for _, m := range moves {
		if err := ctx.Err(); err != nil {
			return r.end(ctx, rep, core.OrganiseDone, err)
		}
		if err := r.moveBack(ctx, runID, m, dirs); err != nil {
			if errors.Is(err, errCrashed) {
				return err
			}
			return r.end(ctx, rep, core.OrganiseDone, err)
		}
		rep.step(core.OrganiseUndoing)
	}
	r.removeEmptyDirs(ctx, runID)
	left, err := r.Store.OrganiseCounts(ctx, runID)
	if err != nil {
		return r.end(ctx, rep, core.OrganiseDone, err)
	}
	if left[core.MoveMoved] > 0 {
		return r.end(ctx, rep, core.OrganiseDone, nil)
	}
	return r.end(ctx, rep, core.OrganiseUndone, nil)
}

// moveBack puts one moved file back where it came from.
func (r *Runner) moveBack(ctx context.Context, runID int64, m store.OrganiseMove, dirs *dirMaker) error {
	keep := func(reason string) error { // the file stays put; the row says why
		return r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveMoved, reason)
	}
	// Only the file Lumen moved goes back. Since the run, the file may have
	// moved on (another run), and something else may be at its place: a
	// stranger's file must never be carried off to the old folder.
	switch info, err := os.Lstat(m.To); {
	case errors.Is(err, fs.ErrNotExist):
		return keep(ReasonMissing)
	case err != nil:
		return keep(reasonOf(err))
	case info.Size() != m.Size || info.ModTime().UnixNano() != m.MTime:
		return keep(ReasonChanged)
	}
	if here, err := r.Store.OrganiseFileAt(ctx, m.AssetID, m.To); err != nil {
		return err
	} else if !here {
		return keep(ReasonMissing) // the library has no such file there any more
	}
	reason, fatal := dirs.ensure(ctx, filepath.Dir(m.From), m.Root)
	if fatal != nil {
		return fatal
	}
	if reason != nil {
		return keep(reasonOf(reason))
	}

	if err := r.Store.JournalOrganiseMove(ctx, runID, m.Seq, core.MoveMoved, core.MoveUndoing, m.Sidecar); err != nil {
		return err
	}
	if r.stopsAt(afterJournal, m.Seq) {
		return errCrashed
	}
	moveErr := Move(m.To, m.From)
	settle := context.WithoutCancel(ctx)
	switch {
	case errors.Is(moveErr, ErrExists):
		return r.Store.SetOrganiseMove(settle, runID, m.Seq, core.MoveMoved, ReasonTaken)
	case moveErr != nil:
		slog.Warn("organise: put a file back", "kind", kindOf(moveErr), "errno", errnoOf(moveErr), "move", describe(m))
		return r.Store.SetOrganiseMove(settle, runID, m.Seq, core.MoveMoved, reasonOf(moveErr))
	}
	if r.stopsAt(afterMove, m.Seq) {
		return errCrashed
	}
	// The sidecar that went with the photo comes back with it. One that was
	// left behind stays as it is.
	side := store.OrganiseSidecar{State: m.Sidecar}
	if m.Sidecar == store.SidecarMoved {
		side = carry(sidecarOf(m.To), sidecarOf(m.From), store.SidecarUndone)
	}
	if r.stopsAt(afterSidecar, m.Seq) {
		return errCrashed
	}
	return r.Store.FinishOrganiseMove(settle, runID, m, m.To, m.From, core.MoveUndone, side)
}

// Recover settles the moves of a run that a crash left at moving or undoing,
// by looking at the disk. The file is where the move was taking it only if it
// is there and the old place is empty; nothing is ever moved or deleted here:
//
//	moving:  only the destination holds the file -> moved
//	         only the old place holds it          -> planned again
//	         both, or neither                     -> failed
//
// and the same the other way round for undoing. A file at the destination
// that is not the size and time the plan saw is not trusted to be ours.
func (r *Runner) Recover(ctx context.Context, runID int64) error {
	moves, err := r.Store.OrganiseMoves(ctx, runID, false, core.MoveMoving, core.MoveUndoing)
	if err != nil {
		return err
	}
	for _, m := range moves {
		// forward: the file should end at To; backward: at From. The sidecar
		// travels with it when the journal says it was to.
		origin, target := m.From, m.To
		resting, restart := core.MoveMoved, core.MovePlanned
		sidecarMoves, sidecarDone := m.Sidecar == store.SidecarPlanned, store.SidecarMoved
		if m.State == core.MoveUndoing {
			origin, target = m.To, m.From
			resting, restart = core.MoveUndone, core.MoveMoved
			sidecarMoves, sidecarDone = m.Sidecar == store.SidecarMoved, store.SidecarUndone
		}
		atOrigin, err := exists(origin)
		if err != nil {
			return err
		}
		atTarget, err := exists(target)
		if err != nil {
			return err
		}
		switch {
		case atTarget && !atOrigin:
			if !sameFile(target, m) {
				err = r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveFailed, "the file at the destination is not the one that was moved")
			} else {
				side := store.OrganiseSidecar{State: m.Sidecar}
				if sidecarMoves {
					side, err = settleSidecar(sidecarOf(origin), sidecarOf(target), sidecarDone)
				}
				if err == nil {
					err = r.Store.FinishOrganiseMove(ctx, runID, m, origin, target, resting, side)
				}
			}
		case atOrigin && !atTarget:
			err = r.Store.SetOrganiseMove(ctx, runID, m.Seq, restart, "")
		case atOrigin && atTarget && m.State == core.MoveUndoing:
			// An undo cut short with something at the old place. If the file
			// there is the one that went back it is finished; otherwise the
			// way is taken, as when no crash came between: the file stays
			// moved, the reason is recorded, and the run can be undone again.
			if sameFile(target, m) {
				side := store.OrganiseSidecar{State: m.Sidecar}
				if sidecarMoves {
					side, err = settleSidecar(sidecarOf(origin), sidecarOf(target), sidecarDone)
				}
				if err == nil {
					err = r.Store.FinishOrganiseMove(ctx, runID, m, origin, target, resting, side)
				}
			} else {
				err = r.Store.SetOrganiseMove(ctx, runID, m.Seq, restart, ReasonTaken)
			}
		case atOrigin && atTarget:
			err = r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveFailed, "a file is at both the old and the new place")
		default:
			err = r.Store.SetOrganiseMove(ctx, runID, m.Seq, core.MoveFailed, "no file is at the old or the new place")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// RecoverAll settles every run a crash interrupted, and leaves it where the
// user can pick it up: a forward run that stopped is failed (resumable, or
// undoable) unless nothing was left to do, and an undo that stopped is done
// again with its files still moved, or undone if none is.
func (r *Runner) RecoverAll(ctx context.Context) error {
	ids, err := r.Store.InterruptedOrganiseRuns(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := r.Recover(ctx, id); err != nil {
			return err
		}
		run, err := r.Store.OrganiseRun(ctx, id)
		if err != nil {
			return err
		}
		counts, err := r.Store.OrganiseCounts(ctx, id)
		if err != nil {
			return err
		}
		state := run.State
		if run.State == core.OrganiseRunning {
			state = core.OrganiseFailed
			if counts[core.MovePlanned] == 0 {
				state = core.OrganiseDone
			}
		}
		if run.State == core.OrganiseUndoing {
			state = core.OrganiseDone
			if counts[core.MoveMoved] == 0 {
				state = core.OrganiseUndone
			}
		}
		if state != run.State {
			if err := r.Store.SetOrganiseRunState(ctx, id, state); err != nil {
				return err
			}
		}
	}
	return nil
}

// exists reports whether anything is at path.
func exists(path string) (bool, error) {
	switch _, err := os.Lstat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, err
	}
}

// sameFile reports whether the file at path has the size and time the plan
// saw (the move keeps both).
func sameFile(path string, m store.OrganiseMove) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Size() == m.Size && info.ModTime().UnixNano() == m.MTime
}

// removeEmptyDirs removes the folders the run created that are empty, deepest
// first. It is best effort: a folder with something in it, or one already gone,
// is not an error; only these folders are ever touched.
func (r *Runner) removeEmptyDirs(ctx context.Context, runID int64) {
	dirs, err := r.Store.OrganiseDirs(ctx, runID)
	if err != nil {
		slog.Warn("organise: list the folders it made", "err", err)
		return
	}
	for _, dir := range dirs {
		switch err := RemoveDir(dir); {
		case err == nil, errors.Is(err, fs.ErrNotExist):
			if err := r.Store.DropOrganiseDir(ctx, runID, dir); err != nil {
				slog.Warn("organise: forget a folder it made", "err", err)
			}
		default:
			slog.Debug("organise: keep a folder it made", "errno", errnoOf(err)) // not empty, most likely
		}
	}
}

// dirMaker makes the folders a move needs, journaling the ones it creates
// first (when journal is set) so that undo knows they are Lumen's.
type dirMaker struct {
	store   *store.Store
	runID   int64
	journal bool
	mkdir   func(path string) error // os.Mkdir when nil
	known   map[string]bool         // folded folders seen to exist
}

// ensure makes dir and its missing parents. reason is why a folder could not
// be made (the move fails, the run goes on); fatal is a journal failure,
// which stops the run, since a folder made without a record could not be undone.
func (d *dirMaker) ensure(ctx context.Context, dir, root string) (reason, fatal error) {
	var missing []string
	for cur := dir; ; {
		key := core.Fold(cur)
		if d.known[key] {
			break
		}
		if root != "" && key == core.Fold(root) {
			// The source folder is where the library already is. It may be a
			// junction or a link to the real folder, which Lstat does not
			// call a folder; it is not Lumen's to create or to question.
			d.known[key] = true
			break
		}
		info, err := os.Lstat(cur)
		if err == nil {
			if !info.IsDir() {
				return errors.New("a file is in the way of the folder " + filepath.Base(cur)), nil
			}
			d.known[key] = true
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err, nil
		}
		missing = append(missing, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	for i := len(missing) - 1; i >= 0; i-- {
		made := missing[i]
		if d.journal {
			if err := d.store.AddOrganiseDir(ctx, d.runID, made); err != nil {
				return nil, err
			}
		}
		mkdir := d.mkdir
		if mkdir == nil {
			mkdir = func(path string) error { return os.Mkdir(path, 0o750) }
		}
		switch err := mkdir(made); {
		case err == nil:
		case errors.Is(err, fs.ErrExist):
			// Someone made it first, so it is not ours to remove.
			if d.journal {
				if err := d.store.DropOrganiseDir(ctx, d.runID, made); err != nil {
					return nil, err
				}
			}
		default:
			return err, nil
		}
		d.known[core.Fold(made)] = true
	}
	return nil, nil
}
