//go:build !windows

package media

import "os/exec"

func contain(*exec.Cmd, uintptr) (func(), error) { return func() {}, nil }
