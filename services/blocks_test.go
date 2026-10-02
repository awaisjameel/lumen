package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/signin"
	"lumen/internal/store"
)

func TestBlockedListRequiresPINAndUndoIsSingleUse(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\a.jpg`, Size: 1,
		Hash: []byte("a"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := st.GridItems(ctx, []int64{1})
	if err != nil || len(items) != 1 {
		t.Fatalf("asset: %+v, %v", items, err)
	}
	s := NewLibraryService(st, nil, nil)
	if _, err := s.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("block before PIN: %v", err)
	}
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	id, err := s.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Blocks(ctx); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("private list without PIN: %v", err)
	}
	// An asset is always blocked with its photo: asking again changes
	// nothing, so it needs no PIN (a locked mode change is refused below).
	if change, err := s.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockLabel); err != nil || change.Created || change.ID != id.ID {
		t.Fatalf("unchanged block without PIN: %+v, %v", change, err)
	}
	if err := s.UnlockBlocks(ctx, "000000"); !errors.Is(err, store.ErrInvalidPIN) {
		t.Fatalf("wrong PIN: %v", err)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if blocks, err := s.Blocks(ctx); err != nil || len(blocks) != 1 {
		t.Fatalf("unlocked list: %+v, %v", blocks, err)
	}
	if change, err := s.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos); err != nil || change.Created {
		t.Fatalf("unlocked existing block update: %+v, %v", change, err)
	}
	if err := s.UndoBlock(ctx, id.ID); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("undo after rule update: %v", err)
	}
	if err := s.RemoveBlock(ctx, id.ID); err != nil {
		t.Fatal(err)
	}
	id, err = s.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := st.CreateTag(ctx, "Private", core.TagManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.TagAsset(ctx, 1, tag); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetBlock(ctx, core.BlockTag, tag, "", core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	s.LockBlocks()
	if _, err := s.SetBlock(ctx, core.BlockTag, tag, "", core.BlockLabel); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("locked photo block downgrade: %v", err)
	}
	if _, err := s.Blocks(ctx); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("list after lock: %v", err)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	blocks, err := s.Blocks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range blocks {
		if b.Kind == core.BlockTag {
			if b.Mode != core.BlockPhotos {
				t.Fatal("locked downgrade changed the stored mode")
			}
			if err := s.RemoveBlock(ctx, b.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.RemoveBlock(ctx, id.ID); err != nil {
		t.Fatal(err)
	}
	id, err = s.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	s.LockBlocks()
	if err := s.UndoBlock(ctx, id.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UndoBlock(ctx, id.ID); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("second undo: %v", err)
	}
	if visible, err := st.AssetVisible(ctx, 1, false); err != nil || !visible {
		t.Fatalf("asset after undo: %v, %v", visible, err)
	}
}

func TestBlockPINRateLimit(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewLibraryService(st, nil, nil)
	now := time.Now()
	s.now = func() time.Time { return now }
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := s.UnlockBlocks(ctx, "000000"); !errors.Is(err, store.ErrInvalidPIN) {
			t.Fatalf("wrong PIN: %v", err)
		}
	}
	if err := s.UnlockBlocks(ctx, "123456"); !errors.Is(err, ErrPINRateLimited) {
		t.Fatalf("PIN during cooldown: %v", err)
	}
	now = now.Add(pinCooldown)
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatalf("PIN after cooldown: %v", err)
	}
	if _, err := s.Blocks(ctx); err != nil {
		t.Fatalf("list after unlock: %v", err)
	}
	for range 2 {
		now = now.Add(blockUnlockIdle - time.Second)
		if _, err := s.Blocks(ctx); err != nil {
			t.Fatalf("list use did not refresh the idle timeout: %v", err)
		}
	}
	// Left unused (the window reloaded before the screen could relock it).
	now = now.Add(blockUnlockIdle + time.Second)
	if _, err := s.Blocks(ctx); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("list after the idle timeout: %v", err)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	s.LockBlocks()
	if _, err := s.Blocks(ctx); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("list after relock: %v", err)
	}
}

func TestConcurrentPINGuessesShareOneCooldown(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewLibraryService(st, nil, nil)
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	const guesses = pinAttempts + 3
	results := make(chan error, guesses)
	for range guesses {
		go func() { results <- s.UnlockBlocks(ctx, "wrong") }()
	}
	invalid, limited := 0, 0
	for range guesses {
		switch err := <-results; {
		case errors.Is(err, store.ErrInvalidPIN):
			invalid++
		case errors.Is(err, ErrPINRateLimited):
			limited++
		default:
			t.Errorf("unexpected PIN failure: %v", err)
		}
	}
	if invalid != pinAttempts || limited != guesses-pinAttempts {
		t.Fatalf("concurrent guesses: %d invalid, %d limited", invalid, limited)
	}
}

func TestUndoExpiresAndOnlyRemovesTheLatestNewBlock(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewLibraryService(st, nil, nil)
	now := time.Now()
	s.now = func() time.Time { return now }
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	var changes []core.BlockChange
	for _, name := range []string{"First", "Second"} {
		id, err := st.CreateTag(ctx, name, core.TagManual, nil)
		if err != nil {
			t.Fatal(err)
		}
		change, err := s.SetBlock(ctx, core.BlockTag, id, "", core.BlockLabel)
		if err != nil {
			t.Fatal(err)
		}
		changes = append(changes, change)
	}
	if err := s.UndoBlock(ctx, changes[0].ID); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("undo earlier block: %v", err)
	}
	now = now.Add(blockUndoWindow + time.Nanosecond)
	if err := s.UndoBlock(ctx, changes[1].ID); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("undo expired block: %v", err)
	}
	if blocks, err := st.Blocks(ctx); err != nil || len(blocks) != 2 {
		t.Fatalf("expired undo changed blocks: %+v, %v", blocks, err)
	}
}

// signInStub stands for the Windows sign-in: it answers with outcome and
// counts its calls.
type signInStub struct {
	outcome error
	calls   int
}

// resetFixture is a library with one photo, a PIN and a photo block, and a
// service whose Windows sign-in is the returned stub, so a test never shows
// a real prompt.
func resetFixture(t *testing.T) (s *LibraryService, st *store.Store, stub *signInStub) {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\a.jpg`, Size: 1,
		Hash: []byte("a"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1}}); err != nil {
		t.Fatal(err)
	}
	s = NewLibraryService(st, nil, nil)
	stub = new(signInStub)
	s.verify = func(_ context.Context, owner uintptr, reason string) error {
		stub.calls++
		if reason == "" {
			t.Error("the sign-in prompt was given no reason")
		}
		if owner != 0 {
			t.Errorf("owner window %d without an application", owner)
		}
		return stub.outcome
	}
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetBlock(ctx, core.BlockAsset, 1, "", core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	return s, st, stub
}

