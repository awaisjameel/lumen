package search

import (
	"slices"
	"sync"

	"lumen/internal/store"
)

type videoFrame struct{ asset, at int64 }

// videoIndex gives every sampled frame a unique internal key. Its public
// search result is the best frame of each asset.
type videoIndex struct {
	mu      sync.RWMutex
	index   *Index
	next    int64
	frame   map[int64]videoFrame
	byAsset map[int64][]int64
}

func newVideoIndex(dim int) *videoIndex {
	return &videoIndex{index: NewIndex(dim), frame: map[int64]videoFrame{}, byAsset: map[int64][]int64{}}
}

func (v *videoIndex) add(asset, at int64, q []int8, scale float32) {
	v.next++
	key := v.next
	v.frame[key] = videoFrame{asset, at}
	v.byAsset[asset] = append(v.byAsset[asset], key)
	v.index.Put(key, q, scale)
}

func (v *videoIndex) replace(asset int64, frames []store.FrameEmbedding) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for _, key := range v.byAsset[asset] {
		v.index.Delete(key)
		delete(v.frame, key)
	}
	delete(v.byAsset, asset)
	for _, f := range frames {
		v.add(asset, f.At, f.Vec, f.Scale)
	}
}

// each visits sampled frames under the read lock. q is valid only in fn.
func (v *videoIndex) each(fn func(asset int64, q []int8, scale float32)) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	v.index.Each(func(key int64, q []int8, scale float32) {
		fn(v.frame[key].asset, q, scale)
	})
}

func (v *videoIndex) scoreAll(q []float32, fn func(asset int64, cos float32)) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	v.index.ScoreAll(q, func(key int64, cos float32) { fn(v.frame[key].asset, cos) })
}

func (v *videoIndex) search(q []float32, floor float32, k int, allow func(int64) bool) []Hit {
	v.mu.RLock()
	defer v.mu.RUnlock()
	// At most 24 sampled frames belong to one video; this many candidates
	// therefore includes at least k distinct videos if k exist above floor.
	frames := v.index.Search(q, floor, k*24, func(key int64) bool {
		return allow == nil || allow(v.frame[key].asset)
	})
	best := make(map[int64]Hit, min(k, len(frames)))
	for _, hit := range frames {
		f := v.frame[hit.ID]
		if old, ok := best[f.asset]; !ok || hit.Score > old.Score {
			best[f.asset] = Hit{ID: f.asset, Score: hit.Score, At: f.at}
		}
	}
	out := make([]Hit, 0, len(best))
	for _, hit := range best {
		out = append(out, hit)
	}
	slices.SortFunc(out, byScore)
	return out[:min(k, len(out))]
}
