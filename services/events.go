package services

import "context"

// SetEventPinned pins or unpins the event holding asset id: pinned events
// sort first, and the pin follows the event as it regroups.
func (s *LibraryService) SetEventPinned(ctx context.Context, id int64, pinned bool) error {
	return unbuilt(ctx, id, pinned) // Phase 5 unit U1
}
