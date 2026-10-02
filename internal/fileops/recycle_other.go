//go:build !windows

package fileops

import (
	"context"
	"errors"
)

// Recycle is Windows-only: it needs the shell's Recycle Bin.
func Recycle(context.Context, []string) ([]Outcome, error) {
	return nil, errors.ErrUnsupported
}
