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
	s.blocked.Lock()
	s.blocked.retryAt = time.Now().Add(-time.Second)
	s.blocked.Unlock()
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatalf("PIN after cooldown: %v", err)
	}
	if _, err := s.Blocks(ctx); err != nil {
		t.Fatalf("list after unlock: %v", err)
	}
	// Left unused (the window reloaded before the screen could relock it).
	s.blocked.Lock()
	s.blocked.usedAt = time.Now().Add(-blockUnlockIdle - time.Second)
	s.blocked.Unlock()
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
