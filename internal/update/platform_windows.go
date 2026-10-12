package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

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

// linked refuses one path that is a symbolic link, junction, mount point or
// any other reparse point: the updater writes and removes real files only.
func linked(name string) error {
	fi, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return ErrInvalid
	}
	ptr, err := windows.UTF16PtrFromString(name)
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
	return nil
}

// noLinks checks name and every folder between it and anchor, the folder
// Lumen owns (the installation or its updater state). What lies above the
// anchor is the user's own choice: a profile or a drive reached through a
// junction is an ordinary place to install to, and no privilege is crossed.
func noLinks(anchor, name string) error {
	rel, err := filepath.Rel(anchor, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ErrInvalid
	}
	for p := filepath.Clean(name); rel != "."; p, rel = filepath.Dir(p), filepath.Dir(rel) {
		if err := linked(p); err != nil {
			return err
		}
	}
	return nil
}

// Lease prevents activation while any library from this installation is open.
type Lease struct{ handle windows.Handle }

// AcquireLease shares the process lease for normal launches, exclusively for activation.
func AcquireLease(ctx context.Context, root string, exclusive bool) (*Lease, error) {
	return acquire(ctx, root, leaseFile, exclusive)
}

// acquire opens dir's lock file, waiting while another process holds it in a
// way that excludes this one.
func acquire(ctx context.Context, dir, name string, exclusive bool) (*Lease, error) {
	p, err := windows.UTF16PtrFromString(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	access, share := uint32(windows.GENERIC_READ), uint32(windows.FILE_SHARE_READ)
	if exclusive {
		access, share = windows.GENERIC_READ|windows.GENERIC_WRITE, 0
	}
	for {
		// Opening the reparse point itself means a link planted at the lock's
		// name is never followed.
		h, err := windows.CreateFile(p, access, share, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err == nil {
			return &Lease{handle: h}, nil
		}
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, fmt.Errorf("acquire %s: %w", name, err)
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

// onBattery reports whether the PC is running on its battery. Desktops, and
// any failure to tell, count as mains power (as indexing's politeness does).
func onBattery() bool {
	// SYSTEM_POWER_STATUS: ACLineStatus is 0 on battery, 1 on mains, 255 unknown.
	var status struct {
		ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag byte
		BatteryLifeTime, BatteryFullLifeTime                            uint32
	}
	ok, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus").Call(uintptr(unsafe.Pointer(&status)))
	return ok != 0 && status.ACLineStatus == 0
}
