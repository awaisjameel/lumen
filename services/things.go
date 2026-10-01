package services

import "context"

// RenameThing gives a Thing a new name, which the search box matches from
// then on. An empty name gives a renamed built-in back its own.
func (s *SearchService) RenameThing(ctx context.Context, id int64, name string) error {
	return unbuilt(ctx, id, name) // Phase 5 unit U1
}

// SetThingPinned pins or unpins a Thing: pinned Things sort first.
func (s *SearchService) SetThingPinned(ctx context.Context, id int64, pinned bool) error {
	return unbuilt(ctx, id, pinned) // Phase 5 unit U1
}
