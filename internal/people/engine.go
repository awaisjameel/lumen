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
	// stateIdle is how long after a run the kept faces and sums are held. They
	// make the next run cheap while faces keep arriving, and are about 100 MB
	// for 120,000 faces, so a library at rest gives them back; the run after
	// that loads everything once, as the first run does.
	stateIdle = 2 * time.Minute
)

// Engine keeps the people in step with the faces: whenever faces are saved
// or the user corrects someone, it clusters again once things go quiet.
type Engine struct {
	store  *store.Store
	model  ai.FaceModel
	params Params
	links  *Linker       // used only by Run's goroutine
	state  *clusterState // Only Run's goroutine uses this state.
	idle   time.Duration // stateIdle unless a test sets it
	nudge  chan struct{}
}

// NewEngine prepares an engine for faces made by model. Call Run to start it.
func NewEngine(st *store.Store, m ai.FaceModel) *Engine {
	return &Engine{store: st, model: m, params: DefaultParams, links: NewLinker(DefaultParams), idle: stateIdle, nudge: make(chan struct{}, 1)}
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
	var rest <-chan time.Time // fires once the kept state has gone unused for e.idle
	for {
		select {
		case <-ctx.Done():
			return
		case <-rest:
			e.release()
			rest = nil
			continue
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
		rest = time.After(e.idle)
	}
}

// release forgets the kept faces and sums; the linker keeps its links, so the
// next run is the full load the engine makes after starting.
func (e *Engine) release() { e.state = nil }

// recluster places the faces no one has placed yet, finds new people and
// merges people found twice. It reports whether anything changed.
func (e *Engine) recluster(ctx context.Context) (bool, error) {
	t := time.Now()
	c, count, err := e.decide(ctx)
	if err != nil {
		return false, err
	}
	if len(c.Assign) == 0 && len(c.NewPeople) == 0 && len(c.Merges) == 0 {
		return false, nil
	}
	if err := e.store.ApplyClusters(ctx, c); err != nil {
		return false, err
	}
	slog.Info("faces grouped", "faces", count, "placed", len(c.Assign), "new people", len(c.NewPeople),
		"merged", len(c.Merges), "took", time.Since(t).Round(time.Millisecond))
	return true, nil
}

// decide retains the input snapshot, not the proposed changes. A concurrent
// user correction or a refused merge is therefore picked up by the next read.
func (e *Engine) decide(ctx context.Context) (store.ClusterChanges, int, error) {
	if e.state == nil || e.state.params != e.params {
		e.state = newClusterState(e.params)
	}
	snapshot, err := e.store.ClusterSince(ctx, e.model.Version, e.state.cursor)
	if err != nil {
		return store.ClusterChanges{}, 0, err
	}
	if err := e.state.update(ctx, snapshot); err != nil {
		e.state = nil
		return store.ClusterChanges{}, 0, err
	}
	loose := e.state.looseFaces()
	var links map[int64][]int64
	if snapshot.Full {
		links, err = e.links.Update(ctx, snapshot.Faces)
	} else {
		links, err = e.links.updateChanged(ctx, loose, snapshot.Faces, snapshot.Removed)
	}
	if err != nil {
		// A retry must offer every changed fingerprint to the linker again.
		e.state = nil
		return store.ClusterChanges{}, 0, err
	}
	if snapshot.Full {
		c, err := Cluster(ctx, Input{Faces: snapshot.Faces, People: snapshot.People, Rejections: snapshot.Rejections, Links: links}, e.params)
		return c, len(e.state.faces), err
	}
	c, err := e.state.cluster(ctx, loose, snapshot.People, links)
	return c, len(e.state.faces), err
}
