package index

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

type fakeImageEncoder struct {
	calls    int
	threads  int    // CPU threads a run keeps busy: 0 is on the GPU
	onEncode func() // called in each run, if set
}

func (*fakeImageEncoder) BatchSize() int    { return 2 }
func (e *fakeImageEncoder) CPUThreads() int { return e.threads }
func (e *fakeImageEncoder) Encode(inputs [][]byte) ([][]float32, error) {
	e.calls++
	if e.onEncode != nil {
		e.onEncode()
	}
	return make([][]float32, len(inputs)), nil
}

func TestImageBatchOnTheCPUTakesItsThreadsFromTheBudget(t *testing.T) {
	budget := newCPUBudget(4)
	free := func() int {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		return budget.free
	}
	holding, release, err := budget.acquire(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		ctx      context.Context
		threads  int
		wantFree int // during the run
	}{
		{"on the CPU", t.Context(), 2, 1},
		{"on the GPU", t.Context(), 0, 3},
		{"in work holding threads", holding, 2, 3},
	} {
		during := -1
		enc := &fakeImageEncoder{threads: c.threads, onEncode: func() { during = free() }}
		batch := &modelBatch{enc: enc, cpu: budget}
		if err := batch.add(c.ctx, []byte{1}, func([]float32) {}); err != nil {
			t.Fatal(err)
		}
		if err := batch.flush(c.ctx); err != nil {
			t.Fatal(err)
		}
		if during != c.wantFree || free() != 3 {
			t.Errorf("%s: %d of 4 threads free during the run (want %d), %d after (want 3)", c.name, during, c.wantFree, free())
		}
	}
	release()
}

func TestImageBatchDoesNotRunQueuedInputsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	enc := &fakeImageEncoder{}
	batch := &modelBatch{enc: enc}
	if err := batch.add(ctx, []byte{1}, func([]float32) {}); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := batch.add(ctx, []byte{2}, func([]float32) {}); !errors.Is(err, context.Canceled) {
		t.Fatalf("add after cancellation = %v", err)
	}
	if err := batch.flush(ctx); !errors.Is(err, context.Canceled) || enc.calls != 0 {
		t.Fatalf("flush after cancellation = %v, model calls = %d", err, enc.calls)
	}
}

func TestFacesDoNotStartDetectionAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := findFaces(ctx, nil, media.Image{}, 0, &store.FaceResult{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("detect after cancellation = %v", err)
	}
}

func TestCancelledStageDoesNotFetchMoreJobs(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	fetches := 0
	runStage(ctx, ix, stage[int]{
		fetch: func(context.Context, int) ([]store.JobItem, error) {
			fetches++
			return nil, ctx.Err()
		},
	})
	if fetches != 0 {
		t.Fatalf("fetched %d times after cancellation", fetches)
	}
}

func TestStageIsAtWorkFromItsBatchUntilItRunsOut(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	working := func() core.Working { ix.mu.Lock(); defer ix.mu.Unlock(); return ix.workingNow() }
	var during, paused core.Working
	var fetches atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		runStage(ctx, ix, stage[int]{
			name: core.StageThumbnail, work: workThumbnail, wake: make(chan struct{}),
			fetch: func(context.Context, int) ([]store.JobItem, error) {
				if fetches.Add(1) == 1 {
					return []store.JobItem{{AssetID: 1}}, nil
				}
				return nil, nil
			},
			process: func(context.Context, []store.JobItem) ([]int, error) {
				during = working()
				ix.SetPaused(true) // a held stage finishes its batch, but isn't at work
				paused = working()
				ix.SetPaused(false)
				return []int{1}, nil
			},
			save: func(context.Context, []int) error { return nil },
		})
	}()
	eventually(t, "the stage to run out", func() bool { return fetches.Load() > 1 && !working().Thumbnail })
	cancel()
	<-done // during and paused are written before it closes
	if during != (core.Working{Thumbnail: true}) || paused != (core.Working{}) {
		t.Fatalf("at work during its batch: %+v, while paused: %+v; want only thumbnails, then nothing", during, paused)
	}
}
