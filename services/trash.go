package services

import (
	"context"

	"lumen/internal/core"
)

// TrashAssets moves assets to Lumen's Trash: they leave every view, and
// their files are untouched.
func (s *LibraryService) TrashAssets(ctx context.Context, ids []int64) error {
	return unbuilt(ctx, ids) // Phase 5 unit U2
}

// RestoreAssets takes assets out of the Trash.
func (s *LibraryService) RestoreAssets(ctx context.Context, ids []int64) error {
	return unbuilt(ctx, ids) // Phase 5 unit U2
}

// RestoreAllTrash takes everything out of the Trash.
func (s *LibraryService) RestoreAllTrash(ctx context.Context) error {
	return unbuilt(ctx) // Phase 5 unit U2
}

// TrashCount is how many assets the Trash holds.
func (s *LibraryService) TrashCount(ctx context.Context) (int, error) {
	return 0, unbuilt(ctx) // Phase 5 unit U2
}

// DeleteFromDisk moves the files of trashed assets to the Windows Recycle
// Bin. Assets that are not in the Trash are refused.
func (s *LibraryService) DeleteFromDisk(ctx context.Context, ids []int64) (core.DiskDeletion, error) {
	return core.DiskDeletion{}, unbuilt(ctx, ids) // Phase 5 unit U2
}

// DeleteAllTrashFromDisk moves the files of everything in the Trash to the
// Windows Recycle Bin.
func (s *LibraryService) DeleteAllTrashFromDisk(ctx context.Context) (core.DiskDeletion, error) {
	return core.DiskDeletion{}, unbuilt(ctx) // Phase 5 unit U2
}
