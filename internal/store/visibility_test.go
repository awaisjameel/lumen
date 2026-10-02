package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"lumen/internal/core"
)

// A place blocked label-only keeps its photos but must not name their event.
func TestPlaceLabelBlockLeavesEventTitlesAlone(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	check(t, errOf(st.w.ExecContext(ctx, `INSERT INTO places(id, name, region, country, lat, lon)
		VALUES (1, 'Lahore', 'Punjab', 'Pakistan', 31.5, 74.3)`)))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET place_id = 1")))
	events := func() []core.Event {
		t.Helper()
		events, err := st.Events(ctx, core.EventGapDefault)
		check(t, err)
		if len(events) != 1 {
			t.Fatalf("events = %+v", events)
		}
		return events
	}
	if e := events()[0]; e.PlaceID != 1 || !strings.HasPrefix(e.Name, "Lahore, ") {
		t.Fatalf("event before the block = %+v", e)
	}
	_, err = st.SetBlock(ctx, core.BlockPlace, 1, "", core.BlockLabel)
	check(t, err)
	names, err := st.SearchNames(ctx, "photos in LAHORE")
	check(t, err)
	if len(names) != 1 || names[0].Kind != core.BlockPlace || !names[0].Blocked || names[0].Name != "Lahore" {
		t.Fatalf("blocked place search names = %+v", names)
	}
	if e := events()[0]; e.PlaceID != 0 || strings.Contains(e.Name, "Lahore") {
		t.Fatalf("blocked place still names its event: %+v", e)
	}
}

// Clustering must never merge a blocked person away: the block would move
// to whoever they were merged into, hiding someone the user did not block.
func TestClusteringNeverMergesABlockedPerson(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	alice, err := st.NameFaces(ctx, []int64{facesOf(t, st, a)[0].ID}, "Alice")
	check(t, err)
	check(t, st.ApplyClusters(ctx, ClusterChanges{NewPeople: [][]int64{{facesOf(t, st, b)[0].ID}}}))
	var twin int64
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM people WHERE name IS NULL").Scan(&twin))
	_, err = st.SetBlock(ctx, core.BlockPerson, twin, "", core.BlockLabel)
	check(t, err)

	_, people, _, err := st.ClusterData(ctx, "f1")
	check(t, err)
	for _, p := range people {
		if p.ID == twin && !p.Confirmed {
			t.Error("a blocked person is not confirmed for clustering")
		}
	}
	check(t, st.ApplyClusters(ctx, ClusterChanges{Merges: []Merge{{From: twin, Into: alice}}}))
	if blocked, err := st.IsBlocked(ctx, core.BlockPerson, alice); err != nil || blocked {
		t.Fatalf("the block moved to Alice: %v, %v", blocked, err)
	}
	if blocked, err := st.IsBlocked(ctx, core.BlockPerson, twin); err != nil || !blocked {
		t.Fatalf("the blocked person was merged away: %v, %v", blocked, err)
	}
}

// A blocked Thing's name is neither applied nor revealed as existing.
func TestTagByNameRefusesABlockedThing(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	tag, err := st.CreateTag(ctx, "Private", core.TagManual, nil)
	check(t, err)
	if id, _, err := st.TagByName(ctx, "private"); err != nil || id != tag {
		t.Fatalf("before the block: %d, %v", id, err)
	}
	_, err = st.SetBlock(ctx, core.BlockTag, tag, "", core.BlockLabel)
	check(t, err)
	if _, _, err := st.TagByName(ctx, "PRIVATE"); !errors.Is(err, ErrBlockedName) {
		t.Fatalf("blocked Thing by name: %v", err)
	}
}

// The visibility revision follows what can be browsed, not every metadata or
// thumbnail batch (search rebuilds its exclusions whenever it moves), and
// only tags a block names.
func TestVisibilityRevisionFollowsOnlyVisibility(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 2)
	check(t, err)
	a, b := jobs[0].AssetID, jobs[1].AssetID
	blocked, err := st.CreateTag(ctx, "Private", core.TagManual, nil)
	check(t, err)
	other, err := st.CreateTag(ctx, "Beach", core.TagManual, nil)
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockTag, blocked, "", core.BlockPhotos)
	check(t, err)

	moved := func(what string, change func() error, want bool) {
		t.Helper()
		before, err := st.VisibilityRevision(ctx)
		check(t, err)
		check(t, change())
		after, err := st.VisibilityRevision(ctx)
		check(t, err)
		if got := after != before; got != want {
			t.Errorf("%s: revision moved = %v, want %v", what, got, want)
		}
	}
	moved("metadata", func() error {
		return st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: a, Orientation: 1, TakenAt: 1000}})
	}, false)
	moved("thumbnail", func() error { return st.SaveThumbnails(ctx, "t", []ThumbResult{{AssetID: a}}) }, false)
	moved("an unblocked tag", func() error { return st.TagAsset(ctx, a, other) }, false)
	moved("the blocked tag", func() error { return st.TagAsset(ctx, b, blocked) }, true)
	moved("removing its last file", func() error {
		known, err := st.KnownFiles(ctx, src.ID, "")
		if err != nil {
			return err
		}
		return st.RemoveFiles(ctx, []int64{known[core.Fold(`C:\Photos\a.jpg`)].ID})
	}, true)
}

