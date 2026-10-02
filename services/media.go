package services

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

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
	store  *store.Store
	cache  *media.Cache
	stream *Streamer
	gate   *BlockGate                          // the Blocked screen's lock: covered photos are served only while it is open
	views  viewFlusher                         // saves which thumbnails were shown, for the cache's size cap
	open   func(path string) (*os.File, error) // opens a cached picture; tests make it fail
}

// NewMediaService returns the media service. stream may be nil (no videos);
// a nil gate is one that stays locked.
func NewMediaService(st *store.Store, cache *media.Cache, stream *Streamer, gate *BlockGate) *MediaService {
	if gate == nil {
		gate = &BlockGate{}
	}
	return &MediaService{store: st, cache: cache, stream: stream, gate: gate,
		views: viewFlusher{cache: cache, sink: st, every: viewFlushEvery}, open: core.OpenShared}
}

// ServiceStartup starts saving thumbnail views (Wails calls it once the app runs).
func (m *MediaService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	m.views.start(ctx)
	return nil
}

// ServiceShutdown saves the views still in memory. Wails stops services in
// reverse order, so this runs before the database closes.
func (m *MediaService) ServiceShutdown() error {
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
	visible, err := m.store.AssetVisible(r.Context(), id, m.gate.Unlocked())
	if err != nil {
		http.Error(w, "the file could not be read", http.StatusInternalServerError)
		return
	}
	if !visible {
		http.NotFound(w, r)
		return
	}
	switch {
	case parts[0] == "thumb" && len(parts) == 3:
		size, err := strconv.Atoi(parts[2])
		if err != nil || (size != core.ThumbSmall && size != core.ThumbLarge) {
			http.NotFound(w, r)
			return
		}
		m.serveThumb(w, r, id, size)
	case parts[0] == "sprite" && len(parts) == 2:
		m.serveSprite(w, r, id)
	case parts[0] == "original" && len(parts) == 2:
		m.serveOriginal(w, r, id)
	case parts[0] == "frame" && len(parts) == 3:
		at, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || at < 0 {
			http.NotFound(w, r)
			return
		}
		m.serveFrame(w, r, id, at)
	default:
		http.NotFound(w, r)
	}
}

func (m *MediaService) serveThumb(w http.ResponseWriter, r *http.Request, id int64, size int) {
	m.serveCachedPicture(w, r, id, m.cache.Path(id, size), func() bool {
		// Missing from the cache (the folder was cleared, or the size cap evicted it): rebuild it now if the asset had one.
		f, path, ok := assetFile(r.Context(), m.store, id, m.gate.Unlocked(), w)
		if !ok {
			return false
		}
		if f.ThumbVersion == 0 {
			http.NotFound(w, r) // not made yet: the grid shows a placeholder
			return false
		}
		return served(w, m.cache.Ensure(r.Context(), id, size, media.Original{Path: path, Kind: f.Kind, Orientation: f.Orientation}), id)
	})
}

func (m *MediaService) serveSprite(w http.ResponseWriter, r *http.Request, id int64) {
	m.serveCachedPicture(w, r, id, m.cache.SpritePath(id), func() bool {
		f, orig, ok := assetFile(r.Context(), m.store, id, m.gate.Unlocked(), w)
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

// serveCachedPicture serves a cached thumbnail or sprite. If it is not there
// it calls rebuild (which answers the request itself and returns false when it
// cannot make it) and opens it again. Opening, not a separate check, decides:
// the size cap can evict the file between a check and the open, and that must
// cost one rebuild, not a 404.
func (m *MediaService) serveCachedPicture(w http.ResponseWriter, r *http.Request, id int64, path string, rebuild func() bool) {
	file, err := m.open(path)
	if err != nil {
		if !rebuild() {
			return
		}
		if file, err = m.open(path); err != nil {
			served(w, err, id)
			return
		}
	}
	defer file.Close()
	m.cache.Viewed(id)
	serveOpened(w, r, file, "image/jpeg")
}

func (m *MediaService) serveFrame(w http.ResponseWriter, r *http.Request, id, at int64) {
	f, path, ok := assetFile(r.Context(), m.store, id, m.gate.Unlocked(), w)
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

func (m *MediaService) serveOriginal(w http.ResponseWriter, r *http.Request, id int64) {
	f, path, ok := assetFile(r.Context(), m.store, id, m.gate.Unlocked(), w)
	if !ok {
		return
	}
	if f.Kind != core.KindPhoto || !core.WebNative(f.Ext) {
		http.NotFound(w, r)
		return
	}
	serveFile(w, r, path, core.ContentType(f.Ext))
}

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
	for _, p := range f.Paths {
		if _, err := os.Stat(p); err == nil {
			return f, p, true
		}
	}
	http.Error(w, "the file is not available", http.StatusNotFound)
	return f, "", false
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
