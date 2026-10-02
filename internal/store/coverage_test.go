package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/core"
)

// coverFixture is four photos, taken at the times in the names of the map:
// three in a private folder (one in a subfolder) and one outside it.
type coverFixture struct {
	st  *Store
	ctx context.Context
	src int64
	ids map[string]int64
}

var coverPaths = map[string]struct {
	path string
	at   int64
}{
	"a": {`C:\Photos\private\a.jpg`, 300},
	"b": {`C:\Photos\private\b.jpg`, 100},
	"c": {`C:\Photos\private\sub\c.jpg`, 200},
	"d": {`C:\Photos\open\d.jpg`, 400},
}

func newCoverFixture(t *testing.T) *coverFixture {
	t.Helper()
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	f := &coverFixture{st: st, ctx: ctx, src: src.ID, ids: map[string]int64{}}
	for name, p := range coverPaths {
		it := item(src.ID, p.path, name)
		it.TakenAt = p.at
		check(t, errOf(st.Ingest(ctx, []IngestItem{it})))
		var id int64
		check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path = ?", p.path).Scan(&id))
		f.ids[name] = id
	}
	return f
}

// names turns ids back into the fixture's photo names, in the order given.
func (f *coverFixture) names(ids ...int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		for name, got := range f.ids {
			if got == id {
				out = append(out, name)
			}
		}
	}
	return out
}

// covered is the names of the photos the block covers, newest first.
func (f *coverFixture) covered(t *testing.T, block int64) []string {
	t.Helper()
	items, err := f.st.BlockCoverage(f.ctx, block)
	check(t, err)
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	return f.names(ids...)
}

func (f *coverFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	check(t, errOf(f.st.w.ExecContext(f.ctx, query, args...)))
}

func TestBlockCoverageListsTheBlocksPhotosNewestFirstWhateverItsMode(t *testing.T) {
	for _, mode := range []core.BlockMode{core.BlockPhotos, core.BlockLabel} {
		t.Run(string(mode), func(t *testing.T) {
			f := newCoverFixture(t)
			block, err := f.st.SetBlock(f.ctx, core.BlockFolder, 0, `C:\Photos\private`, mode)
			check(t, err)
			if got, want := f.covered(t, block), []string{"a", "c", "b"}; !slices.Equal(got, want) {
				t.Fatalf("covered = %v, want %v", got, want)
			}
		})
	}
}

func TestBlockCoverageListsTheGridFieldsOfEachPhoto(t *testing.T) {
	f := newCoverFixture(t)
	id := f.ids["a"]
	check(t, f.st.SaveMetadata(f.ctx, "v", []MetadataResult{{AssetID: id, Width: 640, Height: 480, Duration: 2500}}))
	check(t, f.st.SaveThumbnails(f.ctx, "v", []ThumbResult{{AssetID: id}}))
	check(t, f.st.SetFavourite(f.ctx, id, true))
	block, err := f.st.SetBlock(f.ctx, core.BlockAsset, id, "", core.BlockPhotos)
	check(t, err)
	items, err := f.st.BlockCoverage(f.ctx, block)
	check(t, err)
	if len(items) != 1 || items[0].ID != id || items[0].W != 640 || items[0].H != 480 || !items[0].Favourite || items[0].Thumb == 0 {
		t.Fatalf("items = %+v", items)
	}
	if items[0].HiddenBy == nil || len(items[0].HiddenBy) != 0 {
		t.Fatalf("hiddenBy = %#v, want an empty list (never null)", items[0].HiddenBy)
	}
}

