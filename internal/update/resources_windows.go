package update

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"lumen/internal/core"
)

// automaticSafe defers checks when connection cost cannot be established.
// Windows' connection profile and power state are queried locally, without a request.
func automaticSafe(ctx context.Context, policy core.UpdatePolicy) bool {
	// The power state costs one system call; ask it before starting PowerShell.
	if policy.DeferBattery && onBattery() {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const script = `$ErrorActionPreference='Stop'; $p=[Windows.Networking.Connectivity.NetworkInformation,Windows,ContentType=WindowsRuntime]::GetInternetConnectionProfile(); if($null -eq $p){exit 2}; $c=$p.GetConnectionCost(); if($c.NetworkCostType -ne 'Unrestricted' -or $c.Roaming -or $c.OverDataLimit -or $c.ApproachingDataLimit){exit 2}; 'allowed'`
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = cleanEnvironment("")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "allowed"
}
