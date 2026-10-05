//go:build !windows

package media

import "os/exec"

func containRender(_ *exec.Cmd) (func(), error) { return func() {}, nil }
