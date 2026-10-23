// Lumen is a local, private AI photo gallery for Windows: a Go engine and a
// Vue UI in one Wails window. main wires the parts together.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	windowevents "github.com/wailsapp/wails/v3/pkg/events"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/diag"
	"lumen/internal/index"
	"lumen/internal/media"
	"lumen/internal/media/vips"
	"lumen/internal/people"
	"lumen/internal/search"
	"lumen/internal/store"
	"lumen/internal/update"
	"lumen/services"
)

// The built Vue app, embedded into the binary.
//
//go:embed all:frontend/dist
var assets embed.FS

//go:embed frontend/public/lumen.png
var appIcon []byte

func main() {
	if len(os.Args) == 4 && os.Args[1] == "--update-restore" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		a, err := update.RestoreRequest(ctx, os.Args[2], os.Args[3])
		if err == nil {
			err = store.RestoreBackup(ctx, a.Backup, filepath.Join(a.DataDir, "library.db"))
		}
		cancel()
		if err != nil {
			slog.Error("restore update library", "err", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--render-worker" {
		if err := media.RenderWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("lumen stopped", "err", err)
		fatalDialog("Lumen could not start:\n\n" + err.Error()) // there is no window to say it in
		os.Exit(1)
	}
}

// run wires everything together and blocks until the window closes. The
// database is closed by a defer on every path that opened it, after the
// indexer has stopped.
func run() (result error) {
	paths, err := core.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	lowerPriority()
	index.CountOwnPrograms() // before the window starts WebView2
	version := diag.Version()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// An installed Lumen runs only as the generation its installation selects,
	// and holds the installation's runtime lease until everything has stopped.
	admission, err := update.Admit(context.Background(), executable, version, paths.DataDir, os.Args[1:])
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, admission.Close()) }()
	reports := diag.New(paths.DataDir, version)

	// The application comes first: a second copy of Lumen must hand over to
	// the running one before it touches the database (two would fight over
	// it) or loads the AI. New exits then.
	var window atomic.Pointer[application.WebviewWindow]
	app := application.New(application.Options{
		Name:         "Lumen",
		Description:  "Local, private AI photo gallery",
		Icon:         appIcon,
		MarshalError: services.MarshalError, // for errors outside bound methods; see service
		PanicHandler: recordPanics(reports),
		Assets: application.AssetOptions{
			Handler:        spa(application.AssetFileServerFS(assets)),
			DisableLogging: true, // thousands of thumbnail requests
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: instanceID(paths),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				// The window itself: Window.Current is nil until it was
				// first activated (Lumen started behind another app).
				if w := window.Load(); w != nil {
					w.Restore()
					w.Focus()
				}
			},
		},
		Mac: application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
		Windows: application.WindowsOptions{
			AdditionalBrowserArgs: devtoolsArgs(),
			WebviewUserDataPath:   webviewDataPath(paths),
		},
	})
	setupLogging(paths) // only the first instance may open or rotate this library's log
	slog.Info("lumen starting", "version", version, "data", paths.DataDir, "runtime", paths.Runtime, "libvips", vips.Version())
	// Only now, past the hand-over to a running Lumen: a second one must not
	// take over the first one's crash output.
	if stop, err := reports.Start(time.Now()); err != nil {
		slog.Error("crash reports unavailable", "err", err)
	} else {
		defer stop()
	}

	st, err := store.Open(paths.DBPath)
	if err != nil {
		return fmt.Errorf("open library: %w", err)
	}
	schema, err := st.SchemaVersion(context.Background())
	if err != nil {
		_ = st.Close()
		return err
	}
	if admission.Health != nil && schema != admission.Health.Candidate.Release.WriteSchema {
		_ = st.Close()
		return fmt.Errorf("updated schema does not match authenticated policy: %w", update.ErrInvalid)
	}
	announce := func(s core.UpdateStatus) { app.Event.Emit(services.EventUpdateStatus, s) }
	updater, err := update.Open(context.Background(), executable, version, schema, announce)
	if err != nil {
		if admission.Health != nil {
			_ = st.Close()
			return err
		}
		// Browsing never depends on the updater: without its state Lumen
		// runs as a build that takes no part in updates, and says so.
		slog.Error("updates unavailable", "err", err)
		updater = update.Inert(context.Background(), version, announce)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		a, pending, pendingErr := updater.PendingClose(paths.DataDir)
		if pendingErr != nil {
			result = errors.Join(result, pendingErr)
		}
		if pending && pendingErr == nil {
			if backupErr := st.Backup(ctx, a.Backup); backupErr != nil {
				result = errors.Join(result, backupErr)
				pending = false
			}
		}
		closeErr := st.Close()
		result = errors.Join(result, closeErr)
		if pending && closeErr == nil {
			result = errors.Join(result, updater.Closed(ctx, a))
		}
	}()
	healthReady := make(chan struct{})
	var healthMu sync.Mutex
	healthDone := false
	var releaseWrites func() error
	if admission.Health != nil {
		if releaseWrites, err = st.HoldWrites(context.Background()); err != nil {
			return err
		}
		defer func() {
			healthMu.Lock()
			defer healthMu.Unlock()
			if releaseWrites != nil {
				result = errors.Join(result, releaseWrites())
			}
		}()
	} else {
		close(healthReady)
		healthDone = true
	}

	decoders := media.Decoders{FFmpeg: paths.FFmpeg(), FFprobe: paths.FFprobe(), Raw: paths.Raw(), JXL: paths.JXL()}
	cache := media.NewCache(paths.ThumbDir, decoders)
	cache.Renderer = media.ProcessRenderer{Executable: executable, FFmpeg: paths.FFmpeg(), FFprobe: paths.FFprobe(), Raw: paths.Raw(), JXL: paths.JXL()}
	// The Blocked screen's lock, shared so that covered photos and videos are
	// served only while it is open.
	gate := &services.BlockGate{}
	stream, err := services.NewStreamer(st, decoders, gate)
	if err != nil {
		slog.Error("videos can't play: no video server", "err", err)
	}
	models := startAI(paths, st)
	events := &services.Emitter{App: app}
	ix := index.New(st, paths, cache, models, events)

	lib := services.NewLibraryService(st, ix, gate, cache)
	lib.BootstrapReady(healthReady)
	compression := services.NewCompressionService(st, lib, paths)
	compression.BootstrapReady(healthReady)
	// What this runtime can do on this PC: the baseline an update must keep,
	// and what its first launch is held to.
	capabilities := func() update.Capabilities {
		return update.Capabilities{Search: models.SearchErr == nil, People: models.PeopleErr == nil, Video: stream != nil}
	}
	health := func(ctx context.Context) (err error) {
		healthMu.Lock()
		defer healthMu.Unlock()
		if healthDone {
			return nil
		}
		// A release that fails its first launch has nothing more to do: the
		// helper recovers the previous one as soon as this process has gone.
		defer func() {
			if err != nil {
				slog.Error("update health check failed", "err", err)
				go app.Quit()
			}
		}()
		if err := st.Integrity(ctx); err != nil {
			return err
		}
		if !capabilities().Covers(admission.Health.Baseline) || paths.FFmpeg() == "" || paths.Raw() == "" || paths.JXL() == "" {
			return fmt.Errorf("required update runtime capability unavailable: %w", update.ErrInvalid)
		}
		if err := update.AcknowledgeHealth(ctx, admission.Install.Root, admission.Health.Token); err != nil {
			return err
		}
		if err := releaseWrites(); err != nil {
			return err
		}
		releaseWrites = nil
		healthDone = true
		close(healthReady)
		if err := updater.Completed(ctx); err != nil {
			slog.Warn("record the completed update", "err", err)
		}
		return nil
	}
	for _, svc := range []application.Service{
		service(&background{ix: ix, search: models.Search, cache: cache, stream: stream, ready: healthReady}, application.ServiceOptions{}),
		service(lib, application.ServiceOptions{}),
		service(compression, application.ServiceOptions{}),
		service(services.NewUpdateService(updater, paths.DataDir, health, capabilities), application.ServiceOptions{}),
		service(services.NewSystemService(lib, reports, paths, version), application.ServiceOptions{}),
		service(services.NewSearchService(st, models.Search, models.People != nil, ix), application.ServiceOptions{}),
		service(services.NewPeopleService(st, models.People), application.ServiceOptions{}),
		service(services.NewMediaService(st, cache, stream, gate), application.ServiceOptions{Route: "/media"}),
	} {
		app.RegisterService(svc)
	}

	w := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Lumen",
		Width:            1280,
		Height:           820,
		MinWidth:         720,
		MinHeight:        480,
		BackgroundColour: application.NewRGB(17, 17, 19),
		URL:              "/",
		Windows:          application.WindowsWindow{BackdropType: application.Mica},
	})
	// The window's WebView2 processes started below normal with Lumen; once
	// the page runs, they are back at normal (see raiseWebView).
	w.OnWindowEvent(windowevents.Common.WindowRuntimeReady, func(*application.WindowEvent) { go raiseWebView() })
	window.Store(w)
	if admission.Health != nil {
		timer := time.AfterFunc(60*time.Second, func() {
			healthMu.Lock()
			done := healthDone
			healthMu.Unlock()
			if !done {
				app.Quit()
			}
		})
		defer timer.Stop()
	}

	return app.Run()
}

