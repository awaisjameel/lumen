package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/media"
	"lumen/internal/store"
	"lumen/services"
)

// The built Vue app, embedded into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := run(); err != nil {
		slog.Error("lumen stopped", "err", err)
		os.Exit(1)
	}
}

// run wires everything together and blocks until the window closes. The
// database is closed by a defer on every path, after the indexer has stopped.
func run() error {
	paths, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	setupLogging(paths)

	st, err := store.Open(paths.DBPath)
	if err != nil {
		return fmt.Errorf("open library: %w", err)
	}
	defer st.Close()

	thumbs := media.GoThumbnailer{}
	events := &services.Emitter{}
	ix := index.New(st, paths, thumbs, events)

	app := application.New(application.Options{
		Name:        "Lumen",
		Description: "Local, private AI photo gallery",
		Services: []application.Service{
			application.NewService(&engine{ix: ix}),
			application.NewService(services.NewLibraryService(st, ix)),
			application.NewServiceWithOptions(services.NewMediaService(st, paths, thumbs),
				application.ServiceOptions{Route: "/media"}),
		},
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(assets),
			DisableLogging: true, // thousands of thumbnail requests
		},
		// Two copies indexing one database would fight; focus the first instead.
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.lumen.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if w := application.Get().Window.Current(); w != nil {
					w.Restore()
					w.Focus()
				}
			},
		},
		Mac:     application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		Windows: application.WindowsOptions{AdditionalBrowserArgs: devtoolsArgs()},
	})
	events.App = app

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Lumen",
		Width:            1280,
		Height:           820,
		MinWidth:         720,
		MinHeight:        480,
		BackgroundColour: application.NewRGB(17, 17, 19),
		URL:              "/",
		Windows:          application.WindowsWindow{BackdropType: application.Mica},
	})

	return app.Run()
}

// engine starts background indexing once the app is running and stops it
// cleanly before the database closes.
type engine struct{ ix *index.Indexer }

func (e *engine) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	e.ix.Start(context.WithoutCancel(ctx))
	return nil
}

func (e *engine) ServiceShutdown() error {
	e.ix.Stop()
	return nil
}

// devtoolsArgs opens the WebView's DevTools protocol on localhost when
// LUMEN_DEVTOOLS_PORT is set, so UI tests can drive the real app. Off by default.
func devtoolsArgs() []string {
	port := os.Getenv("LUMEN_DEVTOOLS_PORT")
	if port == "" {
		return nil
	}
	return []string{"--remote-debugging-port=" + port, "--remote-debugging-address=127.0.0.1"}
}

// setupLogging writes to %LOCALAPPDATA%\Lumen\lumen.log (local only, never
// uploaded), and to stderr when there is a console (dev builds). The file
// stays open until the process exits, so even the final error from run is
// recorded.
func setupLogging(paths core.Paths) {
	f, err := os.OpenFile(filepath.Join(paths.DataDir, "lumen.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Warn("no log file; logging to stderr only", "err", err)
		return
	}
	var w io.Writer = f
	// A GUI build has no stderr, and io.MultiWriter stops at the first
	// failing writer, so stderr is only added when it can be written to.
	if _, err := os.Stderr.Stat(); err == nil {
		w = io.MultiWriter(f, os.Stderr)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})))
}
