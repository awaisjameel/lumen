//go:build !windows

package fileops

import "errors"

// Move is Windows-only: it relies on MoveFileExW's no-replace guarantee.
func Move(string, string) error { return errors.ErrUnsupported }

// RemoveDir is Windows-only, with Move.
func RemoveDir(string) error { return errors.ErrUnsupported }