// recordPanics handles a panic Wails recovered in a call from the UI: the
// call fails and Lumen runs on, with the bug logged and kept as a report.
// (Wails' default handler ends the app.)
func recordPanics(reports *diag.Reports) func(*application.PanicDetails) {
	return func(d *application.PanicDetails) {
		slog.Error("recovered panic", "err", d.Error, "stack", d.StackTrace)
		reports.RecordPanic(d.Error, d.FullStackTrace, d.Time)
	}
}

// instanceID is what makes a second Lumen hand over to the first: one per
// library, so a scratch one (LUMEN_DATA_DIR, as UI tests run) opens beside
// the user's instead of focusing it.
func instanceID(paths core.Paths) string {
	if defaultLibrary(paths) {
		return "io.lumen.app"
	}
	sum := sha256.Sum256([]byte(dataPathKey(paths.DataDir)))
	return "io.lumen.app." + hex.EncodeToString(sum[:8])
}

// defaultLibrary reports whether paths is the user's own library, even when
// LUMEN_DATA_DIR names it: one library is one instance, whichever way it was
// reached.
func defaultLibrary(paths core.Paths) bool {
	dir, err := core.DefaultDataDir()
	return err == nil && dataPathKey(paths.DataDir) == dataPathKey(dir)
}

// dataPathKey is dir as two paths to one folder compare: absolute, clean
// and, on Windows, case-folded.
func dataPathKey(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		dir = core.Fold(dir)
	}
	return dir
}

