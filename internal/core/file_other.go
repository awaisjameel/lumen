//go:build !windows

package core

import "os"

// OpenShared opens a file for reading; elsewhere than Windows, an open file
// never stops it being renamed or deleted.
func OpenShared(path string) (*os.File, error) { return os.Open(path) }
