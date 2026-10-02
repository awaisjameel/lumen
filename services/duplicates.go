package services

import (
	"context"
	"errors"
	"fmt"
	"os"

	"lumen/internal/core"
	"lumen/internal/store"
)

// DeleteCopies moves the files recycle of the asset assetID, the extra files
// of exact copies, to the Windows Recycle Bin, keeping the file keep. keep and
// every path of recycle must be current files of the asset, and recycle must
// not be empty or hold keep, else nothing happens and the call fails with
// ErrNotFound. Files of the asset in neither list are left alone, and so is a
// file edited since it was indexed: it is reported in Kept.
func (s *LibraryService) DeleteCopies(ctx context.Context, assetID int64, keep string, recycle []string) (core.DiskDeletion, error) {
	// One request at a time, from the checks to the removal of the rows: two
	// requests that each keep a different file would otherwise each see the
	// other's keeper as an extra copy and recycle every file.
	s.recycling.Lock()
	defer s.recycling.Unlock()

	// keep and recycle must be current files of a photo the library shows,
	// and only the files the user reviewed are touched (not one the scanner
	// found since); else nothing is named.
	kept, extra, err := s.store.CopiesToRecycle(ctx, assetID, keep, recycle)
	if err != nil {
		return core.DiskDeletion{}, err
	}
	// The file that stays is the only one left once the rest are recycled, so
	// it must be there to stay, and still the file that was indexed: never
	// recycle the others on the strength of a database row alone. A keeper
	// that changed means the copies are no longer known to be identical, so
	// the whole call is refused.
	switch state, err := checkOnDisk(kept); state {
	case fileOK:
	case fileChanged, fileMissing:
		return core.DiskDeletion{}, store.ErrNotFound
	default:
		return core.DiskDeletion{}, fmt.Errorf("check the file to keep: %w", err)
	}

	// A file to recycle that was edited since it was indexed is not the copy
	// the user reviewed: it stays where it is and is reported. One already gone
	// goes on to the recycler, which reports it as gone, so its row is forgotten.
	var send []store.TrashedFile
	var left []string
	for _, f := range extra {
		switch state, _ := checkOnDisk(f); state {
		case fileOK, fileMissing:
			send = append(send, f.TrashedFile)
		default:
			left = append(left, f.Path)
		}
	}
	result, err := s.recycleFiles(ctx, send)
	result.Kept = append(result.Kept, left...)
	return result, err
}

// diskState is what a file's row and the file on disk say together.
type diskState int

const (
	fileOK         diskState = iota // the file is the one the row records
	fileMissing                     // there is no such file
	fileChanged                     // it is not a regular file, or its size or time differ from the row's
	fileUnreadable                  // the check itself failed
)

// checkOnDisk compares the file with the size and modified time its row
// records, which the scanner takes from the same call.
func checkOnDisk(f store.CopyFile) (diskState, error) {
	info, err := os.Stat(f.Path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fileMissing, nil
	case err != nil:
		return fileUnreadable, err
	case !info.Mode().IsRegular() || info.Size() != f.Size || info.ModTime().UnixNano() != f.MTime:
		return fileChanged, nil
	}
	return fileOK, nil
}
