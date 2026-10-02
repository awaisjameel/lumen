//go:build windows

package fileops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Move renames the file at from to to, on the same volume. It is
// MoveFileExW with no flags: MOVEFILE_REPLACE_EXISTING is not set, so a move
// never overwrites whatever is at to, however late it appeared; and
// MOVEFILE_COPY_ALLOWED is not set either, so it never degrades into a copy
// and delete across volumes. A folder is refused: Lumen moves files.
func Move(from, to string) error {
	if !filepath.IsAbs(from) || !filepath.IsAbs(to) {
		return errors.New("move: the paths must be absolute")
	}
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("move: the path is a folder")
	}
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	switch err := windows.MoveFileEx(src, dst, 0); {
	case errors.Is(err, windows.ERROR_ALREADY_EXISTS), errors.Is(err, windows.ERROR_FILE_EXISTS):
		return fmt.Errorf("%w: %s", ErrExists, to)
	case err != nil:
		return &os.LinkError{Op: "move", Old: from, New: to, Err: err}
	}
	return nil
}

// RemoveDir removes an empty folder. RemoveDirectory cannot remove a file, so
// whatever is at path, nothing but an empty folder can go.
func RemoveDir(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := windows.RemoveDirectory(p); err != nil {
		return &os.PathError{Op: "remove folder", Path: path, Err: err}
	}
	return nil
}
