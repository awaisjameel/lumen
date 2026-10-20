//go:build windows

package testenv

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// ShortPath makes a scratch folder addressed by its Windows 8.3 alias.
// Filesystems without short names cannot exercise this behaviour.
func ShortPath(tb testing.TB) string {
	tb.Helper()
	dir := tb.TempDir()
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		tb.Fatal(err)
	}
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
		if err != nil {
			tb.Fatal(err)
		}
		if n >= uint32(len(buf)) {
			buf = make([]uint16, n+1)
			continue
		}
		short := windows.UTF16ToString(buf[:n])
		if strings.EqualFold(short, dir) {
			tb.Skip("this filesystem does not provide 8.3 aliases")
		}
		return short
	}
}
