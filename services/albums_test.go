package services

import (
	"errors"
	"testing"

	"lumen/internal/core"
	"lumen/internal/store"
)

func TestSmartAlbumPreviewUsesCurrentTagConfidenceAndRefusesBlockedReferences(t *testing.T) {
	l, tag := screenshotLibrary(t)
	ctx := t.Context()
	check(t, l.st.SaveEmbeddings(ctx, "fixture", "v", []store.EmbedResult{{
		AssetID: l.a, Vec: []int8{1}, Scale: 1, Tags: []store.TagScore{{TagID: tag, Score: 0.1}},
	}}))
	check(t, l.st.TagAsset(ctx, l.b, tag))
	svc := &LibraryService{store: l.st}
	rule := core.AlbumRule{Tag: tag}
	check(t, l.st.SetFloat(ctx, core.SettingTagConfidence, core.TagConfidenceMax))
	got, err := svc.PreviewAlbum(ctx, rule)
	check(t, err)
	if got.Count != 1 || len(got.Items) != 1 || got.Items[0].ID != l.b {
		t.Fatalf("strict confidence preview = %+v", got)
	}
	check(t, l.st.SetFloat(ctx, core.SettingTagConfidence, core.TagConfidenceDefault))
	got, err = svc.PreviewAlbum(ctx, rule)
	check(t, err)
	if got.Count != 2 || len(got.Items) != 2 {
		t.Fatalf("lower confidence preview = %+v", got)
	}
	_, err = l.st.SetBlock(ctx, core.BlockTag, tag, "", core.BlockLabel)
	check(t, err)
	got, err = svc.PreviewAlbum(ctx, rule)
	if !errors.Is(err, store.ErrNotFound) || got.Count != 0 || len(got.Items) != 0 {
		t.Fatalf("blocked reference preview = %+v, %v", got, err)
	}
}
