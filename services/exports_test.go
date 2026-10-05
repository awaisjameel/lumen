package services

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

type cancelledRenderer struct{ started chan struct{} }

func (r cancelledRenderer) Render(ctx context.Context, _ string, _, _ int) ([]byte, error) {
	close(r.started)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestFilePreviewsCannotReplaceJournalRowsWhileAnExportRuns(t *testing.T) {
	l := newShownLibrary(t, 2)
	s := NewLibraryService(l.st, nil, nil, l.cache)
	t.Cleanup(func() { check(t, s.ServiceShutdown()) })
	started := make(chan struct{})
	l.cache.Renderer = cancelledRenderer{started}
	dir := t.TempDir()
	options := core.ExportOptions{Format: core.ExportPNG, Frame: -1}
	jobs, err := s.PlanExport(t.Context(), l.ids, dir, options)
	check(t, err)
	done := make(chan error, 1)
	go func() {
		_, err := s.RunExport(t.Context(), jobs[0].Batch)
		done <- err
	}()
	select {
	case <-started:
	case <-t.Context().Done():
		t.Fatal("export did not start")
	}
	if _, err := s.PlanExport(t.Context(), l.ids, dir, options); !errors.Is(err, ErrOrganiseBusy) {
		t.Errorf("export preview during execution: %v", err)
	}
	if _, err := s.PlanFileMoves(t.Context(), l.ids, dir, ""); !errors.Is(err, ErrOrganiseBusy) {
		t.Errorf("move preview during execution: %v", err)
	}
	if _, err := s.PlanOrganise(t.Context(), "{year}/{name}", 0); !errors.Is(err, ErrOrganiseBusy) {
		t.Errorf("organise preview during execution: %v", err)
	}
	current, err := s.ExportJobs(t.Context(), jobs[0].Batch)
	check(t, err)
	if len(current) != 2 || current[1].State != core.ExportPlanned {
		t.Fatalf("active journal was pruned: %+v", current)
	}
	s.CancelExport(jobs[0].Batch)
	check(t, <-done)
	current, err = s.ExportJobs(t.Context(), jobs[0].Batch)
	check(t, err)
	for _, job := range current {
		if job.State != core.ExportCancelled {
			t.Errorf("cancelled outcome: %+v", job)
		}
	}
	_, err = s.PlanExport(t.Context(), l.ids, dir, options)
	check(t, err)
}

func TestFilePlanningIsCancelledAndAwaitedByShutdown(t *testing.T) {
	s := &LibraryService{}
	ctx, finish, err := s.organise.hold(t.Context(), planning)
	check(t, err)
	done := make(chan error, 1)
	go func() { done <- s.ServiceShutdown() }()
	<-ctx.Done()
	select {
	case <-done:
		t.Error("shutdown did not wait for planning")
	default:
	}
	finish()
	check(t, <-done)
	if _, _, err := s.organise.hold(t.Context(), planning); !errors.Is(err, ErrOrganiseStopping) {
		t.Errorf("planning after shutdown: %v", err)
	}
}

func TestPlanExportGivesATakenNameASuffixInsteadOfRefusingTheBatch(t *testing.T) {
	l := newShownLibrary(t, 2)
	s := NewLibraryService(l.st, nil, nil, nil)
	dir := t.TempDir()
	check(t, os.WriteFile(filepath.Join(dir, "1.jpg"), []byte("keep this"), 0o600))
	jobs, err := s.PlanExport(t.Context(), l.ids, dir, core.ExportOptions{Format: core.ExportOriginal, Frame: -1})
	check(t, err)
	if len(jobs) != 2 || jobs[0].To != filepath.Join(dir, "1 (2).jpg") || jobs[1].To != filepath.Join(dir, "2.jpg") {
		t.Fatalf("planned destinations: %+v", jobs)
	}
}

type fixedRenderer []byte

func (r fixedRenderer) Render(context.Context, string, int, int) ([]byte, error) { return r, nil }

func TestRenderedExportBakesTheDisplayRotationOnlyWhenAsked(t *testing.T) {
	l := newShownLibrary(t, 1)
	s := NewLibraryService(l.st, nil, nil, l.cache)
	var rendered bytes.Buffer
	check(t, jpeg.Encode(&rendered, image.NewNRGBA(image.Rect(0, 0, 4, 2)), nil))
	l.cache.Renderer = fixedRenderer(rendered.Bytes())
	id, ctx := l.ids[0], t.Context()
	_, err := l.st.SetAdjustment(ctx, id, 1, false)
	check(t, err)
	f, err := l.st.AssetFile(ctx, id, false)
	check(t, err)
	for _, c := range []struct {
		adjust bool
		w, h   int
	}{{false, 4, 2}, {true, 2, 4}} {
		data, err := s.exportPixels(ctx, core.ExportJob{AssetID: id, From: f.Paths[0],
			Options: core.ExportOptions{Format: core.ExportPNG, Side: 1024, Adjust: c.adjust, Frame: -1}})
		check(t, err)
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		check(t, err)
		if cfg.Width != c.w || cfg.Height != c.h {
			t.Errorf("adjust %v: exported %dx%d, want %dx%d", c.adjust, cfg.Width, cfg.Height, c.w, c.h)
		}
	}
}
