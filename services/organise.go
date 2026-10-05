package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

// ErrOrganiseBusy means files are already being moved or put back: one run
// at a time.
var ErrOrganiseBusy = errors.New("files are already being organised")

// ErrOrganiseState means the run cannot be started or undone from where it
// stands (a finished run is not started again; a preview that never ran has
// nothing to undo).
var ErrOrganiseState = errors.New("that run cannot be started or undone now")

// ErrOrganiseStopping means Lumen is closing, and no new run is taken.
var ErrOrganiseStopping = errors.New("the app is closing")

// recovering is the place a startup recovery takes in organiser.active: no
// run has this id.
const recovering = -1

// organiser is the organise runs' bookkeeping: the one run at a time, and the
// goroutine that makes it. LibraryService's zero value for it works.
type organiser struct {
	mu     sync.Mutex            // guards the fields below
	active int64                 // the run being made, put back or recovered; 0 when none
	state  core.OrganiseRunState // its public state until all cleanup has finished
	ctx    context.Context
	stop   context.CancelFunc
	wg     sync.WaitGroup
	closed bool // shutdown has begun: no run or recovery is taken any more
	// emit sends progress to the UI (the app's event when nil); a test's can
	// listen instead.
	emit func(core.OrganiseProgress)
}

// lifetime is the context runs live in: cancelled when the service shuts
// down, not when the call that started a run returns.
func (o *organiser) lifetime() context.Context {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.ctx == nil {
		o.ctx, o.stop = context.WithCancel(context.Background())
	}
	return o.ctx
}

// claim takes the one place for a run, or says it is taken.
func (o *organiser) claim(runID int64, state core.OrganiseRunState) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return ErrOrganiseStopping
	}
	if o.active != 0 {
		return ErrOrganiseBusy
	}
	o.active = runID
	o.state = state
	return nil
}

// track registers a goroutine that shutdown must wait for, or says shutdown
// has begun and it must not start. The registration is made under the lock
// that closes the door, so a goroutine is never added after Wait began.
func (o *organiser) track() (done func(), ok bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, false
	}
	o.wg.Add(1)
	return o.wg.Done, true
}

// release makes the next action available. A completion event is sent under
// the same lock, so it cannot arrive after a newly started action's event.
// completed, like the progress emitter, must not block or re-enter the owner.
func (o *organiser) release(completed func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.active = 0
	o.state = ""
	if completed != nil {
		completed()
	}
}

// ServiceStartup settles the organise runs a crash left in doubt (Wails calls
// it once the app runs): a move whose file may or may not have moved is
// resolved from the disk, and the run is left for the user to resume or undo.
// It waits for nothing the window needs, so it works in the background.
func (s *LibraryService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	life := s.organise.lifetime()
	if done, ok := s.organise.track(); ok {
		go func() {
			defer done()
			s.settleOrganise(life)
		}()
	}
	return nil
}

// settleOrganise is recoverOrganise for a goroutine, logging what fails. The
// indexer runs it before its first scan (see NewLibraryService), and
// ServiceStartup runs it again for an indexer that was started by then: it
// finds nothing left to settle.
func (s *LibraryService) settleOrganise(ctx context.Context) {
	if err := s.recoverOrganise(ctx); err != nil && ctx.Err() == nil {
		slog.Error("settle interrupted organise runs", "err", err)
	}
}

// ServiceShutdown stops a run in progress (it ends failed, so it can be
// resumed) and waits for it. Wails stops services before the database closes.
func (s *LibraryService) ServiceShutdown() error {
	s.organise.lifetime() // so that there is a stop to call
	s.organise.mu.Lock()
	s.organise.closed = true
	stop := s.organise.stop
	s.organise.mu.Unlock()
	stop()
	s.organise.wg.Wait()
	return nil
}

// recoverOrganise settles interrupted runs, holding scans so that none sees a
// file between its old place and its new one.
func (s *LibraryService) recoverOrganise(ctx context.Context) error {
	open, err := s.store.InterruptedOrganiseRuns(ctx)
	if err != nil {
		return err
	}
	exports, err := s.store.HasInterruptedExports(ctx)
	if err != nil || (len(open) == 0 && !exports) {
		return err
	}
	if s.organise.claim(recovering, "") != nil {
		return nil // a run the user started settles its own rows first
	}
	defer s.organise.release(nil)
	unhold, err := s.holdScans(ctx)
	if err != nil {
		return err
	}
	defer unhold()
	if err := s.exportRunner().Recover(ctx); err != nil {
		return err
	}
	return s.runner().RecoverAll(ctx)
}

