package services

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"

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
}

// NewMediaService returns the media service. stream may be nil (no videos).
func NewMediaService(st *store.Store, cache *media.Cache, stream *Streamer) *MediaService {
	return &MediaService{store: st, cache: cache, stream: stream}
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
	path := m.cache.Path(id, size)
	if _, err := os.Stat(path); err != nil {
		// Evicted from the cache (or cleared): rebuild it now if the asset had one.
		f, path, ok := assetFile(r.Context(), m.store, id, w)
		if !ok {
			return
		}
		if f.ThumbVersion == 0 {
			http.NotFound(w, r) // not made yet: the grid shows a placeholder
			return
		}
		if !served(w, m.cache.Ensure(r.Context(), id, size, media.Original{Path: path, Kind: f.Kind, Orientation: f.Orientation}), id) {
			return
		}
	}
	serveCached(w, r, m.cache.Path(id, size), r.URL.Query().Has("v"))
}

func (m *MediaService) serveSprite(w http.ResponseWriter, r *http.Request, id int64) {
	path := m.cache.SpritePath(id)
	if _, err := os.Stat(path); err != nil {
		f, orig, ok := assetFile(r.Context(), m.store, id, w)
		if !ok {
			return
		}
		if f.Kind != core.KindVideo || f.ThumbVersion == 0 {
			http.NotFound(w, r)
			return
		}
		if !served(w, m.cache.EnsureSprite(r.Context(), id, media.Original{Path: orig, Kind: f.Kind, Orientation: f.Orientation}), id) {
			return
		}
	}
	serveCached(w, r, path, r.URL.Query().Has("v"))
}

func (m *MediaService) serveFrame(w http.ResponseWriter, r *http.Request, id, at int64) {
	f, path, ok := assetFile(r.Context(), m.store, id, w)
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
	w.Header().Set("Cache-Control", "private, max-age=86400") // the next version of Lumen may frame it better
	serveCached(w, r, frame, false)
}

func (m *MediaService) serveOriginal(w http.ResponseWriter, r *http.Request, id int64) {
	f, path, ok := assetFile(r.Context(), m.store, id, w)
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
// request itself (404, or 500) when there is nothing to serve.
func assetFile(ctx context.Context, st *store.Store, id int64, w http.ResponseWriter) (store.MediaFile, string, bool) {
	f, err := st.AssetFile(ctx, id)
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

// serveCached serves a JPEG from the thumbnail cache. versioned URLs (?v=)
// never change: a new version gets a new URL.
func serveCached(w http.ResponseWriter, r *http.Request, path string, versioned bool) {
	if versioned {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	serveFile(w, r, path, "image/jpeg")
}

// serveFile serves a file with Range support. An asset's content never
// changes under its id (new content is a new asset), so it may be cached.
func serveFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	file, err := os.Open(path)
	if err != nil {
		served(w, err, 0)
		return
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		served(w, err, 0)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "private, max-age=86400")
	}
	http.ServeContent(w, r, "", fi.ModTime(), file)
}
