//go:build !windows

package scanner

import (
	"context"
	"io/fs"
)

func isHiddenOrSystem(fs.DirEntry) bool { return false }

// Watch is Windows-only for now; elsewhere the startup reconcile scan and
// manual rescans keep the library current.
func Watch(ctx context.Context, dir string, onChange func(path string)) error { return nil }
