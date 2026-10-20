package core

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ReadDirShared lists a folder sorted by name, as os.ReadDir does, but opens
// it with OpenDirShared.
func ReadDirShared(path string) ([]fs.DirEntry, error) {
	f, err := OpenDirShared(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // The listing owns no writable data.
	entries, err := f.ReadDir(-1)
	slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, err
}

// WalkDir is filepath.WalkDir reading each folder with ReadDirShared, so a
// scan never stops the user deleting or renaming a folder inside it. It
// visits the same entries in the same order and honours fs.SkipDir and
// fs.SkipAll the same way.
func WalkDir(root string, fn fs.WalkDirFunc) error {
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkDir(root, fs.FileInfoToDirEntry(info), fn)
	}
	if errors.Is(err, fs.SkipDir) || errors.Is(err, fs.SkipAll) {
		return nil
	}
	return err
}

func walkDir(path string, d fs.DirEntry, fn fs.WalkDirFunc) error {
	if err := fn(path, d, nil); err != nil || !d.IsDir() {
		if errors.Is(err, fs.SkipDir) && d.IsDir() {
			err = nil // skip this folder's contents
		}
		return err
	}
	entries, err := ReadDirShared(path)
	if err != nil {
		// A second call reports the folder could not be read.
		if err = fn(path, d, err); err != nil {
			if errors.Is(err, fs.SkipDir) && d.IsDir() {
				err = nil
			}
			return err
		}
	}
	for _, e := range entries {
		if err := walkDir(filepath.Join(path, e.Name()), e, fn); err != nil {
			if errors.Is(err, fs.SkipDir) {
				break // skip the rest of this folder
			}
			return err
		}
	}
	return nil
}
