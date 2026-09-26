package parallel

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestForCallsEachIndexOnce(t *testing.T) {
	for _, workers := range []int{0, 1, 3, 64} {
		for _, n := range []int{0, 1, 2, 100} {
			seen := make([]atomic.Int32, n)
			For(context.Background(), n, workers, func(i int) { seen[i].Add(1) })
			for i := range seen {
				if c := seen[i].Load(); c != 1 {
					t.Fatalf("workers %d, n %d: index %d called %d times", workers, n, i, c)
				}
			}
		}
	}
}

func TestForStopsStartingWhenCancelled(t *testing.T) {
	for _, workers := range []int{1, 4} {
		ctx, cancel := context.WithCancel(context.Background())
		var calls atomic.Int32
		For(ctx, 1000, workers, func(int) {
			if calls.Add(1) == 10 {
				cancel()
			}
		})
		// A worker may take an index or two before the feeder sees the
		// cancellation, but far from all of them.
		if c := calls.Load(); c < 10 || c >= 100 {
			t.Errorf("workers %d: %d calls after cancelling at the 10th", workers, c)
		}
	}
}
