package services

import (
	"context"

	"lumen/internal/core"
)

// Selection resolves the view or ranked search IDs under current visibility.
func (s *LibraryService) Selection(ctx context.Context, view core.View, ids []int64, search bool) (core.Selection, error) {
	return s.store.Selection(ctx, view, ids, search)
}

// ValidateSelection refuses stale membership before an action is admitted.
func (s *LibraryService) ValidateSelection(ctx context.Context, sel core.Selection) error {
	return s.store.ValidateSelection(ctx, sel)
}
