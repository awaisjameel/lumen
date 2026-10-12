package update

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// VerifyPublisher asks Windows to verify both Authenticode and the pinned signer.
// Certificate renewal adds a reviewed certificate digest to the compiled pins.
func VerifyPublisher(ctx context.Context, file string, pins []string) error {
	if len(pins) == 0 {
		return ErrPublisher
	}
	for _, pin := range pins {
		if !validHash(pin) {
			return ErrPublisher
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// The command is constant. Paths travel as an environment value, never code.
	const script = `$ErrorActionPreference='Stop'; $s=Get-AuthenticodeSignature -LiteralPath $env:LUMEN_VERIFY_FILE; if ($s.Status -ne 'Valid' -or $null -eq $s.SignerCertificate -or $null -eq $s.TimeStamperCertificate) { exit 2 }; $h=[Security.Cryptography.SHA256]::Create(); try { $b=$h.ComputeHash($s.SignerCertificate.RawData); [BitConverter]::ToString($b).Replace('-','').ToLowerInvariant() | ConvertTo-Json -Compress } finally { $h.Dispose() }`
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	ps := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, ps, "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = append(os.Environ(), "LUMEN_VERIFY_FILE="+file)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("windows signature verification: %w", ErrPublisher)
	}
	var digest string
	if json.Unmarshal(out, &digest) != nil {
		return ErrPublisher
	}
	if _, err := hex.DecodeString(digest); err != nil || !slices.Contains(pins, strings.ToLower(digest)) {
		return ErrPublisher
	}
	return nil
}
