package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/index"
	"lumen/internal/media"
	"lumen/internal/search"
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

	cache := media.NewCache(paths.ThumbDir, media.Decoders{FFmpeg: paths.FFmpeg()})
	engine, aiErr := startAI(paths, st)
	if aiErr != nil {
		slog.Error("AI unavailable; search and Things are off", "err", aiErr)
	}
	events := &services.Emitter{}
	ix := index.New(st, paths, cache, engine, aiErr, events)

	app := application.New(application.Options{
		Name:        "Lumen",
		Description: "Local, private AI photo gallery",
		Services: []application.Service{
			application.NewService(&background{ix: ix, search: engine}),
			application.NewService(services.NewLibraryService(st, ix)),
			application.NewService(services.NewSearchService(st, engine)),
			application.NewServiceWithOptions(services.NewMediaService(st, cache),
				application.ServiceOptions{Route: "/media"}),
		},
		Assets: application.AssetOptions{
			Handler:        spa(application.AssetFileServerFS(assets)),
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

// startAI loads ONNX Runtime and the SigLIP model description. Models load
// lazily later; this only fails if they aren't installed or the runtime
// can't start, in which case Lumen runs without search and Things.
func startAI(paths core.Paths, st *store.Store) (*search.Engine, error) {
	if err := ai.Init(paths.Runtime); err != nil {
		return nil, err
	}
	m, err := ai.LoadModel(paths.SigLIP())
	if err != nil {
		return nil, err
	}
	return search.NewEngine(st, m), nil
}

// background starts the search engine and indexing once the app is running,
// and stops them cleanly before the database closes.
type background struct {
	ix     *index.Indexer
	search *search.Engine // nil without AI
	cancel context.CancelFunc
}

func (b *background) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	ctx, b.cancel = context.WithCancel(context.WithoutCancel(ctx))
	if b.search != nil {
		b.search.Start(ctx)
	}
	b.ix.Start(ctx)
	return nil
}

func (b *background) ServiceShutdown() error {
	b.cancel()
	b.ix.Stop()
	if b.search != nil {
		b.search.Close() // after the indexer: it no longer adds embeddings
		ai.Shutdown()    // after every model is closed
	}
	return nil
}

// spa serves the UI, answering client-side routes (/things, /settings…),
// which are not files, with index.html, so they survive a reload.
func spa(files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Ext(r.URL.Path) == "" && r.URL.Path != "/" {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
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
