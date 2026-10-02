//go:build windows

package fileops

import "golang.org/x/sys/windows"

// publishNew moves the new file from to the name to, which must not exist:
// MoveFileExW without MOVEFILE_REPLACE_EXISTING fails with
// ERROR_ALREADY_EXISTS (fs.ErrExist) rather than overwrite, whatever raced
// the call. os.Rename is not used for this because Go's always replaces.
func publishNew(from, to string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dst, 0)
}
