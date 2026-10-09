package fileops

import (
	"context"
	"path/filepath"
	"slices"
	"sync"

	"golang.org/x/sys/windows"
)

// LeaseCopyFolders holds each ancestor against writes and renames, closing the
// junction-swap window between checking a path and publishing into it.
func LeaseCopyFolders(ctx context.Context, dirs ...string) (func(), error) {
	var handles []windows.Handle
	var once sync.Once
	release := func() {
		once.Do(func() {
			for _, h := range handles {
				_ = windows.CloseHandle(h)
			}
		})
	}
	seen := make(map[string]bool)
	for _, dir := range dirs {
		var ancestors []string
		for dir = filepath.Clean(dir); ; dir = filepath.Dir(dir) {
			ancestors = append(ancestors, dir)
			if filepath.Dir(dir) == dir {
				break
			}
		}
		slices.Reverse(ancestors)
		for _, path := range ancestors {
			if seen[path] {
				continue
			}
			if err := ctx.Err(); err != nil {
				release()
				return nil, err
			}
			p, err := windows.UTF16PtrFromString(path)
			if err != nil {
				release()
				return nil, err
			}
			h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
			if err != nil {
				release()
				return nil, err
			}
			handles = append(handles, h)
			var info windows.ByHandleFileInformation
			if err := windows.GetFileInformationByHandle(h, &info); err != nil {
				release()
				return nil, err
			}
			if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
				release()
				return nil, ErrDestinationFolder
			}
			seen[path] = true
		}
	}
	return release, nil
}
