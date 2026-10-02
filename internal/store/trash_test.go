package store

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// trashFixture is two photos taken together (so one event, one day), each
// with a tag, a person, a place and an album in common, and the first one
// also with an exact copy and a look-alike (the second), so every reader has
// both to show. a is the one the tests trash.
type trashFixture struct {
	st   *Store
	ctx  context.Context
	a, b int64
}

func newTrashFixture(t *testing.T) trashFixture {
	t.Helper()
	st, ctx := openTest(t), context.Background()
	photos, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	copies, err := st.AddSource(ctx, `C:\Copies`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(photos.ID, `C:\Photos\a.jpg`, "a"), item(photos.ID, `C:\Photos\b.jpg`, "b"),
		item(copies.ID, `C:\Copies\a-copy.jpg`, "a"),
	})))
	var a, b int64
	check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", `C:\Photos\a.jpg`).Scan(&a))
	check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", `C:\Photos\b.jpg`).Scan(&b))
	check(t, st.SaveFaces(ctx, "f1", "f1", []FaceResult{
		{AssetID: a, Faces: []DetectedFace{face(0.1, 1)}},
		{AssetID: b, Faces: []DetectedFace{face(0.3, 1)}},
	}))
	check(t, st.SaveThumbnails(ctx, "t1", []ThumbResult{{AssetID: a}, {AssetID: b}}))
	// Identical perceptual hashes make the two a look-alike pair.
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET phash = 0, width = 1000, height = 800, taken_at = 1700000000")))

	tag, err := st.CreateTag(ctx, "Beach", core.TagManual, nil)
	check(t, err)
	check(t, st.TagAsset(ctx, a, tag))
	check(t, st.TagAsset(ctx, b, tag))
	_, err = st.NameFaces(ctx, []int64{facesOf(t, st, a)[0].ID, facesOf(t, st, b)[0].ID}, "Alice")
	check(t, err)

	check(t, errOf(st.w.ExecContext(ctx, `INSERT INTO places(id, name, region, country, lat, lon)
		VALUES (1, 'Lahore', 'Punjab', 'Pakistan', 31.5, 74.3)`)))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET place_id = 1")))

	_, err = st.CreateAlbum(ctx, "Trip", core.AlbumManual, core.AlbumRule{}, []int64{a, b})
	check(t, err)
	return trashFixture{st, ctx, a, b}
}

func (f trashFixture) trash(t *testing.T, ids ...int64) {
	t.Helper()
	check(t, f.st.TrashAssets(f.ctx, ids))
}
func (f trashFixture) restore(t *testing.T, ids ...int64) {
	t.Helper()
	check(t, f.st.RestoreAssets(f.ctx, ids))
}

// trashReaders are the places a photo can show up, each as how many of the
// fixture's two photos it shows, and how many are left once a is trashed.
var trashReaders = []struct {
	name string
	left int
	read func(t *testing.T, f trashFixture) int
}{
	{"the timeline", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		buckets, err := f.st.DayBuckets(f.ctx, core.View{}, core.TagConfidenceDefault)
		check(t, err)
		n := 0
		for _, b := range buckets {
			n += b.Count
		}
		return n
	}},
	{"a day's items", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		days, err := f.st.DayItems(f.ctx, "2023-11-14", "2023-11-15", core.View{}, core.TagConfidenceDefault)
		check(t, err)
		n := 0
		for _, d := range days {
			n += len(d.Items)
		}
		return n
	}},
	{"grid items", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		items, err := f.st.GridItems(f.ctx, []int64{f.a, f.b})
		check(t, err)
		return len(items)
	}},
	{"the visible subset", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		ids, err := f.st.VisibleAssets(f.ctx, []int64{f.a, f.b})
		check(t, err)
		return len(ids)
	}},
	{"search's exclusions", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		gone, err := f.st.UnavailableAssets(f.ctx)
		check(t, err)
		return 2 - len(gone)
	}},
	{"Things", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		things, err := f.st.Things(f.ctx, core.TagConfidenceDefault)
		check(t, err)
		for _, tag := range things {
			if tag.Name == "Beach" {
				return tag.Count
			}
		}
		return 0
	}},
	{"People", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		people, err := f.st.People(f.ctx)
		check(t, err)
		for _, p := range people {
			if p.Name == "Alice" {
				return p.Count
			}
		}
		return 0
	}},
	{"a person's photos", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		ids, err := f.st.PeopleAssets(f.ctx, []int64{personNamed(t, f, "Alice")})
		check(t, err)
		return len(ids)
	}},
	{"Places", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		places, err := f.st.Places(f.ctx)
		check(t, err)
		for _, p := range places {
			return p.Count
		}
		return 0
	}},
	{"Events", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		events, err := f.st.Events(f.ctx, core.EventGapDefault)
		check(t, err)
		n := 0
		for _, e := range events {
			n += e.Count
		}
		return n
	}},
	{"albums", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		albums, err := f.st.Albums(f.ctx, core.TagConfidenceDefault)
		check(t, err)
		n := 0
		for _, a := range albums {
			n += a.Count
		}
		return n
	}},
	{"Duplicates", 0, func(t *testing.T, f trashFixture) int {
		t.Helper()
		groups, err := f.st.DuplicateGroups(f.ctx)
		check(t, err)
		seen := map[int64]bool{}
		for _, g := range groups {
			for _, it := range g.Items {
				seen[it.ID] = true
			}
		}
		return len(seen)
	}},
	{"the grid's count", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		n, err := f.st.CountVisible(f.ctx)
		check(t, err)
		return n
	}},
	{"the progress counts", 1, func(t *testing.T, f trashFixture) int {
		t.Helper()
		check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE asset_jobs SET status = 'pending' WHERE stage = 'embedding'")))
		pending, _, _, err := f.st.JobCounts(f.ctx)
		check(t, err)
		return pending[core.StageEmbedding]
	}},
}

