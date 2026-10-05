package services

import (
	"context"
	"errors"

	"lumen/internal/core"
	"lumen/internal/store"
)

// Adjustment reads presentation metadata under the current privacy gate.
func (s *LibraryService) Adjustment(ctx context.Context, id int64) (core.DisplayAdjustment, error) {
	a, err := s.store.Adjustment(ctx, id, false)
	if errors.Is(err, store.ErrNotFound) && s.gate.Unlocked() {
		return s.store.Adjustment(ctx, id, true)
	}
	return a, err
}

// SetAdjustment persists a non-destructive change and refreshes its consumers.
func (s *LibraryService) SetAdjustment(ctx context.Context, id int64, rotation int, flip bool) (core.DisplayAdjustment, error) {
	a, err := s.store.SetAdjustment(ctx, id, rotation, flip)
	if err == nil && s.ix != nil {
		s.ix.LibraryChanged(core.Changes{Assets: true})
	}
	return a, err
}

// PlaybackPosition reads resume history for an ordinary visible video.
func (s *LibraryService) PlaybackPosition(ctx context.Context, id int64) (int64, error) {
	return s.store.PlaybackPosition(ctx, id)
}

// SetPlaybackPosition records history only when the preference permits it.
func (s *LibraryService) SetPlaybackPosition(ctx context.Context, id, position int64) error {
	return s.store.SetPlaybackPosition(ctx, id, position)
}

// ClearPlaybackHistory forgets all recorded viewing positions.
func (s *LibraryService) ClearPlaybackHistory(ctx context.Context) error {
	return s.store.ClearPlaybackHistory(ctx)
}
