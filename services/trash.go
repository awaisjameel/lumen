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

// ErrTrashChanged means what the user confirmed for Delete from disk is no
// longer in the Trash as it was: nothing was deleted.
var ErrTrashChanged = errors.New("the Trash changed since it was confirmed")

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

// TrashSelection is what Delete from disk would take for the trashed assets
// ids, or, with no ids, for everything in the Trash: the assets and how many
// files they have (an asset with exact copies has several), for the user to
// confirm. Ids that are not in the Trash are refused.
func (s *LibraryService) TrashSelection(ctx context.Context, ids []int64) (core.TrashSelection, error) {
	var files []store.TrashedFile
	var err error
	if len(ids) == 0 {
		files, err = s.store.AllTrashedFiles(ctx)
	} else {
		files, err = s.store.TrashedFiles(ctx, ids)
	}
	if err != nil {
		return core.TrashSelection{}, err
	}
	sel := core.TrashSelection{Assets: []int64{}, Files: len(files)}
	seen := make(map[int64]bool, len(files))
	for _, f := range files {
		if !seen[f.AssetID] {
			seen[f.AssetID] = true
			sel.Assets = append(sel.Assets, f.AssetID)
		}
	}
	return sel, nil
}

// DeleteFromDisk moves the files of a confirmed TrashSelection to the Windows
// Recycle Bin. If any of its assets left the Trash, or their files are no
// longer the number confirmed, it deletes nothing and fails with
// ErrTrashChanged: what was trashed meanwhile is never taken unasked.
func (s *LibraryService) DeleteFromDisk(ctx context.Context, sel core.TrashSelection) (core.DiskDeletion, error) {
	s.recycling.Lock()
	defer s.recycling.Unlock()
	files, err := s.store.TrashedFiles(ctx, sel.Assets)
	if errors.Is(err, store.ErrNotFound) {
		return core.DiskDeletion{}, fmt.Errorf("%w: %w", ErrTrashChanged, err)
	}
	if err != nil {
		return core.DiskDeletion{}, err
	}
	if len(files) != sel.Files {
		return core.DiskDeletion{}, fmt.Errorf("%w: %d files, %d confirmed", ErrTrashChanged, len(files), sel.Files)
	}
	return s.recycleFiles(ctx, files)
}

// recycleFiles moves files to the Recycle Bin and forgets the ones that left
// their folders, so their assets become missing and keep their names, tags
// and albums should the file come back. A file the shell would not recycle,
// or could not, is untouched and reported as kept. Nothing here deletes a
// file: that is the recycler's whole job, and it only ever recycles.
// The caller holds s.recycling, from reading the files to be recycled.
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

// viewsChanged tells every view: a block or the Trash changes which assets,
// Things and people are shown.
func (s *LibraryService) viewsChanged() {
	if s.ix != nil {
		s.ix.LibraryChanged(core.Changes{Assets: true, Tags: true, People: true})
	}
}
