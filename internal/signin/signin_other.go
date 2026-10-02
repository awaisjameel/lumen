//go:build !windows

package signin

import "context"

// verify has no way to check elsewhere: Lumen's sign-in is Windows'.
func verify(context.Context, uintptr, string) error { return ErrUnavailable }
