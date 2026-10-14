package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/store"
)

// BenchmarkPhotoScanWorkers uses a caller-selected real corpus without changing it.
// Repeated reads are warm; this benchmark does not evict the system file cache.
func BenchmarkPhotoScanWorkers(b *testing.B) {
	root := os.Getenv("LUMEN_SCAN_BENCH_ROOT")
	if root == "" {
		b.Fatal("LUMEN_SCAN_BENCH_ROOT must name a real photo corpus")
	}
	for _, workers := range []int{2, 4, 8, 16, 20} {
		b.Run(fmt.Sprint(workers), func(b *testing.B) {
			b.StopTimer()
			for range b.N {
				st, err := store.Open(filepath.Join(b.TempDir(), "library.db"))
				if err != nil {
					b.Fatal(err)
				}
				src, err := st.AddSource(b.Context(), root)
				if err != nil {
					b.Fatal(err)
				}
				s := Scanner{Store: st, HashWorkers: workers}
				b.StartTimer()
				start := time.Now()
				res, err := s.Scan(b.Context(), src, "", Options{})
				elapsed := time.Since(start)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				b.Logf("workers=%d seen=%d changed=%d scan_ms=%.3f", workers, res.Seen, res.Changed, float64(elapsed)/float64(time.Millisecond))
				if err := st.Close(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
