package services

import (
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
	usedAt   time.Time // the last use while unlocked (see blockUnlockIdle)
}

// Unlocked reports whether the gate is open, and counts this as a use.
func (g *BlockGate) Unlocked() bool { return g.unlockedAt(time.Now()) }

// unlockedAt is Unlocked at the time now: a gate unused for blockUnlockIdle
// locks itself, in case the screen never said it was left.
func (g *BlockGate) unlockedAt(now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.unlocked && now.Sub(g.usedAt) > blockUnlockIdle {
		g.unlocked = false
	}
	if g.unlocked {
		g.usedAt = now
	}
	return g.unlocked
}

// open unlocks the gate at the time now.
func (g *BlockGate) open(now time.Time) {
	g.mu.Lock()
	g.unlocked, g.usedAt = true, now
	g.mu.Unlock()
}

// close locks the gate.
func (g *BlockGate) close() {
	g.mu.Lock()
	g.unlocked = false
	g.mu.Unlock()
}
