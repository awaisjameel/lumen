//go:build windows

package scanner

import (
	"context"
	"io/fs"
	"log/slog"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func isHiddenOrSystem(d fs.DirEntry) bool {
	info, err := d.Info()
	if err != nil {
		return false
	}
	if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return a.FileAttributes&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0
	}
	return false
}

// Watch reports changes anywhere under dir until ctx is cancelled. It uses one
// recursive ReadDirectoryChangesW handle per source instead of one watch per
// sub-folder. onChange receives the full path of each changed entry; if Windows
// drops events (buffer overflow) it receives dir itself, meaning "rescan all".
func Watch(ctx context.Context, dir string, onChange func(path string)) error {
	_, err := watch(ctx, dir, onChange)
	return err
}

// watch is Watch plus a channel that closes once the handle is released.
func watch(ctx context.Context, dir string, onChange func(path string)) (<-chan struct{}, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return nil, err
	}

	// The reader blocks in ReadDirectoryChanges; this goroutine owns the handle.
	// On cancel it interrupts the read, retrying in case the reader was between
	// reads, and closes the handle only once the reader has returned, so
	// CancelIoEx never touches a closed (and possibly reused) handle.
	readerDone := make(chan struct{})
	released := make(chan struct{})
	go func() {
		defer close(released)
		defer windows.CloseHandle(h)
		select {
		case <-readerDone:
			return
		case <-ctx.Done():
		}
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			windows.CancelIoEx(h, nil)
			select {
			case <-readerDone:
				return
			case <-t.C:
			}
		}
	}()

	go func() {
		defer close(readerDone)
		const mask = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
			windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE
		buf := make([]byte, 64<<10) // must be DWORD-aligned; Go's allocator guarantees it
		for {
			var n uint32
			err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, mask, &n, nil, 0)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				slog.Warn("folder watch stopped", "dir", dir, "err", err)
				return
			}
			if n == 0 { // overflow: too many changes to report individually
				onChange(dir)
				continue
			}
			for off := uint32(0); ; {
				info := (*windows.FileNotifyInformation)(unsafe.Pointer(&buf[off]))
				name := unsafe.Slice(&info.FileName, info.FileNameLength/2)
				onChange(filepath.Join(dir, windows.UTF16ToString(name)))
				if info.NextEntryOffset == 0 {
					break
				}
				off += info.NextEntryOffset
			}
		}
	}()
	return released, nil
}
