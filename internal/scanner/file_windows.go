//go:build windows

package scanner

import (
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// isHiddenOrSystem reports whether Windows marks the file or folder hidden or
// as part of the system.
func isHiddenOrSystem(info fs.FileInfo) bool {
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0
	}
	return false
}

// openShared opens a file for reading without stopping anyone else from
// renaming or deleting it meanwhile: os.Open does not share deletion, so
// Explorer would refuse to move a photo while it is being fingerprinted.
func openShared(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
