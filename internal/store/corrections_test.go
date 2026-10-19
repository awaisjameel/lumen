package store

import (
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"lumen/internal/core"
)

func correctionAssets(t *testing.T) (st *Store, ids []int64) {
	t.Helper()
	st = openTest(t)
	src, err := st.AddSource(t.Context(), `C:\Photos`)
	check(t, err)
	a, b := item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.mp4`, "b")
	b.Kind = core.KindVideo
	check(t, errOf(st.Ingest(t.Context(), []IngestItem{a, b})))
	ids, err = queryAll(t.Context(), st.r, scanID, "SELECT id FROM assets ORDER BY id")
	check(t, err)
	return st, ids
}

func TestCorrectionsSurviveMetadataRerunsAndRestoreTheLatestOriginal(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC).Unix()
	lat, lon := 31.5546, 74.3572
	check(t, st.SaveMetadata(ctx, "first", []MetadataResult{{AssetID: ids[0], TakenAt: base, Lat: &lat, Lon: &lon}}))
	check(t, st.SetTakenAt(ctx, ids, base+86400))
	check(t, st.SetLocation(ctx, ids, 51.5074, -0.1278))
	// A second edit must retain metadata, rather than replacing it with the
	// previous correction. Epoch zero is a valid user date.
	check(t, st.SetTakenAt(ctx, ids, 0))
	check(t, st.SetLocation(ctx, ids, 48.8566, 2.3522))
	lat, lon = 33.6938, 73.0652
	check(t, st.SaveMetadata(ctx, "upgraded", []MetadataResult{
		{AssetID: ids[0], TakenAt: base + 3600, Lat: &lat, Lon: &lon},
		{AssetID: ids[1], TakenAt: base + 7200},
	}))
	for _, id := range ids {
		d, err := st.AssetDetail(ctx, id, false)
		check(t, err)
		if d.DateFrom != core.DateFromUser || d.TakenAt != 0 || d.OriginalDate == nil || d.OriginalDate.From != core.DateFromEXIF || d.OriginalLocation == nil || d.Lat == nil || *d.Lat != 48.8566 {
			t.Fatalf("correction overwritten: %+v", d)
		}
	}
	check(t, st.RestoreTakenAt(ctx, ids))
	check(t, st.RestoreLocation(ctx, ids))
	for i, id := range ids {
		d, err := st.AssetDetail(ctx, id, false)
		check(t, err)
		if d.TakenAt != base+int64(i+1)*3600 || d.DateFrom != core.DateFromEXIF || d.OriginalDate != nil || d.OriginalLocation != nil {
			t.Fatalf("wrong restored metadata: %+v", d)
		}
		if i == 0 && (d.Lat == nil || *d.Lat != lat) || i == 1 && (d.Lat != nil || d.Lon != nil || d.PlaceID != 0) {
			t.Fatalf("wrong restored GPS: %+v", d)
		}
	}
}

func TestCorrectedDatesMoveKeptAndCountedDaysAndInvalidateEventsAndSelections(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC).Unix()
	check(t, st.SaveMetadata(ctx, "first", []MetadataResult{{AssetID: ids[0], TakenAt: base}, {AssetID: ids[1], TakenAt: base + 60}}))
	before, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(before) != 1 {
		t.Fatalf("fixture events: %+v", before)
	}
	sel, err := st.Selection(ctx, core.View{}, nil, false)
	check(t, err)
	check(t, st.SetTakenAt(ctx, ids[:1], base+86400))
	if !errors.Is(st.ValidateSelection(ctx, sel), ErrSelectionChanged) {
		t.Fatal("date edit retained stale selection")
	}
	for _, filter := range []core.Filter{core.FilterAll, core.FilterPhotos, core.FilterVideos} {
		view := core.View{Filter: filter}
		kept, err := st.DayBuckets(ctx, view, core.TagConfidenceDefault)
		check(t, err)
		counted, err := st.dayBucketsCounted(ctx, view, core.TagConfidenceDefault)
		check(t, err)
		if !slices.Equal(kept, counted) {
			t.Fatalf("filter %q: kept %+v, counted %+v", filter, kept, counted)
		}
	}
	pages, err := st.DayItems(ctx, "2026-01-01", "2026-01-02", core.View{Filter: core.FilterPhotos}, core.TagConfidenceDefault)
	check(t, err)
	if len(pages) != 1 || pages[0].Day != "2026-01-02" || pages[0].Items[0].ID != ids[0] {
		t.Fatalf("wrong page: %+v", pages)
	}
	after, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(after) != 2 {
		t.Fatalf("events did not regroup: %+v", after)
	}
	check(t, st.RestoreTakenAt(ctx, ids))
	after, err = st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(after) != 1 {
		t.Fatalf("restoring did not regroup: %+v", after)
	}
}

func TestCorrectedPlacesApplyBlocksAtomicallyAndCanBeRestored(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	check(t, st.SetLocation(ctx, ids[:1], 51.5074, -0.1278))
	d, err := st.AssetDetail(ctx, ids[0], false)
	check(t, err)
	place := d.PlaceID
	check(t, st.RestoreLocation(ctx, ids[:1]))
	_, err = st.SetBlock(ctx, core.BlockPlace, place, "", core.BlockPhotos)
	check(t, err)
	before, err := st.VisibilityRevision(ctx)
	check(t, err)
	check(t, st.SetLocation(ctx, ids, 51.5074, -0.1278))
	after, err := st.VisibilityRevision(ctx)
	check(t, err)
	n, err := st.CountVisible(ctx)
	check(t, err)
	if after <= before || n != 0 {
		t.Fatalf("block missed correction: revisions %d/%d, visible %d", before, after, n)
	}
	check(t, st.read(ctx, func(tx *sql.Tx) error { compareKeptCounts(ctx, t, tx); return nil }))
	check(t, st.RestoreLocation(ctx, ids))
	n, err = st.CountVisible(ctx)
	check(t, err)
	if n != 2 {
		t.Fatalf("cannot undo a correction into a block: %d", n)
	}
}

func TestCorrectionsValidateTheWholeSelectionBeforeWriting(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	for _, call := range []func() error{
		func() error { return st.SetTakenAt(ctx, nil, 0) },
		func() error { return st.RestoreTakenAt(ctx, nil) },
		func() error { return st.SetLocation(ctx, nil, 0, 0) },
		func() error { return st.RestoreLocation(ctx, nil) },
	} {
		if !errors.Is(call(), ErrCorrectionSelection) {
			t.Fatal("empty selection accepted")
		}
	}
	for _, value := range []int64{math.MaxInt64, math.MinInt64, time.Date(1799, 12, 31, 0, 0, 0, 0, time.UTC).Unix(), time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).Unix()} {
		if !errors.Is(st.SetTakenAt(ctx, ids, value), ErrCorrectionDate) {
			t.Fatalf("invalid date accepted: %d", value)
		}
	}
	for _, point := range [][2]float64{{math.NaN(), 0}, {0, math.Inf(1)}, {-91, 0}, {0, 181}} {
		if !errors.Is(st.SetLocation(ctx, ids, point[0], point[1]), ErrCorrectionLocation) {
			t.Fatalf("invalid location accepted: %v", point)
		}
	}
	if !errors.Is(st.SetTakenAt(ctx, []int64{ids[0], 999}, 0), ErrNotFound) {
		t.Fatal("missing ID accepted")
	}
	d, err := st.AssetDetail(ctx, ids[0], false)
	check(t, err)
	if d.OriginalDate != nil {
		t.Fatal("partial date write")
	}
}

func TestCorrectedDateKeepsTheScannerGuessAndMetadataFallbackCurrent(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	check(t, st.SetTakenAt(ctx, ids[:1], 0))
	source, err := st.ListSources(ctx)
	check(t, err)
	duplicate := item(source[0].ID, `C:\Photos\2020-01-02.jpg`, "a")
	duplicate.TakenAt, duplicate.DateFrom = 1577923200, core.DateFromFilename
	check(t, errOf(st.Ingest(ctx, []IngestItem{duplicate})))
	d, err := st.AssetDetail(ctx, ids[0], false)
	check(t, err)
	if d.TakenAt != 0 || d.OriginalDate == nil || d.OriginalDate.From != core.DateFromFilename || d.OriginalDate.TakenAt != duplicate.TakenAt {
		t.Fatalf("rescan lost correction or guess: %+v", d)
	}
	check(t, st.SaveMetadata(ctx, "exif", []MetadataResult{{AssetID: ids[0], TakenAt: 1609459200}}))
	check(t, st.SaveMetadata(ctx, "no-exif", []MetadataResult{{AssetID: ids[0]}}))
	d, err = st.AssetDetail(ctx, ids[0], false)
	check(t, err)
	if d.TakenAt != 0 || d.OriginalDate == nil || d.OriginalDate.TakenAt != duplicate.TakenAt || d.OriginalDate.From != core.DateFromFilename {
		t.Fatalf("metadata fallback lost correction: %+v", d)
	}
	check(t, st.RestoreTakenAt(ctx, ids[:1]))
	d, err = st.AssetDetail(ctx, ids[0], false)
	check(t, err)
	if d.TakenAt != duplicate.TakenAt || d.DateFrom != core.DateFromFilename {
		t.Fatalf("stale restored guess: %+v", d)
	}
}

func TestCorrectedDatesFollowSmartRulesAndTheirBlocks(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	album, err := st.CreateAlbum(ctx, "New year", core.AlbumSmart, core.AlbumRule{From: "2026-01-01", To: "2026-01-01"}, nil)
	check(t, err)
	check(t, st.SetTakenAt(ctx, ids[:1], 1767261600))
	buckets, err := st.DayBuckets(ctx, core.View{Album: album}, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 1 || buckets[0].Count != 1 {
		t.Fatalf("smart date rule missed correction: %+v", buckets)
	}
	_, err = st.SetBlock(ctx, core.BlockAlbum, album, "", core.BlockPhotos)
	check(t, err)
	n, err := st.CountVisible(ctx)
	check(t, err)
	if n != 1 {
		t.Fatalf("smart date block missed correction: %d", n)
	}
	check(t, st.RestoreTakenAt(ctx, ids[:1]))
	n, err = st.CountVisible(ctx)
	check(t, err)
	if n != 2 {
		t.Fatalf("smart date block kept restored asset hidden: %d", n)
	}
}

func TestPhotosIntersectPersonThingPlaceAndEventAndExcludeUnavailableMedia(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	check(t, st.SetLocation(ctx, ids, 51.5074, -0.1278))
	d, err := st.AssetDetail(ctx, ids[0], false)
	check(t, err)
	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	tag, err := st.CreateTag(ctx, "Journey", core.TagManual, nil)
	check(t, err)
	for _, id := range ids {
		check(t, st.TagAsset(ctx, id, tag))
	}
	writeInTx(t, st, `INSERT INTO people(id, name, created_at) VALUES (1, 'Amina', 0);
		INSERT INTO faces(asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by)
		SELECT id, 0, 0, 1, 1, 1, 20, 0, 'test', zeroblob(512), 1, 'user' FROM assets`)
	for _, view := range []core.View{{}, {Person: 1}, {Tag: tag}, {Place: d.PlaceID}, {Event: events[0].ID}} {
		view.Filter = core.FilterPhotos
		sel, err := st.Selection(ctx, view, nil, false)
		check(t, err)
		if !slices.Equal(sel.IDs, ids[:1]) {
			t.Fatalf("view %+v selected %v", view, sel.IDs)
		}
	}
	check(t, st.TrashAssets(ctx, ids[:1]))
	buckets, err := st.DayBuckets(ctx, core.View{Filter: core.FilterPhotos}, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 0 {
		t.Fatalf("trashed photo counted: %+v", buckets)
	}
	check(t, st.RestoreAssets(ctx, ids))
	_, err = st.SetBlock(ctx, core.BlockAsset, ids[0], "", core.BlockPhotos)
	check(t, err)
	sel, err := st.Selection(ctx, core.View{Filter: core.FilterPhotos}, nil, false)
	check(t, err)
	if len(sel.IDs) != 0 {
		t.Fatalf("blocked photo selected: %v", sel.IDs)
	}
}

func TestSidebarCountsFollowKeptVisibilityAndTheTrashPredicate(t *testing.T) {
	st, ids := correctionAssets(t)
	ctx := t.Context()
	check(t, st.SetFavourite(ctx, ids[0], true))
	writeInTx(t, st, `INSERT INTO people(id, name, created_at) VALUES (1, 'Amina', 0), (2, NULL, 0);
		INSERT INTO faces(asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by)
		SELECT id, 0, 0, 1, 1, 1, 20, 0, 'test', zeroblob(512), id, 'user' FROM assets`)
	c, err := st.Counts(ctx)
	check(t, err)
	if c != (core.LibraryCounts{Photos: 1, Videos: 1, Favourites: 1, People: 1}) {
		t.Fatalf("counts: %+v", c)
	}
	block, err := st.SetBlock(ctx, core.BlockPerson, 1, "", core.BlockPhotos)
	check(t, err)
	check(t, st.TrashAssets(ctx, ids))
	c, err = st.Counts(ctx)
	check(t, err)
	if c != (core.LibraryCounts{Trash: 2}) {
		t.Fatalf("trash ignores blocks: %+v", c)
	}
	check(t, st.RemoveBlock(ctx, block))
	writeInTx(t, st, "UPDATE assets SET missing_since = 1 WHERE id = ?", ids[0])
	c, err = st.Counts(ctx)
	check(t, err)
	n, err := st.TrashCount(ctx)
	check(t, err)
	if c.Trash != 1 || c.Trash != n {
		t.Fatalf("trash mismatch: %+v / %d", c, n)
	}
	check(t, st.RestoreAssets(ctx, ids))
	c, err = st.Counts(ctx)
	check(t, err)
	if c.Trash != 0 || c.Videos != 1 || c.Photos != 0 {
		t.Fatalf("restore counts: %+v", c)
	}
}

func TestCorrectionMigrationBackfillsTrashAndCorrectionsSurviveReopening(t *testing.T) {
	ctx, path := t.Context(), filepath.Join(t.TempDir(), "library.db")
	legacy, err := sql.Open(driverName, path+"?_foreign_keys=1")
	check(t, err)
	t.Cleanup(func() { _ = legacy.Close() })
	names, err := migrationFS.ReadDir("migrations")
	check(t, err)
	for _, name := range names {
		if name.Name() >= "0039" {
			break
		}
		body, err := migrationFS.ReadFile("migrations/" + name.Name())
		check(t, err)
		check(t, errOf(legacy.ExecContext(ctx, string(body))))
	}
	check(t, errOf(legacy.ExecContext(ctx, `INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0);
		INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, favourite, trashed_at, missing_since)
		VALUES (1, 'a', 'photo', '.jpg', 10, 0, 1, NULL, NULL),
		(2, 'b', 'video', '.mp4', 20, 0, 0, NULL, NULL),
		(3, 'c', 'photo', '.jpg', 30, 0, 0, 1, NULL),
		(4, 'd', 'photo', '.jpg', 40, 0, 0, 1, 1);
		INSERT INTO files(asset_id, source_id, path, size, mtime)
		SELECT id, 1, 'C:\Photos\'||id||ext, 1, 0 FROM assets WHERE id < 4;
		PRAGMA user_version = 38`)))
	check(t, legacy.Close())
	st, err := Open(path)
	check(t, err)
	c, err := st.Counts(ctx)
	check(t, err)
	if c != (core.LibraryCounts{Photos: 1, Videos: 1, Favourites: 1, Trash: 1}) {
		t.Fatalf("upgrade counts: %+v", c)
	}
	check(t, st.SetTakenAt(ctx, []int64{1}, 0))
	check(t, st.SetLocation(ctx, []int64{1}, 0, 0))
	check(t, st.Close())
	st, err = Open(path)
	check(t, err)
	t.Cleanup(func() { check(t, st.Close()) })
	d, err := st.AssetDetail(ctx, 1, false)
	check(t, err)
	if d.TakenAt != 0 || d.DateFrom != core.DateFromUser || d.OriginalDate == nil || d.OriginalDate.TakenAt != 10 || d.OriginalLocation == nil || d.OriginalLocation.Lat != nil || d.Lat == nil || *d.Lat != 0 {
		t.Fatalf("persisted correction: %+v", d)
	}
	check(t, st.RestoreTakenAt(ctx, []int64{1}))
	check(t, st.RestoreLocation(ctx, []int64{1}))
}