// Asking the block what it covers must give the photos its photo mode hides,
// no more and no fewer, for every kind of target: one source of truth.
func TestBlockCoverageIsExactlyWhatThePhotoModeBlockHides(t *testing.T) {
	cases := []struct {
		name string
		want []string
		// target makes the thing to block and returns the block's kind and target.
		target func(t *testing.T, f *coverFixture) (core.BlockKind, int64, string)
	}{
		{"a photo", []string{"b"}, func(_ *testing.T, f *coverFixture) (core.BlockKind, int64, string) {
			return core.BlockAsset, f.ids["b"], ""
		}},
		{"a folder, with its subfolders", []string{"a", "c", "b"}, func(_ *testing.T, _ *coverFixture) (core.BlockKind, int64, string) {
			return core.BlockFolder, 0, `C:\Photos\private`
		}},
		{"a Thing the user put on photos", []string{"d", "c"}, func(t *testing.T, f *coverFixture) (core.BlockKind, int64, string) {
			t.Helper()
			tag, err := f.st.CreateTag(f.ctx, "Receipts", core.TagManual, nil)
			check(t, err)
			check(t, f.st.TagAsset(f.ctx, f.ids["c"], tag))
			check(t, f.st.TagAsset(f.ctx, f.ids["d"], tag))
			return core.BlockTag, tag, ""
		}},
		{"a person", []string{"a", "b"}, func(t *testing.T, f *coverFixture) (core.BlockKind, int64, string) {
			t.Helper()
			check(t, f.st.SaveFaces(f.ctx, "f1", "f1", []FaceResult{
				{AssetID: f.ids["a"], Faces: []DetectedFace{face(0.1, 1)}},
				{AssetID: f.ids["b"], Faces: []DetectedFace{face(0.3, 1)}},
				{AssetID: f.ids["d"], Faces: []DetectedFace{face(0.3, 2)}},
			}))
			person, err := f.st.NameFaces(f.ctx, []int64{facesOf(t, f.st, f.ids["a"])[0].ID, facesOf(t, f.st, f.ids["b"])[0].ID}, "Ali")
			check(t, err)
			return core.BlockPerson, person, ""
		}},
		{"a place", []string{"d", "b"}, func(t *testing.T, f *coverFixture) (core.BlockKind, int64, string) {
			t.Helper()
			f.exec(t, "INSERT INTO places(id, name, region, country, lat, lon) VALUES (7, 'Lisbon', '', 'PT', 38.7, -9.1)")
			f.exec(t, "UPDATE assets SET place_id = 7 WHERE id IN (?, ?)", f.ids["b"], f.ids["d"])
			return core.BlockPlace, 7, ""
		}},
		{"a manual album", []string{"d", "a"}, func(t *testing.T, f *coverFixture) (core.BlockKind, int64, string) {
			t.Helper()
			album, err := f.st.CreateAlbum(f.ctx, "Trip", core.AlbumManual, core.AlbumRule{}, []int64{f.ids["a"], f.ids["d"]})
			check(t, err)
			return core.BlockAlbum, album, ""
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newCoverFixture(t)
			kind, target, path := c.target(t, f)
			label, err := f.st.SetBlock(f.ctx, kind, target, path, core.BlockLabel)
			check(t, err)
			if kind != core.BlockAsset && kind != core.BlockFolder { // those two are always photo blocks
				if got := f.covered(t, label); !slices.Equal(got, c.want) {
					t.Fatalf("label-only block covers %v, want %v", got, c.want)
				}
				if n, _ := f.st.CountVisible(f.ctx); n != 4 {
					t.Fatalf("label-only block hid %d photos", 4-n)
				}
			}
			block, err := f.st.SetBlock(f.ctx, kind, target, path, core.BlockPhotos)
			check(t, err)
			if got := f.covered(t, block); !slices.Equal(got, c.want) {
				t.Fatalf("photo block covers %v, want %v", got, c.want)
			}
			for name, id := range f.ids {
				visible, err := f.st.AssetVisible(f.ctx, id, false)
				check(t, err)
				if visible == slices.Contains(c.want, name) {
					t.Errorf("photo %s: visible = %v, but the block covers %v", name, visible, c.want)
				}
			}
		})
	}
}

