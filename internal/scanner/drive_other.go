//go:build !windows

package scanner

import (
	"context"
	"errors"
)

func driveSeekPenalty(ctx context.Context, _ string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, errors.New("drive seek penalty is unavailable on this platform")
}
