package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
)

func TestPlacesAndEventsFollowMetadataAndVisibility(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	paths := []string{`C:\Photos\a.jpg`, `C:\Photos\b.jpg`, `C:\Photos\c.jpg`, `C:\Photos\d.jpg`}
	items := make([]IngestItem, 0, len(paths))
	for i, path := range paths {
		items = append(items, item(src.ID, path, string(rune('a'+i))))
	}
	_, err = st.Ingest(ctx, items)
	check(t, err)
	ids := make([]int64, len(paths))
	for i, path := range paths {
		check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", path).Scan(&ids[i]))
	}
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC).Unix()
	lahoreLat, lahoreLon := 31.5546, 74.3572
	islamabadLat, islamabadLon := 33.6938, 73.0652
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{
		{AssetID: ids[0], TakenAt: base, Lat: &lahoreLat, Lon: &lahoreLon},
		{AssetID: ids[1], TakenAt: base + 3600, Lat: &lahoreLat, Lon: &lahoreLon},
		{AssetID: ids[2], TakenAt: base + 7200, Lat: &islamabadLat, Lon: &islamabadLon},
		{AssetID: ids[3], TakenAt: base + 23*3600, Lat: &islamabadLat, Lon: &islamabadLon},
	}))
	places, err := st.Places(ctx)
	check(t, err)
	if len(places) != 2 || places[0].Count != 2 || places[1].Count != 2 {
		t.Fatalf("places = %+v", places)
	}
	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(events) != 3 || events[0].Count != 1 || events[1].Count != 1 || events[2].Count != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[1].PlaceID == events[2].PlaceID || events[0].PlaceID != events[1].PlaceID {
		t.Fatalf("location split = %+v", events)
	}
	check(t, st.SetEventName(ctx, events[2].ID, "Family lunch", core.EventGapDefault))
	renamed, err := st.Event(ctx, events[2].ID, core.EventGapDefault)
	check(t, err)
	if renamed.Name != "Family lunch" {
		t.Fatalf("renamed event = %+v", renamed)
	}
	buckets, err := st.DayBuckets(ctx, core.View{Event: events[2].ID}, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 1 || buckets[0].Count != 2 {
		t.Fatalf("event buckets = %+v", buckets)
	}
	buckets, err = st.DayBuckets(ctx, core.View{Place: events[2].PlaceID}, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 1 || buckets[0].Count != 2 {
		t.Fatalf("place buckets = %+v", buckets)
	}
	check(t, st.SetFavourite(ctx, ids[0], true))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", ids[0])))
	places, err = st.Places(ctx)
	check(t, err)
	if len(places) != 2 || places[1].Count != 1 {
		t.Fatalf("places after hide = %+v", places)
	}
}

func TestBackfillPlacesResumes(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	_, err = st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})
	check(t, err)
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE assets SET lat = 31.5546, lon = 74.3572, place_checked = 0`)))
	n, err := st.BackfillPlaces(ctx)
	check(t, err)
	if n != 1 {
		t.Fatalf("backfilled %d, want 1", n)
	}
	if n, err := st.BackfillPlaces(ctx); err != nil || n != 0 {
		t.Fatalf("second backfill = %d, %v", n, err)
	}
	places, err := st.Places(ctx)
	check(t, err)
	if len(places) != 1 || places[0].Name != "Lahore" {
		t.Fatalf("backfilled places = %+v", places)
	}
}

func TestEventNameSurvivesEarlierPhoto(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	_, err = st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})
	check(t, err)
	var id int64
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets").Scan(&id))
	at := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC).Unix()
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: id, TakenAt: at}}))
	check(t, st.SetEventName(ctx, id, "Morning walk", core.EventGapDefault))
	_, err = st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\earlier.jpg`, "earlier")})
	check(t, err)
	var earlier int64
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE id != ?", id).Scan(&earlier))
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: earlier, TakenAt: at - 1800}}))
	event, err := st.Event(ctx, id, core.EventGapDefault)
	check(t, err)
	if event.ID != earlier || event.Name != "Morning walk" || event.Count != 2 {
		t.Fatalf("renamed event after earlier import = %+v", event)
	}
	check(t, st.SetEventName(ctx, id, "", core.EventGapDefault))
	event, err = st.Event(ctx, earlier, core.EventGapDefault)
	check(t, err)
	if event.Name == "Morning walk" {
		t.Fatal("reset retained the old name")
	}
}

