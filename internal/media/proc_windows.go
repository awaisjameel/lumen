package media

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideWindow stops a console program started from the GUI app from flashing
// a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
