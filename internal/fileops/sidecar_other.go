//go:build !windows

package fileops

import "errors"

// publishNew is Windows-only: the rename that never replaces is
// MoveFileExW without MOVEFILE_REPLACE_EXISTING.
func publishNew(string, string) error { return errors.ErrUnsupported }
