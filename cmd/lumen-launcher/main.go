// The stable launcher depends on no replaceable DLL and selects one generation.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"lumen/internal/update"
)

func main() {
	exe, err := os.Executable()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		root := filepath.Dir(exe)
		err = update.RecoverPending(ctx, root)
		cancel()
		if err == nil {
			err = update.Launch(context.Background(), root, os.Getenv("LUMEN_DATA_DIR"), os.Args[1:])
		}
	}
	// The application ran and ended badly: that is its own failure, which it
	// has reported itself, not an installation that needs repair.
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		update.ShowFailure(err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
