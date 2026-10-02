package services

import (
	"context"
	"errors"
	"fmt"
	"os"

	"lumen/internal/core"
	"lumen/internal/store"
)

// DeleteCopies moves every file of the asset assetID except keep to the
// Windows Recycle Bin: the extra files of exact copies. Phase 5 unit U9.
func (s *LibraryService) DeleteCopies(ctx context.Context, assetID int64, keep string) (core.DiskDeletion, error) {
	// keep must be one of the asset's files and the asset one the library
	// shows, else nothing is named; a file the shell would not recycle stays
	// where it is and is reported in Kept.
	extra, err := s.store.ExtraCopies(ctx, assetID, keep)
	if err != nil {
		return core.DiskDeletion{}, err
	}
	// The copy that stays is the only one left once the rest are recycled, so
	// it must be there to stay: never recycle the others on the strength of a
	// database row alone.
	info, err := os.Stat(keep)
	switch {
	case errors.Is(err, os.ErrNotExist) || (err == nil && !info.Mode().IsRegular()):
		return core.DiskDeletion{}, store.ErrNotFound
	case err != nil:
		return core.DiskDeletion{}, fmt.Errorf("check the file to keep: %w", err)
	}
	return s.recycleFiles(ctx, extra)
}
