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
)

type renderFunc func(context.Context, string, int, int) ([]byte, error)

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

func TestRenderCacheUsesSourceAndPresentationRevisions(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	source := filepath.Join(t.TempDir(), "source.tiff")
	if err := os.WriteFile(source, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.Renderer = renderFunc(func(context.Context, string, int, int) ([]byte, error) { calls++; return renderJPEG(t, 400, 200), nil })
	a, info, err := c.RenderFile(t.Context(), 1, source, 1, 1024, 0)
	if err != nil || info.Width != 400 || info.Height != 200 {
		t.Fatalf("render: %+v %v", info, err)
	}
	b, _, err := c.RenderFile(t.Context(), 1, source, 1, 1024, 0)
	if err != nil || a != b || calls != 1 {
		t.Fatalf("cache was not reused: %v %d", err, calls)
	}
	b, _, err = c.RenderFile(t.Context(), 1, source, 1, 1024, 1)
	if err != nil || a == b || calls != 2 {
		t.Fatalf("adjustment did not invalidate: %v %d", err, calls)
	}
	if err := os.WriteFile(source, []byte("changed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _, err = c.RenderFile(t.Context(), 1, source, 1, 1024, 0)
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
	if _, _, err := c.RenderFile(t.Context(), 1, source, 1, 1024, 0); err == nil {
		t.Fatal("published a stale decode")
	}
	c.Renderer = renderFunc(func(context.Context, string, int, int) ([]byte, error) { return []byte("not pixels"), nil })
	if _, _, err := c.RenderFile(t.Context(), 1, source, 1, 1024, 0); err == nil {
		t.Fatal("published corrupt output")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := c.RenderFile(ctx, 1, source, 1, 1024, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(c.Dir, "render"))
	if err == nil && len(entries) != 0 {
		t.Fatal("failed output entered the render cache")
	}
}
