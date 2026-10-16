package services

import (
	"context"

	"lumen/internal/core"
	"lumen/internal/places"
)

// Counts supplies cheap sidebar totals. Refresh after local mutations and
// library:changed (assets or people); index:progress alone changes no count.
func (s *LibraryService) Counts(ctx context.Context) (core.LibraryCounts, error) {
	return s.store.Counts(ctx)
}

// SetTakenAt sets wall-clock seconds for all IDs atomically. The UI should
// validate its calendar input before converting to seconds, and reload the
// detail after success. library:changed invalidates grids and collections.
func (s *LibraryService) SetTakenAt(ctx context.Context, ids []int64, takenAt int64) error {
	if err := s.store.SetTakenAt(ctx, ids, takenAt); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}

// RestoreTakenAt removes date corrections for all IDs atomically.
func (s *LibraryService) RestoreTakenAt(ctx context.Context, ids []int64) error {
	if err := s.store.RestoreTakenAt(ctx, ids); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}

// SetLocation sets coordinates for all IDs atomically, retaining the metadata
// location. A newly matching block applies before this call returns.
func (s *LibraryService) SetLocation(ctx context.Context, ids []int64, lat, lon float64) error {
	if err := s.store.SetLocation(ctx, ids, lat, lon); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}

// RestoreLocation removes location corrections for all IDs atomically.
func (s *LibraryService) RestoreLocation(ctx context.Context, ids []int64) error {
	if err := s.store.RestoreLocation(ctx, ids); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}

// FindCities searches the bundled catalogue, never the network. The result
// limit defaults to 20 and is held to 1..50; an empty query yields no matches.
func (*LibraryService) FindCities(ctx context.Context, query string, limit int) ([]core.CityMatch, error) {
	return places.FindCities(ctx, query, limit)
}
