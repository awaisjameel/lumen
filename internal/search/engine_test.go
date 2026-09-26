package search

import (
	"testing"

	"lumen/internal/ai"
	"lumen/internal/core"
)

// basis returns the i-th unit vector of a dim-dimensional space.
func basis(dim, i int) []float32 {
	v := make([]float32, dim)
	v[i] = 1
	return v
}

func TestTagsGiveEachPhotoItsBestKind(t *testing.T) {
	m := ai.Model{Dim: 4, LogitScale: 112.67, LogitBias: -16.77}
	e := &Engine{model: m}
	e.labels = []label{
		{id: 1, vecs: [][]float32{basis(4, 0)}, exclusive: true}, // "People"
		{id: 2, vecs: [][]float32{basis(4, 1)}, exclusive: true}, // "Screenshots"
		{id: 3, vecs: [][]float32{basis(4, 2)}},                  // an ordinary Thing
	}
	// A photo somewhat like kind 1 (cos 0.09: SigLIP probability ~0.001, far
	// below any threshold) and unlike kind 2, and very like Thing 3.
	photo := []float32{0.09, 0.0, 0.3, 0.95}
	tags := e.Tags(photo)
	got := map[int64]float32{}
	for _, tg := range tags {
		got[tg.TagID] = tg.Score
	}
	if got[1] < kindShare {
		t.Errorf("best kind not applied: %v", got)
	}
	if _, ok := got[2]; ok {
		t.Errorf("losing kind applied: %v", got)
	}
	if got[3] < core.TagConfidenceMax {
		t.Errorf("confident Thing missing: %v", got)
	}

	// Two kinds equally likely: neither clearly wins, so neither is forced on.
	tie := []float32{0.09, 0.09, 0, 0.99}
	for _, tg := range e.Tags(tie) {
		if tg.TagID == 1 || tg.TagID == 2 {
			t.Errorf("kind applied on a tie: %+v", tg)
		}
	}
}

func TestSinkKindAbsorbsButNeverTags(t *testing.T) {
	m := ai.Model{Dim: 3, LogitScale: 112.67, LogitBias: -16.77}
	e := &Engine{model: m}
	e.labels = []label{
		{id: 1, vecs: [][]float32{basis(3, 0)}, exclusive: true},
		{id: 2, vecs: [][]float32{basis(3, 1)}, exclusive: true, sink: true},
		{id: 3, vecs: [][]float32{basis(3, 2)}, floor: 0.9}, // needs a very sure score
	}
	// Closest to the sink: no kind is applied at all, and the sure-only
	// Thing's middling score doesn't count.
	for _, tg := range e.Tags([]float32{0.05, 0.12, 0.13}) {
		t.Errorf("unexpected tag %+v", tg)
	}
}