// webviewDataPath is where the WebView keeps its own data (storage, cache):
// Wails' default (%APPDATA%\Lumen.exe) for the user's library, and the data
// folder for another, so a scratch library shares neither settings nor
// cached images with it, and WebView2 lets both run at once (it refuses a
// second instance of one folder started with other options).
func webviewDataPath(paths core.Paths) string {
	if defaultLibrary(paths) {
		return ""
	}
	return filepath.Join(paths.DataDir, "webview")
}

// service registers a Wails service whose methods' errors carry their codes
// (services.MarshalError). Wails v3 beta.30 gives a bound method only its
// service's marshaler, falling back to its own: application.Options'
// MarshalError never reaches them, so every service names it.
func service[T any](svc *T, opts application.ServiceOptions) application.Service {
	opts.MarshalError = services.MarshalError
	return application.NewServiceWithOptions(svc, opts)
}

// startAI loads ONNX Runtime and the model descriptions; the models
// themselves load when there is work for them. A part that can't start
// (models missing, runtime failing) is left out with the reason, and Lumen
// runs without it: search and Things, or people.
func startAI(paths core.Paths, st *store.Store) index.Models {
	var m index.Models
	if err := ai.Init(paths.Runtime); err != nil {
		m.SearchErr, m.PeopleErr = err, err
	} else {
		if sm, err := ai.LoadModel(paths.SigLIP()); err != nil {
			m.SearchErr = err
		} else {
			m.Search = search.NewEngine(st, sm)
		}
		if fm, err := ai.LoadFaceModel(paths.Faces()); err != nil {
			m.PeopleErr = err
		} else {
			m.People = people.NewEngine(st, fm)
		}
	}
	if m.SearchErr != nil {
		slog.Error("AI unavailable; search and Things are off", "err", m.SearchErr)
	}
	if m.PeopleErr != nil {
		slog.Error("face models unavailable; people are off", "err", m.PeopleErr)
	}
	return m
}

// background starts the search engine, indexing (which runs the people
// engine) and the video server once the app is running, and stops them
// cleanly before the database closes.
type background struct {
	ready   <-chan struct{} // update bootstrap health precedes indexing and recovery
	ix      backgroundIndexer
	search  *search.Engine // nil without AI
	cache   backgroundCache
	stream  *services.Streamer // nil if it couldn't listen
	cancel  context.CancelFunc
	workers sync.WaitGroup // the video server and cache cleanup, which shutdown waits for
}

// backgroundCache is the startup cleanup, separated so shutdown can be tested
// with a sweep that is still running.
type backgroundCache interface {
	SweepTemp() error
	SweepFrames(context.Context) error
}

// backgroundIndexer is the lifecycle the application owns; tests can check
// shutdown without starting the indexing pipeline.
type backgroundIndexer interface {
	Start(context.Context)
	Stop()
}

