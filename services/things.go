package services

import (
	"context"

	"lumen/internal/core"
)

// RenameThing gives a Thing a new name, which the search box matches from
// then on. An empty name gives a renamed built-in back its own.
func (s *SearchService) RenameThing(ctx context.Context, id int64, name string) error {
	if err := s.store.RenameTag(ctx, id, name); err != nil {
		return err
	}
	s.thingsChanged()
	return nil
}

// SetThingPinned pins or unpins a Thing: pinned Things sort first.
func (s *SearchService) SetThingPinned(ctx context.Context, id int64, pinned bool) error {
	if err := s.store.SetTagPinned(ctx, id, pinned); err != nil {
		return err
	}
	s.thingsChanged()
	return nil
}

// thingsChanged tells open views that Things were renamed or pinned: their
// names, order and suggestions are read again (the tags change).
func (s *SearchService) thingsChanged() {
	if s.ix != nil {
		s.ix.LibraryChanged(core.Changes{Tags: true})
	}
}
