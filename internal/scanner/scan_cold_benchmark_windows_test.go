//go:build windows

package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"lumen/internal/store"
)

// BenchmarkColdPhotoScanWorkers reads each supplied corpus once. The caller
// prepares copies with uncached writes; this benchmark never empties the cache.
func BenchmarkColdPhotoScanWorkers(b *testing.B) {
	if b.N != 1 {
		b.Fatal("cold scans require -benchtime=1x, with five unused copies per worker count")
	}
	root := os.Getenv("LUMEN_COLD_SCAN_BENCH_ROOT")
	if root == "" {
		b.Fatal("LUMEN_COLD_SCAN_BENCH_ROOT must name unused copies of a real corpus")
	}
	b.StopTimer()
	for sample := 1; sample <= 5; sample++ {
		counts := []int{2, 4, 8, 16, 20}
		if sample%2 == 0 {
			counts = []int{20, 16, 8, 4, 2}
		}
		for _, workers := range counts {
			folder := filepath.Join(root, fmt.Sprintf("%d-%d", workers, sample))
			st, err := store.Open(filepath.Join(b.TempDir(), "library.db"))
			if err != nil {
				b.Fatal(err)
			}
			src, err := st.AddSource(b.Context(), folder)
			if err != nil {
				b.Fatal(err)
			}
			s := Scanner{Store: st, HashWorkers: workers}
			before := scanCPUTime(b)
			start := time.Now()
			b.StartTimer()
			res, err := s.Scan(b.Context(), src, "", Options{})
			b.StopTimer()
			elapsed := time.Since(start)
			cpu := scanCPUTime(b) - before
			if err != nil || res.Seen == 0 || res.Changed != res.Seen {
				b.Fatalf("cold scan=%+v err=%v", res, err)
			}
			b.Logf("sample=%d workers=%d seen=%d changed=%d scan_ms=%.3f cpu_ms=%.3f", sample, workers, res.Seen, res.Changed, float64(elapsed)/float64(time.Millisecond), float64(cpu)/float64(time.Millisecond))
			if err := st.Close(); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func scanCPUTime(b *testing.B) time.Duration {
	b.Helper()
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user); err != nil {
		b.Fatal(err)
	}
	ticks := (uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)) + (uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime))
	return time.Duration(ticks) * 100
}
