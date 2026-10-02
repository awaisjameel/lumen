package services

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

// ErrDeleteFailed means moving files to the Recycle Bin could not be done at
// all (the shell would not start the operation). Files it did recycle before
// failing are already forgotten by the library; the others were not touched.
var ErrDeleteFailed = errors.New("could not move the files to the Recycle Bin")

// recycler moves files to the Recycle Bin, saying what happened to each
// (fileops.Recycle; tests pass a fake).
type recycler func(ctx context.Context, paths []string) ([]fileops.Outcome, error)

// TrashAssets moves assets to Lumen's Trash: they leave every view, and
// their files are untouched.
func (s *LibraryService) TrashAssets(ctx context.Context, ids []int64) error {
	if err := s.store.TrashAssets(ctx, ids); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}

// RestoreAssets takes assets out of the Trash.
func (s *LibraryService) RestoreAssets(ctx context.Context, ids []int64) error {
	if err := s.store.RestoreAssets(ctx, ids); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}

// RestoreAllTrash takes everything out of the Trash.
func (s *LibraryService) RestoreAllTrash(ctx context.Context) error {
	if err := s.store.RestoreAllTrash(ctx); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}

// TrashCount is how many assets the Trash holds.
func (s *LibraryService) TrashCount(ctx context.Context) (int, error) {
	return s.store.TrashCount(ctx)
}

// TrashFileCount is how many files the trashed assets ids have on disk (an
// asset with exact copies has several), or, with no ids, how many everything
// in the Trash has: what Delete from disk will move to the Recycle Bin. Ids
// that are not in the Trash are refused.
func (s *LibraryService) TrashFileCount(ctx context.Context, ids []int64) (int, error) {
	var files []store.TrashedFile
	var err error
	if len(ids) == 0 {
		files, err = s.store.AllTrashedFiles(ctx)
	} else {
		files, err = s.store.TrashedFiles(ctx, ids)
	}
	return len(files), err
}

// DeleteFromDisk moves the files of trashed assets to the Windows Recycle
// Bin. Assets that are not in the Trash are refused.
func (s *LibraryService) DeleteFromDisk(ctx context.Context, ids []int64) (core.DiskDeletion, error) {
	files, err := s.store.TrashedFiles(ctx, ids)
	if err != nil {
		return core.DiskDeletion{}, err
	}
	return s.recycleFiles(ctx, files)
}

// DeleteAllTrashFromDisk moves the files of everything in the Trash to the
// Windows Recycle Bin.
func (s *LibraryService) DeleteAllTrashFromDisk(ctx context.Context) (core.DiskDeletion, error) {
	files, err := s.store.AllTrashedFiles(ctx)
	if err != nil {
		return core.DiskDeletion{}, err
	}
	return s.recycleFiles(ctx, files)
}

// recycleFiles moves files to the Recycle Bin and forgets the ones that left
// their folders, so their assets become missing and keep their names, tags
// and albums should the file come back. A file the shell would not recycle,
// or could not, is untouched and reported as kept. Nothing here deletes a
// file: that is the recycler's whole job, and it only ever recycles.
func (s *LibraryService) recycleFiles(ctx context.Context, files []store.TrashedFile) (core.DiskDeletion, error) {
	result := core.DiskDeletion{Kept: []string{}}
	if len(files) == 0 {
		return result, nil
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	recycle := s.recycle
	if recycle == nil {
		recycle = fileops.Recycle
	}
	outcomes, recycleErr := recycle(ctx, paths)
	if len(outcomes) != len(files) {
		// The recycler lost track of files; it cannot be said which are safe to forget.
		return result, fmt.Errorf("%w: %d outcomes for %d files", ErrDeleteFailed, len(outcomes), len(files))
	}

	var forget []store.TrashedFile
	failed := 0
	var failure error
	for i, o := range outcomes {
		switch o.Status {
		case fileops.Recycled:
			result.Recycled++
			forget = append(forget, files[i])
		case fileops.Gone:
			forget = append(forget, files[i])
		case fileops.Kept:
			result.Kept = append(result.Kept, o.Path)
		case fileops.Failed:
			failed++
			failure = cmp.Or(failure, o.Err)
			result.Kept = append(result.Kept, o.Path)
		}
	}
	if failed > 0 {
		// Never a path: the log must not say which photos were handled (they
		// may be blocked ones). The paths go back to the UI in Kept.
		slog.Warn("recycle files", "failed", failed, "kind", fmt.Sprintf("%T", failure))
	}
	// Even if ctx ended, the files already in the bin must leave the library.
	if err := s.store.RemoveRecycledFiles(context.WithoutCancel(ctx), forget); err != nil {
		return result, fmt.Errorf("forget recycled files: %w", err)
	}
	if len(forget) > 0 {
		s.viewsChanged()
	}
	if recycleErr != nil {
		if errors.Is(recycleErr, context.Canceled) || errors.Is(recycleErr, context.DeadlineExceeded) {
			return result, recycleErr
		}
		return result, fmt.Errorf("%w: %w", ErrDeleteFailed, recycleErr)
	}
	return result, nil
}

// viewsChanged tells every view: the Trash changes which assets, Things and
// people are shown.
func (s *LibraryService) viewsChanged() {
	if s.ix != nil {
		s.ix.LibraryChanged(core.Changes{Assets: true, Tags: true, People: true})
	}
}
