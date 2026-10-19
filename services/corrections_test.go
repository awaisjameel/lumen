package services

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/store"
)

func TestCorrectionFailuresCarryTypedCodesAndTheServiceReturnsOriginals(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\a.jpg`, Size: 1, Hash: []byte("a"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1}})
	if err != nil {
		t.Fatal(err)
	}
	s := NewLibraryService(st, nil, nil, nil)
	for _, tc := range []struct {
		run  func() error
		code ErrorCode
	}{
		{func() error { return s.SetTakenAt(ctx, nil, 0) }, CodeCorrectionSelection},
		{func() error { return s.SetTakenAt(ctx, []int64{1}, time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).Unix()) }, CodeCorrectionDate},
		{func() error { return s.SetLocation(ctx, []int64{1}, 91, 0) }, CodeCorrectionLocation},
		{func() error { _, err := s.Timeline(ctx, core.View{Filter: "invalid"}); return err }, CodeInvalidFilter},
	} {
		if got := errorCodeOf(tc.run()); got != tc.code {
			t.Fatalf("code %s, want %s", got, tc.code)
		}
	}
	if err := s.SetTakenAt(ctx, []int64{1}, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLocation(ctx, []int64{1}, 51.5074, -0.1278); err != nil {
		t.Fatal(err)
	}
	d, err := s.Asset(ctx, 1)
	if err != nil || d.DateFrom != core.DateFromUser || d.OriginalDate == nil || d.OriginalDate.TakenAt != 1 || d.OriginalLocation == nil {
		t.Fatalf("detail: %+v, %v", d, err)
	}
	if err := s.RestoreTakenAt(ctx, []int64{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreLocation(ctx, []int64{1}); err != nil {
		t.Fatal(err)
	}
	c, err := s.Counts(ctx)
	if err != nil || c.Photos != 1 {
		t.Fatalf("counts: %+v, %v", c, err)
	}
}

func TestUndoUnblockUsesItsServerReceiptAndExpiresAfterTheUndoWindow(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\a.jpg`, Size: 1, Hash: []byte("a"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1}})
	if err != nil {
		t.Fatal(err)
	}
	s := NewLibraryService(st, nil, nil, nil)
	now := time.Now()
	s.now = func() time.Time { return now }
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	// Events use event-tag anchors, so reconstructing a block with the
	// returned target as a live event ID would lose the original meaning.
	events, err := s.Events(ctx)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %+v, %v", events, err)
	}
	block, err := s.SetBlock(ctx, core.BlockEvent, events[0].ID, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Unblock(ctx, block.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.LockBlocks()
	if err := s.UndoUnblock(ctx, receipt+1); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("wrong receipt accepted: %v", err)
	}
	if err := s.UndoUnblock(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	n, err := st.CountVisible(ctx)
	if err != nil || n != 0 {
		t.Fatalf("re-block missed anchor: %d, %v", n, err)
	}
	if err := s.UndoUnblock(ctx, receipt); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("receipt reused: %v", err)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	blocks, err := s.Blocks(ctx)
	if err != nil || len(blocks) != 1 {
		t.Fatalf("blocks: %+v, %v", blocks, err)
	}
	nextReceipt, err := s.Unblock(ctx, blocks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UndoUnblock(ctx, receipt); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("stale receipt accepted after row ID reuse: %v", err)
	}
	now = now.Add(blockUndoWindow + time.Second)
	if err := s.UndoUnblock(ctx, nextReceipt); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("expired receipt accepted: %v", err)
	}
}

func TestUndoUnblockDoesNotTakeAnEventTagAlongWithAMemberThatMovedAway(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Ingest(ctx, []store.IngestItem{
		{SourceID: src.ID, Path: `C:\Photos\a.jpg`, Size: 1, Hash: []byte("a"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1},
		{SourceID: src.ID, Path: `C:\Photos\b.jpg`, Size: 1, Hash: []byte("b"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := NewLibraryService(st, nil, nil, nil)
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx)
	if err != nil || len(events) != 1 {
		t.Fatalf("events: %+v, %v", events, err)
	}
	block, err := s.SetBlock(ctx, core.BlockEvent, events[0].ID, "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := s.Unblock(ctx, block.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetTakenAt(ctx, []int64{1}, 86401); err != nil {
		t.Fatal(err)
	}
	if err := s.UndoUnblock(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	items, err := st.GridItems(ctx, []int64{1, 2})
	if err != nil || len(items) != 1 || items[0].ID != 1 {
		t.Fatalf("moved member kept the old event's block: %+v, %v", items, err)
	}
}
