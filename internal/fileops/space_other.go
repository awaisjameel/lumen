//go:build !windows

package fileops

import (
	"context"
	"errors"
)

// ErrCopySpace means a candidate would use the reserved free disk space.
var ErrCopySpace = errors.New("not enough space for compression and its reserve")

// CopySpace checks cancellation on platforms without the Windows disk query.
func CopySpace(ctx context.Context, _ string, _ int64) error { return ctx.Err() }
