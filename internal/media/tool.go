package media

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"
)

// RunTool runs a bundled codec below normal priority, with the renderer's
// process-tree containment and memory limit. Diagnostics never retain source metadata.
func RunTool(ctx context.Context, executable, directory string, args ...string) error {
	return runContainedToolAs(ctx, hideWindow, executable, directory, io.Discard, 512<<20, args...)
}

// runContainedToolAs captures bounded output and lets foreground decodes keep normal priority.
func runContainedToolAs(ctx context.Context, start func(*exec.Cmd), executable, directory string, output io.Writer, memory uintptr, args ...string) error {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = directory
	cmd.Stdout, cmd.Stderr = output, io.Discard
	cmd.WaitDelay = 2 * time.Second
	start(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	cleanup, err := contain(cmd, memory)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	// Cancellation closes the job before Wait: child processes cannot keep it alive.
	stop := context.AfterFunc(ctx, cleanup)
	defer func() { stop(); cleanup() }()
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("run bundled codec: %w", err)
	}
	return nil
}