func TestJobCountsOnlyIncludeVisibleAssets(t *testing.T) {
	for _, state := range []string{"blocked", "hidden", "trashed", "missing"} {
		t.Run(state, func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			src, err := st.AddSource(ctx, `C:\Photos`)
			check(t, err)
			check(t, errOf(st.Ingest(ctx, []IngestItem{
				item(src.ID, `C:\Photos\public.jpg`, "public"),
				item(src.ID, `C:\Photos\private.jpg`, "private"),
			})))
			var private int64
			check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", `C:\Photos\private.jpg`).Scan(&private))
			check(t, errOf(st.w.ExecContext(ctx, `UPDATE asset_jobs SET status = CASE stage
				WHEN 'metadata' THEN 'error' WHEN 'thumbnail' THEN 'skipped'
				WHEN 'embedding' THEN 'pending' ELSE 'waiting' END`)))
			counts := func(want int) {
				t.Helper()
				pending, failed, skipped, err := st.JobCounts(ctx)
				check(t, err)
				if len(pending) != 2 || pending[core.StageEmbedding] != want || pending[core.StageFaces] != want || failed != want || skipped != want {
					t.Fatalf("counts = %v, failed %d, skipped %d; want %d of each", pending, failed, skipped, want)
				}
			}
			counts(2)
			switch state {
			case "blocked":
				tag, err := st.CreateTag(ctx, "Private", core.TagManual, nil)
				check(t, err)
				check(t, st.TagAsset(ctx, private, tag))
				_, err = st.SetBlock(ctx, core.BlockTag, tag, "", core.BlockLabel)
				check(t, err)
				counts(2)
				_, err = st.SetBlock(ctx, core.BlockTag, tag, "", core.BlockPhotos)
				check(t, err)
				jobs, err := st.PendingJobsKind(ctx, core.StageEmbedding, core.KindPhoto, nil, 10)
				check(t, err)
				if len(jobs) != 2 {
					t.Fatalf("blocking stopped background indexing: %+v", jobs)
				}
			case "hidden":
				check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", private)))
			case "trashed":
				check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET trashed_at = 1 WHERE id = ?", private)))
			case "missing":
				check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET missing_since = 1 WHERE id = ?", private)))
			}
			counts(1)
		})
	}
}

func TestPeopleAssetsRefuseLabelBlockedPeople(t *testing.T) {
	st, ctx, a, _, _ := peopleFixture(t)
	faces := facesOf(t, st, a)
	first, err := st.NameFaces(ctx, []int64{faces[0].ID}, "Alice")
	check(t, err)
	second, err := st.NameFaces(ctx, []int64{faces[1].ID}, "Bob")
	check(t, err)
	ids, err := st.PeopleAssets(ctx, []int64{first, second, first})
	check(t, err)
	if len(ids) != 1 || ids[0] != a {
		t.Fatalf("visible people did not match their shared photo: %v", ids)
	}
	block, err := st.SetBlock(ctx, core.BlockPerson, first, "", core.BlockLabel)
	check(t, err)
	for _, people := range [][]int64{{first}, {first, first}, {first, second}} {
		ids, err := st.PeopleAssets(ctx, people)
		check(t, err)
		if len(ids) != 0 {
			t.Fatalf("blocked people %v returned photos: %v", people, ids)
		}
	}
	ids, err = st.PeopleAssets(ctx, []int64{second})
	check(t, err)
	if len(ids) != 1 || ids[0] != a {
		t.Fatalf("label block hid another person's photo: %v", ids)
	}
	check(t, st.RemoveBlock(ctx, block))
	ids, err = st.PeopleAssets(ctx, []int64{first, second})
	check(t, err)
	if len(ids) != 1 || ids[0] != a {
		t.Fatalf("unblocked people did not match their photo: %v", ids)
	}
}

func TestTimelineFiltersRecheckLabelBlocksInTheirReadSnapshot(t *testing.T) {
	for _, kind := range []core.BlockKind{core.BlockTag, core.BlockPerson, core.BlockAlbum, core.BlockPlace} {
		t.Run(string(kind), func(t *testing.T) {
			st, ctx, asset, _, _ := peopleFixture(t)
			var view core.View
			var target int64
			var err error
			switch kind {
			case core.BlockTag:
				target, err = st.CreateTag(ctx, "Private", core.TagManual, nil)
				check(t, err)
				check(t, st.TagAsset(ctx, asset, target))
				view.Tag = target
			case core.BlockPerson:
				target, err = st.NameFaces(ctx, []int64{facesOf(t, st, asset)[0].ID}, "Private")
				view.Person = target
			case core.BlockAlbum:
				target, err = st.CreateAlbum(ctx, "Private", core.AlbumManual, core.AlbumRule{}, []int64{asset})
				view.Album = target
			case core.BlockPlace:
				target = 1
				check(t, errOf(st.w.ExecContext(ctx, `INSERT INTO places(id, name, region, country, lat, lon)
					VALUES (1, 'Private', '', '', 0, 0)`)))
				err = errOf(st.w.ExecContext(ctx, "UPDATE assets SET place_id = ? WHERE id = ?", target, asset))
				view.Place = target
			default:
				t.Fatalf("unsupported fixture kind: %s", kind)
			}
			check(t, err)
			where, args, err := st.viewSQL(ctx, view, core.TagConfidenceDefault)
			check(t, err)
			count := func(want int) {
				t.Helper()
				var n int
				check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+where, args...).Scan(&n))
				if n != want {
					t.Fatalf("prepared %s timeline returned %d assets, want %d", kind, n, want)
				}
			}
			count(1)
			// Commit the block between resolving the filter and reading its assets.
			block, err := st.SetBlock(ctx, kind, target, "", core.BlockLabel)
			check(t, err)
			count(0)
			if _, err := st.DayBuckets(ctx, view, core.TagConfidenceDefault); !errors.Is(err, ErrNotFound) {
				t.Fatalf("blocked timeline did not return not found: %v", err)
			}
			visible, err := st.AssetVisible(ctx, asset, false)
			check(t, err)
			if !visible {
				t.Fatal("label-only block hid the asset")
			}
			check(t, st.RemoveBlock(ctx, block))
			count(1)
		})
	}
}
