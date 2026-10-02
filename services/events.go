package services

import "context"

// SetEventPinned pins or unpins the event holding asset id: pinned events
// sort first, and the pin follows the event as it regroups.
func (s *LibraryService) SetEventPinned(ctx context.Context, id int64, pinned bool) error {
	gap, err := s.store.EventGapHours(ctx)
	if err != nil {
		return err
	}
	return s.store.SetEventPinned(ctx, id, pinned, gap)
}
