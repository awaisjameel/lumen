package services

import (
	"context"
	"errors"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/signin"
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
func (s *LibraryService) unlockedNow() bool { return s.gate.unlockedAt(s.now()) }

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
	s.gate.open(s.now())
	s.blocked.failures = 0
	return nil
}

// resetReason is what the Windows sign-in prompt says it is for.
const resetReason = "Sign in to choose a new PIN for Lumen's Blocked screen."

// ResetBlockPIN replaces a forgotten PIN after the user signs in to Windows
// (Windows Hello, or the PC's password). Blocks are kept, and the list
// stays locked until the new PIN unlocks it.
//
// The PIN's format is checked before Windows is asked, so nobody signs in for
// a PIN that would be refused. The attempt counter and cooldown are left as
// they are: a reset is no way round them, and a failed sign-in is not a wrong
// PIN, so it adds nothing to them either. The prompt can take as long as the
// user does, so the lock on the Blocked state is not held while it is open.
func (s *LibraryService) ResetBlockPIN(ctx context.Context, pin string) error {
	if !store.ValidPIN(pin) {
		return store.ErrInvalidPIN
	}
	configured, err := s.store.HasBlockPIN(ctx)
	if err != nil {
		return err
	}
	if !configured {
		return store.ErrNotFound
	}
	verify := s.verify
	if verify == nil {
		verify = signin.Verify
	}
	if err := verify(ctx, lumenWindow(), resetReason); err != nil {
		return err
	}
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if err := s.store.ReplaceBlockPIN(ctx, pin); err != nil {
		return err
	}
	s.gate.close()
	return nil
}

// lumenWindow is the native handle of Lumen's window, which the Windows
// sign-in prompt belongs to so that it opens in front of it. It is the window
// Wails reports as current (the one the user just pressed the button in),
// or else the first window there is, or 0 when there is none, as in tests:
// the prompt then has no owner.
func lumenWindow() uintptr {
	app := application.Get()
	if app == nil {
		return 0
	}
	w := app.Window.Current()
	if w == nil {
		if all := app.Window.GetAll(); len(all) > 0 {
			w = all[0]
		}
	}
	if w == nil {
		return 0
	}
	return uintptr(w.NativeWindow())
}

// LockBlocks closes access to the private list as the user leaves the screen.
func (s *LibraryService) LockBlocks() {
	s.blocked.Lock()
	s.gate.close()
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
		s.viewsChanged()
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
	s.viewsChanged()
	return nil
}

// SetBlockMode changes what the block blockID hides: its label only, or its
// photos too. It needs the PIN unlock, as RemoveBlock does, and takes the
// block's own id, which is not always what SetBlock's target means.
func (s *LibraryService) SetBlockMode(ctx context.Context, blockID int64, mode core.BlockMode) error {
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if !s.unlockedNow() {
		return ErrBlockLocked
	}
	if err := s.store.SetBlockMode(ctx, blockID, mode); err != nil {
		return err
	}
	s.viewsChanged()
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
	s.viewsChanged()
	return nil
}

// BlockedItems lists the photos the block blockID covers, for the Blocked
// screen's browser; it needs the PIN unlock. The lock is held while the list
// is read, so nothing is handed out after the screen has locked.
func (s *LibraryService) BlockedItems(ctx context.Context, blockID int64) ([]core.CoveredItem, error) {
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if !s.unlockedNow() {
		return nil, ErrBlockLocked
	}
	return s.store.BlockCoverage(ctx, blockID)
}

// TrashCovered moves photos a block covers to the Trash, from the Blocked
// screen's browser; it needs the PIN unlock. Like TrashAssets, it leaves the
// files where they are.
func (s *LibraryService) TrashCovered(ctx context.Context, ids []int64) error {
	s.blocked.Lock()
	defer s.blocked.Unlock()
	if !s.unlockedNow() {
		return ErrBlockLocked
	}
	if err := s.store.TrashAssets(ctx, ids); err != nil {
		return err
	}
	s.viewsChanged()
	return nil
}