// Event and face blocks cover the photos of rows their own units keep; the
// rows are inserted here as those units write them.
func TestBlockCoverageOfAnEventOrFaceBlockReadsItsMembers(t *testing.T) {
	f := newCoverFixture(t)
	f.exec(t, "INSERT INTO event_tags(id, kind, created_at) VALUES (5, 'block', 1)")
	f.exec(t, "INSERT INTO event_members(tag_id, asset_id) VALUES (5, ?), (5, ?)", f.ids["a"], f.ids["d"])
	f.exec(t, "INSERT INTO blocks(id, kind, target_id, mode, name, created_at) VALUES (20, 'event', 5, 'label', 'Birthday', 1)")

	check(t, f.st.SaveFaces(f.ctx, "f1", "f1", []FaceResult{{AssetID: f.ids["c"], Faces: []DetectedFace{face(0.1, 1)}}}))
	faceID := facesOf(t, f.st, f.ids["c"])[0].ID
	f.exec(t, "INSERT INTO face_blocks(id, asset_id, at_ms, x, y, w, h, face_id, lookalikes) VALUES (6, ?, 0, 0.1, 0.2, 0.2, 0.3, ?, 0)", f.ids["c"], faceID)
	f.exec(t, "INSERT INTO face_block_faces(face_block_id, face_id, asset_id) VALUES (6, ?, ?)", faceID, f.ids["c"])
	f.exec(t, "INSERT INTO blocks(id, kind, target_id, mode, name, created_at) VALUES (21, 'face', 6, 'label', 'A face', 1)")

	if got, want := f.covered(t, 20), []string{"d", "a"}; !slices.Equal(got, want) {
		t.Fatalf("event block covers %v, want %v", got, want)
	}
	if got, want := f.covered(t, 21), []string{"c"}; !slices.Equal(got, want) {
		t.Fatalf("face block covers %v, want %v", got, want)
	}
	if n, _ := f.st.CountVisible(f.ctx); n != 4 {
		t.Fatalf("label-only blocks hid %d photos", 4-n)
	}
	f.exec(t, "UPDATE blocks SET mode = 'photos' WHERE id IN (20, 21)")
	if n, _ := f.st.CountVisible(f.ctx); n != 1 {
		t.Fatalf("photo-mode blocks left %d photos, want 1", n)
	}
	if got, want := f.covered(t, 20), []string{"d", "a"}; !slices.Equal(got, want) {
		t.Fatalf("photo-mode event block covers %v, want %v", got, want)
	}
}

func TestBlockCoverageLeavesOutPhotosThatAreHiddenTrashedOrGone(t *testing.T) {
	f := newCoverFixture(t)
	block, err := f.st.SetBlock(f.ctx, core.BlockFolder, 0, `C:\Photos\private`, core.BlockPhotos)
	check(t, err)
	f.exec(t, "UPDATE assets SET hidden = 1 WHERE id = ?", f.ids["a"])
	if got, want := f.covered(t, block), []string{"c", "b"}; !slices.Equal(got, want) {
		t.Fatalf("with one hidden: %v, want %v", got, want)
	}
	check(t, f.st.TrashAssets(f.ctx, []int64{f.ids["c"]}))
	if got, want := f.covered(t, block), []string{"b"}; !slices.Equal(got, want) {
		t.Fatalf("with one trashed: %v, want %v", got, want)
	}
	f.exec(t, "UPDATE assets SET missing_since = 5 WHERE id = ?", f.ids["b"])
	if got := f.covered(t, block); len(got) != 0 {
		t.Fatalf("with one missing: %v, want none", got)
	}
}

