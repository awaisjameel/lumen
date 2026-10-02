package store

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// xmpFixture is a library of three photos. Photo a is 4000 x 3000 with the
// Things Beaches (AI, sure enough), Dogs (AI, too unsure) and Holiday (the
// user's), and two faces: Ali's and an unnamed one. Photo b has one face, an
// unnamed person's. Photo c has nothing.
type xmpFixture struct {
	st            *Store
	ctx           context.Context
	a, b, c       int64
	beaches, dogs int64
	holiday       int64
	ali           int64
	aliFace       int64
}

func newXMPFixture(t *testing.T) xmpFixture {
	t.Helper()
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b"), item(src.ID, `C:\Photos\c.jpg`, "c")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	check(t, err)
	ids := []int64{jobs[0].AssetID, jobs[1].AssetID, jobs[2].AssetID} // newest first
	slices.Sort(ids)
	f := xmpFixture{st: st, ctx: ctx, a: ids[0], b: ids[1], c: ids[2]}
	check(t, st.SaveMetadata(ctx, "v", []MetadataResult{
		{AssetID: f.a, Width: 4000, Height: 3000, Orientation: 1},
		{AssetID: f.b, Width: 100, Height: 100, Orientation: 1},
		{AssetID: f.c, Width: 100, Height: 100, Orientation: 1},
	}))
	check(t, st.SyncVocabulary(ctx, []VocabLabel{{Name: "Beaches", Prompts: []string{"a beach"}}, {Name: "Dogs", Prompts: []string{"a dog"}}}))
	f.beaches, _, err = st.TagByName(ctx, "Beaches")
	check(t, err)
	f.dogs, _, err = st.TagByName(ctx, "Dogs")
	check(t, err)
	check(t, st.SaveEmbeddings(ctx, "m", "m", []EmbedResult{
		{AssetID: f.a, Vec: []int8{1}, Scale: 1, Tags: []TagScore{{f.beaches, 0.4}, {f.dogs, 0.001}}},
		{AssetID: f.b, Skipped: true}, {AssetID: f.c, Skipped: true},
	}))
	f.holiday, err = st.CreateTag(ctx, "Holiday", core.TagManual, nil)
	check(t, err)
	check(t, st.TagAsset(ctx, f.a, f.holiday))
	check(t, st.SaveFaces(ctx, "f1", "f1", []FaceResult{
		{AssetID: f.a, Faces: []DetectedFace{face(0.1, 1), face(0.6, 2)}},
		{AssetID: f.b, Faces: []DetectedFace{face(0.3, 3)}},
		{AssetID: f.c, Skipped: true},
	}))
	f.aliFace = facesOf(t, st, f.a)[0].ID
	f.ali, err = st.NameFaces(ctx, []int64{f.aliFace}, "Ali")
	check(t, err)
	return f
}