// ServiceStartup starts the background work (Wails calls it once the app runs).
//
//nolint:unparam // the signature is Wails' service interface
func (b *background) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	ctx, b.cancel = context.WithCancel(context.WithoutCancel(ctx))
	if b.ready != nil {
		b.workers.Go(func() {
			select {
			case <-ctx.Done():
				return
			case <-b.ready:
				b.start(ctx)
			}
		})
		return nil
	}
	b.start(ctx)
	return nil
}

func (b *background) start(ctx context.Context) {
	// Before indexing writes thumbnails: it removes the folder they are
	// written through.
	if err := b.cache.SweepTemp(); err != nil {
		slog.Warn("tidy thumbnail cache", "err", err)
	}
	if b.search != nil {
		b.search.Start(ctx)
	}
	b.ix.Start(ctx)
	if b.stream != nil {
		b.workers.Go(b.stream.Run)
	}
	b.workers.Go(func() {
		if err := b.cache.SweepFrames(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("tidy thumbnail cache", "err", err)
		}
	})
}

// ServiceShutdown stops it, before the database closes.
//
//nolint:unparam // the signature is Wails' service interface
func (b *background) ServiceShutdown() error {
	b.cancel()
	if b.stream != nil {
		if err := b.stream.Close(); err != nil {
			slog.Warn("stop video server", "err", err)
		}
	}
	b.workers.Wait()
	b.ix.Stop()
	if b.search != nil {
		b.search.Close() // after the indexer: it no longer adds embeddings
	}
	ai.Shutdown() // after every model is closed
	return nil
}

// spa serves the UI, answering client-side routes (/things, /settings…),
// which are not files, with index.html, so they survive a reload. A path
// under "/@" is never a route: it is one of the development server's own
// modules (/@vite/client, /@id/virtual:…), which have no extension either
// and must reach it as they are, or the UI cannot start in development.
func spa(files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Ext(r.URL.Path) == "" && r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/@") {
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

// maxLogSize is when lumen.log starts over (the previous one is kept as
// lumen.old.log), so years of use never grow it without bound.
const maxLogSize = 10 << 20

// setupLogging writes to %LOCALAPPDATA%\Lumen\lumen.log (local only, never
// uploaded), and to stderr when there is a console (dev builds). The file
// stays open until the process exits, so even the final error from run is
// recorded.
func setupLogging(paths core.Paths) {
	log, err := openLog(paths.Log, paths.OldLog, maxLogSize)
	if err != nil {
		slog.Warn("no log file; logging to stderr only", "err", err)
		return
	}
	var w io.Writer = log
	// A GUI build has no stderr, and io.MultiWriter stops at the first
	// failing writer, so stderr is only added when it can be written to.
	if _, err := os.Stderr.Stat(); err == nil {
		w = io.MultiWriter(log, os.Stderr)
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo})))
}

// rotatingLog appends to a file, and when it passes max bytes renames it to
// old (replacing the one before) and starts a new one, while running too: a
// long session that logs a warning per failed request can't fill the disk.
type rotatingLog struct {
	mu        sync.Mutex // guards f and size, through writes and rotation
	name, old string
	max, size int64
	f         *os.File
}

func openLog(name, old string, maxSize int64) (*rotatingLog, error) {
	l := &rotatingLog{name: name, old: old, max: maxSize}
	if fi, err := os.Stat(name); err == nil && fi.Size() > maxSize {
		if err := l.rotate(); err != nil {
			return nil, err
		}
		return l, nil
	}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *rotatingLog) open() error {
	f, err := os.OpenFile(l.name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	l.f, l.size = f, fi.Size()
	return nil
}

func (l *rotatingLog) rotate() error {
	renameErr := os.Rename(l.name, l.old)
	if renameErr != nil {
		// An old log held open on Windows must not let the active one grow
		// forever. Discard it when keeping a backup is impossible.
		if err := os.Truncate(l.name, 0); err != nil {
			return err
		}
	}
	if err := l.open(); err != nil {
		return err
	}
	if renameErr != nil {
		// Write directly because slog may be writing through this same lock.
		n, err := fmt.Fprintf(l.f, "level=WARN msg=%q err=%q\n", "log rotation failed; truncated active log", renameErr)
		l.size += int64(n)
		if err != nil {
			_ = l.f.Close()
			return err
		}
	}
	return nil
}

func (l *rotatingLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.size+int64(len(p)) > l.max && l.size > 0 {
		_ = l.f.Close()
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := l.f.Write(p)
	l.size += int64(n)
	return n, err
}