func TestBlockCoverageNamesTheOtherPhotoBlocksThatAlsoCoverAPhoto(t *testing.T) {
	f := newCoverFixture(t)
	folder, err := f.st.SetBlock(f.ctx, core.BlockFolder, 0, `C:\Photos\private`, core.BlockPhotos)
	check(t, err)
	album, err := f.st.CreateAlbum(f.ctx, "Trip", core.AlbumManual, core.AlbumRule{}, []int64{f.ids["a"], f.ids["d"]})
	check(t, err)
	albumBlock, err := f.st.SetBlock(f.ctx, core.BlockAlbum, album, "", core.BlockPhotos)
	check(t, err)
	// A label-only block hides no photo, so it is not what still hides one.
	tag, err := f.st.CreateTag(f.ctx, "Receipts", core.TagManual, nil)
	check(t, err)
	check(t, f.st.TagAsset(f.ctx, f.ids["a"], tag))
	_, err = f.st.SetBlock(f.ctx, core.BlockTag, tag, "", core.BlockLabel)
	check(t, err)

	items, err := f.st.BlockCoverage(f.ctx, folder)
	check(t, err)
	hidden := map[string][]core.BlockRef{}
	for _, it := range items {
		hidden[f.names(it.ID)[0]] = it.HiddenBy
	}
	if want := []core.BlockRef{{ID: albumBlock, Name: "Trip"}}; !slices.Equal(hidden["a"], want) {
		t.Errorf("photo a is also hidden by %+v, want %+v", hidden["a"], want)
	}
	for _, name := range []string{"b", "c"} {
		if len(hidden[name]) != 0 {
			t.Errorf("photo %s is also hidden by %+v, want nothing", name, hidden[name])
		}
	}
	// From the other block's side the overlap is the folder, and the album's
	// own photo outside the folder is hidden by nothing else.
	items, err = f.st.BlockCoverage(f.ctx, albumBlock)
	check(t, err)
	got := map[string][]core.BlockRef{}
	for _, it := range items {
		got[f.names(it.ID)[0]] = it.HiddenBy
	}
	if want := []core.BlockRef{{ID: folder, Name: `C:\Photos\private`}}; !slices.Equal(got["a"], want) {
		t.Errorf("album block: photo a is also hidden by %+v, want %+v", got["a"], want)
	}
	if len(got["d"]) != 0 {
		t.Errorf("album block: photo d is also hidden by %+v, want nothing", got["d"])
	}
}

func TestBlockCoverageNamesOverlappingBlocksNewestFirstAndByTheirLiveName(t *testing.T) {
	f := newCoverFixture(t)
	target, err := f.st.SetBlock(f.ctx, core.BlockAsset, f.ids["a"], "", core.BlockPhotos)
	check(t, err)
	first, err := f.st.CreateAlbum(f.ctx, "First", core.AlbumManual, core.AlbumRule{}, []int64{f.ids["a"]})
	check(t, err)
	second, err := f.st.CreateAlbum(f.ctx, "Second", core.AlbumManual, core.AlbumRule{}, []int64{f.ids["a"]})
	check(t, err)
	check(t, f.st.UpdateAlbum(f.ctx, second, "Renamed", core.AlbumRule{})) // a blocked album can no longer be renamed
	b1, err := f.st.SetBlock(f.ctx, core.BlockAlbum, first, "", core.BlockPhotos)
	check(t, err)
	b2, err := f.st.SetBlock(f.ctx, core.BlockAlbum, second, "", core.BlockPhotos)
	check(t, err)
	f.exec(t, "UPDATE blocks SET created_at = ? WHERE id = ?", 100, b1)
	f.exec(t, "UPDATE blocks SET created_at = ? WHERE id = ?", 200, b2)

	items, err := f.st.BlockCoverage(f.ctx, target)
	check(t, err)
	want := []core.BlockRef{{ID: b2, Name: "Renamed"}, {ID: b1, Name: "First"}}
	if len(items) != 1 || !slices.Equal(items[0].HiddenBy, want) {
		t.Fatalf("items = %+v, want one hidden by %+v", items, want)
	}
}

func TestBlockCoverageOfABlockThatIsGoneIsNotFound(t *testing.T) {
	f := newCoverFixture(t)
	if _, err := f.st.BlockCoverage(f.ctx, 99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown block: %v, want ErrNotFound", err)
	}
	block, err := f.st.SetBlock(f.ctx, core.BlockAsset, f.ids["a"], "", core.BlockPhotos)
	check(t, err)
	check(t, f.st.RemoveBlock(f.ctx, block))
	if _, err := f.st.BlockCoverage(f.ctx, block); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a removed block: %v, want ErrNotFound", err)
	}
}