// export reads the assets XMPAssets returns, by id.
func (f xmpFixture) export(t *testing.T) map[int64]XMPAsset {
	t.Helper()
	assets, err := f.st.XMPAssets(f.ctx, 0, 100)
	check(t, err)
	out := map[int64]XMPAsset{}
	for _, a := range assets {
		out[a.ID] = a
	}
	return out
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func faceNames(faces []XMPFace) []string {
	out := make([]string, len(faces))
	for i, f := range faces {
		out[i] = f.Name
	}
	return out
}

func TestXMPAssetsReadTheThingsPeopleAndFavouriteOfAVisiblePhoto(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetFavourite(f.ctx, f.a, true))
	got := f.export(t)
	a, ok := got[f.a]
	if !ok {
		t.Fatalf("photo a is not exported: %+v", got)
	}
	if !a.Exportable || !a.Favourite || a.Video || a.Width != 4000 || a.Height != 3000 {
		t.Fatalf("photo a = %+v", a)
	}
	// The user's Thing and the sure AI one; not the AI's unsure one.
	if want := []string{"Beaches", "Holiday"}; !slices.Equal(a.Tags, want) {
		t.Fatalf("tags = %v, want %v", a.Tags, want)
	}
	// Ali only: the other face is nobody's name.
	if len(a.Faces) != 1 || a.Faces[0].Name != "Ali" {
		t.Fatalf("faces = %+v, want Ali's alone", a.Faces)
	}
	// The box is stored as float32.
	if got := a.Faces[0]; !near(got.X, 0.1) || !near(got.Y, 0.2) || !near(got.W, 0.2) || !near(got.H, 0.3) {
		t.Fatalf("Ali's box = %+v, want 0.1, 0.2, 0.2, 0.3", got)
	}
	if len(a.Files) != 1 || a.Files[0].Path != `C:\Photos\a.jpg` || a.Files[0].Wrote != nil {
		t.Fatalf("files = %+v", a.Files)
	}
	// Photo b has only an unnamed face and photo c nothing: nothing to say.
	if _, ok := got[f.b]; ok {
		t.Fatalf("photo b has nothing to say but is exported: %+v", got[f.b])
	}
	if _, ok := got[f.c]; ok {
		t.Fatalf("photo c has nothing to say but is exported: %+v", got[f.c])
	}
}

func TestXMPAssetsFollowTheTagConfidence(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetFloat(f.ctx, core.SettingTagConfidence, 0.0005))
	if tags := f.export(t)[f.a].Tags; !slices.Equal(tags, []string{"Beaches", "Dogs", "Holiday"}) {
		t.Fatalf("tags at a low confidence = %v, want Dogs too", tags)
	}
}

func TestXMPAssetsLeaveOutAThingTheUserRemovedAndTheHiddenSinks(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.UntagAsset(f.ctx, f.a, f.beaches))
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE tags SET hidden = 1 WHERE id = ?", f.holiday)))
	if a, ok := f.export(t)[f.a]; ok && len(a.Tags) > 0 {
		t.Fatalf("tags = %v, want none: Beaches was removed and Holiday is hidden", a.Tags)
	}
}

func TestXMPAssetsNeverReadABlockedThingOrABlockedPersonsName(t *testing.T) {
	for _, mode := range []core.BlockMode{core.BlockLabel, core.BlockPhotos} {
		t.Run(string(mode), func(t *testing.T) {
			f := newXMPFixture(t)
			check(t, errOf(f.st.SetBlock(f.ctx, core.BlockTag, f.beaches, "", mode)))
			check(t, errOf(f.st.SetBlock(f.ctx, core.BlockPerson, f.ali, "", mode)))
			a, ok := f.export(t)[f.a]
			if mode == core.BlockPhotos {
				// Photos of the blocked Thing and person are blocked themselves.
				if ok {
					t.Fatalf("photo a is blocked but exported: %+v", a)
				}
				return
			}
			if !ok {
				t.Fatal("a label block hid the whole photo from export")
			}
			if slices.Contains(a.Tags, "Beaches") || len(a.Faces) != 0 {
				t.Fatalf("a blocked label reached the export: tags %v, faces %v", a.Tags, faceNames(a.Faces))
			}
			if !slices.Equal(a.Tags, []string{"Holiday"}) {
				t.Fatalf("tags = %v, want the unblocked Holiday", a.Tags)
			}
		})
	}
}

func TestXMPAssetsSkipBlockedHiddenTrashedAndMissingPhotos(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetFavourite(f.ctx, f.a, true))
	check(t, f.st.SetFavourite(f.ctx, f.b, true))
	check(t, f.st.SetFavourite(f.ctx, f.c, true))
	if got := f.export(t); len(got) != 3 {
		t.Fatalf("exported %d favourites, want 3", len(got))
	}
	check(t, errOf(f.st.SetBlock(f.ctx, core.BlockAsset, f.a, "", core.BlockPhotos)))
	check(t, f.st.TrashAssets(f.ctx, []int64{f.b}))
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", f.c)))
	if got := f.export(t); len(got) != 0 {
		t.Fatalf("exported %v, want nothing: a is blocked, b trashed, c hidden", got)
	}
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET hidden = 0, missing_since = 1 WHERE id = ?", f.c)))
	if got := f.export(t); len(got) != 0 {
		t.Fatalf("exported a missing photo: %v", got)
	}
}

