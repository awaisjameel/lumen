package update

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"

	"lumen/internal/core"
)

func cleanEnvironment(dataDir string) []string {
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "LUMEN_") {
			continue
		}
		env = append(env, entry)
	}
	if dataDir != "" {
		env = append(env, "LUMEN_DATA_DIR="+dataDir)
	}
	return env
}

func startHelper(ctx context.Context, helper, root, token string) error {
	var install Installation
	if err := readJSON(filepath.Join(root, ".lumen-install.json"), &install); err != nil {
		return err
	}
	dir, err := StateDirectory(install)
	if err != nil {
		return err
	}
	// The independently bounded helper must survive the parent application's exit.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), helper, "--apply", root, token)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = cleanEnvironment("")
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		var ready struct {
			Token string `json:"token"`
		}
		if err := readJSON(filepath.Join(dir, "helper-ready-"+token+".json"), &ready); err == nil && ready.Token == token {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func launchHealth(ctx context.Context, root, dir string, a Activation) error {
	active, err := LoadActive(ctx, root)
	if err != nil {
		return err
	}
	// The application bounds its own health bootstrap and must outlive this helper.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), filepath.Join(root, "runtime", active.Generation, "lumen-app.exe"), "--update-health", a.Token)
	cmd.Dir = filepath.Dir(cmd.Path)
	cmd.Env = cleanEnvironment(a.DataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return Recover(ctx, root)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(120 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return ErrState
		case <-ticker.C:
			var state Activation
			if err := readJSON(filepath.Join(dir, "activation.json"), &state); err == nil && state.Token == a.Token && state.Phase == core.UpdateComplete {
				return nil
			}
		case <-done:
			var state Activation
			if err := readJSON(filepath.Join(dir, "activation.json"), &state); err == nil && state.Phase == core.UpdateComplete {
				return nil
			}
			if err := Recover(ctx, root); err != nil {
				return err
			}
			return Launch(ctx, root, a.DataDir, nil)
		}
	}
}

func restoreLibrary(ctx context.Context, root string, a Activation) error {
	config, err := ConfiguredBuild()
	if err != nil {
		return err
	}
	exe := filepath.Join(root, "runtime", a.Previous.Generation, "lumen-app.exe")
	if err := VerifyPublisher(ctx, exe, config.PublisherPins); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, "--update-restore", root, a.Token)
	cmd.Env = cleanEnvironment(a.DataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func updateRegistration(root, version string) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\Lumen contributorsLumen`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = key.Close() }()
	location, _, err := key.GetStringValue("InstallLocation")
	if err != nil {
		return err
	}
	if core.Fold(filepath.Clean(location)) != core.Fold(filepath.Clean(root)) {
		return ErrInvalid
	}
	return key.SetStringValue("DisplayVersion", version)
}

// Launch validates one coherent generation and holds a shared runtime lease.
func Launch(ctx context.Context, root, dataDir string, args []string) error {
	if dataDir != "" && !filepath.IsAbs(dataDir) {
		return ErrInvalid
	}
	lease, err := AcquireLease(ctx, root, false)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	if _, err := os.Stat(filepath.Join(root, ".lumen-installing")); !errors.Is(err, os.ErrNotExist) {
		return ErrState
	}
	if _, err := os.Stat(filepath.Join(root, ".lumen-activating.json")); !errors.Is(err, os.ErrNotExist) {
		return ErrState
	}
	active, err := LoadActive(ctx, root)
	if err != nil {
		return err
	}
	if active.Pending != "" {
		return ErrState
	}
	exe := filepath.Join(root, "runtime", active.Generation, "lumen-app.exe")
	// Authenticode was verified during the requested install/activation. Normal
	// offline launches verify inventory bytes without contacting certificate services.
	if len(args) != 0 {
		return ErrInvalid
	}
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = cleanEnvironment(dataDir)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Run()
}
