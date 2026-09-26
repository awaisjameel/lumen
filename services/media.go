package services

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// MediaService serves images to the WebView on the app's own origin, so the
// grid loads thumbnails like ordinary <img> URLs (no base64, browser-cached):
//
//	/media/thumb/{id}/{256|1024}?v={thumbVersion}
//	/media/original/{id}   (only formats the WebView can display)
type MediaService struct {
	store  *store.Store
	paths  core.Paths
	thumbs media.Thumbnailer
	regen  sync.Map // asset id -> *sync.Mutex while a missing thumb is being rebuilt
}

func NewMediaService(st *store.Store, paths core.Paths, t media.Thumbnailer) *MediaService {
	return &MediaService{store: st, paths: paths, thumbs: t}
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
	default:
		http.NotFound(w, r)
	}
}

func (m *MediaService) serveThumb(w http.ResponseWriter, r *http.Request, id int64, size int) {
	path := media.ThumbPath(m.paths.ThumbDir, id, size)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		// Evicted from the cache (or cleared): rebuild it now if the asset had one.
		if err := m.rebuild(r.Context(), id, path); err != nil {
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

func (m *MediaService) rebuild(ctx context.Context, id int64, path string) error {
	v, _ := m.regen.LoadOrStore(id, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer func() {
		m.regen.Delete(id) // later requests find the file (or retry afresh)
		mu.Unlock()
	}()
	if _, err := os.Stat(path); err == nil {
		return nil // another request rebuilt it while we waited
	}
	f, err := m.store.AssetFile(ctx, id)
	if err != nil || f.ThumbVersion == 0 {
		return errors.New("no thumbnail")
	}
	t, err := m.thumbs.Thumbnail(f.Path, f.Kind, f.Orientation)
	if err != nil {
		return err
	}
	return media.WriteThumbs(m.paths.ThumbDir, id, t)
}

func (m *MediaService) serveOriginal(w http.ResponseWriter, r *http.Request, id int64) {
	f, err := m.store.AssetFile(r.Context(), id)
	if err != nil || !core.WebNative(f.Ext) {
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