func TestXMPAssetsKeepAnAssetThatIsNoLongerExportableWhileASidecarIsRecordedForIt(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SaveXMPExports(f.ctx, []XMPRecord{{AssetID: f.a, Path: `C:\Photos\a.jpg.xmp`, Hash: []byte("h")}}))
	check(t, errOf(f.st.SetBlock(f.ctx, core.BlockAsset, f.a, "", core.BlockPhotos)))
	a, ok := f.export(t)[f.a]
	if !ok {
		t.Fatal("a blocked photo with a sidecar of Lumen's is not offered for its sidecar to be reduced")
	}
	if a.Exportable || len(a.Tags) != 0 || len(a.Faces) != 0 {
		t.Fatalf("blocked photo carries content: %+v", a)
	}
	if len(a.Files) != 1 || string(a.Files[0].Wrote) != "h" {
		t.Fatalf("files = %+v, want the recorded hash", a.Files)
	}
	// Once its files are missing there is no sidecar to reach.
	check(t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET missing_since = 1 WHERE id = ?", f.a)))
	if _, ok := f.export(t)[f.a]; ok {
		t.Fatal("a missing photo is offered")
	}
}

func TestXMPAssetsPageByID(t *testing.T) {
	f := newXMPFixture(t)
	for _, id := range []int64{f.a, f.b, f.c} {
		check(t, f.st.SetFavourite(f.ctx, id, true))
	}
	var seen []int64
	for after := int64(0); ; {
		page, err := f.st.XMPAssets(f.ctx, after, 2)
		check(t, err)
		if len(page) == 0 {
			break
		}
		for _, a := range page {
			seen = append(seen, a.ID)
		}
		after = page[len(page)-1].ID
	}
	if want := []int64{f.a, f.b, f.c}; !slices.Equal(seen, want) {
		t.Fatalf("pages gave %v, want %v", seen, want)
	}
}

func TestSaveXMPExportsReplacesTheRecordOfAPathWhateverItsCaseAndFindsItBesideTheFile(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetFavourite(f.ctx, f.a, true))
	check(t, f.st.SaveXMPExports(f.ctx, []XMPRecord{
		{AssetID: f.a, Path: `C:\PHOTOS\A.JPG.XMP`, Hash: []byte("one")},
		{AssetID: 9999, Path: `C:\Photos\gone.jpg.xmp`, Hash: []byte("x")}, // deleted meanwhile: skipped
	}))
	if got := f.export(t)[f.a].Files; len(got) != 1 || string(got[0].Wrote) != "one" {
		t.Fatalf("the record is not found beside the file: %+v", got)
	}
	// The same path in another spelling, now beside photo b: one record.
	check(t, f.st.SaveXMPExports(f.ctx, []XMPRecord{{AssetID: f.b, Path: `c:\photos\a.jpg.xmp`, Hash: []byte("two")}}))
	var n int
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT count(*) FROM xmp_exports").Scan(&n))
	if n != 1 {
		t.Fatalf("%d records, want 1: the path is one file", n)
	}
	if got := f.export(t)[f.a].Files; string(got[0].Wrote) != "two" {
		t.Fatalf("the newest record did not win: %+v", got)
	}
}