func personNamed(t *testing.T, f trashFixture, name string) int64 {
	t.Helper()
	id, err := f.st.PersonByName(f.ctx, name)
	check(t, err)
	return id
}

func TestEveryReaderDropsATrashedPhotoAndRestoreBringsItBack(t *testing.T) {
	for _, r := range trashReaders {
		t.Run(r.name, func(t *testing.T) {
			f := newTrashFixture(t)
			if got := r.read(t, f); got != 2 {
				t.Fatalf("before trashing: %d photos, want 2", got)
			}
			f.trash(t, f.a)
			if got := r.read(t, f); got != r.left {
				t.Errorf("after trashing: %d photos, want %d", got, r.left)
			}
			f.restore(t, f.a)
			if got := r.read(t, f); got != 2 {
				t.Errorf("after restoring: %d photos, want 2", got)
			}
		})
	}
}

func TestTrashShowsTrashedPhotosNewestCaptureFirstWithDaysAndItems(t *testing.T) {
	f := newTrashFixture(t)
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET taken_at = 1700000000 + id * 86400 WHERE id IN (?, ?)", f.a, f.b)))
	older, newer := f.a, f.b
	if older > newer {
		older, newer = newer, older
	}
	trash := core.View{Filter: core.FilterTrash}
	if buckets, err := f.st.DayBuckets(f.ctx, trash, core.TagConfidenceDefault); err != nil || len(buckets) != 0 {
		t.Fatalf("empty Trash buckets = %+v, %v", buckets, err)
	}
	f.trash(t, older, newer)

	buckets, err := f.st.DayBuckets(f.ctx, trash, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 2 || buckets[0].Day <= buckets[1].Day || buckets[0].Count != 1 {
		t.Fatalf("Trash buckets = %+v, want two days, newest first", buckets)
	}
	days, err := f.st.DayItems(f.ctx, buckets[1].Day, buckets[0].Day, trash, core.TagConfidenceDefault)
	check(t, err)
	if len(days) != 2 || days[0].Items[0].ID != newer || days[1].Items[0].ID != older {
		t.Fatalf("Trash items = %+v, want the newer capture first", days)
	}
	if n, err := f.st.TrashCount(f.ctx); err != nil || n != 2 {
		t.Fatalf("TrashCount = %d, %v", n, err)
	}
	// The Trash is not the library: the grid's own view stays empty of them.
	if buckets, err := f.st.DayBuckets(f.ctx, core.View{}, core.TagConfidenceDefault); err != nil || len(buckets) != 0 {
		t.Fatalf("library buckets while everything is trashed = %+v, %v", buckets, err)
	}
}

func TestTrashShowsOnlyPhotosThatAreStillOnDiskAndNotHidden(t *testing.T) {
	f := newTrashFixture(t)
	f.trash(t, f.a, f.b)
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", f.a)))
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET missing_since = 1 WHERE id = ?", f.b)))
	if n, err := f.st.TrashCount(f.ctx); err != nil || n != 0 {
		t.Fatalf("TrashCount = %d, %v, want 0", n, err)
	}
	if ok, err := f.st.AssetVisible(f.ctx, f.a, false); err != nil || ok {
		t.Fatalf("a hidden trashed photo is servable: %v, %v", ok, err)
	}
	if ok, err := f.st.AssetVisible(f.ctx, f.b, false); err != nil || ok {
		t.Fatalf("a missing trashed photo is servable: %v, %v", ok, err)
	}
}

func TestATrashedBlockedPhotoShowsInTheTrashAndItsMediaIsServed(t *testing.T) {
	f := newTrashFixture(t)
	_, err := f.st.SetBlock(f.ctx, core.BlockAsset, f.a, "", core.BlockPhotos)
	check(t, err)
	if ok, err := f.st.AssetVisible(f.ctx, f.a, false); err != nil || ok {
		t.Fatalf("a blocked photo is servable outside the Trash: %v, %v", ok, err)
	}
	f.trash(t, f.a)

	buckets, err := f.st.DayBuckets(f.ctx, core.View{Filter: core.FilterTrash}, core.TagConfidenceDefault)
	check(t, err)
	if len(buckets) != 1 || buckets[0].Count != 1 {
		t.Fatalf("Trash buckets = %+v, want the blocked photo", buckets)
	}
	if ok, err := f.st.AssetVisible(f.ctx, f.a, false); err != nil || !ok {
		t.Fatalf("the blocked trashed photo is not servable: %v, %v", ok, err)
	}
	if file, err := f.st.AssetFile(f.ctx, f.a, false); err != nil || len(file.Paths) == 0 {
		t.Fatalf("AssetFile = %+v, %v", file, err)
	}
	if d, err := f.st.AssetDetail(f.ctx, f.a, false); err != nil || d.ID != f.a {
		t.Fatalf("AssetDetail = %+v, %v", d, err)
	}
	// It is still blocked elsewhere: restoring it hides it again.
	f.restore(t, f.a)
	if ok, err := f.st.AssetVisible(f.ctx, f.a, false); err != nil || ok {
		t.Fatalf("a restored blocked photo is servable: %v, %v", ok, err)
	}
	if _, err := f.st.AssetFile(f.ctx, f.a, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("AssetFile of a restored blocked photo: %v", err)
	}
}

func TestMediaOfAVisibleOrTrashedPhotoIsServedAndOthersAreNot(t *testing.T) {
	for _, c := range []struct {
		name   string
		change string
		want   bool
	}{
		{"visible", "", true},
		{"trashed", "trashed_at = 1", true},
		{"hidden", "hidden = 1", false},
		{"missing", "missing_since = 1", false},
		{"trashed and hidden", "trashed_at = 1, hidden = 1", false},
		{"trashed and missing", "trashed_at = 1, missing_since = 1", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newTrashFixture(t)
			if c.change != "" {
				check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET "+c.change+" WHERE id = ?", f.a)))
			}
			if ok, err := f.st.AssetVisible(f.ctx, f.a, false); err != nil || ok != c.want {
				t.Errorf("AssetVisible = %v, %v, want %v", ok, err, c.want)
			}
			_, fileErr := f.st.AssetFile(f.ctx, f.a, false)
			_, detailErr := f.st.AssetDetail(f.ctx, f.a, false)
			if (fileErr == nil) != c.want || (detailErr == nil) != c.want {
				t.Errorf("AssetFile: %v, AssetDetail: %v, want served = %v", fileErr, detailErr, c.want)
			}
		})
	}
}

