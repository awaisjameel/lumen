package services

import (
	"context"
	"sync"
	"time"
)

// BlockGate is whether the Blocked screen is unlocked. LibraryService opens
// and closes it; it is shared with MediaService and the Streamer, so the
// photos a block covers are served only while the screen is open, and stop
// being served the moment it locks. The zero value is locked.
type BlockGate struct {
	mu       sync.Mutex
	unlocked bool
	usedAt   time.Time                     // the last use while unlocked (see blockUnlockIdle)
	holds    map[uint64]context.CancelFunc // requests being answered only because it is open
	holdID   uint64
	clock    func() time.Time // time.Now unless a test sets it
}

func (g *BlockGate) now() time.Time {
	if g.clock != nil {
		return g.clock()
	}
	return time.Now()
}

// Unlocked reports whether the gate is open, and counts this as a use.
func (g *BlockGate) Unlocked() bool { return g.unlockedAt(g.now()) }

// unlockedAt is Unlocked at the time now: a gate unused for blockUnlockIdle
// locks itself, in case the screen never said it was left. A covered photo
// still being sent is a use that has not ended, so it keeps the gate open.
func (g *BlockGate) unlockedAt(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.unlocked && len(g.holds) == 0 && now.Sub(g.usedAt) > blockUnlockIdle {
		g.lock()
	}
	if g.unlocked {
		g.usedAt = now
	}
	return g.unlocked
}

// Hold counts as a use and, if the gate is open, returns a context that ends
// when it locks, and the function that releases it once the request is over.
// A request that is answered only because the Blocked screen is open must be
// answered under it, so locking stops a response already on its way. A locked
// gate answers false.
func (g *BlockGate) Hold(parent context.Context) (context.Context, context.CancelFunc, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	if g.unlocked && len(g.holds) == 0 && now.Sub(g.usedAt) > blockUnlockIdle {
		g.lock()
	}
	if !g.unlocked {
		return parent, func() {}, false
	}
	g.usedAt = now
	ctx, cancel := context.WithCancel(parent)
	if g.holds == nil {
		g.holds = map[uint64]context.CancelFunc{}
	}
	g.holdID++
	id := g.holdID
	g.holds[id] = cancel
	return ctx, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		delete(g.holds, id)
		if g.unlocked {
			g.usedAt = g.now() // the response was a use until it ended
		}
		cancel()
	}, true
}

// open unlocks the gate at the time now.
func (g *BlockGate) open(now time.Time) {
	g.mu.Lock()
	g.unlocked, g.usedAt = true, now
	g.mu.Unlock()
}

// close locks the gate, and ends every request held open by it.
func (g *BlockGate) close() {
	g.mu.Lock()
	g.lock()
	g.mu.Unlock()
}

// lock locks the gate; the caller holds mu.
func (g *BlockGate) lock() {
	g.unlocked = false
	for id, cancel := range g.holds {
		cancel()
		delete(g.holds, id)
	}
}
