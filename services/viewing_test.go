package services

import (
	"testing"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

func TestRenderCapabilityMatchesTheRuntimeAndSourceBudget(t *testing.T) {
	l := newShownLibrary(t, 1)
	s := NewLibraryService(l.st, nil, nil)
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