func TestTrashingAndRestoringMoveTheVisibilityRevision(t *testing.T) {
	f := newTrashFixture(t)
	revision := func() int64 {
		t.Helper()
		r, err := f.st.VisibilityRevision(f.ctx)
		check(t, err)
		return r
	}
	before := revision()
	f.trash(t, f.a)
	trashed := revision()
	if trashed == before {
		t.Fatal("trashing left the visibility revision alone")
	}
	f.restore(t, f.a)
	if revision() == trashed {
		t.Fatal("restoring left the visibility revision alone")
	}
}

func TestTrashingNeedsPresentAssetsAndFailsAsAWhole(t *testing.T) {
	f := newTrashFixture(t)
	if err := f.st.TrashAssets(f.ctx, []int64{f.a, 9999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trash with an unknown id: %v, want ErrNotFound", err)
	}
	if n, _ := f.st.TrashCount(f.ctx); n != 0 {
		t.Fatalf("a failed call trashed %d photos", n)
	}
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET missing_since = 1 WHERE id = ?", f.b)))
	if err := f.st.TrashAssets(f.ctx, []int64{f.a, f.b}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trash with a missing asset: %v, want ErrNotFound", err)
	}
	check(t, f.st.TrashAssets(f.ctx, nil))
	check(t, f.st.TrashAssets(f.ctx, []int64{f.a, f.a}))
	check(t, f.st.TrashAssets(f.ctx, []int64{f.a})) // again: nothing changes
	if n, _ := f.st.TrashCount(f.ctx); n != 1 {
		t.Fatalf("TrashCount = %d, want 1", n)
	}
}

func TestTrashingTouchesOnlyTheAssetAndKeepsTheOriginalTrashTime(t *testing.T) {
	f := newTrashFixture(t)
	f.trash(t, f.a)
	var files int
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT count(*) FROM files WHERE asset_id = ?", f.a).Scan(&files))
	if files != 2 {
		t.Fatalf("files of the trashed asset = %d, want both copies kept: trashing is the app's, not the disk's", files)
	}
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET trashed_at = 5 WHERE id = ?", f.a)))
	f.trash(t, f.a)
	var at int64
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT trashed_at FROM assets WHERE id = ?", f.a).Scan(&at))
	if at != 5 {
		t.Fatalf("trashing again moved trashed_at from 5 to %d", at)
	}
}

func TestRestoreTakesAssetsOutOfTheTrashOneAtATimeOrAllAtOnce(t *testing.T) {
	f := newTrashFixture(t)
	f.trash(t, f.a, f.b)
	f.restore(t, f.a)
	if n, _ := f.st.TrashCount(f.ctx); n != 1 {
		t.Fatalf("TrashCount after restoring one = %d, want 1", n)
	}
	f.restore(t, f.a) // again: nothing changes
	if err := f.st.RestoreAssets(f.ctx, []int64{f.b, 9999}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("restore with an unknown id: %v, want ErrNotFound", err)
	}
	if n, _ := f.st.TrashCount(f.ctx); n != 1 {
		t.Fatalf("a failed restore changed the Trash: %d", n)
	}
	check(t, f.st.RestoreAllTrash(f.ctx))
	if n, _ := f.st.TrashCount(f.ctx); n != 0 {
		t.Fatalf("TrashCount after restore all = %d", n)
	}
	if n, _ := f.st.CountVisible(f.ctx); n != 2 {
		t.Fatalf("CountVisible after restore all = %d, want 2", n)
	}
}

func TestRestoreAllLeavesTrashedPhotosWhoseFileWasRecycledMarked(t *testing.T) {
	f := newTrashFixture(t)
	f.trash(t, f.a, f.b)
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET missing_since = 1 WHERE id = ?", f.b)))
	check(t, f.st.RestoreAllTrash(f.ctx))
	var trashed bool
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT trashed_at IS NOT NULL FROM assets WHERE id = ?", f.b).Scan(&trashed))
	if !trashed {
		t.Fatal("restore all cleared the mark of a photo that is not on disk: a file that comes back from the Recycle Bin would return to the library")
	}
}

func TestTrashedFilesAreTheFilesOfTrashedAssetsOnly(t *testing.T) {
	f := newTrashFixture(t)
	if _, err := f.st.TrashedFiles(f.ctx, []int64{f.a}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("files of a photo that is not in the Trash: %v, want ErrNotFound", err)
	}
	f.trash(t, f.a)
	files, err := f.st.TrashedFiles(f.ctx, []int64{f.a})
	check(t, err)
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
		if file.AssetID != f.a || file.ID == 0 {
			t.Errorf("file = %+v", file)
		}
	}
	if !slices.Equal(paths, []string{`C:\Copies\a-copy.jpg`, `C:\Photos\a.jpg`}) {
		t.Fatalf("paths = %v, want both copies of a", paths)
	}
	if _, err := f.st.TrashedFiles(f.ctx, []int64{f.a, f.b}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("files of a mix of trashed and other photos: %v, want ErrNotFound", err)
	}
	all, err := f.st.AllTrashedFiles(f.ctx)
	check(t, err)
	if len(all) != 2 {
		t.Fatalf("AllTrashedFiles = %+v, want a's two copies", all)
	}
	if files, err := f.st.TrashedFiles(f.ctx, nil); err != nil || files == nil || len(files) != 0 {
		t.Fatalf("files of no assets = %v, %v", files, err)
	}
}

