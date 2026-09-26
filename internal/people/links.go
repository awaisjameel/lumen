package people

import (
	"cmp"

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
// person).
func (l *Linker) Update(faces []store.ClusterFace) map[int64][]int64 {
	var loose []store.ClusterFace
	isLoose := map[int64]bool{}
	for _, f := range faces {
		if f.Person == 0 {
			loose = append(loose, f)
			isLoose[f.ID] = true
		}
	}
	for id := range l.near {
		if !isLoose[id] {
			delete(l.near, id)
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
	parallel(len(fresh), func(k int) {
		v := loose[fresh[k]]
		for _, o := range loose {
			if o.ID == v.ID {
				continue
			}
			s := dot(v.Vec, o.Vec)
			if s < l.floor {
				continue
			}
			lists[k] = keepTop(lists[k], near{o.ID, s}, l.k, byNear)
			if _, old := l.near[o.ID]; old {
				back[k] = append(back[k], near{o.ID, s})
			}
		}
	})
	for k, i := range fresh {
		l.near[loose[i].ID] = lists[k]
	}
	for k, i := range fresh {
		for _, b := range back[k] {
			l.near[b.id] = keepTop(l.near[b.id], near{loose[i].ID, b.s}, l.k, byNear)
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
	return out
}

// byNear orders most similar first, ties by ID, so results don't depend on
// the order faces were compared in.
func byNear(a, b near) int {
	if c := cmp.Compare(b.s, a.s); c != 0 {
		return c
	}
	return cmp.Compare(a.id, b.id)
}
