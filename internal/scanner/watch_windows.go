//go:build windows

package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"lumen/internal/core"
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
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, err
	}
	event, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, err
	}
	read := &directoryRead{overlapped: windows.Overlapped{HEvent: event}}
	var pin runtime.Pinner
	pin.Pin(read)
	// Queue the first read before announcing readiness: changes made as soon
	// as Watch returns must already have a kernel notification buffer.
	if err := read.start(ctx, h); err != nil {
		pin.Unpin()
		_ = windows.CloseHandle(event)
		_ = windows.CloseHandle(h)
		return nil, err
	}

	// The reader waits for overlapped completion; this goroutine owns the handle.
	// On cancel it interrupts the read, retrying in case the reader was between
	// reads, and closes the handle only once the reader has returned, so
	// CancelIoEx never touches a closed (and possibly reused) handle.
	readerDone := make(chan error, 1)
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() {
			if ctx.Err() != nil {
				err = nil
			}
			done <- err
		}()
		defer pin.Unpin()
		defer func() { _ = windows.CloseHandle(event) }()
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
			case err = <-readerDone:
				return // The deferred function normalises cancellation to nil.
			case <-t.C:
			}
		}
	}()

	go func() { readerDone <- readChanges(ctx, h, dir, read, onChange) }()
	return done, nil
}

type directoryRead struct {
	buf        [64 << 10]byte // DWORD-aligned by Go's allocator
	overlapped windows.Overlapped
}

func (r *directoryRead) start(ctx context.Context, h windows.Handle) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Attributes too: a folder hidden or shown in Explorer leaves or joins
	// the library (scans skip hidden ones).
	const mask = windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_DIR_NAME |
		windows.FILE_NOTIFY_CHANGE_SIZE | windows.FILE_NOTIFY_CHANGE_LAST_WRITE | windows.FILE_NOTIFY_CHANGE_ATTRIBUTES
	return windows.ReadDirectoryChanges(h, &r.buf[0], uint32(len(r.buf)), true, mask, nil, &r.overlapped, 0)
}

// readChanges reports changes on h until ctx ends (nil) or reading fails.
func readChanges(ctx context.Context, h windows.Handle, dir string, read *directoryRead, onChange func(path string)) error {
	longDir := core.LongPath(ctx, dir)
	for {
		var n uint32
		err := windows.GetOverlappedResult(h, &read.overlapped, &n, true)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Too many changes to report one by one (a zero-byte result, or, on
		// network shares, ERROR_NOTIFY_ENUM_DIR): rescan everything.
		switch {
		case errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR) || (err == nil && n == 0):
			onChange(dir)
		case err != nil:
			onChange(dir) // what changed before the failure went unreported
			return err
		default:
			for off := uint32(0); ; {
				info := (*windows.FileNotifyInformation)(unsafe.Pointer(&read.buf[off]))
				name := unsafe.Slice(&info.FileName, info.FileNameLength/2)
				path := core.LongPath(ctx, filepath.Join(longDir, windows.UTF16ToString(name)))
				// Keep the source's spelling: the scanner records paths beneath dir,
				// even when Windows expands its 8.3 alias in notifications.
				rel, err := filepath.Rel(longDir, path)
				if err != nil {
					onChange(dir)
				} else {
					onChange(filepath.Join(dir, rel))
				}
				if info.NextEntryOffset == 0 {
					break
				}
				off += info.NextEntryOffset
			}
		}
		if err := read.start(ctx, h); err != nil {
			if ctx.Err() == nil {
				onChange(dir)
			}
			return err
		}
	}
}
