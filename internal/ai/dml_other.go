//go:build !windows

package ai

import "errors"

// preloadDirectML: DirectML is Windows-only, so models run on the CPU.
func preloadDirectML(string) error { return errors.New("DirectML is only available on Windows") }
