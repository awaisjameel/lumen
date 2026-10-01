package index

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

type fakeImageEncoder struct{ calls int }

func (*fakeImageEncoder) BatchSize() int { return 2 }
func (e *fakeImageEncoder) Encode(inputs [][]byte) ([][]float32, error) {
	e.calls++
	return make([][]float32, len(inputs)), nil
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
	ix := New(nil, core.Paths{}, nil, Models{}, &fakeEvents{})
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
