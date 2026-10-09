package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/compress"
	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

// ErrCompressionBusy means another candidate or publication is still running.
var ErrCompressionBusy = errors.New("a compression run is already active")

// ErrCompressionState means the requested transition requires another review.
var ErrCompressionState = errors.New("the compression run cannot do that now")

// CompressionService exposes the coordinator through generated wire types.
type CompressionService struct {
	coordinator *compress.Coordinator
	library     *LibraryService
	mu          sync.Mutex // guards admission, cancellation and the worker lifetime
	active      int64
	state       core.CompressionState
	cancel      context.CancelFunc
	intent      core.CompressionState
	closed      bool
	wg          sync.WaitGroup
	ctx         context.Context
	stop        context.CancelFunc
	recovery    sync.Mutex // one recovery at a time; guards recovered
	recovered   bool
}

// NewCompressionService shares the library's file-operation and resource owners.
func NewCompressionService(st *store.Store, library *LibraryService, paths core.Paths) *CompressionService {
	ctx, stop := context.WithCancel(context.Background())
	c := &compress.Coordinator{Store: st, Root: filepath.Join(paths.DataDir, "compression"), Codec: compress.Codec{Runtime: paths.Runtime}}
	c.Admit = func(ctx context.Context) (context.Context, func(), error) {
		ctx, finish, err := library.organise.hold(ctx, exporting)
		if err != nil {
			return ctx, nil, err
		}
		release, err := library.holdScans(ctx)
		if err != nil {
			finish()
			return ctx, nil, err
		}
		return ctx, func() { release(); finish() }, nil
	}
	if library.ix != nil {
		c.Acquire = library.ix.AcquireCompression
	}
	s := &CompressionService{coordinator: c, library: library, ctx: ctx, stop: stop}
	if library.ix != nil {
		library.ix.BeforeFirstScan(s.settle)
	}
	return s
}

// ServiceStartup settles interrupted publications before admitting new runs.
// A journal that cannot be settled yet is logged, never returned: Wails stops
// the whole application on a service's start-up error, and the library must
// still open when, say, a copy's drive is unplugged. Compression then refuses
// new work until a later attempt settles it (see begin).
func (s *CompressionService) ServiceStartup(_ context.Context, _ application.ServiceOptions) error {
	s.settle(s.ctx)
	return nil
}

// settle is recover for the indexer's first scan, which has nowhere to report to.
func (s *CompressionService) settle(ctx context.Context) {
	if err := s.recover(ctx); err != nil {
		slog.Error("recover compression journal", "err", err)
	}
}

// recover settles the journal once. A failed attempt is made again by the
// next caller, so a passing fault does not disable compression until restart.
func (s *CompressionService) recover(ctx context.Context) error {
	s.recovery.Lock()
	defer s.recovery.Unlock()
	if s.recovered {
		return nil
	}
	if err := s.coordinator.Recover(ctx); err != nil {
		return err
	}
	s.recovered = true
	return nil
}

// Restore creates a reviewed restoration plan for a verified reversible run.
func (s *CompressionService) Restore(ctx context.Context, runID, jobID int64, directory string) (core.CompressionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return core.CompressionRun{}, ErrOrganiseStopping
	}
	id, err := s.coordinator.RestorePlan(ctx, runID, jobID, directory)
	if err != nil {
		return core.CompressionRun{}, err
	}
	return s.coordinator.Store.CompressionRun(ctx, id)
}

// ServiceShutdown cancels and waits for all owned workers before the store closes.
func (s *CompressionService) ServiceShutdown() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.stop()
	s.wg.Wait()
	return nil
}

// Available says whether the pinned codec tools exist locally.
func (s *CompressionService) Available(ctx context.Context) bool {
	return s.coordinator.Codec.Available(ctx)
}

// Reveal opens a verified saved output, including a copy outside the library.
func (s *CompressionService) Reveal(ctx context.Context, runID, jobID int64) error {
	j, err := s.coordinator.Store.VisibleCompressionJob(ctx, runID, jobID)
	if err != nil {
		return err
	}
	if j.State != core.CompressionDone {
		return store.ErrNotFound
	}
	release, err := fileops.LeaseCopyFolders(ctx, filepath.Dir(j.To))
	if err != nil {
		return err
	}
	defer release()
	digest, size, err := fileops.FileDigest(ctx, j.To)
	if err != nil {
		return err
	}
	if digest != j.OutputDigest || size != j.OutputSize {
		return fileops.ErrSourceChanged
	}
	return showInFolder(ctx, j.To)
}