func TestEventJumpAcrossPhotoWithoutGPS(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	for i, name := range []string{"a", "b", "c"} {
		_, err := st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\`+name+`.jpg`, name)})
		check(t, err)
		var id int64
		check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte(name)).Scan(&id))
		result := MetadataResult{AssetID: id, TakenAt: time.Date(2026, 1, 1, 9+i, 0, 0, 0, time.UTC).Unix()}
		switch i {
		case 0:
			lat, lon := 31.5546, 74.3572
			result.Lat, result.Lon = &lat, &lon
		case 2:
			lat, lon := 33.6938, 73.0652
			result.Lat, result.Lon = &lat, &lon
		}
		check(t, st.SaveMetadata(ctx, "m", []MetadataResult{result}))
	}
	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(events) != 2 || events[0].Count != 1 || events[1].Count != 2 {
		t.Fatalf("events across missing GPS = %+v", events)
	}
}

func TestEventViewComposesFiltersAndUsesTimeRange(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	_, err = st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})
	check(t, err)
	ids, err := queryAll(ctx, st.r, scanID, "SELECT id FROM assets ORDER BY id")
	check(t, err)
	at := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC).Unix()
	lat, lon := 31.5546, 74.3572
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: ids[0], TakenAt: at, Lat: &lat, Lon: &lon}, {AssetID: ids[1], TakenAt: at}}))
	check(t, st.SetFavourite(ctx, ids[0], true))
	buckets, err := st.DayBuckets(ctx, core.View{Filter: core.FilterFavourites, Event: ids[0]}, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 1 || buckets[0].Count != 1 {
		t.Fatalf("event favourites = %+v", buckets)
	}
	buckets, err = st.DayBuckets(ctx, core.View{Filter: core.FilterVideos, Event: ids[0]}, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 0 {
		t.Fatalf("event videos = %+v", buckets)
	}
	places, err := st.Places(ctx)
	check(t, err)
	for _, test := range []struct {
		name  string
		view  core.View
		count int
	}{
		{"favourites", core.View{Filter: core.FilterFavourites, Event: ids[0]}, 1},
		{"tag", core.View{Tag: 9999, Event: ids[0]}, 0},
		{"person", core.View{Person: 9999, Event: ids[0]}, 0},
		{"place", core.View{Place: places[0].ID, Event: ids[0]}, 1},
	} {
		items, err := st.DayItems(ctx, "2026-01-01", "2026-01-01", test.view, core.TagConfidenceDefault)
		check(t, err)
		count := 0
		for _, day := range items {
			count += len(day.Items)
		}
		if count != test.count {
			t.Errorf("event + %s returned %d items, want %d", test.name, count, test.count)
		}
	}
	where, args := (assetFilter{event: &core.Event{StartAt: at, EndAt: at, FirstID: ids[1], LastID: ids[1]}}).where(0)
	selected, err := queryAll(ctx, st.r, scanID, "SELECT id FROM assets WHERE "+where, args...)
	check(t, err)
	if len(selected) != 1 || selected[0] != ids[1] {
		t.Fatalf("equal-timestamp boundaries = %v", selected)
	}
	rows, err := st.r.QueryContext(ctx, "EXPLAIN QUERY PLAN SELECT count(*) FROM assets WHERE "+where, args...)
	check(t, err)
	defer rows.Close()
	foundRange := false
	for rows.Next() {
		var node, parent, unused int
		var detail string
		check(t, rows.Scan(&node, &parent, &unused, &detail))
		foundRange = foundRange || strings.Contains(detail, "SEARCH assets USING INDEX assets_timeline (taken_at>")
	}
	check(t, rows.Err())
	if !foundRange {
		t.Fatal("event predicate did not use the timeline time range")
	}
}

func TestRenamedEventMembersSurviveSplitRetimeAndAnchorRemoval(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	_, err = st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})
	check(t, err)
	ids, err := queryAll(ctx, st.r, scanID, "SELECT id FROM assets ORDER BY id")
	check(t, err)
	at := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC).Unix()
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: ids[0], TakenAt: at}, {AssetID: ids[1], TakenAt: at + 13*3600}}))
	check(t, st.SetEventName(ctx, ids[0], "First original", 12))
	check(t, st.SetEventName(ctx, ids[1], "Second original", 12))
	check(t, st.SetEventName(ctx, ids[0], "Merged replacement", 24))
	for _, id := range ids {
		event, err := st.Event(ctx, id, 12)
		check(t, err)
		if event.Name != "Merged replacement" {
			t.Fatalf("split event = %+v", event)
		}
	}
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", ids[0])))
	event, err := st.Event(ctx, ids[1], 24)
	check(t, err)
	if event.Name != "Merged replacement" {
		t.Fatalf("surviving member = %+v", event)
	}
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: ids[1], TakenAt: at + 72*3600}}))
	check(t, st.SetEventName(ctx, ids[1], "", 24))
	event, err = st.Event(ctx, ids[1], 24)
	check(t, err)
	if event.Name == "Merged replacement" || event.Name == "Second original" {
		t.Fatalf("reset retimed event = %+v", event)
	}
}

func TestEventCacheRefreshesOnlyCoversAndReturnsCopies(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	check(t, errOf(st.w.ExecContext(ctx, `INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at)
		VALUES (1, x'01', 'photo', '.jpg', 1767261600, 0), (2, x'02', 'photo', '.jpg', 1767261601, 0)`)))
	events, err := st.Events(ctx, 12)
	check(t, err)
	revision := st.eventsCache.revision
	events[0].Name = "caller mutation"
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET thumb_version = 1 WHERE id = 1")))
	events, err = st.Events(ctx, 12)
	check(t, err)
	if st.eventsCache.revision != revision || events[0].Cover != 1 || events[0].Name == "caller mutation" {
		t.Fatalf("cover refresh or copy isolation failed: %+v", events)
	}
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET thumb_version = 2 WHERE id = 2")))
	events, err = st.Events(ctx, 12)
	check(t, err)
	if events[0].Cover != 2 || events[0].CoverThumb != 2 || st.eventsCache.revision != revision {
		t.Fatalf("newer cover = %+v", events)
	}
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET thumb_version = 0 WHERE id = 2")))
	events, err = st.Events(ctx, 12)
	check(t, err)
	if events[0].Cover != 1 || st.eventsCache.revision != revision {
		t.Fatalf("fallback cover = %+v", events)
	}
	results := make(chan error, 8)
	for range 8 {
		go func() {
			for range 10 {
				items, err := st.Events(ctx, 12)
				if err != nil {
					results <- err
					return
				}
				items[0].Name = "private copy"
			}
			results <- nil
		}()
	}
	for range 8 {
		check(t, <-results)
	}
	events, err = st.Events(ctx, 12)
	check(t, err)
	if events[0].Name == "private copy" {
		t.Fatal("concurrent reader changed cached events")
	}
}

func TestBackfillPlacesUsesPendingIndexAndResumesBatches(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	check(t, errOf(st.w.ExecContext(ctx, `WITH RECURSIVE seq(value) AS (
		VALUES(1) UNION ALL SELECT value + 1 FROM seq WHERE value < 1001)
		INSERT INTO assets(id, content_hash, kind, ext, taken_at, lat, lon, added_at)
		SELECT value, CAST(value AS BLOB), 'photo', '.jpg', 1767261600, 31.5546, 74.3572, 0 FROM seq`)))
	rows, err := st.r.QueryContext(ctx, "EXPLAIN QUERY PLAN "+pendingPlacesSQL)
	check(t, err)
	defer rows.Close()
	foundPending := false
	for rows.Next() {
		var node, parent, unused int
		var detail string
		check(t, rows.Scan(&node, &parent, &unused, &detail))
		foundPending = foundPending || strings.Contains(detail, "USING INDEX assets_pending_place")
	}
	check(t, rows.Err())
	if !foundPending {
		t.Fatal("backfill did not use the pending-GPS index")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if n, err := st.BackfillPlaces(canceled); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled backfill = %d, %v", n, err)
	}
	check(t, errOf(st.w.ExecContext(ctx, `CREATE TRIGGER fail_place_batch BEFORE UPDATE OF place_checked ON assets
		WHEN NEW.id = 501 BEGIN SELECT RAISE(ABORT, 'transient backfill failure'); END`)))
	if n, err := st.BackfillPlaces(ctx); n != 500 || err == nil {
		t.Fatalf("partially committed backfill = %d, %v; want 500 and error", n, err)
	}
	check(t, errOf(st.w.ExecContext(ctx, "DROP TRIGGER fail_place_batch")))
	n, err := st.BackfillPlaces(ctx)
	check(t, err)
	if n != 501 {
		t.Fatalf("resumed backfill updated %d, want 501", n)
	}
	n, err = st.BackfillPlaces(ctx)
	check(t, err)
	if n != 0 {
		t.Fatalf("completed backfill updated %d", n)
	}
}

func TestEventCountPreviewsAGapWithoutReplacingTheCachedList(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	paths := []string{`C:\Photos\a.jpg`, `C:\Photos\b.jpg`, `C:\Photos\c.jpg`}
	items := make([]IngestItem, 0, len(paths))
	for i, path := range paths {
		items = append(items, item(src.ID, path, string(rune('a'+i))))
	}
	_, err = st.Ingest(ctx, items)
	check(t, err)
	ids := make([]int64, len(paths))
	for i, path := range paths {
		check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", path).Scan(&ids[i]))
	}
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC).Unix()
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{
		{AssetID: ids[0], TakenAt: base},
		{AssetID: ids[1], TakenAt: base + 2*3600},
		{AssetID: ids[2], TakenAt: base + 20*3600},
	}))
	events, err := st.Events(ctx, core.EventGapDefault)
	check(t, err)
	if len(events) != 2 {
		t.Fatalf("events at the default gap = %+v", events)
	}
	for _, test := range []struct{ hours, want int }{{1, 3}, {core.EventGapDefault, 2}, {24, 1}} {
		n, err := st.EventCount(ctx, test.hours)
		check(t, err)
		if n != test.want {
			t.Errorf("EventCount(%d) = %d, want %d", test.hours, n, test.want)
		}
	}
	// The preview neither saves the gap nor regroups the list the views read.
	if gap, err := st.EventGapHours(ctx); err != nil || gap != core.EventGapDefault {
		t.Fatalf("gap after preview = %d, %v", gap, err)
	}
	if st.eventsCache.gap != core.EventGapDefault || len(st.eventsCache.items) != 2 {
		t.Fatalf("cache after preview = gap %d, %d events", st.eventsCache.gap, len(st.eventsCache.items))
	}
}

func TestEventCardsKeepMediaCountsAndTheNameOverrideThroughRegrouping(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	f := newEventFixture(t, st)
	photo := f.add(0)
	video := f.addVideo(2)
	event, err := st.Event(ctx, photo, core.EventGapDefault)
	check(t, err)
	if event.Count != 2 || event.Videos != 1 || event.NameOverride != "" {
		t.Fatalf("automatic event = %+v", event)
	}
	check(t, st.SetEventName(ctx, photo, "Trip", core.EventGapDefault))
	check(t, st.SetEventGapHours(ctx, 1))
	for _, id := range []int64{photo, video} {
		event, err = st.Event(ctx, id, 1)
		check(t, err)
		if event.Name != "Trip" || event.NameOverride != "Trip" || event.Count != 1 {
			t.Fatalf("named split event = %+v", event)
		}
	}
	if event.Videos != 1 {
		t.Fatalf("video event = %+v", event)
	}
	check(t, st.SetEventName(ctx, video, "", 1))
	event, err = st.Event(ctx, video, 1)
	check(t, err)
	if event.NameOverride != "" || event.Name == "Trip" {
		t.Fatalf("automatic name was not restored: %+v", event)
	}
}

func TestEventCountPreviewsRespectLabelAndPhotoBlocks(t *testing.T) {
	for _, mode := range []core.BlockMode{core.BlockLabel, core.BlockPhotos} {
		t.Run(string(mode), func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			f := newEventFixture(t, st)
			a := f.add(0)
			f.add(2)
			f.add(24)
			f.block(a, mode)
			n, err := st.EventCount(ctx, core.EventGapDefault)
			check(t, err)
			if n != 1 {
				t.Fatalf("preview includes a blocked event: %d", n)
			}
		})
	}
}

func TestEventGapHoursUsesValidatedIntegerSettings(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	for _, test := range []struct{ stored, want int }{{36, 36}, {0, core.EventGapDefault}, {100, core.EventGapDefault}} {
		check(t, st.SetInt(ctx, core.SettingEventGapHours, test.stored))
		got, err := st.EventGapHours(ctx)
		check(t, err)
		if got != test.want {
			t.Errorf("gap %d = %d, want %d", test.stored, got, test.want)
		}
	}
}

func benchmarkOrganizationStore(b *testing.B) (*Store, context.Context) {
	b.Helper()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := st.Close(); err != nil {
			b.Error(err)
		}
	})
	ctx := context.Background()
	_, err = st.w.ExecContext(ctx, `WITH RECURSIVE seq(value) AS (
		VALUES(1) UNION ALL SELECT value + 1 FROM seq WHERE value < 100000)
		INSERT INTO assets(id, content_hash, kind, ext, taken_at, lat, lon, added_at)
		SELECT value, CAST(value AS BLOB), 'photo', '.jpg', 1767261600 + value * 60, 31.5546, 74.3572, 0 FROM seq`)
	if err != nil {
		b.Fatal(err)
	}
	return st, ctx
}

func BenchmarkEventCache(b *testing.B) {
	st, ctx := benchmarkOrganizationStore(b)
	b.Run("cold", func(b *testing.B) {
		for b.Loop() {
			st.eventsCache.Lock()
			st.eventsCache.loaded = false
			st.eventsCache.Unlock()
			if _, err := st.Events(ctx, core.EventGapDefault); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cover", func(b *testing.B) {
		for b.Loop() {
			b.StopTimer()
			if _, err := st.w.ExecContext(ctx, "UPDATE assets SET thumb_version = thumb_version + 1 WHERE id = 1"); err != nil {
				b.Fatal(err)
			}
			b.StartTimer()
			if _, err := st.Events(ctx, core.EventGapDefault); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cached", func(b *testing.B) {
		for b.Loop() {
			if _, err := st.Events(ctx, core.EventGapDefault); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkBackfillPlaces(b *testing.B) {
	st, ctx := benchmarkOrganizationStore(b)
	for b.Loop() {
		b.StopTimer()
		if _, err := st.w.ExecContext(ctx, "UPDATE assets SET place_id = NULL, place_checked = 0"); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if n, err := st.BackfillPlaces(ctx); err != nil || n != 100000 {
			b.Fatalf("backfill = %d, %v", n, err)
		}
	}
}
