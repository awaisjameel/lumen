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
	results, err := st.batch(ctx, ids, visible, func(tx *sql.Tx, id int64) error {
		err := setFavourite(ctx, tx, id, true)
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
