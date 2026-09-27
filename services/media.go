package services

import (
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

// MediaService serves images to the WebView on the app's own origin, so the
// grid loads thumbnails like ordinary <img> URLs (no base64, browser-cached):
//
//	/media/thumb/{id}/{256|1024}?v={thumbVersion}
//	/media/sprite/{id}?v={thumbVersion}
//	/media/original/{id}   (only formats the WebView can display)
type MediaService struct {
	store *store.Store
	cache *media.Cache
}

// NewMediaService returns the media service.
func NewMediaService(st *store.Store, cache *media.Cache) *MediaService {
	return &MediaService{store: st, cache: cache}
}

func (m *MediaService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	switch parts[0] {
	case "thumb":
		size := core.ThumbSmall
		if len(parts) > 2 && parts[2] == strconv.Itoa(core.ThumbLarge) {
			size = core.ThumbLarge
		}
		m.serveThumb(w, r, id, size)
	case "original":
		m.serveOriginal(w, r, id)
	case "sprite":
		m.serveSprite(w, r, id)
	case "play":
		m.servePlay(w, r, id)
	case "frame":
		m.serveFrame(w, r, id, parts)
	default:
		http.NotFound(w, r)
	}
}

func (m *MediaService) serveFrame(w http.ResponseWriter, r *http.Request, id int64, parts []string) {
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	at, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || at < 0 {
		http.NotFound(w, r)
		return
	}
	f, err := m.store.AssetFile(r.Context(), id)
	if err != nil || f.Kind != core.KindVideo || (f.Duration > 0 && at > f.Duration) {
		http.NotFound(w, r)
		return
	}
	path, err := m.cache.EnsureFrame(r.Context(), id, at, f.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func (m *MediaService) servePlay(w http.ResponseWriter, r *http.Request, id int64) {
	f, err := m.store.AssetFile(r.Context(), id)
	if err != nil || f.Kind != core.KindVideo {
		http.NotFound(w, r)
		return
	}
	decoder, ok := m.cache.Decoder.(media.Decoders)
	if !ok || decoder.FFmpeg == "" {
		http.Error(w, "video conversion unavailable", http.StatusServiceUnavailable)
		return
	}
	at, err := strconv.ParseInt(r.URL.Query().Get("at"), 10, 64)
	if r.URL.Query().Has("at") && (err != nil || at < 0) {
		http.Error(w, "invalid time", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	if err := decoder.StreamMP4(r.Context(), f.Path, at, w); err != nil && !errors.Is(err, r.Context().Err()) {
		slog.Warn("video conversion stopped", "asset", id, "err", err)
	}
}

func (m *MediaService) serveSprite(w http.ResponseWriter, r *http.Request, id int64) {
	f, err := m.store.AssetFile(r.Context(), id)
	if err != nil || f.Kind != core.KindVideo || f.ThumbVersion == 0 {
		http.NotFound(w, r)
		return
	}
	path := m.cache.SpritePath(id)
	if _, err := os.Stat(path); err != nil {
		if err := m.cache.EnsureSprite(r.Context(), id,
			media.Original{Path: f.Path, Kind: f.Kind, Orientation: f.Orientation}); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	if r.URL.Query().Has("v") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, path)
}

func (m *MediaService) serveThumb(w http.ResponseWriter, r *http.Request, id int64, size int) {
	path := m.cache.Path(id, size)
	if _, err := os.Stat(path); err != nil {
		// Evicted from the cache (or cleared): rebuild it now if the asset had one.
		f, err := m.store.AssetFile(r.Context(), id)
		if err != nil || f.ThumbVersion == 0 {
			http.NotFound(w, r)
			return
		}
		if err := m.cache.Ensure(r.Context(), id, size, media.Original{Path: f.Path, Kind: f.Kind, Orientation: f.Orientation}); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	if r.URL.Query().Has("v") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, path)
}

func (m *MediaService) serveOriginal(w http.ResponseWriter, r *http.Request, id int64) {
	f, err := m.store.AssetFile(r.Context(), id)
	if err != nil || (f.Kind == core.KindPhoto && !core.WebNative(f.Ext)) ||
		(f.Kind == core.KindVideo && core.PlaybackOf(f.Ext, f.VideoCodec, f.AudioCodec) == core.PlayConvert) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(f.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	fi, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), file) // supports Range, for video seeking
}
