package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// MediaService serves pictures to the WebView on the app's own origin, so the
// grid loads thumbnails like ordinary <img> URLs (no base64, browser-cached):
//
//	/media/thumb/{id}/{256|1024}?v={thumbVersion}
//	/media/sprite/{id}?v={thumbVersion}
//	/media/original/{id}   (photos the WebView can display)
//	/media/frame/{id}/{ms} (one video frame, e.g. for a face found in it)
//
// Videos stream from the Streamer instead: the app's origin hands the
// WebView a response only once it is complete.
type MediaService struct {
	store   *store.Store
	cache   *media.Cache
	stream  *Streamer
	gate    *BlockGate                          // the Blocked screen's lock: covered photos are served only while it is open
	views   viewFlusher                         // saves which thumbnails were shown, for the cache's size cap
	open    func(path string) (*os.File, error) // opens a cached picture; tests make it fail
	renders renderLifetime
}

// NewMediaService returns the media service. stream may be nil (no videos);
// a nil gate is one that stays locked.
func NewMediaService(st *store.Store, cache *media.Cache, stream *Streamer, gate *BlockGate) *MediaService {
	if gate == nil {
		gate = &BlockGate{}
	}
	m := &MediaService{store: st, cache: cache, stream: stream, gate: gate,
		views: viewFlusher{cache: cache, sink: st, every: viewFlushEvery}, open: core.OpenShared}
	m.renders.start(context.Background())
	return m
}

// ServiceStartup starts saving thumbnail views (Wails calls it once the app runs).
func (m *MediaService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	m.views.start(ctx)
	return nil
}

// ServiceShutdown saves the views still in memory. Wails stops services in
// reverse order, so this runs before the database closes.
func (m *MediaService) ServiceShutdown() error {
	m.renders.stop()
	m.views.stop()
	return nil
}

// VideoBase is where videos stream from: the Streamer's address, which the
// bridge puts before /video/{id} and /play/{id}. "" when videos can't play.
func (m *MediaService) VideoBase() string {
	if m.stream == nil {
		return ""
	}
	return m.stream.Base()
}

func (m *MediaService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	// A thumbnail is shown as the user turned it, unless the caller draws on
	// the stored pixels (a face's box). Whether it may be served and how it is
	// turned are one question of the store.
	turned := parts[0] == "thumb" && r.URL.Query().Get("canonical") != "1"
	var adjustment core.DisplayAdjustment
	adm, ok := admit(w, r, m.gate, id, func(ctx context.Context, unlocked bool) error {
		if turned {
			var err error
			adjustment, err = m.store.Adjustment(ctx, id, unlocked)
			return err
		}
		visible, err := m.store.AssetVisible(ctx, id, unlocked)
		if err == nil && !visible {
			err = store.ErrNotFound
		}
		return err
	})
	if !ok {
		return
	}
	defer adm.release()
	r, w = adm.r, adm.w
	switch {
	case parts[0] == "subtitle" && len(parts) == 2:
		m.serveSubtitles(w, r, id, adm.unlocked)
	case parts[0] == "render" && len(parts) == 3:
		side, ok := renderSide(parts[2])
		if !ok {
			http.NotFound(w, r)
			return
		}
		m.serveRender(w, r, id, side, adm.unlocked)
	case parts[0] == "thumb" && len(parts) == 3:
		size, err := strconv.Atoi(parts[2])
		if err != nil || (size != core.ThumbSmall && size != core.ThumbLarge) {
			http.NotFound(w, r)
			return
		}
		m.serveThumb(w, r, id, size, adm.unlocked, adjustment)
	case parts[0] == "sprite" && len(parts) == 2:
		m.serveSprite(w, r, id, adm.unlocked)
	case parts[0] == "original" && len(parts) == 2:
		m.serveOriginal(w, r, id, adm.unlocked)
	case parts[0] == "frame" && len(parts) == 3:
		at, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || at < 0 {
			http.NotFound(w, r)
			return
		}
		m.serveFrame(w, r, id, at, adm.unlocked)
	default:
		http.NotFound(w, r)
	}
}

