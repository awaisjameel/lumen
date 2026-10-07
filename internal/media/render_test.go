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

// pagedRenderer renders each page as a picture as wide as its number says.
type pagedRenderer struct {
	t     *testing.T
	calls []int
}

func (r *pagedRenderer) Render(ctx context.Context, path string, orientation, side int) ([]byte, error) {
	return r.RenderPage(ctx, path, orientation, side, 0)
}

func (r *pagedRenderer) RenderPage(_ context.Context, _ string, _, _, page int) ([]byte, error) {
	r.calls = append(r.calls, page)
	return renderJPEG(r.t, 8+page, 8), nil
}

func TestEachPageOfAFileIsRenderedAndCachedOnItsOwn(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	r := &pagedRenderer{t: t}
	c.Renderer = r
	source := filepath.Join(t.TempDir(), "scan.tiff")
	if err := os.WriteFile(source, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, a, err := c.RenderPage(t.Context(), 1, source, 1, 1024, 0)
	if err != nil {
		t.Fatal(err)
	}
	third, b, err := c.RenderPage(t.Context(), 1, source, 1, 1024, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first == third || a.Revision == b.Revision || a.Width != 8 || b.Width != 10 || a.Page != 0 || b.Page != 2 {
		t.Fatalf("pages share a render: %+v at %s, %+v at %s", a, first, b, third)
	}
	// The first page is the same render RenderFile always made; both are cached.
	if again, info, err := c.RenderFile(t.Context(), 1, source, 1, 1024); err != nil || again != first || info.Revision != a.Revision {
		t.Fatalf("the first page moved: %s, %v", again, err)
	}
	if _, _, err := c.RenderPage(t.Context(), 1, source, 1, 1024, 2); err != nil || len(r.calls) != 2 {
		t.Fatalf("renders = %v, %v; want one for each page", r.calls, err)
	}
	// A renderer that knows nothing of pages is not asked for one.
	c.Renderer = renderFunc(func(context.Context, string, int, int) ([]byte, error) { return renderJPEG(t, 8, 8), nil })
	if _, _, err := c.RenderPage(t.Context(), 1, source, 1, 1024, 1); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
}