func TestBlockCoverageOfABlockThatCoversNothingIsAnEmptyList(t *testing.T) {
	f := newCoverFixture(t)
	f.exec(t, "INSERT INTO event_tags(id, kind, created_at) VALUES (5, 'block', 1)")
	f.exec(t, "INSERT INTO blocks(id, kind, target_id, mode, name, created_at) VALUES (20, 'event', 5, 'photos', 'Empty', 1)")
	items, err := f.st.BlockCoverage(f.ctx, 20)
	check(t, err)
	if items == nil || len(items) != 0 {
		t.Fatalf("items = %#v, want an empty, non-nil list", items)
	}
}

func (f *coverFixture) revision(t *testing.T) (value, visible int64) {
	t.Helper()
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT value, visible_value FROM event_revision WHERE id = 1").Scan(&value, &visible))
	return value, visible
}

// An event block's target is a tag of its own, not an asset: the mode switch
// goes by the block's id, and must neither need nor disturb the target.
func TestSetBlockModeSwitchesAnEventBlockByItsOwnID(t *testing.T) {
	f := newCoverFixture(t)
	f.exec(t, "INSERT INTO event_tags(id, kind, created_at) VALUES (1, 'block', 1)")
	f.exec(t, "INSERT INTO event_members(tag_id, asset_id) VALUES (1, ?), (1, ?)", f.ids["a"], f.ids["d"])
	// The block's id (2) is not its target (1), and 1 is also an asset id.
	f.exec(t, "INSERT INTO blocks(id, kind, target_id, mode, name, created_at) VALUES (2, 'event', 1, 'label', 'Birthday', 1)")
	if n, _ := f.st.CountVisible(f.ctx); n != 4 {
		t.Fatalf("label-only block hid %d photos", 4-n)
	}
	check(t, f.st.SetBlockMode(f.ctx, 2, core.BlockPhotos))
	if n, _ := f.st.CountVisible(f.ctx); n != 2 {
		t.Fatalf("photo block left %d photos, want 2", n)
	}
	blocks, err := f.st.Blocks(f.ctx)
	check(t, err)
	if len(blocks) != 1 || blocks[0].ID != 2 || blocks[0].TargetID != 1 || blocks[0].Mode != core.BlockPhotos {
		t.Fatalf("blocks = %+v, want the one event block, now hiding photos", blocks)
	}
	check(t, f.st.SetBlockMode(f.ctx, 2, core.BlockLabel))
	if n, _ := f.st.CountVisible(f.ctx); n != 4 {
		t.Fatalf("back to label only left %d photos, want 4", n)
	}
}

func TestSetBlockModeToTheModeABlockHasChangesNothing(t *testing.T) {
	f := newCoverFixture(t)
	tag, err := f.st.CreateTag(f.ctx, "Receipts", core.TagManual, nil)
	check(t, err)
	block, err := f.st.SetBlock(f.ctx, core.BlockTag, tag, "", core.BlockLabel)
	check(t, err)
	value, visible := f.revision(t)
	check(t, f.st.SetBlockMode(f.ctx, block, core.BlockLabel))
	if v, vis := f.revision(t); v != value || vis != visible {
		t.Fatalf("revisions moved from %d/%d to %d/%d for an unchanged mode", value, visible, v, vis)
	}
	check(t, f.st.SetBlockMode(f.ctx, block, core.BlockPhotos))
	if v, vis := f.revision(t); vis == visible || v == value {
		t.Fatalf("revisions stayed at %d/%d after a real change", v, vis)
	}
}

