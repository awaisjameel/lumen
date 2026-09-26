// Package parallel spreads loops over goroutines.
package parallel

import (
	"context"
	"sync"
)

// For calls fn(0..n-1) on up to workers goroutines, handing out indexes as
// workers free up (so uneven calls balance out), and returns once every
// call has. If ctx ends, calls not yet started are skipped; those running
// finish.
func For(ctx context.Context, n, workers int, fn func(i int)) {
	if workers <= 1 || n <= 1 {
		for i := range n {
			if ctx.Err() != nil {
				return
			}
			fn(i)
		}
		return
	}
	idx := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, n) {
		wg.Go(func() {
			for i := range idx {
				fn(i)
			}
		})
	}
feed:
	for i := range n {
		select {
		case idx <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(idx)
	wg.Wait()
}
