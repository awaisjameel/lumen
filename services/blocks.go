package services

import (
	"context"
	"errors"
	"time"

	"lumen/internal/core"
	"lumen/internal/store"
)

var (
	// ErrBlockLocked protects the private Blocked list after leaving its screen.
	ErrBlockLocked = errors.New("the Blocked screen is locked")
	// ErrPINRateLimited asks the user to wait after repeated guesses.
	ErrPINRateLimited = errors.New("too many incorrect PINs; try again in 30 seconds")
	// ErrBlockUndoExpired means the brief Undo window has passed.
	ErrBlockUndoExpired = errors.New("the block can no longer be undone")
)

const (
	// blockUnlockIdle relocks the Blocked list after this long unused, in case
	// the screen never said it was left (the window was reloaded or closed).
	blockUnlockIdle = 10 * time.Minute
	// pinAttempts wrong PINs in a row wait pinCooldown before the next try.
	pinAttempts = 5
	pinCooldown = 30 * time.Second
	// blockUndoWindow is how long Undo is accepted. The UI offers it for
	// eight seconds; the rest allows for view refresh, WebView scheduling
	// and the round trip after the button is pressed.
	blockUndoWindow = 30 * time.Second
)

// unlockedNow reports whether the list is unlocked, and counts this as a use.
// Call with s.blocked held.
func (s *LibraryService) unlockedNow() bool {
	now := s.now()
	if s.blocked.unlocked && now.Sub(s.blocked.usedAt) > blockUnlockIdle {
		s.blocked.unlocked = false
	}
	if s.blocked.unlocked {
		s.blocked.usedAt = now
	}
	return s.blocked.unlocked
}

// BlockPINConfigured reports whether the first block must establish a PIN.
func (s *LibraryService) BlockPINConfigured(ctx context.Context) (bool, error) {
	return s.store.HasBlockPIN(ctx)
}

// CreateBlockPIN establishes the local PIN once, before the first block.
func (s *LibraryService) CreateBlockPIN(ctx context.Context, pin string) error {
	return s.store.CreateBlockPIN(ctx, pin)
}

// UnlockBlocks unlocks the private list until LockBlocks is called. Repeated
// guesses are delayed in the process; reopening the application relocks it.
func (s *LibraryService) UnlockBlocks(ctx context.Context, pin string) error {
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if s.now().Before(s.blocked.retryAt) {
		return ErrPINRateLimited
	}
	ok, err := s.store.VerifyBlockPIN(ctx, pin)
	if err != nil {
		return err
	}
	if !ok {
		s.blocked.failures++
		if s.blocked.failures >= pinAttempts {
			s.blocked.retryAt = s.now().Add(pinCooldown)
			s.blocked.failures = 0
		}
		return store.ErrInvalidPIN
	}
	s.blocked.unlocked, s.blocked.usedAt = true, s.now()
	s.blocked.failures = 0
	return nil
}

// LockBlocks closes access to the private list as the user leaves the screen.
func (s *LibraryService) LockBlocks() {
	s.blocked.Lock()
	s.blocked.unlocked = false
	s.blocked.Unlock()
}

// Blocks lists the user's visibility rules after PIN unlock.
func (s *LibraryService) Blocks(ctx context.Context) ([]core.Block, error) {
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if !s.unlockedNow() {
		return nil, ErrBlockLocked
	}
	return s.store.Blocks(ctx)
}

// SetBlock adds or changes one visibility rule.
func (s *LibraryService) SetBlock(ctx context.Context, kind core.BlockKind, targetID int64, path string, mode core.BlockMode) (core.BlockChange, error) {
	configured, err := s.store.HasBlockPIN(ctx)
	if err != nil {
		return core.BlockChange{}, err
	}
	if !configured {
		return core.BlockChange{}, ErrBlockLocked
	}
	s.blocked.Lock()
	defer s.blocked.Unlock()
	id, created, err := s.store.SetBlockWithCreated(ctx, kind, targetID, path, mode, s.unlockedNow())
	if errors.Is(err, store.ErrBlockChangeLocked) {
		return core.BlockChange{}, ErrBlockLocked
	}
	if err == nil {
		if created {
			s.blocked.lastID, s.blocked.lastAt = id, s.now()
		} else if id == s.blocked.lastID {
			s.blocked.lastID = 0
		}
		s.blocksChanged()
	}
	return core.BlockChange{ID: id, Created: created}, err
}

// UndoBlock removes the most recent new block during its short Undo window.
func (s *LibraryService) UndoBlock(ctx context.Context, id int64) error {
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if id == 0 || id != s.blocked.lastID || s.now().Sub(s.blocked.lastAt) > blockUndoWindow {
		return ErrBlockUndoExpired
	}
	if err := s.store.RemoveBlock(ctx, id); err != nil {
		return err
	}
	s.blocked.lastID = 0
	s.blocksChanged()
	return nil
}

// RemoveBlock restores the current view of a blocked item.
func (s *LibraryService) RemoveBlock(ctx context.Context, id int64) error {
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if !s.unlockedNow() {
		return ErrBlockLocked
	}
	if err := s.store.RemoveBlock(ctx, id); err != nil {
		return err
	}
	s.blocksChanged()
	return nil
}

// blocksChanged tells every view: a block changes which assets, Things and
// people are shown.
func (s *LibraryService) blocksChanged() {
	if s.ix != nil {
		s.ix.LibraryChanged(core.Changes{Assets: true, Tags: true, People: true})
	}
}