// Plan resolves one or explicitly chosen duplicate copies without writing outputs.
func (s *CompressionService) Plan(ctx context.Context, inputs []core.CompressionInput, directory string, options core.CompressionOptions) (core.CompressionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return core.CompressionRun{}, ErrOrganiseStopping
	}
	if !s.Available(ctx) {
		return core.CompressionRun{}, compress.ErrUnsupported
	}
	id, err := s.coordinator.Plan(ctx, inputs, directory, options)
	if err != nil {
		return core.CompressionRun{}, err
	}
	return s.coordinator.Store.CompressionRun(ctx, id)
}

// Runs returns recent local runs, omitting history hidden by visibility rules.
func (s *CompressionService) Runs(ctx context.Context) ([]core.CompressionRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.coordinator.Store.CompressionRuns(ctx)
	for i := range runs {
		if runs[i].ID == s.active {
			runs[i].State = s.state
		}
	}
	return runs, err
}

// Jobs returns the selected run's visible per-file results, without temporary paths.
func (s *CompressionService) Jobs(ctx context.Context, runID int64, offset int) (core.CompressionPage, error) {
	if runID < 1 {
		return core.CompressionPage{}, store.ErrNotFound
	}
	return s.coordinator.Store.CompressionPage(ctx, runID, offset)
}

// Analyse starts or resumes a bounded candidate pass; failed rows can be retried.
func (s *CompressionService) Analyse(ctx context.Context, runID int64) error {
	return s.begin(ctx, runID, core.CompressionEncoding, func(ctx context.Context) error { return s.coordinator.Analyse(ctx, runID) })
}

// Commit publishes exactly the selected reviewed candidates in the background.
func (s *CompressionService) Commit(ctx context.Context, runID int64, jobs []int64) error {
	if len(jobs) == 0 {
		return compress.ErrPlan
	}
	jobs = slices.Clone(jobs)
	return s.begin(ctx, runID, core.CompressionCommitting, func(ctx context.Context) error {
		err := s.coordinator.Commit(ctx, runID, jobs)
		if s.library.ix != nil {
			s.library.ix.RescanAll()
			s.library.ix.LibraryChanged(core.Changes{Assets: true})
		}
		return err
	})
}

func (s *CompressionService) begin(ctx context.Context, id int64, state core.CompressionState, work func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrOrganiseStopping
	}
	if s.active != 0 {
		return ErrCompressionBusy
	}
	if err := s.recover(ctx); err != nil {
		return fmt.Errorf("recover compression journal: %w", err)
	}
	run, err := s.coordinator.Store.CompressionRun(ctx, id)
	if err != nil {
		return err
	}
	if state == core.CompressionEncoding && (run.State == core.CompressionEncoding || run.State == core.CompressionCommitting) {
		return ErrCompressionState
	}
	if err := s.coordinator.Store.SetCompressionRun(ctx, id, state); err != nil {
		return err
	}
	workerCtx, cancel := context.WithCancel(s.ctx)
	s.active, s.state, s.cancel, s.intent = id, state, cancel, core.CompressionPaused
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		err := work(workerCtx)
		s.mu.Lock()
		defer s.mu.Unlock()
		settle := context.WithoutCancel(workerCtx)
		if workerCtx.Err() != nil {
			if s.intent == core.CompressionCancelled {
				err = s.coordinator.Cancel(settle, id)
			} else {
				err = s.coordinator.Store.SetCompressionRun(settle, id, core.CompressionPaused)
			}
		} else if err != nil {
			if saveErr := s.coordinator.Store.SetCompressionRun(settle, id, core.CompressionFailed); saveErr != nil {
				err = errors.Join(err, saveErr)
			}
		}
		if err != nil {
			slog.Error("compression run stopped", "run", id, "err", err)
		}
		s.active, s.cancel, s.state = 0, nil, ""
	}()
	return nil
}

// Pause stops candidate analysis and leaves the current item retryable.
func (s *CompressionService) Pause(runID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.active != runID || s.state != core.CompressionEncoding || s.cancel == nil || s.intent == core.CompressionCancelled {
		return ErrCompressionState
	}
	s.intent = core.CompressionPaused
	s.cancel()
	return nil
}

// Cancel stops an active worker, or discards idle uncommitted candidates.
func (s *CompressionService) Cancel(ctx context.Context, runID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrOrganiseStopping
	}
	if s.active != 0 {
		if s.active != runID || s.cancel == nil {
			return ErrCompressionBusy
		}
		s.intent = core.CompressionCancelled
		s.cancel()
		return nil
	}
	return s.coordinator.Cancel(ctx, runID)
}