// holdScans keeps the indexer from scanning until the returned function is
// called; without an indexer (in tests) there is nothing to hold.
func (s *LibraryService) holdScans(ctx context.Context) (func(), error) {
	if s.ix == nil {
		return func() {}, nil
	}
	return s.ix.HoldScans(ctx)
}

func (s *LibraryService) runner() *fileops.Runner {
	return &fileops.Runner{Store: s.store, Progress: s.sendOrganiseProgress}
}

// sendOrganiseProgress tells the UI how a run is going.
func (s *LibraryService) sendOrganiseProgress(p core.OrganiseProgress) {
	if s.organise.emit != nil {
		s.organise.emit(p)
		return
	}
	if app := application.Get(); app != nil {
		app.Event.Emit(EventOrganiseProgress, p)
	}
}

// PlanOrganise previews where template would move the files of the source
// sourceID (0: every source), and saves the preview as a planned run.
// Only visible photos are planned, and only visible names reach folder names.
// A scheme that cannot be used fails with fileops.ErrInvalidTemplate.
func (s *LibraryService) PlanOrganise(ctx context.Context, template string, sourceID int64) (core.OrganisePlan, error) {
	if sourceID != 0 {
		sources, err := s.store.ListSources(ctx)
		if err != nil {
			return core.OrganisePlan{}, err
		}
		if !hasEnabledSource(sources, sourceID) {
			return core.OrganisePlan{}, store.ErrNotFound
		}
	}
	// A bad scheme fails before the library is read.
	if _, err := fileops.ParseTemplate(template); err != nil {
		return core.OrganisePlan{}, err
	}
	gap, err := s.store.EventGapHours(ctx)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	snap, err := s.store.OrganiseSnapshot(ctx, sourceID, gap)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	planned, err := fileops.Plan(ctx, template, snap)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	runID, err := s.store.SaveOrganisePlan(ctx, template, planned.Moves)
	if err != nil {
		return core.OrganisePlan{}, err
	}
	moves := make([]core.OrganiseMove, len(planned.Moves))
	for i, m := range planned.Moves {
		moves[i] = organiseMoveOf(m)
	}
	return core.OrganisePlan{RunID: runID, Template: template, Moves: moves, Renamed: planned.Renamed, InPlace: planned.InPlace,
		Sidecars: planned.Sidecars}, nil
}

func hasEnabledSource(sources []core.Source, id int64) bool {
	for _, src := range sources {
		if src.ID == id && src.Enabled {
			return true
		}
	}
	return false
}

func organiseMoveOf(m store.OrganiseMove) core.OrganiseMove {
	return core.OrganiseMove{Seq: m.Seq, AssetID: m.AssetID, From: m.From, To: m.To, State: m.State, Renamed: m.Renamed, Error: m.Error, Sidecar: core.SidecarState(m.Sidecar)}
}

// The states a run can be started or undone from. A run still marked running
// or undoing when nothing is moving files was cut off by a crash; it is
// settled before it goes on.
var (
	startable = []core.OrganiseRunState{core.OrganisePlanned, core.OrganiseFailed, core.OrganiseRunning}
	undoable  = []core.OrganiseRunState{core.OrganiseDone, core.OrganiseFailed, core.OrganiseRunning, core.OrganiseUndoing}
)

// organiseOp is what differs between making a run's moves and putting them back.
type organiseOp struct {
	started core.OrganiseRunState // the state the run is given as it begins
	// allowed says whether a run in this state, with these moves, may begin.
	allowed func(run core.OrganiseRun, counts map[core.OrganiseMoveState]int) bool
	// todo is how many moves the work will go through, for the first progress.
	todo func(counts map[core.OrganiseMoveState]int) int
	work func(ctx context.Context, r *fileops.Runner, runID int64) error
}

// StartOrganise makes (or resumes) the moves of the run runID. It returns once
// the run has started: the moves are made in the background, scans are held
// meanwhile, and progress arrives as organise:progress events. A run that
// failed part-way is resumed; a second start while a run is moving files is
// refused with ErrOrganiseBusy.
func (s *LibraryService) StartOrganise(ctx context.Context, runID int64) error {
	return s.begin(ctx, runID, organiseOp{
		started: core.OrganiseRunning,
		allowed: func(run core.OrganiseRun, counts map[core.OrganiseMoveState]int) bool {
			// Once a file has been put back the run can only be undone: it
			// would otherwise carry on from a plan half taken apart.
			return slices.Contains(startable, run.State) && counts[core.MoveUndone]+counts[core.MoveUndoing] == 0
		},
		todo: func(counts map[core.OrganiseMoveState]int) int {
			return counts[core.MovePlanned] + counts[core.MoveMoving]
		},
		work: func(ctx context.Context, r *fileops.Runner, id int64) error { return r.Run(ctx, id) },
	})
}