func TestRemovingARecycledFilesKeepsTheTrashedAssetsNamesTagsAndAlbums(t *testing.T) {
	f := newTrashFixture(t)
	f.trash(t, f.a)
	files, err := f.st.TrashedFiles(f.ctx, []int64{f.a})
	check(t, err)
	ids := make([]int64, len(files))
	for i, file := range files {
		ids[i] = file.ID
	}
	check(t, f.st.RemoveFiles(f.ctx, ids))

	var missing, trashed bool
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT missing_since IS NOT NULL, trashed_at IS NOT NULL FROM assets WHERE id = ?", f.a).Scan(&missing, &trashed))
	if !missing || !trashed {
		t.Fatalf("missing = %v, trashed = %v, want both: the file is in the bin and the mark stays", missing, trashed)
	}
	if n, _ := f.st.TrashCount(f.ctx); n != 0 {
		t.Fatalf("the Trash still shows a photo with no file: %d", n)
	}
	var tags, albums int
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT (SELECT count(*) FROM asset_tags WHERE asset_id = ?), (SELECT count(*) FROM album_items WHERE asset_id = ?)",
		f.a, f.a).Scan(&tags, &albums))
	if tags != 1 || albums != 1 {
		t.Fatalf("tags = %d, albums = %d, want the asset's own data kept", tags, albums)
	}
}

