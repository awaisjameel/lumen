//go:build windows

package scanner

import (
	"io/fs"
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
