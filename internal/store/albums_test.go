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
	id, err := st.CreateAlbum(ctx, " Weekend ", core.AlbumManual, core.AlbumRule{}, nil)
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
	// No cover until the photo has a thumbnail to show.
	if albums, err := st.Albums(ctx, 0.02); err != nil || len(albums) != 1 || albums[0].Name != "Weekend" || albums[0].Count != 1 || albums[0].Cover != 0 {
		t.Fatalf("albums = %+v, %v", albums, err)
	}
	check(t, st.SaveThumbnails(ctx, "t", []ThumbResult{{AssetID: asset}}))
	if albums, err := st.Albums(ctx, 0.02); err != nil || albums[0].Cover != asset || albums[0].CoverThumb == 0 {
		t.Fatalf("albums with a thumbnail = %+v, %v", albums, err)
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
	album, err := st.CreateAlbum(ctx, "Trip favourites", core.AlbumSmart, rule, nil)
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
	if _, err := st.CreateAlbum(ctx, " ", core.AlbumManual, core.AlbumRule{}, nil); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("blank name: %v", err)
	}
	if _, err := st.CreateAlbum(ctx, "Bad dates", core.AlbumSmart, core.AlbumRule{From: "2025-02-30"}, nil); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("invalid date: %v", err)
	}
	if _, err := st.CreateAlbum(ctx, "Backwards", core.AlbumSmart, core.AlbumRule{From: "2025-04-01", To: "2025-03-01"}, nil); !errors.Is(err, ErrInvalidAlbum) {
		t.Fatalf("backwards dates: %v", err)
	}
	id, err := st.CreateAlbum(ctx, "Holiday", core.AlbumManual, core.AlbumRule{}, nil)
	check(t, err)
	if _, err := st.CreateAlbum(ctx, "holiday", core.AlbumSmart, core.AlbumRule{}, nil); !errors.Is(err, ErrAlbumExists) {
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
	album, err := st.CreateAlbum(ctx, "Ali", core.AlbumSmart, core.AlbumRule{Person: person}, nil)
	check(t, err)
	if albums, err := st.Albums(ctx, 0.02); err != nil || albums[0].Count != 2 {
		t.Fatalf("person album = %+v, %v", albums, err)
	}
	check(t, st.RejectFaces(ctx, person, []int64{fb}))
	if albums, err := st.Albums(ctx, 0.02); err != nil || albums[0].Count != 1 || albums[0].ID != album {
		t.Fatalf("after correction = %+v, %v", albums, err)
	}
}

func TestSmartAlbumFollowsMergedPerson(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	from, err := st.NameFaces(ctx, []int64{facesOf(t, st, a)[0].ID}, "First")
	check(t, err)
	into, err := st.NameFaces(ctx, []int64{facesOf(t, st, b)[0].ID}, "Second")
	check(t, err)
	id, err := st.CreateAlbum(ctx, "Family", core.AlbumSmart, core.AlbumRule{Person: from}, nil)
	check(t, err)
	check(t, st.MergePeople(ctx, from, into))
	albums, err := st.Albums(ctx, 0.02)
	check(t, err)
	if len(albums) != 1 || albums[0].Rule.Person != into || albums[0].Count != 2 {
		t.Fatalf("merged album = %+v", albums)
	}
	buckets, err := st.DayBuckets(ctx, core.View{Album: id}, 0.02)
	check(t, err)
	if len(buckets) != 1 || buckets[0].Count != 2 {
		t.Fatalf("merged timeline = %+v", buckets)
	}
}

func TestSmartAlbumRetainsEmptyUnnamedPerson(t *testing.T) {
	st, ctx, a, _, _ := peopleFixture(t)
	face := facesOf(t, st, a)[0].ID
	person, err := st.NameFaces(ctx, []int64{face}, "")
	check(t, err)
	_, err = st.CreateAlbum(ctx, "Someone", core.AlbumSmart, core.AlbumRule{Person: person}, nil)
	check(t, err)
	// Moving its last face away normally removes an unnamed person.
	_, err = st.NameFaces(ctx, []int64{face}, "Another person")
	check(t, err)
	p, err := st.Person(ctx, person)
	check(t, err)
	if p.Count != 0 {
		t.Fatalf("empty person = %+v", p)
	}
}

func TestSmartAlbumProtectsTagReference(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	tag, err := st.CreateTag(ctx, "Trip", core.TagManual, nil)
	check(t, err)
	id, err := st.CreateAlbum(ctx, "Trips", core.AlbumSmart, core.AlbumRule{Tag: tag}, nil)
	check(t, err)
	if err := st.DeleteTag(ctx, tag); !errors.Is(err, ErrTagInAlbum) {
		t.Fatalf("deleting referenced tag: %v", err)
	}
	check(t, st.UpdateAlbum(ctx, id, "Trips", core.AlbumRule{}))
	check(t, st.DeleteTag(ctx, tag))
	for _, rule := range []core.AlbumRule{{Tag: tag}, {Person: 999}} {
		if _, err := st.CreateAlbum(ctx, "Invalid", core.AlbumSmart, rule, nil); !errors.Is(err, ErrInvalidAlbum) {
			t.Fatalf("creating dangling rule %+v: %v", rule, err)
		}
		if err := st.UpdateAlbum(ctx, id, "Invalid", rule); !errors.Is(err, ErrInvalidAlbum) {
			t.Fatalf("updating dangling rule %+v: %v", rule, err)
		}
	}
}

func TestVocabularyRetainsTagUsedBySmartAlbum(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	check(t, st.SyncVocabulary(ctx, []VocabLabel{{Name: "Old Thing", Prompts: []string{"old"}}}))
	tag, _, err := st.TagByName(ctx, "Old Thing")
	check(t, err)
	_, err = st.CreateAlbum(ctx, "Saved", core.AlbumSmart, core.AlbumRule{Tag: tag}, nil)
	check(t, err)
	check(t, st.SyncVocabulary(ctx, []VocabLabel{{Name: "New Thing", Prompts: []string{"new"}}}))
	got, kind, err := st.TagByName(ctx, "Old Thing")
	check(t, err)
	if got != tag || kind != core.TagManual {
		t.Fatalf("retired tag = %d, %s", got, kind)
	}
}
