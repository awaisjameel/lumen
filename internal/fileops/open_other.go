//go:build !windows

package fileops

import "errors"

func Open(string, bool) error {
	return errors.New("opening media in another application requires Windows")
}
