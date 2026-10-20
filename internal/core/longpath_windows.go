//go:build windows

package core

import (
	"context"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// LongPath expands Windows 8.3 aliases so paths reported by the shell match
// those supplied by callers. A vanished file keeps its name while its parent
// is expanded; an unavailable path keeps its original spelling.
func LongPath(ctx context.Context, path string) string {
	if ctx.Err() != nil || !strings.Contains(path, "~") {
		return path
	}
	if long, ok := getLongPathName(ctx, path); ok {
		return long
	}
	if dir, ok := getLongPathName(ctx, filepath.Dir(path)); ok {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}

func getLongPathName(ctx context.Context, path string) (string, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, windows.MAX_PATH)
	for {
		if ctx.Err() != nil {
			return "", false
		}
		n, err := windows.GetLongPathName(p, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return "", false
		}
		if n < uint32(len(buf)) {
			return windows.UTF16ToString(buf[:n]), true
		}
		buf = make([]uint16, n+1)
	}
}
