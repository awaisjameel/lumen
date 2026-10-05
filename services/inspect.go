package services

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

type subtitleBuffer struct{ data []byte }

func (b *subtitleBuffer) Write(p []byte) (int, error) {
	if len(b.data)+len(p) > 4<<20 {
		return 0, errors.New("subtitles exceed budget")
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

// Inspect reads file metadata while holding its lifetime and privacy access.
func (m *MediaService) Inspect(ctx context.Context, id int64) (core.MediaInspection, error) {
	ctx, finish, ok := m.renders.hold(ctx)
	if !ok {
		return core.MediaInspection{}, context.Canceled
	}
	defer finish()
	unlocked := false
	f, err := m.store.AssetFile(ctx, id, false)
	if errors.Is(err, store.ErrNotFound) {
		var release func()
		ctx, release, unlocked = m.gate.Hold(ctx)
		defer release()
		if unlocked {
			f, err = m.store.AssetFile(ctx, id, true)
		}
	}
	if err != nil {
		return core.MediaInspection{}, err
	}
	path, ok := readablePath(f)
	if !ok {
		return core.MediaInspection{}, store.ErrNotFound
	}
	info, err := m.cache.Inspect(ctx, path, f.Kind)
	if err != nil {
		return info, err
	}
	if err := ctx.Err(); err != nil {
		return core.MediaInspection{}, err
	}
	if _, err := m.store.AssetFile(ctx, id, unlocked); err != nil {
		return core.MediaInspection{}, err
	}
	return info, nil
}

func (m *MediaService) serveSubtitles(w http.ResponseWriter, r *http.Request, id int64, unlocked bool) {
	ctx, finish, ok := m.renders.hold(r.Context())
	if !ok {
		http.Error(w, "stopping", http.StatusServiceUnavailable)
		return
	}
	defer finish()
	r = r.WithContext(ctx)
	f, path, ok := assetFile(r.Context(), m.store, id, unlocked, w)
	if !ok || f.Kind != core.KindVideo {
		return
	}
	track, err := strconv.Atoi(r.URL.Query().Get("track"))
	if err != nil || track < 0 {
		http.NotFound(w, r)
		return
	}
	at, err := strconv.ParseInt(r.URL.Query().Get("at"), 10, 64)
	if err != nil || at < 0 {
		http.NotFound(w, r)
		return
	}
	d, ok := m.cache.Decoder.(media.Decoders)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// Conversion completes before headers, so unsupported tracks cannot look
	// like a valid empty subtitle document. Text output has a strict cap.
	var out subtitleBuffer
	if !served(w, d.Subtitles(ctx, path, track, at, &out), id) {
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out.data)
}
