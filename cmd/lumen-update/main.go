// The independent helper owns runtime activation only, never library SQL.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"lumen/internal/update"
)

func main() {
	if err := run(); err != nil {
		if len(os.Args) > 1 && os.Args[1] == "--apply" {
			update.ShowFailure(err)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// Activation bounds each of its waits itself; this is the outer limit, with
	// room to prove two whole generations on a slow disk.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var err error
	switch {
	case len(os.Args) == 4 && os.Args[1] == "--apply":
		err = update.Apply(ctx, os.Args[2], os.Args[3])
	case len(os.Args) == 3 && os.Args[1] == "--bootstrap":
		err = update.Bootstrap(ctx, os.Args[2])
	case len(os.Args) == 3 && os.Args[1] == "--prepare-install":
		bounded, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		err = update.PrepareInstall(bounded, os.Args[2])
	case len(os.Args) == 3 && os.Args[1] == "--remove-generations":
		err = update.RemoveGenerations(ctx, os.Args[2])
	default:
		return update.ErrInvalid
	}
	return err
}
