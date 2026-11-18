package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"lumen/internal/core"
)

func TestBatchEditsRevalidateVisibilityBesideTheirWrites(t *testing.T) {
	for _, action := range []core.BatchAction{core.BatchFavourite, core.BatchUnfavourite, core.BatchAlbum, core.BatchRemoveAlbum, core.BatchTrash} {
		t.Run(string(action), func(t *testing.T) {
			st, ids := viewingFixture(t)
			ctx := t.Context()
			album, err := st.CreateAlbum(ctx, "Batch", core.AlbumManual, core.AlbumRule{}, nil)
			check(t, err)
			_, err = st.SetBlock(ctx, core.BlockAsset, ids[0], "", core.BlockPhotos)
			check(t, err)
			results, err := st.ApplyBatch(ctx, ids, action, album)
			check(t, err)
			if len(results) != 2 || !errors.Is(results[0].Err, ErrNotFound) || results[1].Err != nil {
				t.Fatalf("batch results: %+v", results)
			}
			// The protected photo's state is unchanged even for Trash.
			d, err := st.AssetDetail(ctx, ids[0], true)
			check(t, err)
			if d.Favourite {
				t.Fatal("edited a covered photo")
			}
			var trashedAt sql.NullInt64
			check(t, st.r.QueryRowContext(ctx, "SELECT trashed_at FROM assets WHERE id = ?", ids[0]).Scan(&trashedAt))
			if trashedAt.Valid {
				t.Fatal("trashed a covered photo")
			}
		})
	}
}

func TestBatchTaggingRefusesCoveredAndTrashedPhotos(t *testing.T) {
	st, ids := viewingFixture(t)
	ctx := t.Context()
	_, err := st.SetBlock(ctx, core.BlockAsset, ids[0], "", core.BlockPhotos)
	check(t, err)
	check(t, st.TrashAssets(ctx, []int64{ids[1]}))
	tag, err := st.CreateTag(ctx, "Batch", core.TagManual, nil)
	check(t, err)
	results, err := st.TagAssets(ctx, ids, tag)
	check(t, err)
	for _, result := range results {
		if !errors.Is(result.Err, ErrNotFound) {
			t.Fatalf("protected tag outcome: %+v", result)
		}
	}
	var count int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM asset_tags WHERE tag_id = ?", tag).Scan(&count))
	if count != 0 {
		t.Fatal("tagged protected photos")
	}
	results, err = st.ApplyBatch(ctx, ids, core.BatchRestore, 0)
	check(t, err)
	if !errors.Is(results[0].Err, ErrNotFound) || results[1].Err != nil {
		t.Fatalf("only the trashed photo should restore: %+v", results)
	}
}

func TestCancelledBatchRollsBackEarlierItemsAndReportsNoCommittedSuccesses(t *testing.T) {
	st, ids := viewingFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	results, err := st.batch(ctx, ids, visible, func(tx *sql.Tx, row *BatchResult) error {
		err := setFavourite(ctx, tx, row.ID, true)
		cancel()
		return err
	})
	if !errors.Is(err, context.Canceled) || results != nil {
		t.Fatalf("cancelled part: %+v, %v", results, err)
	}
	for _, id := range ids {
		d, err := st.AssetDetail(t.Context(), id, false)
		check(t, err)
		if d.Favourite {
			t.Fatal("cancelled part committed an earlier item")
		}
	}
}

func TestBatchAlbumOutcomesCaptureActualChangesIncludingRepeatedIDs(t *testing.T) {
	st, ids := viewingFixture(t)
	ctx := t.Context()
	album, err := st.CreateAlbum(ctx, "Membership", core.AlbumManual, core.AlbumRule{}, ids[:1])
	check(t, err)
	for _, action := range []core.BatchAction{core.BatchAlbum, core.BatchRemoveAlbum} {
		rows, err := st.ApplyBatch(ctx, []int64{ids[0], ids[1], ids[1], 99999}, action, album)
		check(t, err)
		want := []bool{action == core.BatchRemoveAlbum, true, false, false}
		for i, row := range rows {
			if row.Changed != want[i] || (i < 3 && row.Err != nil) || (i == 3 && !errors.Is(row.Err, ErrNotFound)) {
				t.Fatalf("%s row %d: %+v, want changed %v", action, i, row, want[i])
			}
		}
	}
}

func TestMembershipWritesRefuseBlockedAlbumsAndRecoverAfterUnblocking(t *testing.T) {
	for _, action := range []core.BatchAction{core.BatchAlbum, core.BatchRemoveAlbum} {
		t.Run(string(action), func(t *testing.T) {
			st, ids := viewingFixture(t)
			ctx := t.Context()
			var members []int64
			if action == core.BatchRemoveAlbum {
				members = ids
			}
			album, err := st.CreateAlbum(ctx, "Private collection", core.AlbumManual, core.AlbumRule{}, members)
			check(t, err)
			block, err := st.SetBlock(ctx, core.BlockAlbum, album, "", core.BlockLabel)
			check(t, err)
			rows, err := st.ApplyBatch(ctx, ids, action, album)
			check(t, err)
			for _, row := range rows {
				if !errors.Is(row.Err, ErrNotFound) || row.Changed {
					t.Errorf("blocked membership outcome = %+v", row)
				}
			}
			if err := st.SetAlbumItem(ctx, ids[0], album, action == core.BatchAlbum); !errors.Is(err, ErrNotFound) {
				t.Errorf("single blocked membership = %v", err)
			}
			var count int
			check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM album_items WHERE album_id = ?", album).Scan(&count))
			if count != len(members) {
				t.Fatalf("blocked album changed: got %d members, want %d", count, len(members))
			}
			check(t, st.RemoveBlock(ctx, block))
			rows, err = st.ApplyBatch(ctx, ids, action, album)
			check(t, err)
			for _, row := range rows {
				if row.Err != nil || !row.Changed {
					t.Fatalf("unblocked retry = %+v", row)
				}
			}
		})
	}
}

func TestCancelledMembershipBatchPublishesNoChangedFlags(t *testing.T) {
	st, ids := viewingFixture(t)
	album, err := st.CreateAlbum(t.Context(), "Cancelled", core.AlbumManual, core.AlbumRule{}, nil)
	check(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rows, err := st.batch(ctx, ids, visible, func(tx *sql.Tx, row *BatchResult) error {
		changed, err := setAlbumItem(ctx, tx, row.ID, album, true)
		row.Changed = changed
		cancel()
		return err
	})
	if !errors.Is(err, context.Canceled) || rows != nil {
		t.Fatalf("rolled-back membership outcomes: %+v, %v", rows, err)
	}
	for _, id := range ids {
		memberships, err := st.AssetAlbums(t.Context(), id)
		check(t, err)
		if len(memberships) != 0 {
			t.Fatalf("rolled-back asset %d memberships: %v", id, memberships)
		}
	}
}
