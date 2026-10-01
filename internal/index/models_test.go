package index

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancelledModelAcquisitionDoesNotWaitForAnotherBatch(t *testing.T) {
	var loads atomic.Int32
	m := &model[int]{
		open:   func() (int, error) { loads.Add(1); return 1, nil },
		report: func(int, bool, error) {},
	}
	_, release, err := m.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, release, err := m.acquire(ctx)
		release()
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || loads.Load() != 1 {
			t.Fatalf("cancelled acquisition = %v, loads = %d", err, loads.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled acquisition waited for the active batch")
	}
}

func TestCancelledModelAcquisitionDoesNotLoadAnIdleModel(t *testing.T) {
	loads := 0
	m := &model[int]{
		open:   func() (int, error) { loads++; return 1, nil },
		report: func(int, bool, error) {},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, release, err := m.acquire(ctx)
	release()
	if !errors.Is(err, context.Canceled) || loads != 0 {
		t.Fatalf("cancelled acquisition = %v, loads = %d", err, loads)
	}
}

func TestModelIsReleasedOnlyAfterItsBatchFinishes(t *testing.T) {
	var closes atomic.Int32
	m := &model[int]{
		open:   func() (int, error) { return 1, nil },
		close:  func(int) { closes.Add(1) },
		report: func(int, bool, error) {},
	}
	_, release, err := m.acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { m.release(); close(done) }()
	if closes.Load() != 0 {
		t.Fatal("model released while its batch was active")
	}
	release()
	select {
	case <-done:
		if closes.Load() != 1 {
			t.Fatalf("closes = %d, want 1", closes.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("model release did not finish")
	}
}
