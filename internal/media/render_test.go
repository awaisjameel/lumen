package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

type renderFunc func(context.Context, string, int, int) ([]byte, error)

type inspectionRenderer struct {
	renderFunc
	inspections int
}

func (r *inspectionRenderer) Inspect(context.Context, string) (core.MediaInspection, error) {
	r.inspections++
	return core.MediaInspection{Pages: 1}, nil
}

func TestImageInspectionWaitsForRenderingAndCancelsBeforeLaunchingAWorker(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	started, release := make(chan struct{}), make(chan struct{})
	r := &inspectionRenderer{renderFunc: func(context.Context, string, int, int) ([]byte, error) {
		close(started)
		<-release
		return renderJPEG(t, 4, 3), nil
	}}
	c.Renderer = r
	source := filepath.Join(t.TempDir(), "source.tiff")
	if err := os.WriteFile(source, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := c.RenderFile(t.Context(), 1, source, 1, 1024)
		done <- err
	}()
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Inspect(ctx, source, core.KindPhoto)
	close(release)
	if !errors.Is(err, context.Canceled) || r.inspections != 0 {
		t.Fatalf("inspection escaped render admission: %v, %d calls", err, r.inspections)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := c.Inspect(t.Context(), source, core.KindPhoto); err != nil || r.inspections != 1 {
		t.Fatalf("released admission was not reusable: %v", err)
	}
}

func (f renderFunc) Render(ctx context.Context, path string, orientation, side int) ([]byte, error) {
	return f(ctx, path, orientation, side)
}

func renderJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestRenderCacheIsKeyedBySourceRevisionAndRequestedSize(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	source := filepath.Join(t.TempDir(), "source.tiff")
	if err := os.WriteFile(source, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.Renderer = renderFunc(func(context.Context, string, int, int) ([]byte, error) { calls++; return renderJPEG(t, 400, 200), nil })
	a, info, err := c.RenderFile(t.Context(), 1, source, 1, 1024)
	if err != nil || info.Width != 400 || info.Height != 200 {
		t.Fatalf("render: %+v %v", info, err)
	}
	b, _, err := c.RenderFile(t.Context(), 1, source, 1, 1024)
	if err != nil || a != b || calls != 1 {
		t.Fatalf("cache was not reused: %v %d", err, calls)
	}
	b, _, err = c.RenderFile(t.Context(), 1, source, 1, 512)
	if err != nil || a == b || calls != 2 {
		t.Fatalf("requested size did not invalidate: %v %d", err, calls)
	}
	if err := os.WriteFile(source, []byte("changed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _, err = c.RenderFile(t.Context(), 1, source, 1, 1024)
	if err != nil || a == b || calls != 3 {
		t.Fatalf("source did not invalidate: %v %d", err, calls)
	}
}

func TestRenderRefusesChangedSourcesAndInvalidOutputs(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	source := filepath.Join(t.TempDir(), "source.tiff")
	if err := os.WriteFile(source, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.Renderer = renderFunc(func(context.Context, string, int, int) ([]byte, error) {
		if err := os.WriteFile(source, []byte("changed"), 0o600); err != nil {
			t.Fatal(err)
		}
		return renderJPEG(t, 400, 200), nil
	})
	if _, _, err := c.RenderFile(t.Context(), 1, source, 1, 1024); err == nil {
		t.Fatal("published a stale decode")
	}
	c.Renderer = renderFunc(func(context.Context, string, int, int) ([]byte, error) { return []byte("not pixels"), nil })
	if _, _, err := c.RenderFile(t.Context(), 1, source, 1, 1024); err == nil {
		t.Fatal("published corrupt output")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := c.RenderFile(ctx, 1, source, 1, 1024); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(c.Dir, "render"))
	if err == nil && len(entries) != 0 {
		t.Fatal("failed output entered the render cache")
	}
}
