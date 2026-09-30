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
		known, _, err := st.KnownFiles(ctx, src.ID, "")
		if err != nil {
			return err
		}
		return st.RemoveFiles(ctx, []int64{known[core.Fold(`C:\Photos\a.jpg`)].ID})
	}, true)
}
