package index

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

func TestIndexerTakesTheThreadsSettingAtStartAndAtOnce(t *testing.T) {
	paths := core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := st.SetCPUThreads(ctx, 1); err != nil {
		t.Fatal(err)
	}
	ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{}, &fakeEvents{})
	size := func() int {
		ix.cpu.mu.Lock()
		defer ix.cpu.mu.Unlock()
		return ix.cpu.size
	}
	ix.Start(ctx)
	defer func() {
		cancel()
		ix.Stop()
	}()
	if got := size(); got != 1 {
		t.Fatalf("started with %d threads, want the stored 1", got)
	}
	ix.SetCPUThreads(2)
	if got := size(); got != 2 {
		t.Fatalf("%d threads after the setting changed, want 2", got)
	}
	(*Indexer)(nil).SetCPUThreads(2) // a library without an indexer only stores it
}

func TestCPUBudgetNeverHandsOutMoreThanItsSlots(t *testing.T) {
	b := newCPUBudget(4)
	var busy, most atomic.Int32
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			n := i%3 + 1
			_, release, err := b.acquire(t.Context(), n)
			if err != nil {
				t.Error(err)
				return
			}
			now := busy.Add(int32(n))
			for {
				m := most.Load()
				if now <= m || most.CompareAndSwap(m, now) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			busy.Add(-int32(n))
			release()
		})
	}
	wg.Wait()
	if m := most.Load(); m > 4 || m < 2 {
		t.Fatalf("at most %d slots were busy at once, want 2 to 4 of 4", m)
	}
	if b.free != 4 || len(b.waiting) != 0 {
		t.Fatalf("after all released: %d free, %d waiting", b.free, len(b.waiting))
	}
}

func TestCPUBudgetWorkHoldingSlotsTakesNoMore(t *testing.T) {
	b := newCPUBudget(2)
	ctx, release, err := b.acquire(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	// A video's frames go to the image model while FFmpeg's slots are held:
	// waiting for more would wait forever.
	done := make(chan error, 1)
	go func() {
		_, inner, err := b.acquire(ctx, 2)
		inner()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nested work waited for slots its caller holds")
	}
	release()
	release() // a second release gives nothing back twice
	if b.free != 2 {
		t.Fatalf("%d slots free, want 2", b.free)
	}
}

func TestCPUBudgetCancelledWaitLetsTheNextIn(t *testing.T) {
	b := newCPUBudget(2)
	_, holding, err := b.acquire(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// The first in line wants both slots, so the one behind it waits too.
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() {
		_, release, err := b.acquire(ctx, 2)
		release()
		first <- err
	}()
	waitFor(t, "the first in line", func() bool { b.mu.Lock(); defer b.mu.Unlock(); return len(b.waiting) == 1 })
	second := make(chan error, 1)
	go func() {
		_, release, err := b.acquire(t.Context(), 1)
		release()
		second <- err
	}()
	waitFor(t, "the second in line", func() bool { b.mu.Lock(); defer b.mu.Unlock(); return len(b.waiting) == 2 })
	cancel()
	if err := <-first; err == nil {
		t.Fatal("a cancelled wait took its slots")
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the next in line kept waiting after the first gave up")
	}
	holding()
	if b.free != 2 {
		t.Fatalf("%d slots free, want 2", b.free)
	}
}

func TestCPUBudgetResizesWhileWorkRuns(t *testing.T) {
	b := newCPUBudget(2)
	_, first, err := b.acquire(t.Context(), 2)
	if err != nil {
		t.Fatal(err)
	}
	// More threads: work waiting starts at once.
	more := make(chan func(), 1)
	go func() {
		_, release, err := b.acquire(t.Context(), 3) // more than there are yet
		if err != nil {
			t.Error(err)
		}
		more <- release
	}()
	waitFor(t, "work to wait", func() bool { b.mu.Lock(); defer b.mu.Unlock(); return len(b.waiting) == 1 })
	b.resize(5)
	second := <-more
	// Fewer: what runs keeps its slots, and new work waits for them back.
	b.resize(1)
	got := make(chan struct{})
	go func() {
		_, release, err := b.acquire(t.Context(), 1)
		if err != nil {
			t.Error(err)
		}
		release()
		close(got)
	}()
	first()
	select {
	case <-got:
		t.Fatal("work started while more slots were busy than the budget has")
	case <-time.After(50 * time.Millisecond):
	}
	second()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("work kept waiting after the running work finished")
	}
	if b.free != 1 || b.size != 1 {
		t.Fatalf("%d of %d slots free, want 1 of 1", b.free, b.size)
	}
}
