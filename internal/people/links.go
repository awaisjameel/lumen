package people

import (
	"cmp"
	"context"
	"runtime"
	"slices"

	"lumen/internal/ai"
	"lumen/internal/parallel"
	"lumen/internal/store"
)

// Linker finds each unplaced face's most similar unplaced faces: the links
// clustering follows. Comparing every face with every other is the one
// quadratic step of clustering, so a Linker remembers the links between
// runs and only compares faces it hasn't seen with the rest. Not safe for
// concurrent use.
type Linker struct {
	k     int
	floor float32
	near  map[int64][]near // face -> most similar others, best first
}

type near struct {
	id int64
	s  float32
}

// NewLinker returns an empty Linker for p.
func NewLinker(p Params) *Linker {
	return &Linker{k: p.Neighbours, floor: p.Threshold, near: map[int64][]near{}}
}

// Update returns the links of every unplaced face among faces: up to k
// others each, at least as similar as the threshold. Faces placed since the
// last call are forgotten (a link to one still counts: it leads to their
// person); links to faces no longer among faces (their photo was hidden or
// is gone) are dropped, and those faces' lists are worked out again. If ctx
// ends first, nothing is remembered and ctx's error is returned.
func (l *Linker) Update(ctx context.Context, faces []store.ClusterFace) (map[int64][]int64, error) {
	var loose []store.ClusterFace
	isLoose := make(map[int64]bool, len(faces))
	present := make(map[int64]bool, len(faces))
	for _, f := range faces {
		present[f.ID] = true
		if f.Person == 0 {
			loose = append(loose, f)
			isLoose[f.ID] = true
		}
	}
	for id, ns := range l.near {
		if !isLoose[id] || slices.ContainsFunc(ns, func(n near) bool { return !present[n.id] }) {
			delete(l.near, id) // placed, or lost a neighbour: compared afresh below
		}
	}
	var fresh []int // indexes into loose of faces not seen before
	for i, f := range loose {
		if _, ok := l.near[f.ID]; !ok {
			fresh = append(fresh, i)
		}
	}
	// Each new face against every loose face. Pairs with an old face are
	// also offered to the old face's list afterwards.
	lists := make([][]near, len(fresh))
	back := make([][]near, len(fresh)) // (old face, similarity to this new face)
	parallel.For(ctx, len(fresh), runtime.NumCPU(), func(k int) {
		v := loose[fresh[k]]
		for _, o := range loose {
			if o.ID == v.ID {
				continue
			}
			s := ai.Dot(v.Vec, o.Vec)
			if s < l.floor {
				continue
			}
			lists[k] = keepTop(lists[k], near{o.ID, s}, l.k, byNear)
			if _, old := l.near[o.ID]; old {
				back[k] = append(back[k], near{o.ID, s})
			}
		}
	})
	if err := ctx.Err(); err != nil {
		return nil, err // a half-made list would pass for a finished one next time
	}
	for k, i := range fresh {
		l.near[loose[i].ID] = lists[k]
	}
	for k, i := range fresh {
		id := loose[i].ID
		for _, b := range back[k] {
			// A face placed and then taken out again can still be listed.
			if !slices.ContainsFunc(l.near[b.id], func(n near) bool { return n.id == id }) {
				l.near[b.id] = keepTop(l.near[b.id], near{id, b.s}, l.k, byNear)
			}
		}
	}
	out := make(map[int64][]int64, len(l.near))
	for id, ns := range l.near {
		ids := make([]int64, len(ns))
		for i, n := range ns {
			ids[i] = n.id
		}
		out[id] = ids
	}
	return out, nil
}

// byNear orders most similar first, ties by ID, so results don't depend on
// the order faces were compared in.
func byNear(a, b near) int {
	if c := cmp.Compare(b.s, a.s); c != 0 {
		return c
	}
	return cmp.Compare(a.id, b.id)
}
