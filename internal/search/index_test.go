package search

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func unitVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	var n float64
	for i := range v {
		v[i] = float32(rng.NormFloat64())
		n += float64(v[i]) * float64(v[i])
	}
	for i := range v {
		v[i] /= float32(math.Sqrt(n))
	}
	return v
}

func TestQuantizeRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	v := unitVec(rng, 768)
	q, scale := Quantize(v)
	var worst float64
	for i := range v {
		worst = max(worst, math.Abs(float64(q[i])*float64(scale)-float64(v[i])))
	}
	if worst > float64(scale)/2+1e-6 {
		t.Errorf("worst error %g exceeds half a step (%g)", worst, scale/2)
	}
	if z, s := Quantize(make([]float32, 4)); s != 0 || slices.ContainsFunc(z, func(x int8) bool { return x != 0 }) {
		t.Errorf("zero vector: %v %v", z, s)
	}
}

// The int8 scan must rank like an exact float scan: same top results.
func TestSearchMatchesExactScan(t *testing.T) {
	const dim, n = 64, 5000
	rng := rand.New(rand.NewPCG(3, 4))
	x := NewIndex(dim)
	vecs := make([][]float32, n)
	for i := range vecs {
		vecs[i] = unitVec(rng, dim)
		q, s := Quantize(vecs[i])
		x.Put(int64(i+1), q, s)
	}
	query := unitVec(rng, dim)
	type pair struct {
		id int64
		s  float32
	}
	exact := make([]pair, n)
	for i, v := range vecs {
		var s float32
		for j := range v {
			s += v[j] * query[j]
		}
		exact[i] = pair{int64(i + 1), s}
	}
	slices.SortFunc(exact, func(a, b pair) int {
		if a.s > b.s {
			return -1
		}
		return 1
	})

	hits := x.Search(query, -1, 20, nil)
	if len(hits) != 20 {
		t.Fatalf("got %d hits, want 20", len(hits))
	}
	overlap := 0
	for _, h := range hits {
		for _, e := range exact[:20] {
			if h.ID == e.id {
				overlap++
			}
		}
	}
	if overlap < 18 {
		t.Errorf("top-20 overlap with the exact scan: %d", overlap)
	}
	if !slices.IsSortedFunc(hits, byScore) {
		t.Error("hits are not best first")
	}
	if h := x.Search(query, exact[0].s+0.1, 20, nil); len(h) != 0 {
		t.Errorf("minimum score ignored: %d hits", len(h))
	}
	even := func(id int64) bool { return id%2 == 0 }
	if h := x.Search(query, -1, 20, even); len(h) != 20 || slices.ContainsFunc(h, func(h Hit) bool { return !even(h.ID) }) {
		t.Errorf("filtered search returned other assets: %+v", h)
	}
}

func TestPutReplaces(t *testing.T) {
	x := NewIndex(2)
	x.Put(7, []int8{127, 0}, 1.0/127)
	x.Put(7, []int8{0, 127}, 1.0/127)
	x.Put(8, []int8{1}, 1) // wrong size: ignored
	if x.Len() != 1 {
		t.Fatalf("len %d, want 1", x.Len())
	}
	if h := x.Search([]float32{0, 1}, 0.5, 5, nil); len(h) != 1 || h[0].ID != 7 {
		t.Fatalf("hits %+v", h)
	}
}
