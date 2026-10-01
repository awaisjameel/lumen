//go:build windows

package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Watch reports changes anywhere under dir until ctx is cancelled, then
// returns nil; if the watch fails (the folder was deleted, its drive
// unplugged) it returns why. It uses one recursive ReadDirectoryChangesW
// handle per source instead of one watch per sub-folder. ready, if not nil,
// is called once the watch runs. onChange receives the full path of each
// changed entry; if Windows drops events (buffer overflow) it receives dir
// itself, meaning "rescan all".
func Watch(ctx context.Context, dir string, ready func(), onChange func(path string)) error {
	done, err := watch(ctx, dir, onChange)
	if err != nil {
		return err
	}
	if ready != nil {
		ready()
	}
	return <-done
}

// watch starts watching dir. done receives the watch's outcome (nil when
// cancelled) once its handle is released.
func watch(ctx context.Context, dir string, onChange func(path string)) (<-chan error, error) {
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
	readerDone := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() { done <- err }()
		defer windows.CloseHandle(h) //nolint:errcheck // nothing to do if closing fails
		select {
		case err = <-readerDone:
			return
		case <-ctx.Done():
		}
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for {
			_ = windows.CancelIoEx(h, nil) // fails once the read has ended; the loop checks
			select {
			case <-readerDone:
				return // cancelled: err stays nil
			case <-t.C:
			}
		}
	}()

	go func() { readerDone <- readChanges(ctx, h, dir, onChange) }()
	return done, nil
}

// readChanges reports changes on h until ctx ends (nil) or reading fails.
func readChanges(ctx context.Context, h windows.Handle, dir string, onChange func(path string)) error {
	// Attributes too: a folder hidden or shown in Explorer leaves or joins
	// the library (scans skip hidden ones).
	const mask = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
		windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE | windows.FILE_NOTIFY_CHANGE_ATTRIBUTES
	buf := make([]byte, 64<<10) // must be DWORD-aligned; Go's allocator guarantees it
	for {
		var n uint32
		err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, mask, &n, nil, 0)
		if ctx.Err() != nil {
			return nil //nolint:nilerr // cancelled: the read was interrupted on purpose
		}
		// Too many changes to report one by one (a zero-byte result, or, on
		// network shares, ERROR_NOTIFY_ENUM_DIR): rescan everything.
		if errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) || (err == nil && n == 0) {
			onChange(dir)
			continue
		}
		if err != nil {
			onChange(dir) // what changed before the failure went unreported
			return err
		}
		for off := uint32(0); ; {
			info := (*windows.FileNotifyInformation)(unsafe.Pointer(&buf[off]))
			name := unsafe.Slice(&info.FileName, info.FileNameLength/2)
			onChange(longName(filepath.Join(dir, windows.UTF16ToString(name))))
			if info.NextEntryOffset == 0 {
				break
			}
			off += info.NextEntryOffset
		}
	}
}

// longName expands 8.3 short names ("PHOTOS~1") that Windows may report,
// so a changed path matches the one the scan recorded. A path that no longer
// exists keeps its short last part, but its folder is expanded.
func longName(path string) string {
	if !strings.Contains(path, "~") {
		return path
	}
	if long, ok := getLongPathName(path); ok {
		return long
	}
	if dir, ok := getLongPathName(filepath.Dir(path)); ok {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}

func getLongPathName(path string) (string, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetLongPathName(p, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return "", false
		}
		if n <= uint32(len(buf)) {
			return windows.UTF16ToString(buf[:n]), true
		}
		buf = make([]uint16, n) // n is the size needed
	}
}
