package fileops

import (
	"context"
	"errors"

	"golang.org/x/sys/windows"
)

// ErrCopySpace means a candidate would use the reserved free disk space.
var ErrCopySpace = errors.New("not enough space for compression and its reserve")

// CopySpace reserves temporary and retained copy bytes plus a fixed safety margin.
func CopySpace(ctx context.Context, folder string, bytes int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(folder)
	if err != nil {
		return err
	}
	var free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, nil, nil); err != nil {
		return err
	}
	if bytes < 0 || free < uint64(bytes)+(64<<20) {
		return ErrCopySpace
	}
	return nil
}
