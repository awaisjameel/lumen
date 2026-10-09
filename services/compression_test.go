package services

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
