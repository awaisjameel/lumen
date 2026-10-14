package people

import (
	"cmp"
	"context"
	"math"
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
	vecOf map[int64]uint64 // face -> fingerprint of the vector near was worked out from
}

type near struct {
	id int64
	s  float32
}

// NewLinker returns an empty Linker for p.
func NewLinker(p Params) *Linker {
	return &Linker{k: p.Neighbours, floor: p.Threshold, near: map[int64][]near{}, vecOf: map[int64]uint64{}}
}

// Update returns the links of every unplaced face among faces: up to k
// others each, at least as similar as the threshold. Faces placed since the
// last call are forgotten (a link to one still counts: it leads to their
// person); links to faces no longer among faces (their photo was hidden or
// is gone), or whose vector changed (the photo was looked at again), are
// dropped, and those faces' lists are worked out again. If ctx ends first,
// nothing is remembered and ctx's error is returned.
func (l *Linker) Update(ctx context.Context, faces []store.ClusterFace) (map[int64][]int64, error) {
	var loose []store.ClusterFace
	isLoose := make(map[int64]bool, len(faces))
	current := make(map[int64]bool, len(faces)) // present, with the vector the lists were made from
	prints := make(map[int64]uint64, len(faces))
	for _, f := range faces {
		fp := fingerprint(f.Vec)
		prints[f.ID] = fp
		if old, seen := l.vecOf[f.ID]; !seen || old == fp {
			current[f.ID] = true
		}
		if f.Person == 0 {
			loose = append(loose, f)
			isLoose[f.ID] = true
		}
	}
	invalid := map[int64]bool{}
	for id := range l.vecOf {
		if !current[id] {
			invalid[id] = true
		}
	}
	out, err := l.updateLoose(ctx, loose, isLoose, invalid)
	if err == nil {
		l.vecOf = prints
	}
	return out, err
}

// updateChanged preserves the reference's retained links without fingerprinting
// placed faces again. Full snapshots still go through Update.
func (l *Linker) updateChanged(ctx context.Context, loose, changed []store.ClusterFace, removed []int64) (map[int64][]int64, error) {
	invalid := map[int64]bool{}
	for _, id := range removed {
		invalid[id] = true
	}
	prints := map[int64]uint64{}
	for _, f := range changed {
		fp := fingerprint(f.Vec)
		prints[f.ID] = fp
		if old, seen := l.vecOf[f.ID]; seen && old != fp {
			invalid[f.ID] = true
		}
	}
	isLoose := make(map[int64]bool, len(loose))
	for _, f := range loose {
		isLoose[f.ID] = true
	}
	out, err := l.updateLoose(ctx, loose, isLoose, invalid)
	if err == nil {
		for _, id := range removed {
			delete(l.vecOf, id)
		}
		for id, fp := range prints {
			l.vecOf[id] = fp
		}
	}
	return out, err
}

func (l *Linker) updateLoose(ctx context.Context, loose []store.ClusterFace, isLoose, invalid map[int64]bool) (map[int64][]int64, error) {
	for id, ns := range l.near {
		if !isLoose[id] || invalid[id] || slices.ContainsFunc(ns, func(n near) bool { return invalid[n.id] }) {
			delete(l.near, id) // placed, changed, or lost a neighbour: compared afresh below
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
		for i, o := range loose {
			if i%1024 == 0 && ctx.Err() != nil {
				return
			}
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

// fingerprint identifies a vector (FNV-1a over its bits), to notice one
// rewritten in place.
func fingerprint(v []float32) uint64 {
	h := uint64(14695981039346656037)
	for _, x := range v {
		h = (h ^ uint64(math.Float32bits(x))) * 1099511628211
	}
	return h
}

// byNear orders most similar first, ties by ID, so results don't depend on
// the order faces were compared in.
func byNear(a, b near) int {
	if c := cmp.Compare(b.s, a.s); c != 0 {
		return c
	}
	return cmp.Compare(a.id, b.id)
}
