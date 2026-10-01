package services

import (
	"context"
	"errors"
)

// unbuilt is what a Phase 5 method answers until its unit lands: the lead
// declares each new method first, so the bindings and the bridge are fixed
// before the units are built in parallel. This file goes once none is left.
func unbuilt(ctx context.Context, _ ...any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.ErrUnsupported
}
