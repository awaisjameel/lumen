package services

import (
	"context"

	"lumen/internal/core"
)

// SetXMP turns writing XMP sidecars on or off. Phase 5 unit U11.
func (s *LibraryService) SetXMP(ctx context.Context, on bool) error {
	return s.ix.SetXMP(ctx, on)
}

// ExportXMPNow writes every photo's sidecar that is out of date now.
// Phase 5 unit U11.
func (s *LibraryService) ExportXMPNow(ctx context.Context) (core.XMPExport, error) {
	return s.ix.ExportXMPNow(ctx)
}