func TestSetBlockModeRefusesWhatCannotBeDone(t *testing.T) {
	f := newCoverFixture(t)
	if err := f.st.SetBlockMode(f.ctx, 99, core.BlockPhotos); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown block: %v, want ErrNotFound", err)
	}
	asset, err := f.st.SetBlock(f.ctx, core.BlockAsset, f.ids["a"], "", core.BlockPhotos)
	check(t, err)
	if err := f.st.SetBlockMode(f.ctx, asset, core.BlockLabel); !errors.Is(err, ErrInvalidBlock) {
		t.Fatalf("a photo block made label-only: %v, want ErrInvalidBlock", err)
	}
	if err := f.st.SetBlockMode(f.ctx, asset, core.BlockMode("everything")); !errors.Is(err, ErrInvalidBlock) {
		t.Fatalf("an unknown mode: %v, want ErrInvalidBlock", err)
	}
	if n, _ := f.st.CountVisible(f.ctx); n != 3 {
		t.Fatalf("the refused calls changed the photo block: %d visible, want 3", n)
	}
}

// While the Blocked screen is open its photos are served, but unlocking
// reveals nothing else: hidden and missing photos stay unserved.
func TestUnlockingServesCoveredPhotosAndNothingElse(t *testing.T) {
	f := newCoverFixture(t)
	for _, name := range []string{"a", "b", "c"} {
		_, err := f.st.SetBlock(f.ctx, core.BlockAsset, f.ids[name], "", core.BlockPhotos)
		check(t, err)
	}
	f.exec(t, "UPDATE assets SET hidden = 1 WHERE id = ?", f.ids["b"])
	f.exec(t, "UPDATE assets SET missing_since = 5 WHERE id = ?", f.ids["c"])
	for _, c := range []struct {
		name     string
		unlocked bool
		served   bool
	}{
		{"a", false, false}, {"a", true, true}, // covered
		{"b", true, false}, {"c", true, false}, // covered but hidden, or gone
		{"d", false, true}, {"d", true, true}, // not covered
	} {
		id := f.ids[c.name]
		visible, err := f.st.AssetVisible(f.ctx, id, c.unlocked)
		check(t, err)
		_, fileErr := f.st.AssetFile(f.ctx, id, c.unlocked)
		_, detailErr := f.st.AssetDetail(f.ctx, id, c.unlocked)
		if visible != c.served || (fileErr == nil) != c.served || (detailErr == nil) != c.served {
			t.Errorf("photo %s, unlocked %v: visible %v, file %v, detail %v, want served = %v",
				c.name, c.unlocked, visible, fileErr, detailErr, c.served)
		}
	}
}

// BenchmarkBlockCoverage is a tag block covering 20,000 photos of a 100,000
// photo library, 5,000 of them also covered by a second photo block, which is
// the browser's worst case: its list and the overlaps both run.
func BenchmarkBlockCoverage(b *testing.B) {
	ctx := context.Background()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer st.Close()
	seedThumbed(b, st, 100_000)
	mustBench(b, errOf(st.w.ExecContext(ctx, "INSERT INTO tags(id, name, kind, created_at) VALUES (1, 'a', 'manual', 0), (2, 'b', 'manual', 0)")))
	mustBench(b, errOf(st.w.ExecContext(ctx, `INSERT INTO asset_tags(asset_id, tag_id, source)
		SELECT id, 1, 'user' FROM assets WHERE id % 5 = 0`)))
	mustBench(b, errOf(st.w.ExecContext(ctx, `INSERT INTO asset_tags(asset_id, tag_id, source)
		SELECT id, 2, 'user' FROM assets WHERE id % 20 = 0`)))
	target, err := st.SetBlock(ctx, core.BlockTag, 1, "", core.BlockPhotos)
	mustBench(b, err)
	_, err = st.SetBlock(ctx, core.BlockTag, 2, "", core.BlockPhotos)
	mustBench(b, err)
	b.ResetTimer()
	for b.Loop() {
		items, err := st.BlockCoverage(ctx, target)
		mustBench(b, err)
		if len(items) != 20_000 {
			b.Fatalf("covered %d photos, want 20000", len(items))
		}
	}
}

func mustBench(b *testing.B, err error) {
	b.Helper()
	if err != nil {
		b.Fatal(err)
	}
}