func TestTheTrashQueryUsesItsPartialIndex(t *testing.T) {
	f := newTrashFixture(t)
	rows, err := f.st.r.QueryContext(f.ctx, "EXPLAIN QUERY PLAN SELECT count(*) FROM assets WHERE "+trashed)
	check(t, err)
	defer rows.Close()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		check(t, rows.Scan(&id, &parent, &unused, &detail))
		if strings.Contains(detail, "INDEX assets_trash") {
			return
		}
		t.Logf("plan: %s", detail)
	}
	check(t, rows.Err())
	t.Fatal("the Trash count does not use assets_trash")
}

func TestRemovingRecycledFilesForgetsOnlyRowsStillHoldingTheRecycledPath(t *testing.T) {
	f := newTrashFixture(t)
	f.trash(t, f.a)
	files, err := f.st.TrashedFiles(f.ctx, []int64{f.a})
	check(t, err)
	// The scanner dropped the first row meanwhile, and SQLite gave its id to
	// an unrelated file (here: another path takes over the same id).
	stale := files[0]
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE files SET path = ? WHERE id = ?", `C:\Photos\someone-else.jpg`, stale.ID)))
	check(t, f.st.RemoveRecycledFiles(f.ctx, files))

	var n int
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT count(*) FROM files WHERE id = ?", stale.ID).Scan(&n))
	if n != 1 {
		t.Fatal("a row that no longer holds the recycled path was removed")
	}
	// The other copy goes, whatever the case of the recorded path.
	other := files[1]
	other.Path = strings.ToUpper(other.Path)
	check(t, f.st.RemoveRecycledFiles(f.ctx, []TrashedFile{other}))
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT count(*) FROM files WHERE id = ?", other.ID).Scan(&n))
	if n != 0 {
		t.Fatal("the recycled file's row was kept")
	}
	// Both rows gone would mark the asset missing; one left keeps it present.
	var missing bool
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT missing_since IS NOT NULL FROM assets WHERE id = ?", f.a).Scan(&missing))
	if missing {
		t.Fatal("an asset that still has a file was marked missing")
	}
}

func TestEveryStoreWriteThatChangesTheTrashGoesThroughATransaction(t *testing.T) {
	src, err := os.ReadFile("trash.go")
	check(t, err)
	if strings.Contains(string(src), "s.w.") {
		t.Fatal("trash.go writes with the bare writer: Store.tx must wrap every write that changes trashed_at")
	}
}
