package services

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
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
	s := NewLibraryService(st, nil)
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
	if visible, err := st.AssetVisible(ctx, 1); err != nil || !visible {
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
	s := NewLibraryService(st, nil)
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
	s := NewLibraryService(st, nil)
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
	s := NewLibraryService(st, nil)
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
