// Package search turns text into ranked assets and decides which Things an
// image shows. Image embeddings live in SQLite (the source of truth) and, for
// speed, in an in-memory int8 Index that a plain parallel scan searches: at
// 300,000 photos that is about 230 MB and 17 ms (spikes/README.md).
package search

import (
	"cmp"
	"math"
	"runtime"
	"slices"
	"sync"
)

// Index holds every image embedding as int8 in one flat slice.
type Index struct {
	dim int

	mu     sync.RWMutex
	ids    []int64
	scales []float32
	vecs   []int8 // len(ids) * dim
	pos    map[int64]int
}

// NewIndex returns an empty index for vectors of dim values.
func NewIndex(dim int) *Index { return &Index{dim: dim, pos: map[int64]int{}} }

// Quantize maps a vector to int8 with one scale: v ≈ q * scale. A vector
// that is not finite quantizes to zero, which matches nothing.
func Quantize(v []float32) (q []int8, scale float32) {
	var m float32
	for _, x := range v {
		m = max(m, float32(math.Abs(float64(x))))
	}
	q = make([]int8, len(v))
	if m == 0 || math.IsNaN(float64(m)) || math.IsInf(float64(m), 0) {
		return q, 0
	}
	scale = m / 127
	for i, x := range v {
		q[i] = int8(max(-127, min(127, math.Round(float64(x/scale)))))
	}
	return q, scale
}

// Put adds or replaces id's vector (q is copied).
func (x *Index) Put(id int64, q []int8, scale float32) {
	if len(q) != x.dim {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if i, ok := x.pos[id]; ok {
		copy(x.vecs[i*x.dim:], q)
		x.scales[i] = scale
		return
	}
	x.pos[id] = len(x.ids)
	x.ids = append(x.ids, id)
	x.scales = append(x.scales, scale)
	x.vecs = append(x.vecs, q...)
}

// Delete removes a vector, keeping the index packed for parallel scans.
func (x *Index) Delete(id int64) {
	x.mu.Lock()
	defer x.mu.Unlock()
	i, ok := x.pos[id]
	if !ok {
		return
	}
	last := len(x.ids) - 1
	if i != last {
		x.ids[i] = x.ids[last]
		x.scales[i] = x.scales[last]
		copy(x.vecs[i*x.dim:(i+1)*x.dim], x.vecs[last*x.dim:(last+1)*x.dim])
		x.pos[x.ids[i]] = i
	}
	x.ids = x.ids[:last]
	x.scales = x.scales[:last]
	x.vecs = x.vecs[:last*x.dim]
	delete(x.pos, id)
}

// Len is how many vectors the index holds.
func (x *Index) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.ids)
}

// Hit is one asset and its cosine similarity to the query.
type Hit struct {
	ID    int64
	Score float32
	At    int64 // ms of the best matching video frame; 0 for photos
}

// rowsPerWorker is the least rows a search worker scans: fewer would cost
// more to start than they save.
const rowsPerWorker = 2048

// Search returns up to k assets whose cosine similarity to the unit vector
// q is at least minCos, best first. With allow, only assets it accepts count.
func (x *Index) Search(q []float32, minCos float32, k int, allow func(id int64) bool) []Hit {
	qq, qs := Quantize(q)
	x.mu.RLock()
	defer x.mu.RUnlock()
	n := len(x.ids)
	if n == 0 || k <= 0 || qs == 0 {
		return nil
	}
	workers := max(1, min(runtime.NumCPU(), n/rowsPerWorker))
	parts := make([][]Hit, workers)
	chunk := (n + workers - 1) / workers
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			lo, hi := w*chunk, min((w+1)*chunk, n)
			parts[w] = x.scan(qq, qs, lo, hi, minCos, k, allow)
		})
	}
	wg.Wait()
	all := slices.Concat(parts...)
	slices.SortFunc(all, byScore)
	return all[:min(k, len(all))]
}

// scan scores rows [lo, hi) and keeps the best k at or above minCos.
func (x *Index) scan(q []int8, qs float32, lo, hi int, minCos float32, k int, allow func(int64) bool) []Hit {
	var top []Hit
	floor := minCos
	for i := lo; i < hi; i++ {
		if allow != nil && !allow(x.ids[i]) {
			continue
		}
		s := x.cos(i, q, qs)
		if !(s >= floor) { // also drops NaN, which compares false
			continue
		}
		top = append(top, Hit{ID: x.ids[i], Score: s})
		if len(top) >= 2*k {
			// Keep the best k; anything below the k-th can't make it now.
			slices.SortFunc(top, byScore)
			top = top[:k]
			floor = max(floor, top[k-1].Score)
		}
	}
	return top
}

// Each calls fn with every vector. fn must not use the index; q is only
// valid during the call.
func (x *Index) Each(fn func(id int64, q []int8, scale float32)) {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for i, id := range x.ids {
		fn(id, x.vecs[i*x.dim:(i+1)*x.dim], x.scales[i])
	}
}

// ScoreAll calls fn with the cosine similarity of every vector to the unit
// vector q (for re-scoring one tag across the library).
func (x *Index) ScoreAll(q []float32, fn func(id int64, cos float32)) {
	qq, qs := Quantize(q)
	x.mu.RLock()
	defer x.mu.RUnlock()
	for i, id := range x.ids {
		fn(id, x.cos(i, qq, qs))
	}
}

// cos is the cosine similarity of row i to the quantized query q * qs.
// Call with mu held.
func (x *Index) cos(i int, q []int8, qs float32) float32 {
	v := x.vecs[i*x.dim : (i+1)*x.dim]
	var dot int32
	for j, a := range v {
		dot += int32(a) * int32(q[j])
	}
	return float32(dot) * qs * x.scales[i]
}

func byScore(a, b Hit) int {
	if c := cmp.Compare(b.Score, a.Score); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}