// UndoOrganise moves the files of the run runID back, never replacing a file
// that has since appeared at their old place (the run then stays done, for a
// second try once the way is clear). It returns once the undo has started.
func (s *LibraryService) UndoOrganise(ctx context.Context, runID int64) error {
	return s.begin(ctx, runID, organiseOp{
		started: core.OrganiseUndoing,
		allowed: func(run core.OrganiseRun, _ map[core.OrganiseMoveState]int) bool {
			return slices.Contains(undoable, run.State)
		},
		todo: func(counts map[core.OrganiseMoveState]int) int {
			return counts[core.MoveMoved] + counts[core.MoveUndoing]
		},
		work: func(ctx context.Context, r *fileops.Runner, id int64) error { return r.Undo(ctx, id) },
	})
}

// begin starts a run or an undo in the background: it checks the run may go
// ahead, claims the one place, marks the run as started, and hands the work to
// a goroutine that holds scans for its duration.
func (s *LibraryService) begin(ctx context.Context, runID int64, op organiseOp) error {
	if err := s.organise.claim(runID, op.started); err != nil {
		return err
	}
	handed := false
	defer func() {
		if !handed {
			s.organise.release(nil)
		}
	}()
	run, err := s.store.OrganiseRun(ctx, runID)
	if err != nil {
		return err
	}
	counts, err := s.store.OrganiseCounts(ctx, runID)
	if err != nil {
		return err
	}
	if !op.allowed(run, counts) {
		return fmt.Errorf("%w: it is %s", ErrOrganiseState, run.State)
	}
	if err := s.store.SetOrganiseRunState(ctx, runID, op.started); err != nil {
		return err
	}
	done, ok := s.organise.track()
	if !ok {
		return ErrOrganiseStopping
	}
	life := s.organise.lifetime()
	handed = true
	go func() {
		defer done()
		var finished *core.OrganiseProgress
		defer s.organise.release(func() {
			if finished != nil {
				s.sendOrganiseProgress(*finished)
			}
		})
		s.sendOrganiseProgress(core.OrganiseProgress{RunID: runID, State: op.started, Total: op.todo(counts)})
		unhold, err := s.holdScans(life)
		if err != nil {
			// Shutting down before the hold: nothing moved, and the run keeps
			// the state it was given, which the next start settles.
			slog.Warn("hold scans for an organise run", "err", err)
			return
		}
		defer unhold()
		runner := s.runner()
		runner.Progress = func(p core.OrganiseProgress) {
			if p.State == core.OrganiseRunning || p.State == core.OrganiseUndoing {
				s.sendOrganiseProgress(p)
			} else {
				// The journal may be finished before retries and scan cleanup.
				// Offer the next action only once those have finished too.
				finished = &p
			}
		}
		if err := op.work(life, runner, runID); err != nil && life.Err() == nil {
			slog.Error("organise run stopped", "run", runID, "err", err)
			if finished == nil {
				// It stopped before it could record an end (settling a move in
				// doubt, reading its moves): failed runs can be tried again.
				if err := s.store.SetOrganiseRunState(life, runID, core.OrganiseFailed); err != nil {
					slog.Error("record an organise run as failed", "run", runID, "err", err)
				}
				finished = &core.OrganiseProgress{RunID: runID, State: core.OrganiseFailed, Total: op.todo(counts)}
			}
		}
		s.afterOrganise(life, runID)
	}()
	return nil
}

// afterOrganise tells the rest of Lumen that files have moved: the paths
// changed, and a stage that read a file just as it moved failed its job
// through no fault of the file, so those assets' failed jobs run again.
func (s *LibraryService) afterOrganise(ctx context.Context, runID int64) {
	if s.ix == nil {
		return
	}
	s.ix.LibraryChanged(core.Changes{Assets: true})
	assets, err := s.store.OrganiseMovedAssets(ctx, runID)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("list the assets an organise run moved", "err", err)
		}
		return
	}
	s.ix.RetryFailedOf(ctx, assets)
}

// OrganiseRuns lists the runs that have started, newest first.
func (s *LibraryService) OrganiseRuns(ctx context.Context) ([]core.OrganiseRun, error) {
	// Keep the read and public state together: a terminal journal entry does
	// not mean another action can start while the owner is still cleaning up.
	s.organise.mu.Lock()
	defer s.organise.mu.Unlock()
	runs, err := s.store.OrganiseRuns(ctx)
	for i := range runs {
		if runs[i].ID == s.organise.active {
			runs[i].State = s.organise.state
		}
	}
	return runs, err
}
