package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestOverlongFrameLogReleasesTimestampWaiterBeforeEOF(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	log := readLog(r)
	written := make(chan struct{})
	go func() {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxLogLine+1))
		close(written)
	}()
	<-written
	ready := make(chan struct{})
	go func() {
		_, _ = log.time(t.Context(), 0)
		close(ready)
	}()
	select {
	case <-ready:
	case <-time.After(time.Second):
		_ = w.Close()
		<-ready
		<-log.finished
		t.Fatal("timestamp waiter blocks until stderr closes after an overlong line")
	}
	_ = w.Close()
	<-log.finished
}

func TestTimestampWaitEndsOnCancellation(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	log := readLog(r)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { _, err := log.time(ctx, 0); done <- err }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("timestamp wait: %v", err)
		}
	case <-time.After(time.Second):
		_ = w.Close()
		<-done
		t.Fatal("cancelled timestamp waiter did not stop")
	}
	_ = w.Close()
	<-log.finished
}

func TestFrameCancellationDropsBufferedFrames(t *testing.T) {
	log := readLog(bytes.NewBufferString("[info] pts_time:0\n[info] pts_time:1\n"))
	defer func() { <-log.finished }()
	ppm := append([]byte("P6\n1 1\n255\n"), 1, 2, 3)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	got, err := readFrames(ctx, bytes.NewReader(bytes.Repeat(ppm, 2)), log, 0, func(Frame) error {
		calls++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || got != 1 || calls != 1 {
		t.Fatalf("cancelled frames: %d frames, %d calls, %v", got, calls, err)
	}
}
