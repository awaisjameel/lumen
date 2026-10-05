package services

import (
	"context"
	"net/http"
	"strconv"
	"sync"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

type renderLifetime struct {
	sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	closing bool
	active  sync.WaitGroup
}

func (l *renderLifetime) start(ctx context.Context) {
	l.ctx, l.cancel = context.WithCancel(ctx)
}

func (l *renderLifetime) hold(ctx context.Context) (context.Context, func(), bool) {
	l.Lock()
	defer l.Unlock()
	if l.closing {
		return ctx, func() {}, false
	}
	l.active.Add(1)
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(l.ctx, cancel)
	return ctx, func() { stop(); cancel(); l.active.Done() }, true
}

func (l *renderLifetime) stop() {
	l.Lock()
	l.closing = true
	l.cancel()
	l.Unlock()
	l.active.Wait()
}

// Render prepares an upright sRGB viewing copy and describes its true
// resolution. The served URL still checks visibility on every request.
func (m *MediaService) Render(ctx context.Context, id int64, side int) (core.RenderInfo, error) {
	unlocked := false
	visible, err := m.store.AssetVisible(ctx, id, false)
	if err != nil {
		return core.RenderInfo{}, err
	}
	if !visible {
		var release func()
		ctx, release, unlocked = m.gate.Hold(ctx)
		defer release()
		if !unlocked {
			return core.RenderInfo{}, store.ErrNotFound
		}
	}
	_, info, err := m.prepareRender(ctx, id, side, unlocked)
	return info, err
}

func (m *MediaService) prepareRender(ctx context.Context, id int64, side int, unlocked bool) (string, core.RenderInfo, error) {
	var info core.RenderInfo
	ctx, release, ok := m.renders.hold(ctx)
	if !ok {
		return "", info, context.Canceled
	}
	defer release()
	f, err := m.store.AssetFile(ctx, id, unlocked)
	if err != nil {
		return "", info, err
	}
	if f.Kind != core.KindPhoto {
		return "", info, store.ErrNotFound
	}
	path, ok := readablePath(f)
	if !ok {
		return "", info, store.ErrNotFound
	}
	a, err := m.store.Adjustment(ctx, id, unlocked)
	if err != nil {
		return "", info, err
	}
	dst, info, err := m.cache.RenderFile(ctx, id, path, f.Orientation, side, a.Version)
	if err != nil {
		return "", info, err
	}
	d, err := m.store.AssetDetail(ctx, id, unlocked)
	if err != nil {
		return "", info, err
	}
	info.SourceWidth, info.SourceHeight = d.Width, d.Height
	info.Limited = info.Width < d.Width || info.Height < d.Height
	return dst, info, nil
}

func (m *MediaService) serveRender(w http.ResponseWriter, r *http.Request, id int64, side int, unlocked bool) {
	dst, info, err := m.prepareRender(r.Context(), id, side, unlocked)
	if !served(w, err, id) {
		return
	}
	if r.URL.Query().Get("v") != info.Revision {
		http.Error(w, "source changed", http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	serveFile(w, r, dst, "image/jpeg")
}

func renderSide(value string) (int, bool) {
	n, err := strconv.Atoi(value)
	return n, err == nil && n >= 256 && n <= media.RenderSide
}
