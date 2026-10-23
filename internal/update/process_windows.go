package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

func startHelper(ctx context.Context, helper, root, dir, id string) error {
	// The independently bounded helper must survive the parent application's exit.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), helper, "--apply", root, id)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = cleanEnvironment("")
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for {
		var ready token
		if err := readJSON(filepath.Join(dir, "helper-ready-"+id+".json"), &ready); err == nil && ready.Token == id {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// launchHealth starts the selected generation's first, transaction-bound
// launch and waits for its health commit. An application that stops without
// committing is recovered from; the error tells Apply to reopen the previous one.
func launchHealth(ctx context.Context, root, dir string, a Activation) error {
	completed := func() bool {
		var state Activation
		return readJSON(filepath.Join(dir, activationFile), &state) == nil && state.Token == a.Token && state.Phase == core.UpdateComplete
	}
	// The application bounds its own health bootstrap and must outlive this helper.
	cmd := exec.CommandContext(context.WithoutCancel(ctx), filepath.Join(generationDir(root, a.candidateGeneration()), appName), "--update-health", a.Token)
	// No HideWindow here: Windows would apply it to the application's first
	// window, and the updated Lumen would open invisibly.
	cmd.Dir = filepath.Dir(cmd.Path)
	cmd.Env = cleanEnvironment(a.DataDir)
	if err := cmd.Start(); err != nil {
		return errors.Join(err, Recover(ctx, root))
	}
	done := make(chan struct{})
	go func() {
		// The health record determines success; this only announces process completion.
		_ = cmd.Wait()
		close(done)
	}()
	timer := time.NewTimer(120 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			// Still running, and holding the lease recovery needs: the next
			// launch recovers once it has gone.
			return fmt.Errorf("the updated application did not report its health: %w", ErrState)
		case <-ticker.C:
			if completed() {
				return nil
			}
		case <-done:
			if completed() {
				return nil
			}
			return errors.Join(fmt.Errorf("the updated application failed its health check: %w", ErrState), Recover(ctx, root))
		}
	}
}

// relaunch reopens Lumen through its stable launcher and lets go of it: the
// application must outlive the helper that started it.
func relaunch(ctx context.Context, root, dataDir string) error {
	cmd := exec.CommandContext(context.WithoutCancel(ctx), filepath.Join(root, launcherName))
	cmd.Dir = root
	cmd.Env = cleanEnvironment(dataDir)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func restoreLibrary(ctx context.Context, root string, a Activation) error {
	config, err := ConfiguredBuild()
	if err != nil {
		return err
	}
	exe := filepath.Join(generationDir(root, a.Previous.Generation), appName)
	if err := VerifyPublisher(ctx, exe, config.PublisherPins); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, exe, "--update-restore", root, a.Token)
	cmd.Env = cleanEnvironment(a.DataDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// register records the selected version where Windows lists installed
// programs. Failing to is logged, never fatal: the selection is already durable.
func register(root, version string) {
	if err := updateRegistration(root, version); err != nil {
		slog.Warn("record the installed version for Windows", "err", err)
	}
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

// Launch validates one coherent generation and holds a shared runtime lease
// for as long as the application it starts runs. An application that started
// and then failed is reported as its *exec.ExitError.
func Launch(ctx context.Context, root, dataDir string, args []string) error {
	// No argument is approved for relaunch yet; none reaches replaceable code.
	if len(args) != 0 || (dataDir != "" && !filepath.IsAbs(dataDir)) {
		return ErrInvalid
	}
	lease, err := AcquireLease(ctx, root, false)
	if err != nil {
		return err
	}
	defer func() { _ = lease.Close() }()
	for _, barrier := range []string{installingFile, barrierFile} {
		if _, err := os.Stat(filepath.Join(root, barrier)); !errors.Is(err, os.ErrNotExist) {
			return ErrState
		}
	}
	active, err := readActive(root)
	if err != nil {
		return err
	}
	if active.Pending != "" {
		return ErrState
	}
	// Bytes and the publisher were proven when this generation was installed
	// or activated. An ordinary, possibly offline launch confirms the folder
	// is still that whole generation without reading a gigabyte of models.
	if err := VerifyLayout(ctx, generationDir(root, active.Generation), active.Release); err != nil {
		return err
	}
	exe := filepath.Join(generationDir(root, active.Generation), appName)
	cmd := exec.CommandContext(ctx, exe)
	cmd.Env = cleanEnvironment(dataDir)
	cmd.Dir = filepath.Dir(exe)
	return cmd.Run()
}
