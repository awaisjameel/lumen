//go:build !windows

package scanner

import (
	"context"
	"io/fs"
	"os"
)

func isHiddenOrSystem(fs.FileInfo) bool { return false }

func openShared(path string) (*os.File, error) { return os.Open(path) }

// Watch is Windows-only for now; elsewhere the startup reconcile scan and
// manual rescans keep the library current.
func Watch(ctx context.Context, dir string, ready func(), onChange func(path string)) error {
	<-ctx.Done()
	return nil
}
