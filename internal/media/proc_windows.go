package media

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hideWindow stops a console program started from the GUI app from flashing
// a console window, and runs it below normal priority: FFmpeg reading videos
// for the index is background work, which must give way to the programs the
// user is working in (Lumen runs below normal for the same reason).
func hideWindow(cmd *exec.Cmd) { startHidden(cmd, windows.BELOW_NORMAL_PRIORITY_CLASS) }

// foreground is hideWindow at normal priority, for the conversion a playing
// video waits on.
func foreground(cmd *exec.Cmd) { startHidden(cmd, windows.NORMAL_PRIORITY_CLASS) }

func startHidden(cmd *exec.Cmd, priority uint32) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW | priority}
}
