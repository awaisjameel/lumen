package search

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/store"
)

// BenchmarkSearchStartupLoad measures the real loader without loading a model.
// Its peak is sampled every millisecond, so it is a lower bound on transient heap.
func BenchmarkSearchStartupLoad(b *testing.B) {
	for _, n := range []int{100000, 300000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			st := seedSearchLoad(b, n)
			const dim = 768
			model := ai.Model{Dim: dim, Version: "bench"}
			b.ResetTimer()
			for range b.N {
				b.StopTimer()
				runtime.GC()
				var before runtime.MemStats
				runtime.ReadMemStats(&before)
				done := make(chan struct{})
				var wg sync.WaitGroup
				var peak uint64
				wg.Go(func() {
					tick := time.NewTicker(time.Millisecond)
					defer tick.Stop()
					for {
						var m runtime.MemStats
						runtime.ReadMemStats(&m)
						peak = max(peak, m.HeapAlloc)
						select {
						case <-done:
							return
						case <-tick.C:
						}
					}
				})
				e := NewEngine(st, model)
				b.StartTimer()
				start := time.Now()
				err := e.load(b.Context())
				elapsed := time.Since(start)
				b.StopTimer()
				close(done)
				wg.Wait()
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				peak = max(peak, after.HeapAlloc)
				if err != nil || e.Indexed() != n {
					b.Fatalf("load: %d vectors, %v", e.Indexed(), err)
				}
				b.Logf("vectors=%d load_ms=%.3f allocated_bytes=%d peak_heap_bytes=%d baseline_heap_bytes=%d", n, float64(elapsed)/float64(time.Millisecond), after.TotalAlloc-before.TotalAlloc, peak, before.HeapAlloc)
				runtime.KeepAlive(e)
				e.Close()
				b.StartTimer()
			}
		})
	}
}

func seedSearchLoad(b *testing.B, n int) *store.Store {
	b.Helper()
	root := b.TempDir()
	if supplied := os.Getenv("LUMEN_SEARCH_BENCH_DIR"); supplied != "" {
		root = filepath.Join(supplied, fmt.Sprint(n))
		if err := os.MkdirAll(root, 0o750); err != nil {
			b.Fatal(err)
		}
	}
	name := filepath.Join(root, "library.db")
	_, exists := os.Stat(name)
	st, err := store.Open(name)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := st.Close(); err != nil {
			b.Error(err)
		}
	})
	if exists == nil {
		return st
	}
	src, err := st.AddSource(b.Context(), `C:\Photos`)
	if err != nil {
		b.Fatal(err)
	}
	const dim = 768
	const batch = 2000
	for lo := 0; lo < n; lo += batch {
		items := make([]store.IngestItem, min(batch, n-lo))
		embeddings := make([]store.EmbedResult, len(items))
		for i := range items {
			id := lo + i + 1
			items[i] = store.IngestItem{SourceID: src.ID, Path: fmt.Sprintf(`C:\Photos\%d.jpg`, id), Hash: []byte(fmt.Sprintf("%016x", id)), Kind: core.KindPhoto, TakenAt: int64(id) * 86400}
			embeddings[i] = store.EmbedResult{AssetID: int64(id), Vec: make([]int8, dim), Scale: 0.01}
			embeddings[i].Vec[id%dim] = 100
		}
		if _, err := st.Ingest(b.Context(), items); err != nil {
			b.Fatal(err)
		}
		if err := st.SaveEmbeddings(b.Context(), "bench", "bench", embeddings); err != nil {
			b.Fatal(err)
		}
	}
	if err := st.SetInt(b.Context(), core.SettingVocabulary, VocabularyVersion); err != nil {
		b.Fatal(err)
	}

	return st
}

// BenchmarkEmbeddingStartupComparison alternates the streaming loaders on one
// library to separate allocation growth from machine load and cache differences.
func BenchmarkEmbeddingStartupComparison(b *testing.B) {
	for _, n := range []int{100000, 300000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			st := seedSearchLoad(b, n)
			b.ResetTimer()
			for range b.N {
				for sample := range 5 {
					modes := []string{"unsized", "sized"}
					if sample%2 != 0 {
						modes = []string{"sized", "unsized"}
					}
					for _, mode := range modes {
						b.StopTimer()
						runtime.GC()
						var before, after runtime.MemStats
						runtime.ReadMemStats(&before)
						x := NewIndex(768)
						b.StartTimer()
						started := time.Now()
						var err error
						if mode == "sized" {
							err = st.LoadEmbeddings(b.Context(), "bench", x.reserve, x.Put)
						} else {
							err = st.Embeddings(b.Context(), "bench", x.Put)
						}
						elapsed := time.Since(started)
						b.StopTimer()
						runtime.ReadMemStats(&after)
						if err != nil || x.Len() != n {
							b.Fatalf("load=%d %v", x.Len(), err)
						}
						b.Logf("sample=%d mode=%s vectors=%d load_ms=%.3f allocated_bytes=%d end_heap_bytes=%d", sample+1, mode, n, float64(elapsed)/float64(time.Millisecond), after.TotalAlloc-before.TotalAlloc, after.HeapAlloc)
						runtime.KeepAlive(x)
						b.StartTimer()
					}
				}
			}
		})
	}
}
