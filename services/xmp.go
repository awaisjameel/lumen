package services

import (
	"context"

	"lumen/internal/core"
	"lumen/internal/index"
)

// SetXMP turns writing XMP sidecars on or off. Phase 5 unit U11.
func (s *LibraryService) SetXMP(ctx context.Context, on bool) error {
	return s.ix.SetXMP(ctx, on)
}

// DeleteXMP turns XMP sidecars off and moves every sidecar Lumen wrote, and
// that nobody has edited since, to the Recycle Bin.
func (s *LibraryService) DeleteXMP(ctx context.Context) (core.XMPDelete, error) {
	var recycle index.Recycler
	if s.recycle != nil {
		recycle = index.Recycler(s.recycle)
	}
	return s.ix.DeleteXMP(ctx, recycle)
}

// ExportXMPNow writes every photo's sidecar that is out of date now.
// Phase 5 unit U11.
func (s *LibraryService) ExportXMPNow(ctx context.Context) (core.XMPExport, error) {
	return s.ix.ExportXMPNow(ctx)
}