func TestResetBlockPINReplacesThePINAfterAWindowsSignIn(t *testing.T) {
	ctx := t.Context()
	s, _, stub := resetFixture(t)
	if err := s.ResetBlockPIN(ctx, "654321"); err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("sign-in asked %d times, want once", stub.calls)
	}
	if err := s.UnlockBlocks(ctx, "123456"); !errors.Is(err, store.ErrInvalidPIN) {
		t.Fatalf("the forgotten PIN after the reset: %v", err)
	}
	if err := s.UnlockBlocks(ctx, "654321"); err != nil {
		t.Fatalf("the new PIN after the reset: %v", err)
	}
}

func TestResetBlockPINKeepsTheBlocksAndLeavesTheListLocked(t *testing.T) {
	ctx := t.Context()
	s, st, _ := resetFixture(t)
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	before, err := s.Blocks(ctx)
	if err != nil || len(before) != 1 {
		t.Fatalf("blocks before: %+v, %v", before, err)
	}
	if err := s.ResetBlockPIN(ctx, "654321"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Blocks(ctx); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("the list after a reset: %v, want it locked", err)
	}
	if visible, err := st.AssetVisible(ctx, 1, false); err != nil || visible {
		t.Fatalf("the blocked photo after a reset: visible %v, %v", visible, err)
	}
	if err := s.UnlockBlocks(ctx, "654321"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Blocks(ctx)
	if err != nil || len(after) != 1 || after[0] != before[0] {
		t.Fatalf("blocks after: %+v, %v; want %+v", after, err, before)
	}
}

func TestResetBlockPINChangesNothingWhenWindowsDoesNotConfirmTheUser(t *testing.T) {
	for name, failure := range map[string]error{
		"the user cancelled or failed": signin.ErrNotVerified,
		"there is no sign-in method":   signin.ErrUnavailable,
		"the request was cancelled":    context.Canceled,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			s, _, stub := resetFixture(t)
			if err := s.UnlockBlocks(ctx, "123456"); err != nil {
				t.Fatal(err)
			}
			stub.outcome = failure
			if err := s.ResetBlockPIN(ctx, "654321"); !errors.Is(err, failure) {
				t.Fatalf("reset: %v, want %v", err, failure)
			}
			if _, err := s.Blocks(ctx); err != nil {
				t.Fatalf("a refused reset locked the list: %v", err)
			}
			s.LockBlocks()
			if err := s.UnlockBlocks(ctx, "654321"); !errors.Is(err, store.ErrInvalidPIN) {
				t.Fatalf("the new PIN after a refused reset: %v", err)
			}
			if err := s.UnlockBlocks(ctx, "123456"); err != nil {
				t.Fatalf("the old PIN after a refused reset: %v", err)
			}
		})
	}
}

