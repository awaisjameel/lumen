// The stable launcher depends on no replaceable DLL and selects one generation.
package main

import (
	"context"
	"fmt"
	"os"
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
	if err != nil {
		update.ShowFailure(err)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