func TestXMPRevisionCountsOnlyWhileExportIsOnAndFollowsEveryInputOfASidecar(t *testing.T) {
	f := newXMPFixture(t)
	ctx, st := f.ctx, f.st
	revision := func() XMPRevision {
		r, err := st.XMPRevision(ctx)
		check(t, err)
		return r
	}

	before := revision()
	check(t, st.SetFavourite(ctx, f.a, true))
	check(t, st.TagAsset(ctx, f.b, f.holiday))
	if revision().Content != before.Content {
		t.Fatal("the revision moved while XMP export is off")
	}

	on := revision()
	check(t, st.SetXMP(ctx, true))
	if revision().Content == on.Content {
		t.Fatal("turning export on did not count as a change")
	}
	if enabled, err := st.Bool(ctx, core.SettingXMP, false); err != nil || !enabled {
		t.Fatalf("setting = %v, %v", enabled, err)
	}

	src, err := st.AddSource(ctx, `D:\More`)
	check(t, err)
	steps := []struct {
		name string
		do   func() error
	}{
		{"a favourite", func() error { return st.SetFavourite(ctx, f.c, true) }},
		{"a Thing put on a photo", func() error { return st.TagAsset(ctx, f.c, f.holiday) }},
		{"a Thing taken off", func() error { return st.UntagAsset(ctx, f.c, f.holiday) }},
		{"a Thing renamed", func() error { return st.RenameTag(ctx, f.holiday, "Trip") }},
		{"the tag confidence", func() error { return st.SetFloat(ctx, core.SettingTagConfidence, 0.1) }},
		{"a person named", func() error { return st.NamePerson(ctx, f.ali, "Alison") }},
		{"a face put with a person", func() error { return errOf(st.NameFaces(ctx, []int64{facesOf(t, st, f.b)[0].ID}, "Sara")) }},
		{"a face moved", func() error {
			return errOf(st.w.ExecContext(ctx, "UPDATE faces SET x = 0.5 WHERE id = ?", f.aliFace))
		}},
		{"a file added", func() error { return errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `D:\More\d.jpg`, "d")})) }},
		{"a face block's members", func() error {
			if err := errOf(st.w.ExecContext(ctx, `INSERT INTO face_blocks (id, asset_id, at_ms, x, y, w, h, lookalikes)
				VALUES (1, ?, 0, 0, 0, 1, 1, 0)`, f.a)); err != nil {
				return err
			}
			return errOf(st.w.ExecContext(ctx, "INSERT INTO face_block_faces (face_block_id, face_id, asset_id) VALUES (1, ?, ?)", f.aliFace, f.a))
		}},
		{"a person removed", func() error { return errOf(st.w.ExecContext(ctx, "DELETE FROM people WHERE id = ?", f.ali)) }},
	}
	for _, s := range steps {
		was := revision()
		check(t, s.do())
		if revision().Content == was.Content {
			t.Errorf("%s did not change the revision", s.name)
		}
	}

	was := revision()
	check(t, st.TrashAssets(ctx, []int64{f.a}))
	if revision().Visible == was.Visible {
		t.Error("trashing a photo did not change the visibility half of the revision")
	}

	check(t, st.SetXMP(ctx, false))
	off := revision()
	check(t, st.SetFavourite(ctx, f.b, true))
	if revision().Content != off.Content {
		t.Error("the revision moved again after export was turned off")
	}
}

func TestXMPTriggersReadTheSettingKeysTheCoreDeclares(t *testing.T) {
	// Migration 0027 spells the keys out, as SQL cannot import a constant.
	body, err := migrationFS.ReadFile("migrations/0027_xmp.sql")
	check(t, err)
	for _, key := range []string{core.SettingXMP, core.SettingTagConfidence} {
		if !strings.Contains(string(body), "'"+key+"'") {
			t.Errorf("migration 0027 does not name the setting %q", key)
		}
	}
}

func TestMigration0027CanRunAgainOverACurrentSchema(t *testing.T) {
	st := openTest(t)
	body, err := migrationFS.ReadFile("migrations/0027_xmp.sql")
	check(t, err)
	check(t, errOf(st.w.ExecContext(t.Context(), string(body))))
}
