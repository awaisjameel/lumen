package services

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

func TestPlanExportGivesATakenNameASuffixInsteadOfRefusingTheBatch(t *testing.T) {
	l := newShownLibrary(t, 2)
	s := NewLibraryService(l.st, nil, nil)
	dir := t.TempDir()
	check(t, os.WriteFile(filepath.Join(dir, "1.jpg"), []byte("keep this"), 0o600))
	jobs, err := s.PlanExport(t.Context(), l.ids, dir, core.ExportOptions{Format: "original", Frame: -1})
	check(t, err)
	if len(jobs) != 2 || jobs[0].To != filepath.Join(dir, "1 (2).jpg") || jobs[1].To != filepath.Join(dir, "2.jpg") {
		t.Fatalf("planned destinations: %+v", jobs)
	}
}

type fixedRenderer []byte

func (r fixedRenderer) Render(context.Context, string, int, int) ([]byte, error) { return r, nil }

func TestRenderedExportBakesTheDisplayRotationOnlyWhenAsked(t *testing.T) {
	l := newShownLibrary(t, 1)
	s := NewLibraryService(l.st, nil, nil)
	s.media = l.cache
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
			Options: core.ExportOptions{Format: "png", Side: 1024, Adjust: c.adjust, Frame: -1}})
		check(t, err)
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		check(t, err)
		if cfg.Width != c.w || cfg.Height != c.h {
			t.Errorf("adjust %v: exported %dx%d, want %dx%d", c.adjust, cfg.Width, cfg.Height, c.w, c.h)
		}
	}
}
