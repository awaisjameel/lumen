package services

import (
	"context"

	"lumen/internal/core"
	"lumen/internal/store"
)

// Selection resolves the view or ranked search IDs under current visibility.
func (s *LibraryService) Selection(ctx context.Context, view core.View, ids []int64, search bool) (core.Selection, error) {
	return s.store.Selection(ctx, view, ids, search)
}

// ValidateSelection refuses stale membership before an action is admitted.
func (s *LibraryService) ValidateSelection(ctx context.Context, sel core.Selection) error {
	return s.store.ValidateSelection(ctx, sel)
}

// ApplyBatch does one thing to each of ids and says what became of each: an
// asset that fails does not stop the others. The UI sends a large selection
// a part at a time, so that it can be stopped between them.
func (s *LibraryService) ApplyBatch(ctx context.Context, ids []int64, action core.BatchAction, albumID int64) ([]core.BatchOutcome, error) {
	var one func(id int64) error
	switch action {
	case core.BatchFavourite, core.BatchUnfavourite:
		one = func(id int64) error { return s.store.SetFavourite(ctx, id, action == core.BatchFavourite) }
	case core.BatchAlbum, core.BatchRemoveAlbum:
		one = func(id int64) error { return s.store.SetAlbumItem(ctx, id, albumID, action == core.BatchAlbum) }
	case core.BatchTrash:
		one = func(id int64) error { return s.store.TrashAssets(ctx, []int64{id}) }
	case core.BatchRestore:
		one = func(id int64) error { return s.store.RestoreAssets(ctx, []int64{id}) }
	default:
		return nil, store.ErrNotFound
	}
	outcomes := batchOutcomes(ctx, ids, one)
	if action == core.BatchTrash || action == core.BatchRestore {
		s.viewsChanged()
	}
	return outcomes, nil
}

// batchOutcomes runs one for each id until ctx ends; the ids it did not
// reach fail with the context's error.
func batchOutcomes(ctx context.Context, ids []int64, one func(id int64) error) []core.BatchOutcome {
	outcomes := make([]core.BatchOutcome, len(ids))
	for i, id := range ids {
		err := ctx.Err()
		if err == nil {
			err = one(id)
		}
		outcomes[i].ID = id
		if err != nil {
			outcomes[i].Code, outcomes[i].Error = string(errorCodeOf(err)), err.Error()
		}
	}
	return outcomes
}
