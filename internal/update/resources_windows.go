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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	const script = `$ErrorActionPreference='Stop'; $p=[Windows.Networking.Connectivity.NetworkInformation,Windows,ContentType=WindowsRuntime]::GetInternetConnectionProfile(); if($null -eq $p){exit 2}; $c=$p.GetConnectionCost(); if($c.NetworkCostType -ne 'Unrestricted' -or $c.Roaming -or $c.OverDataLimit -or $c.ApproachingDataLimit){exit 2}; if($env:LUMEN_DEFER_BATTERY -eq '1'){ Add-Type -AssemblyName System.Windows.Forms; if([Windows.Forms.SystemInformation]::PowerStatus.PowerLineStatus -ne 'Online'){exit 2} }; 'allowed'`
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return false
	}
	cmd := exec.CommandContext(ctx, filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = cleanEnvironment("")
	if policy.DeferBattery {
		cmd.Env = append(cmd.Env, "LUMEN_DEFER_BATTERY=1")
	}
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "allowed"
}
