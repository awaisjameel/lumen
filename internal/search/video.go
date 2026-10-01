package search

import (
	"slices"
	"sync"

	"lumen/internal/media"
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

// load replaces every frame with the ones each adds (the engine loading,
// or loading again after a failed attempt).
func (v *videoIndex) load(each func(add func(asset, at int64, q []int8, scale float32)) error) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.index, v.next = NewIndex(v.index.dim), 0
	clear(v.frame)
	clear(v.byAsset)
	return each(v.add)
}

// add puts one frame in. Call with mu held.
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

// assets returns the videos with searchable moments.
func (v *videoIndex) assets() map[int64]bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make(map[int64]bool, len(v.byAsset))
	for id, keys := range v.byAsset {
		out[id] = len(keys) > 0
	}
	return out
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
	// At most MaxSamples sampled frames belong to one video; this many
	// candidates therefore includes at least k distinct videos if k exist
	// above floor.
	frames := v.index.Search(q, floor, k*media.MaxSamples, func(key int64) bool {
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
