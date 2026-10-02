package index

import (
	"context"
	"slices"
	"sync"
)

// cpuBudget is how many CPU threads the indexer's heavy work keeps busy at
// once, across all its stages: core.DefaultCPUThreads, half the machine's,
// unless the user chose another number (core.SettingCPUThreads). Each stage
// sizes its own workers to run well alone; without a shared bound they ran
// together at several times the machine's threads (thumbnails, faces, the
// image model's inputs, text reading and FFmpeg on a library of films), Lumen
// and FFmpeg held a 20-thread laptop at 100% for minutes and other programs
// froze.
//
// Slots go out first come first served, so a stage with many workers can't
// starve one that asks for several at once. Work that holds slots marks its
// context (see acquire), and anything it calls that would take more runs on
// them instead: waiting for more while holding some could wait forever.
type cpuBudget struct {
	mu      sync.Mutex    // guards the rest
	size    int           // slots in all
	free    int           // size less those held; below 0 just after the size shrank
	waiting []*budgetWait // oldest first
}

type budgetWait struct {
	n     int           // slots wanted; guarded by cpuBudget.mu
	ready chan struct{} // closed once its slots are taken for it
}

// holdingKey marks a context whose work holds slots.
type holdingKey struct{}

func newCPUBudget(size int) *cpuBudget {
	size = max(size, 1)
	return &cpuBudget{size: size, free: size}
}

// resize sets how many slots there are. Work holding slots keeps them: when
// the budget shrinks, new work waits until enough of it has finished.
func (b *cpuBudget) resize(size int) {
	size = max(size, 1)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.free += size - b.size
	b.size = size
	for _, w := range b.waiting {
		w.n = min(w.n, size) // or it would wait forever, and everyone behind it
	}
	b.grant()
}

// acquire waits for n slots (at most the budget's size) and returns a context
// marked as holding them, with the function that gives them back. A context
// already marked takes none. It fails only when ctx ends first.
func (b *cpuBudget) acquire(ctx context.Context, n int) (context.Context, func(), error) {
	if ctx.Value(holdingKey{}) != nil {
		return ctx, func() {}, nil
	}
	b.mu.Lock()
	n = min(max(n, 1), b.size)
	if len(b.waiting) == 0 && b.free >= n {
		b.free -= n
		b.mu.Unlock()
		return b.held(ctx, n)
	}
	w := &budgetWait{n: n, ready: make(chan struct{})}
	b.waiting = append(b.waiting, w)
	b.mu.Unlock()
	select {
	case <-w.ready:
		return b.held(ctx, w.n) // set before ready closed
	case <-ctx.Done():
		b.mu.Lock()
		select {
		case <-w.ready: // granted just as ctx ended
			b.free += w.n
		default:
			b.waiting = slices.DeleteFunc(b.waiting, func(x *budgetWait) bool { return x == w })
		}
		b.grant() // those behind it may fit now
		b.mu.Unlock()
		return ctx, func() {}, ctx.Err()
	}
}

func (b *cpuBudget) held(ctx context.Context, n int) (context.Context, func(), error) {
	var once sync.Once
	return context.WithValue(ctx, holdingKey{}, true), func() {
		once.Do(func() {
			b.mu.Lock()
			b.free += n
			b.grant()
			b.mu.Unlock()
		})
	}, nil
}

// grant hands free slots to the waiting, in order. Call with mu held.
func (b *cpuBudget) grant() {
	for len(b.waiting) > 0 && b.waiting[0].n <= b.free {
		w := b.waiting[0]
		b.free -= w.n
		b.waiting = b.waiting[1:]
		close(w.ready)
	}
}
