package search

import (
	"testing"

	"lumen/internal/store"
)

func TestVideoIndexKeepsBestMomentAndReplacesStaleFrames(t *testing.T) {
	v := newVideoIndex(2)
	v.replace(10, []store.FrameEmbedding{
		{At: 1000, Vec: []int8{70, 70}, Scale: 0.01},
		{At: 5000, Vec: []int8{100, 0}, Scale: 0.01},
	})
	v.replace(20, []store.FrameEmbedding{{At: 3000, Vec: []int8{80, 0}, Scale: 0.01}})
	hits := v.search([]float32{1, 0}, 0.1, 2, nil)
	if len(hits) != 2 || hits[0].ID != 10 || hits[0].At != 5000 || hits[1].ID != 20 {
		t.Fatalf("best video moments: %+v", hits)
	}
	v.replace(10, []store.FrameEmbedding{{At: 9000, Vec: []int8{0, 100}, Scale: 0.01}})
	hits = v.search([]float32{1, 0}, 0.1, 2, nil)
	if len(hits) != 1 || hits[0].ID != 20 {
		t.Fatalf("stale moment survived replacement: %+v", hits)
	}
	hits = v.search([]float32{1, 0}, 0.1, 2, func(id int64) bool { return id == 10 })
	if len(hits) != 0 {
		t.Fatalf("person filter accepted wrong video: %+v", hits)
	}
}
