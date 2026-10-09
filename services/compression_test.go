package services

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/compress"
	"lumen/internal/core"
)

func TestCompressionPauseCancellationAndShutdownWaitForTheOwnedWorker(t *testing.T) {
	for _, action := range []string{"pause", "cancel", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			l := newShownLibrary(t, 1)
			library := NewLibraryService(l.st, nil, nil, nil)
			s := NewCompressionService(l.st, library, core.PathsIn(t.TempDir()))
			t.Cleanup(func() { check(t, s.ServiceShutdown()); check(t, library.ServiceShutdown()) })
			f, err := l.st.AssetFile(t.Context(), 1, false)
			check(t, err)
			id, err := l.st.SaveCompressionPlan(t.Context(), core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: 1, From: f.Paths[0], To: filepath.Join(t.TempDir(), "copy.jpg"), State: core.CompressionPlanned}})
			check(t, err)
			started := make(chan struct{})
			s.coordinator.Acquire = func(ctx context.Context, _ int) (context.Context, func(), int, error) {
				close(started)
				<-ctx.Done()
				return ctx, nil, 0, ctx.Err()
			}
			check(t, s.Analyse(t.Context(), id))
			select {
			case <-started:
			case <-t.Context().Done():
				t.Fatal("worker did not start")
			}
			if err := s.Analyse(t.Context(), id); !errors.Is(err, ErrCompressionBusy) {
				t.Fatalf("second worker: %v", err)
			}
			switch action {
			case "pause":
				check(t, s.Pause(id))
			case "cancel":
				check(t, s.Cancel(t.Context(), id))
			case "shutdown":
				check(t, s.ServiceShutdown())
			}
			s.wg.Wait()
			run, err := l.st.CompressionRun(t.Context(), id)
			check(t, err)
			want := core.CompressionPaused
			if action == "cancel" {
				want = core.CompressionCancelled
			}
			if run.State != want {
				t.Fatalf("settled state: %s, want %s", run.State, want)
			}
			if action == "shutdown" {
				if err := s.Analyse(t.Context(), id); !errors.Is(err, ErrOrganiseStopping) {
					t.Fatalf("work admitted after shutdown: %v", err)
				}
			}
		})
	}
}

// Wails stops the application when a service's start-up returns an error: a
// journal row that cannot be settled must not keep the library from opening.
// Compression refuses work until it is settled, and tries again when asked.
func TestAnUnsettledCompressionJournalNeverStopsStartupAndIsRetried(t *testing.T) {
	l := newShownLibrary(t, 1)
	library := NewLibraryService(l.st, nil, nil, nil)
	s := NewCompressionService(l.st, library, core.PathsIn(t.TempDir()))
	t.Cleanup(func() { check(t, s.ServiceShutdown()); check(t, library.ServiceShutdown()) })
	f, err := l.st.AssetFile(t.Context(), 1, false)
	check(t, err)
	id, err := l.st.SaveCompressionPlan(t.Context(), core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: 1, From: f.Paths[0], To: filepath.Join(t.TempDir(), "copy.jpg"), State: core.CompressionPlanned}})
	check(t, err)
	jobs, err := l.st.CompressionJobs(t.Context(), id)
	check(t, err)
	// An interrupted candidate outside the owned folder is never removed.
	stuck := jobs[0]
	stuck.State, stuck.Temp = core.CompressionEncoding, filepath.Join(t.TempDir(), "elsewhere", "candidate")
	check(t, l.st.SetCompressionJob(t.Context(), stuck))

	if err := s.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatalf("start-up failed with the journal: %v", err)
	}
	if err := s.Analyse(t.Context(), id); !errors.Is(err, compress.ErrPlan) {
		t.Fatalf("work admitted over an unsettled journal: %v", err)
	}
	stuck.Temp = ""
	check(t, l.st.SetCompressionJob(t.Context(), stuck))
	check(t, s.Analyse(t.Context(), id))
	s.wg.Wait()
	jobs, err = l.st.CompressionJobs(t.Context(), id)
	check(t, err)
	if jobs[0].State == core.CompressionEncoding {
		t.Fatalf("the settled journal was not worked on: %+v", jobs[0])
	}
}