func TestResetBlockPINDoesNotBotherWindowsWithAPINItWouldRefuse(t *testing.T) {
	ctx := t.Context()
	s, _, stub := resetFixture(t)
	for _, pin := range []string{"", "12345", "1234567890123", "12345a"} {
		if err := s.ResetBlockPIN(ctx, pin); !errors.Is(err, store.ErrInvalidPIN) {
			t.Fatalf("reset to %q: %v", pin, err)
		}
	}
	if stub.calls != 0 {
		t.Fatalf("the user was asked to sign in %d times for PINs that were refused", stub.calls)
	}
}

func TestResetBlockPINWithNoPINYetAsksNothingAndCreatesNone(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewLibraryService(st, nil, nil)
	asked := false
	s.verify = func(context.Context, uintptr, string) error { asked = true; return nil }
	if err := s.ResetBlockPIN(ctx, "654321"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reset with no PIN: %v", err)
	}
	if asked {
		t.Fatal("the user was asked to sign in with no PIN to reset")
	}
	if configured, err := s.BlockPINConfigured(ctx); err != nil || configured {
		t.Fatalf("a reset created a PIN: %v, %v", configured, err)
	}
}

func TestResetBlockPINNeitherClearsNorBypassesTheRateLimit(t *testing.T) {
	ctx := t.Context()
	s, _, _ := resetFixture(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	for range pinAttempts {
		if err := s.UnlockBlocks(ctx, "000000"); !errors.Is(err, store.ErrInvalidPIN) {
			t.Fatalf("wrong PIN: %v", err)
		}
	}
	if err := s.ResetBlockPIN(ctx, "654321"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlockBlocks(ctx, "654321"); !errors.Is(err, ErrPINRateLimited) {
		t.Fatalf("the new PIN during the cooldown: %v, want the limit to stand", err)
	}
	now = now.Add(pinCooldown)
	if err := s.UnlockBlocks(ctx, "654321"); err != nil {
		t.Fatalf("the new PIN after the cooldown: %v", err)
	}
}

func TestResetBlockPINKeepsCountingWrongPINsWhereTheyWere(t *testing.T) {
	ctx := t.Context()
	s, _, _ := resetFixture(t)
	for range pinAttempts - 2 {
		if err := s.UnlockBlocks(ctx, "000000"); !errors.Is(err, store.ErrInvalidPIN) {
			t.Fatalf("wrong PIN: %v", err)
		}
	}
	if err := s.ResetBlockPIN(ctx, "654321"); err != nil {
		t.Fatal(err)
	}
	// Two more wrong PINs make five: the reset did not give a fresh count.
	for range 2 {
		if err := s.UnlockBlocks(ctx, "000000"); !errors.Is(err, store.ErrInvalidPIN) {
			t.Fatalf("wrong PIN: %v", err)
		}
	}
	if err := s.UnlockBlocks(ctx, "654321"); !errors.Is(err, ErrPINRateLimited) {
		t.Fatalf("the new PIN after five wrong ones: %v", err)
	}
}

func TestAFailedWindowsSignInIsNotAWrongPIN(t *testing.T) {
	ctx := t.Context()
	s, _, stub := resetFixture(t)
	for range pinAttempts - 2 {
		if err := s.UnlockBlocks(ctx, "000000"); !errors.Is(err, store.ErrInvalidPIN) {
			t.Fatalf("wrong PIN: %v", err)
		}
	}
	stub.outcome = signin.ErrNotVerified
	for range pinAttempts {
		if err := s.ResetBlockPIN(ctx, "654321"); !errors.Is(err, signin.ErrNotVerified) {
			t.Fatalf("reset: %v", err)
		}
	}
	// That is one wrong PIN short of the limit. Were each failed sign-in
	// counted, this one would start the cooldown and the right PIN would wait.
	if err := s.UnlockBlocks(ctx, "000000"); !errors.Is(err, store.ErrInvalidPIN) {
		t.Fatalf("the fourth wrong PIN: %v", err)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatalf("the right PIN after failed sign-ins: %v", err)
	}
}

func TestSignInFailuresHaveTheirOwnCodes(t *testing.T) {
	for err, want := range map[error]ErrorCode{
		signin.ErrNotVerified: CodeSigninFailed,
		signin.ErrUnavailable: CodeNoSignin,
	} {
		// The way the package wraps them: with the system's own error.
		wrapped := fmt.Errorf("check the password: %w", errors.Join(err, errors.New("system detail")))
		var failure Failure
		if got := MarshalError(wrapped); json.Unmarshal(got, &failure) != nil || failure.Code != want {
			t.Errorf("%v: code %q, want %q", err, failure.Code, want)
		}
	}
}
