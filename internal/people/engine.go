package people

import (
	"context"
	"log/slog"
	"time"

	"lumen/internal/ai"
	"lumen/internal/store"
)

const (
	// quiet is how long new faces must stop arriving before clustering runs,
	// and maxWait how long it waits at most while they keep coming, so people
	// appear while a large library is still being indexed.
	quiet   = 3 * time.Second
	maxWait = 30 * time.Second
)

// Engine keeps the people in step with the faces: whenever faces are saved
// or the user corrects someone, it clusters again once things go quiet.
type Engine struct {
	store  *store.Store
	model  ai.FaceModel
	params Params
	links  *Linker // used only by Run's goroutine
	nudge  chan struct{}
}

// NewEngine prepares an engine for faces made by model. Call Run to start it.
func NewEngine(st *store.Store, m ai.FaceModel) *Engine {
	return &Engine{store: st, model: m, params: DefaultParams, links: NewLinker(DefaultParams), nudge: make(chan struct{}, 1)}
}

// Model is the face models the engine's faces come from.
func (e *Engine) Model() ai.FaceModel { return e.model }

// Nudge asks for clustering soon: faces were saved or people edited.
func (e *Engine) Nudge() {
	select {
	case e.nudge <- struct{}{}:
	default:
	}
}

// Run clusters once at start and then whenever nudged, until ctx ends.
// changed is called after a run that changed who is who.
func (e *Engine) Run(ctx context.Context, changed func()) {
	e.Nudge()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.nudge:
		}
		// Debounce: wait for quiet, but not forever.
		deadline := time.NewTimer(maxWait)
	wait:
		for {
			select {
			case <-ctx.Done():
				deadline.Stop()
				return
			case <-e.nudge:
			case <-time.After(quiet):
				break wait
			case <-deadline.C:
				break wait
			}
		}
		deadline.Stop()
		did, err := e.recluster(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			slog.Error("group faces", "err", err)
		case did:
			changed()
		}
	}
}

// recluster places the faces no one has placed yet, finds new people and
// merges people found twice. It reports whether anything changed.
func (e *Engine) recluster(ctx context.Context) (bool, error) {
	t := time.Now()
	faces, people, rejections, err := e.store.ClusterData(ctx, e.model.Version)
	if err != nil {
		return false, err
	}
	links, err := e.links.Update(ctx, faces)
	if err != nil {
		return false, err
	}
	c, err := Cluster(ctx, Input{Faces: faces, People: people, Rejections: rejections, Links: links}, e.params)
	if err != nil {
		return false, err
	}
	if len(c.Assign) == 0 && len(c.NewPeople) == 0 && len(c.Merges) == 0 {
		return false, nil
	}
	if err := e.store.ApplyClusters(ctx, c); err != nil {
		return false, err
	}
	slog.Info("faces grouped", "faces", len(faces), "placed", len(c.Assign), "new people", len(c.NewPeople),
		"merged", len(c.Merges), "took", time.Since(t).Round(time.Millisecond))
	return true, nil
}
