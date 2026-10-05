package services

import (
	"testing"

	"lumen/internal/core"
)

func TestABatchGivesEachAssetItsOwnOutcomeAndGoesOnPastOneThatFails(t *testing.T) {
	l := newShownLibrary(t, 3)
	s := NewLibraryService(l.st, nil, nil, nil)
	ctx := t.Context()
	ids := []int64{1, 99, 2}
	outcomes, err := s.ApplyBatch(ctx, ids, core.BatchFavourite, 0)
	check(t, err)
	want := []core.BatchOutcome{{ID: 1}, {ID: 99, Code: string(CodeNotFound), Error: outcomes[1].Error}, {ID: 2}}
	if len(outcomes) != 3 || outcomes[0] != want[0] || outcomes[1] != want[1] || outcomes[2] != want[2] || outcomes[1].Error == "" {
		t.Fatalf("favourite outcomes = %+v", outcomes)
	}
	for id, fav := range map[int64]bool{1: true, 2: true, 3: false} {
		d, err := l.st.AssetDetail(ctx, id, false)
		check(t, err)
		if d.Favourite != fav {
			t.Errorf("asset %d favourite = %v, want %v", id, d.Favourite, fav)
		}
	}

	outcomes, err = s.ApplyBatch(ctx, []int64{1, 2}, core.BatchTrash, 0)
	check(t, err)
	if n, err := s.TrashCount(ctx); err != nil || n != 2 || outcomes[0].Error != "" || outcomes[1].Error != "" {
		t.Fatalf("after trashing two: %d in the Trash, %v, outcomes %+v", n, err, outcomes)
	}
	outcomes, err = s.ApplyBatch(ctx, []int64{2}, core.BatchRestore, 0)
	check(t, err)
	if n, err := s.TrashCount(ctx); err != nil || n != 1 || outcomes[0].Error != "" {
		t.Fatalf("after restoring one: %d in the Trash, %v, outcomes %+v", n, err, outcomes)
	}

	// An album that is not there fails each asset, not the call.
	outcomes, err = s.ApplyBatch(ctx, []int64{3}, core.BatchAlbum, 404)
	check(t, err)
	if outcomes[0].Code != string(CodeNotFound) {
		t.Fatalf("adding to a missing album: %+v", outcomes)
	}
	if _, err := s.ApplyBatch(ctx, []int64{3}, "shred", 0); codeOf(t, err) != CodeNotFound {
		t.Fatalf("an unknown action: %v", err)
	}
}

func TestTaggingASelectionMakesTheTagOnceAndReportsEachPhoto(t *testing.T) {
	l := newShownLibrary(t, 2)
	s := NewSearchService(l.st, nil, true, nil)
	ctx := t.Context()
	outcomes, err := s.TagAssets(ctx, []int64{1, 99, 2}, "Holiday")
	check(t, err)
	if len(outcomes) != 3 || outcomes[0].Error != "" || outcomes[1].Code != string(CodeNotFound) || outcomes[2].Error != "" {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	id, _, err := l.st.TagByName(ctx, "Holiday")
	check(t, err)
	for _, asset := range []int64{1, 2} {
		tags, err := l.st.AssetTags(ctx, asset, core.TagConfidenceDefault)
		check(t, err)
		if len(tags) != 1 || tags[0].ID != id {
			t.Errorf("asset %d tags = %+v, want the one tag %d", asset, tags, id)
		}
	}
	if _, err := s.TagAssets(ctx, []int64{1}, ""); codeOf(t, err) != CodeInvalidName {
		t.Fatalf("an empty tag name: %v", err)
	}
}
