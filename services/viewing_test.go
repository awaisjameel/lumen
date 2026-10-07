package services

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"testing"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

func TestRenderCapabilityMatchesTheRuntimeAndSourceBudget(t *testing.T) {
	l := newShownLibrary(t, 1)
	s := NewLibraryService(l.st, nil, nil, nil)
	s.media = l.cache
	id, ctx := l.ids[0], t.Context()
	d, err := s.Asset(ctx, id)
	check(t, err)
	if d.Caps.Render {
		t.Fatal("advertised a missing render worker")
	}
	l.cache.Renderer = media.ProcessRenderer{}
	d, err = s.Asset(ctx, id)
	check(t, err)
	if !d.Caps.Render {
		t.Fatal("omitted the installed renderer for an admitted photo")
	}
	block, err := l.st.SetBlock(ctx, core.BlockAsset, id, "", core.BlockPhotos)
	check(t, err)
	s.gate.open(s.now())
	d, err = s.Asset(ctx, id)
	check(t, err)
	if !d.Caps.Render {
		t.Fatal("unlocked browsing lost its rendering capability")
	}
	check(t, l.st.RemoveBlock(ctx, block))
	check(t, l.st.SaveMetadata(ctx, "large", []store.MetadataResult{{AssetID: id, Width: 20_000, Height: 20_000}}))
	d, err = s.Asset(ctx, id)
	check(t, err)
	if d.Caps.Render {
		t.Fatal("advertised rendering beyond the worker's pixel budget")
	}
}

// pageRenderer renders page n as a picture n pixels wider than the first.
type pageRenderer struct{ t *testing.T }

func (r pageRenderer) Render(ctx context.Context, path string, orientation, side int) ([]byte, error) {
	return r.RenderPage(ctx, path, orientation, side, 0)
}

func (r pageRenderer) RenderPage(_ context.Context, _ string, _, _, page int) ([]byte, error) {
	var b bytes.Buffer
	check(r.t, jpeg.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 4+page, 3)), nil))
	return b.Bytes(), nil
}

func TestALaterPageIsRenderedAndServedOnlyUnderItsOwnRevision(t *testing.T) {
	l := newShownLibrary(t, 1)
	l.cache.Renderer = pageRenderer{t}
	ctx := t.Context()
	first, err := l.m.Render(ctx, 1, 1024)
	check(t, err)
	third, err := l.m.RenderPage(ctx, 1, 1024, 2)
	check(t, err)
	if first.Page != 0 || third.Page != 2 || first.Width != 4 || third.Width != 6 || first.Revision == third.Revision {
		t.Fatalf("pages = %+v and %+v", first, third)
	}
	if rec := l.get(t, "/render/1/1024?v="+third.Revision+"&page=2"); rec.Code != http.StatusOK {
		t.Fatalf("the third page: status %d", rec.Code)
	}
	// A page's picture is never sent under another page's revision.
	if rec := l.get(t, "/render/1/1024?v="+third.Revision); rec.Code != http.StatusConflict {
		t.Fatalf("the first page under the third's revision: status %d", rec.Code)
	}
	for _, url := range []string{"/render/1/1024?v=x&page=-1", "/render/1/1024?v=x&page=two", "/render/1/1024?v=x&page=99999999"} {
		if rec := l.get(t, url); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", url, rec.Code)
		}
	}
	if _, err := l.m.RenderPage(ctx, 1, 1024, -1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("page -1: %v, want not found", err)
	}
}
