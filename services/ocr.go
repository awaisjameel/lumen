package services

import "context"

// SetOCR turns reading the text in screenshots and documents on or off.
// Turning it on queues the photos it applies to; turning it off stops the
// work, and text already read stays searchable.
func (s *LibraryService) SetOCR(ctx context.Context, on bool) error {
	return s.ix.SetOCR(ctx, on)
}
