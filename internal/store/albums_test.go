package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"lumen/internal/core"
)

func TestManualAlbumsKeepAssetIdentityAndRespectVisibility(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\one.jpg`, "one")})))
	var asset, file int64
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets`).Scan(&asset))
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM files`).Scan(&file))
	id, err := st.CreateAlbum(ctx, " Weekend ", "manual", core.AlbumRule{})
	check(t, err)
	check(t, st.SetAlbumItem(ctx, id, asset, true))
	check(t, st.SetAlbumItem(ctx, id, asset, true)) // idempotent
	if ids, err := st.AssetAlbums(ctx, asset); err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("membership = %v, %v", ids, err)
	}
	view := core.View{Album: id}
	if buckets, err := st.DayBuckets(ctx, view, 0.02); err != nil || len(buckets) != 1 || buckets[0].Count != 1 {
		t.Fatalf("album timeline = %+v, %v", buckets, err)
	}
	if albums, err := st.Albums(ctx, 0.02); err != nil || len(albums) != 1 || albums[0].Name != "Weekend" || albums[0].Count != 1 || albums[0].Cover != asset {
		t.Fatalf("albums = %+v, %v", albums, err)
	}
	check(t, st.RemoveFiles(ctx, []int64{file}))
	if albums, err := st.Albums(ctx, 0.02); err != nil || albums[0].Count != 0 {
		t.Fatalf("missing asset still shown: %+v, %v", albums, err)
	}
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\moved.jpg`, "one")})))
	if albums, err := st.Albums(ctx, 0.02); err != nil || albums[0].Count != 1 {
		t.Fatalf("moved asset lost membership: %+v, %v", albums, err)
	}
	check(t, st.DeleteAlbum(ctx, id))
	if ids, err := st.AssetAlbums(ctx, asset); err != nil || len(ids) != 0 {
		t.Fatalf("membership after delete = %v, %v", ids, err)
	}
	if _, err := st.DayBuckets(ctx, view, 0.02); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted album view: %v", err)
	}
}

func TestSmartAlbumReevaluatesCurrentMetadataAndTags(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	photo := item(src.ID, `C:\Photos\a.jpg`, "photo")
	photo.TakenAt = time.Date(2025, 3, 14, 12, 0, 0, 0, time.UTC).Unix()
	video := item(src.ID, `C:\Photos\clip.mp4`, "video")
	video.Kind, video.TakenAt = core.KindVideo, photo.TakenAt
	check(t, errOf(st.Ingest(ctx, []IngestItem{photo, video})))
	var id int64
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets WHERE kind = 'photo'`).Scan(&id))
	tag, err := st.CreateTag(ctx, "Trip", core.TagManual, nil)
	check(t, err)
	rule := core.AlbumRule{Kind: core.KindPhoto, Favourites: true, From: "2025-03-14", To: "2025-03-14", Tag: tag}
	album, err := st.CreateAlbum(ctx, "Trip favourites", "smart", rule)
	check(t, err)
	assertCount := func(want int) {
		t.Helper()
		albums, err := st.Albums(ctx, 0.3)
		if err != nil || len(albums) != 1 || albums[0].Count != want {
			t.Fatalf("albums = %+v, %v; want count %d", albums, err, want)
		}
		buckets, err := st.DayBuckets(ctx, core.View{Album: album}, 0.3)
		if err != nil || len(buckets) != want {
			t.Fatalf("timeline = %+v, %v; want %d", buckets, err, want)
		}
		if want > 0 {
			items, err := st.DayItems(ctx, "2025-03-14", "2025-03-14", core.View{Album: album}, 0.3)
			if err != nil || len(items) != 1 || len(items[0].Items) != 1 || items[0].Items[0].ID != id {
				t.Fatalf("day items = %+v, %v", items, err)
			}
		}
	}
	assertCount(0)
	check(t, st.SetFavourite(ctx, id, true))
	check(t, st.TagAsset(ctx, id, tag))
	assertCount(1)
	check(t, st.UntagAsset(ctx, id, tag))
	assertCount(0)
	check(t, st.UpdateAlbum(ctx, album, "All March", core.AlbumRule{From: "2025-03-01", To: "2025-03-31"}))
	albums, err := st.Albums(ctx, 0.02)
	if err != nil || albums[0].Name != "All March" || albums[0].Count != 2 {
		t.Fatalf("updated rule = %+v, %v", albums, err)
	}
}

func TestAlbumValidation(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	if _, err := st.CreateAlbum(ctx, " ", "manual", core.AlbumRule{}); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("blank name: %v", err)
	}
	if _, err := st.CreateAlbum(ctx, "Bad dates", "smart", core.AlbumRule{From: "2025-02-30"}); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("invalid date: %v", err)
	}
	if _, err := st.CreateAlbum(ctx, "Backwards", "smart", core.AlbumRule{From: "2025-04-01", To: "2025-03-01"}); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("backwards dates: %v", err)
	}
	id, err := st.CreateAlbum(ctx, "Holiday", "manual", core.AlbumRule{})
	check(t, err)
	if _, err := st.CreateAlbum(ctx, "holiday", "smart", core.AlbumRule{}); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("duplicate name: %v", err)
	}
	if err := st.SetAlbumItem(ctx, id, 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing asset: %v", err)
	}
	if err := st.UpdateAlbum(ctx, 999, "missing", core.AlbumRule{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing album: %v", err)
	}
}

func TestSmartAlbumFollowsPersonCorrections(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	fa, fb := facesOf(t, st, a)[0].ID, facesOf(t, st, b)[0].ID
	person, err := st.NameFaces(ctx, []int64{fa, fb}, "Ali")
	check(t, err)
	album, err := st.CreateAlbum(ctx, "Ali", "smart", core.AlbumRule{Person: person})
	check(t, err)
	if albums, err := st.Albums(ctx, 0.02); err != nil || albums[0].Count != 2 {
		t.Fatalf("person album = %+v, %v", albums, err)
	}
	check(t, st.RejectFaces(ctx, person, []int64{fb}))
	if albums, err := st.Albums(ctx, 0.02); err != nil || albums[0].Count != 1 || albums[0].ID != album {
		t.Fatalf("after correction = %+v, %v", albums, err)
	}
}