func (m *MediaService) serveThumb(w http.ResponseWriter, r *http.Request, id int64, size int, unlocked bool, a core.DisplayAdjustment) {
	file, ok := m.openCached(w, id, m.cache.Path(id, size), func() bool {
		// Missing from the cache (the folder was cleared, or the size cap evicted it): rebuild it now if the asset had one.
		f, path, ok := assetFile(r.Context(), m.store, id, unlocked, w)
		if !ok {
			return false
		}
		if f.ThumbVersion == 0 {
			http.NotFound(w, r) // not made yet: the grid shows a placeholder
			return false
		}
		return served(w, m.cache.Ensure(r.Context(), id, size, media.Original{Path: path, Kind: f.Kind, Orientation: f.Orientation}), id)
	})
	if !ok {
		return
	}
	defer file.Close()
	m.cache.Viewed(id)
	if a.Rotation == 0 && !a.Flip {
		serveOpened(w, r, file, "image/jpeg")
		return
	}
	fi, err := file.Stat()
	if !served(w, err, id) {
		return
	}
	// The turned copy is made for the request, so its tag names what it is
	// made from: the stored thumbnail and the turn. A WebView that still has
	// both is told so, before anything is decoded.
	tag := fmt.Sprintf(`"%x-%d-%t"`, fi.ModTime().UnixNano(), a.Rotation, a.Flip)
	w.Header().Set("Etag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, err := io.ReadAll(file)
	if !served(w, err, id) {
		return
	}
	data, err = media.AdjustJPEG(data, a)
	if !served(w, err, id) {
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

func (m *MediaService) serveSprite(w http.ResponseWriter, r *http.Request, id int64, unlocked bool) {
	m.serveCachedPicture(w, r, id, m.cache.SpritePath(id), func() bool {
		f, orig, ok := assetFile(r.Context(), m.store, id, unlocked, w)
		if !ok {
			return false
		}
		if f.Kind != core.KindVideo || f.ThumbVersion == 0 {
			http.NotFound(w, r)
			return false
		}
		return served(w, m.cache.EnsureSprite(r.Context(), id, media.Original{Path: orig, Kind: f.Kind, Orientation: f.Orientation}), id)
	})
}

// serveCachedPicture serves a cached thumbnail or sprite, made again by
// rebuild if it is not there (see openCached).
func (m *MediaService) serveCachedPicture(w http.ResponseWriter, r *http.Request, id int64, path string, rebuild func() bool) {
	file, ok := m.openCached(w, id, path, rebuild)
	if !ok {
		return
	}
	defer file.Close()
	m.cache.Viewed(id)
	serveOpened(w, r, file, "image/jpeg")
}

// openCached opens a cached picture. If it is not there it calls rebuild
// (which answers the request itself and returns false when it cannot make it)
// and opens it again. Opening, not a separate check, decides: the size cap can
// evict the file between a check and the open, and that must cost one rebuild,
// not a 404. It answers a request it cannot open a picture for.
func (m *MediaService) openCached(w http.ResponseWriter, id int64, path string, rebuild func() bool) (*os.File, bool) {
	file, err := m.open(path)
	if err != nil {
		if !rebuild() {
			return nil, false
		}
		if file, err = m.open(path); err != nil {
			served(w, err, id)
			return nil, false
		}
	}
	return file, true
}

func (m *MediaService) serveFrame(w http.ResponseWriter, r *http.Request, id, at int64, unlocked bool) {
	f, path, ok := assetFile(r.Context(), m.store, id, unlocked, w)
	if !ok {
		return
	}
	if f.Kind != core.KindVideo || (f.Duration > 0 && at > f.Duration) {
		http.NotFound(w, r)
		return
	}
	frame, err := m.cache.EnsureFrame(r.Context(), id, at, path)
	if !served(w, err, id) {
		return
	}
	serveCached(w, r, frame)
}

func (m *MediaService) serveOriginal(w http.ResponseWriter, r *http.Request, id int64, unlocked bool) {
	f, path, ok := assetFile(r.Context(), m.store, id, unlocked, w)
	if !ok {
		return
	}
	if f.Kind != core.KindPhoto || !core.WebNative(f.Ext) {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, path, core.ContentType(f.Ext))
}

// admission is a request that may be answered: r and w are the ones to answer
// it with, and release must be called once it is.
type admission struct {
	r        *http.Request
	w        http.ResponseWriter
	unlocked bool // answered only because the Blocked screen is open
	release  func()
}

// admit decides whether a request for asset id may be answered, check being
// the store's question (it fails with store.ErrNotFound for an asset that may
// not be served) asked with or without the Blocked screen open. The locked
// question comes first and an asset it accepts is answered without the gate,
// so browsing the library never counts as using the Blocked screen. Only for
// an asset that the open screen would allow, and the locked question refused,
// is the gate asked; that counts as a use, and the request is then bound to
// the gate: its context ends, and writing to its
// writer fails, the moment the screen locks, so no covered response outlives
// the unlock. admit answers a request it refuses itself.
func admit(w http.ResponseWriter, r *http.Request, gate *BlockGate, id int64, check func(ctx context.Context, unlocked bool) error) (admission, bool) {
	err := check(r.Context(), false)
	if err == nil {
		return admission{r: r, w: w, release: func() {}}, true
	}
	if !errors.Is(err, store.ErrNotFound) {
		served(w, err, id)
		return admission{}, false
	}
	// Whether the screen being open would allow it is asked of the store
	// first, so a photo that is not there at all is no use of the gate.
	if err := check(r.Context(), true); err != nil {
		served(w, err, id)
		return admission{}, false
	}
	ctx, release, open := gate.Hold(r.Context())
	if !open {
		served(w, store.ErrNotFound, id)
		return admission{}, false
	}
	return admission{r: r.WithContext(ctx), w: boundWriter{ResponseWriter: w, ctx: ctx}, unlocked: true, release: release}, true
}

// boundWriter fails every write once its context has ended, which stops a
// file or a conversion being sent (ServeContent and the converters stop at
// the first failed write).
type boundWriter struct {
	http.ResponseWriter
	ctx context.Context
}

func (w boundWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController reach the connection.
func (w boundWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// assetFile looks an asset up, with the first of its files that can be
// read now (a copy on an unplugged drive is passed over). It answers the
// request itself (404, or 500) when there is nothing to serve. unlocked is
// the Blocked screen's state, read once per request.
func assetFile(ctx context.Context, st *store.Store, id int64, unlocked bool, w http.ResponseWriter) (store.MediaFile, string, bool) {
	f, err := st.AssetFile(ctx, id, unlocked)
	if err != nil {
		served(w, err, id)
		return f, "", false
	}
	path, ok := readablePath(f)
	if !ok {
		http.Error(w, "the file is not available", http.StatusNotFound)
		return f, "", false
	}
	return f, path, true
}

// readablePath is the first of the asset's files that can be read now.
func readablePath(f store.MediaFile) (string, bool) {
	for _, p := range f.Paths {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	return "", false
}

// served reports whether err is nil. Otherwise it answers with the status
// err calls for, logging failures that are not the file's own fault.
func served(w http.ResponseWriter, err error, id int64) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, store.ErrNotFound), errors.Is(err, os.ErrNotExist):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, media.ErrUnsupported):
		http.Error(w, "this format can't be shown", http.StatusUnsupportedMediaType)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// The request went away (the user scrolled on): nothing to tell.
	default:
		slog.Warn("serve media", "asset", id, "err", err)
		http.Error(w, "the file could not be read", http.StatusInternalServerError)
	}
	return false
}

// serveCached serves a JPEG from the thumbnail cache.
func serveCached(w http.ResponseWriter, r *http.Request, path string) {
	serveFile(w, r, path, "image/jpeg")
}

// serveFile serves a file with Range support. Callers check the asset is
// visible first (AssetFile, AssetVisible). The file is opened sharing
// deletion: a video paused in the viewer must not stop Explorer moving it.
func serveFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	file, err := core.OpenShared(path)
	if err != nil {
		served(w, err, 0)
		return
	}
	defer file.Close()
	serveOpened(w, r, file, contentType)
}

// serveOpened serves an open file with Range support.
func serveOpened(w http.ResponseWriter, r *http.Request, file *os.File, contentType string) {
	fi, err := file.Stat()
	if err != nil {
		served(w, err, 0)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	http.ServeContent(w, r, "", fi.ModTime(), file)
}
