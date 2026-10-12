package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

func replace(from, to string) error {
	src, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	dst, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(src, dst, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func freeSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var available uint64
	err = windows.GetDiskFreeSpaceEx(p, &available, nil, nil)
	return available, err
}

func windowsBuild() uint32 { return windows.RtlGetVersion().BuildNumber }

func noLinks(name string) error {
	for p := filepath.Clean(name); ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return ErrInvalid
		}
		ptr, err := windows.UTF16PtrFromString(p)
		if err != nil {
			return err
		}
		attrs, err := windows.GetFileAttributes(ptr)
		if err != nil {
			return err
		}
		if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return ErrInvalid
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}

// Lease prevents activation while any library from this installation is open.
type Lease struct{ handle windows.Handle }

// AcquireLease shares the process lease for normal launches, exclusively for activation.
func AcquireLease(ctx context.Context, root string, exclusive bool) (*Lease, error) {
	if err := noLinks(root); err != nil {
		return nil, err
	}
	p, err := windows.UTF16PtrFromString(filepath.Join(root, ".lumen-process-lock"))
	if err != nil {
		return nil, err
	}
	access, share := uint32(windows.GENERIC_READ), uint32(windows.FILE_SHARE_READ)
	if exclusive {
		access, share = windows.GENERIC_READ|windows.GENERIC_WRITE, 0
	}
	for {
		h, err := windows.CreateFile(p, access, share, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err == nil {
			return &Lease{handle: h}, nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, fmt.Errorf("acquire runtime lease: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Close releases the process handle after all runtime work has stopped.
func (l *Lease) Close() error { return windows.CloseHandle(l.handle) }

func processIdentity() (pid uint32, createdAt uint64, err error) {
	var created, exited, kernel, user windows.Filetime
	err = windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user)
	return uint32(os.Getpid()), uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), err
}

func waitParent(ctx context.Context, pid uint32, identity uint64) error {
	if pid == 0 || identity == 0 {
		return ErrInvalid
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid) //nolint:misspell // SYNCHRONIZE is the Windows API constant's spelling, as in media's process tests.
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return err
	}
	if uint64(created.HighDateTime)<<32|uint64(created.LowDateTime) != identity {
		return nil
	}
	for {
		status, err := windows.WaitForSingleObject(h, 100)
		if err != nil {
			return err
		}
		if status == windows.WAIT_OBJECT_0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}
