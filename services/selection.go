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
	results, err := s.store.ApplyBatch(ctx, ids, action, albumID)
	if err != nil {
		return nil, err
	}
	if action == core.BatchTrash || action == core.BatchRestore {
		s.viewsChanged()
	}
	return batchOutcomes(results), nil
}

func batchOutcomes(results []store.BatchResult) []core.BatchOutcome {
	outcomes := make([]core.BatchOutcome, len(results))
	for i, result := range results {
		outcomes[i].ID = result.ID
		if result.Err != nil {
			outcomes[i].Code, outcomes[i].Error = string(errorCodeOf(result.Err)), result.Err.Error()
		}
	}
	return outcomes
}
