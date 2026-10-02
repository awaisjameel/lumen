//go:build windows

package signin

import (
	"context"
	"runtime"
)

// verify tries Windows Hello, then the account password. Both are COM or
// UI calls that belong to one thread from start to finish, so the goroutine
// is locked to its thread for the call.
func verify(ctx context.Context, owner uintptr, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	verdict, err := helloVerify(ctx, owner, reason)
	if err != nil {
		return err
	}
	switch verdict {
	case helloVerified:
		return nil
	case helloRefused:
		return ErrNotVerified
	case helloAbsent:
	}
	return verifyPassword(ctx, owner, reason)
}
