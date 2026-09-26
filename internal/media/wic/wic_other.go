//go:build !windows

package wic

import "errors"

// Decode is only available on Windows.
func Decode(path string, maxSide int) (pix []byte, w, h int, err error) {
	return nil, 0, 0, errors.New("wic: not available on this platform")
}
