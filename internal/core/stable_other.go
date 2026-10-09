//go:build !windows

package core

import (
	"context"
	"os"
)

// OpenStable opens a snapshot on platforms without Windows file leases.
func OpenStable(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.Open(path)
}
