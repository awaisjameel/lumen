package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/core"
)

// edge is the angle at which two vectors have the cosine similarity
// core.FaceLookalikeMin.
var edge = math.Acos(float64(core.FaceLookalikeMin)) * 180 / math.Pi

// vecAt is a unit vector of dim values, turned deg degrees from the first
// axis, so two of them have the cosine similarity of the angle between them.
func vecAt(dim int, deg float64) []float32 {
	v := make([]float32, dim)
	v[0], v[1] = float32(math.Cos(deg*math.Pi/180)), float32(math.Sin(deg*math.Pi/180))
	return v
}

// faceAt is a 0.2-wide face at x with a four-value vector turned deg degrees.
func faceAt(x float32, deg float64) DetectedFace {
	return DetectedFace{X: x, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: vecAt(4, deg)}
}

// faceLibrary is n photos (p1.jpg, p2.jpg, ...) to put faces in.
type faceLibrary struct {
	st     *Store
	ctx    context.Context
	assets []int64
}

func newFaceLibrary(t *testing.T, n int) *faceLibrary {
	t.Helper()
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	items := make([]IngestItem, n)
	for i := range items {
		items[i] = item(src.ID, fmt.Sprintf(`C:\Photos\p%d.jpg`, i+1), fmt.Sprint("p", i+1))
	}
	check(t, errOf(st.Ingest(ctx, items)))
	assets, err := queryAll(ctx, st.r, scanID, "SELECT id FROM assets ORDER BY id")
	check(t, err)
	return &faceLibrary{st: st, ctx: ctx, assets: assets}
}

// save stores the faces of photo i (0-based) as model finds them, keeping
// the face ids of faces at the same spot, as the faces stage does.
func (l *faceLibrary) save(t *testing.T, model string, i int, faces ...DetectedFace) {
	t.Helper()
	check(t, l.st.SaveFaces(l.ctx, model, model, []FaceResult{{AssetID: l.assets[i], Faces: faces}}))
}

// faceID is the id of the face of photo i at x.
func (l *faceLibrary) faceID(t *testing.T, i int, x float32) int64 {
	t.Helper()
	var id int64
	check(t, l.st.r.QueryRowContext(l.ctx, "SELECT id FROM faces WHERE asset_id = ? AND abs(x - ?) < 0.05", l.assets[i], x).Scan(&id))
	return id
}

// shown reports which of the photos the grid shows.
func (l *faceLibrary) shown(t *testing.T) []bool {
	t.Helper()
	out := make([]bool, len(l.assets))
	for i, id := range l.assets {
		ok, err := l.st.AssetVisible(l.ctx, id, false)
		check(t, err)
		out[i] = ok
	}
	return out
}

// covered returns the faces block covers, as photo index and x of each.
func (l *faceLibrary) covered(t *testing.T, block int64) []string {
	t.Helper()
	rows, err := queryAll(l.ctx, l.st.r, func(r *sql.Rows) (s string, err error) {
		var asset int64
		var x float32
		var model string
		if err = r.Scan(&asset, &x, &model); err == nil {
			s = fmt.Sprintf("p%d@%.1f/%s", slices.Index(l.assets, asset)+1, x, model)
		}
		return s, err
	}, `SELECT f.asset_id, f.x, f.model FROM face_block_faces c JOIN faces f ON f.id = c.face_id
		WHERE c.face_block_id = ? ORDER BY f.asset_id, f.x`, block)
	check(t, err)
	return rows
}

// blockOf is the face_blocks id of the block blockID.
func (l *faceLibrary) blockOf(t *testing.T, blockID int64) int64 {
	t.Helper()
	var id int64
	check(t, l.st.r.QueryRowContext(l.ctx, "SELECT target_id FROM blocks WHERE id = ?", blockID).Scan(&id))
	return id
}

func TestBlockingOneFaceCoversItAndNotItsLookalikes(t *testing.T) {
	l := newFaceLibrary(t, 3)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 10)) // looks like p1's face
	l.save(t, "m1", 2, faceAt(0.1, 120))
	id, created, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, false, false)
	check(t, err)
	if !created {
		t.Fatal("a new block was not reported as created")
	}
	if got, want := l.shown(t), []bool{false, true, true}; !slices.Equal(got, want) {
		t.Fatalf("photos shown = %v, want %v", got, want)
	}
	if got := l.covered(t, l.blockOf(t, id)); !slices.Equal(got, []string{"p1@0.1/m1"}) {
		t.Fatalf("covered faces = %v", got)
	}
}

func TestBlockingAFaceWithLookalikesCoversTheSimilarFacesOfItsModelOnly(t *testing.T) {
	l := newFaceLibrary(t, 5)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, edge-1)) // just inside the threshold
	l.save(t, "m1", 2, faceAt(0.1, edge+1)) // just outside
	l.save(t, "m1", 3, faceAt(0.1, 180))    // the opposite
	l.save(t, "m0", 4, faceAt(0.1, 0))      // another model's vector is not comparable
	id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	if got, want := l.shown(t), []bool{false, false, true, true, true}; !slices.Equal(got, want) {
		t.Fatalf("photos shown = %v, want %v", got, want)
	}
	if got := l.covered(t, l.blockOf(t, id)); !slices.Equal(got, []string{"p1@0.1/m1", "p2@0.1/m1"}) {
		t.Fatalf("covered faces = %v", got)
	}
}

func TestLookalikesAreCoveredInPhotosTheGridDoesNotShow(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 5))
	// Trashed now, restored later: it must come back hidden.
	check(t, errOf(l.st.w.ExecContext(l.ctx, "UPDATE assets SET trashed_at = 1 WHERE id = ?", l.assets[1])))
	id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	if got := l.covered(t, l.blockOf(t, id)); len(got) != 2 {
		t.Fatalf("covered faces = %v, want the trashed photo's too", got)
	}
	check(t, errOf(l.st.w.ExecContext(l.ctx, "UPDATE assets SET trashed_at = NULL WHERE id = ?", l.assets[1])))
	if got := l.shown(t); got[1] {
		t.Fatal("a restored photo with a look-alike face came back shown")
	}
}

func TestAFaceBlockIsNamedForThePersonOrTheFile(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 0))
	_, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 1, 0.1)}, "Sara")
	check(t, err)
	var names []string
	for i := range 2 {
		id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, i, 0.1), core.BlockLabel, false, false)
		check(t, err)
		var name string
		check(t, l.st.r.QueryRowContext(l.ctx, "SELECT name FROM blocks WHERE id = ?", id).Scan(&name))
		names = append(names, name)
	}
	if !slices.Equal(names, []string{"Face in p1.jpg", "Sara"}) {
		t.Fatalf("block names = %q", names)
	}
}

func TestBlockingAFaceThatIsGoneIsNotFound(t *testing.T) {
	l := newFaceLibrary(t, 1)
	if _, _, err := l.st.SetFaceBlock(l.ctx, 999, core.BlockPhotos, true, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("blocking a missing face: %v", err)
	}
}

func TestBlockingAFaceTwiceReturnsTheBlockAndWideningNeedsNoPIN(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 20))
	face := l.faceID(t, 0, 0.1)
	id, created, err := l.st.SetFaceBlock(l.ctx, face, core.BlockPhotos, false, false)
	check(t, err)
	again, created2, err := l.st.SetFaceBlock(l.ctx, face, core.BlockPhotos, false, false)
	if err != nil || created2 || again != id || !created {
		t.Fatalf("second block = %d, %v, %v; first = %d, %v", again, created2, err, id, created)
	}
	if got := l.shown(t); !got[1] {
		t.Fatal("a block of one face hid the other photo")
	}
	// Asking for look-alikes hides more, so the locked app may do it.
	widened, created3, err := l.st.SetFaceBlock(l.ctx, face, core.BlockPhotos, true, false)
	if err != nil || created3 || widened != id {
		t.Fatalf("widened block = %d, %v, %v", widened, created3, err)
	}
	if got := l.shown(t); got[1] {
		t.Fatal("the widened block left the look-alike's photo shown")
	}
	// A mode change still needs the PIN.
	if _, _, err := l.st.SetFaceBlock(l.ctx, face, core.BlockLabel, true, false); !errors.Is(err, ErrBlockChangeLocked) {
		t.Fatalf("mode change while locked: %v", err)
	}
	if _, _, err := l.st.SetFaceBlock(l.ctx, face, core.BlockLabel, true, true); err != nil {
		t.Fatal(err)
	}
	if got := l.shown(t); !slices.Equal(got, []bool{true, true}) {
		t.Fatalf("label mode left photos hidden: %v", got)
	}
}

func TestLabelModeTakesCoveredFacesOutOfFaceListsButKeepsThePhotos(t *testing.T) {
	l := newFaceLibrary(t, 3)
	// p1 has the blocked face and a friend; p2 has a look-alike; p3 has the friend.
	l.save(t, "m1", 0, faceAt(0.1, 0), faceAt(0.6, 150))
	l.save(t, "m1", 1, faceAt(0.1, 10))
	l.save(t, "m1", 2, faceAt(0.3, 150))
	check(t, l.st.SaveThumbnails(l.ctx, "t1", []ThumbResult{{AssetID: l.assets[0]}, {AssetID: l.assets[1]}, {AssetID: l.assets[2]}}))
	blocked, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 0, 0.1), l.faceID(t, 1, 0.1)}, "Ali")
	check(t, err)
	friend, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 0, 0.6), l.faceID(t, 2, 0.3)}, "Sara")
	check(t, err)
	_, _, err = l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockLabel, true, false)
	check(t, err)

	if got := l.shown(t); !slices.Equal(got, []bool{true, true, true}) {
		t.Fatalf("label mode hid photos: %v", got)
	}
	for i, want := range []int{1, 0, 1} {
		if faces, err := l.st.AssetFaces(l.ctx, l.assets[i]); err != nil || len(faces) != want {
			t.Fatalf("photo %d faces = %+v, %v; want %d", i+1, faces, err, want)
		}
	}
	faces, vecs, err := l.st.PersonFaces(l.ctx, blocked)
	if err != nil || len(faces) != 0 || len(vecs) != 0 {
		t.Fatalf("blocked person's faces for review = %+v, %v", faces, err)
	}
	if faces, _, err := l.st.PersonFaces(l.ctx, friend); err != nil || len(faces) != 2 {
		t.Fatalf("friend's faces = %+v, %v", faces, err)
	}
	people, err := l.st.People(l.ctx)
	check(t, err)
	if len(people) != 1 || people[0].ID != friend || people[0].Count != 2 {
		t.Fatalf("people = %+v, want only Sara", people)
	}
	if assets, err := l.st.PeopleAssets(l.ctx, []int64{blocked}); err != nil || len(assets) != 0 {
		t.Fatalf("photos of the blocked face's person = %v, %v", assets, err)
	}
}

// gridOf is the photos (by index) the grid shows for the view v.
func (l *faceLibrary) gridOf(t *testing.T, v core.View) []int {
	t.Helper()
	where, args, err := l.st.viewFilterSQL(l.ctx, v, 0.2)
	check(t, err)
	ids, err := queryAll(l.ctx, l.st.r, scanID, "SELECT id FROM assets WHERE "+where+" ORDER BY id", args...)
	check(t, err)
	var out []int
	for _, id := range ids {
		out = append(out, slices.Index(l.assets, id)+1)
	}
	return out
}

func TestLabelModeKeepsACoveredFaceOutOfThePersonGridAndPersonRuleAlbums(t *testing.T) {
	l := newFaceLibrary(t, 3)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 10))
	l.save(t, "m1", 2, faceAt(0.1, 170))
	ali, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 0, 0.1), l.faceID(t, 2, 0.1)}, "Ali")
	check(t, err)
	album, err := l.st.CreateAlbum(l.ctx, "Ali", core.AlbumSmart, core.AlbumRule{Person: ali}, nil)
	check(t, err)
	if got := l.gridOf(t, core.View{Person: ali}); !slices.Equal(got, []int{1, 3}) {
		t.Fatalf("Ali's grid before the block = %v", got)
	}
	_, _, err = l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockLabel, true, false)
	check(t, err)
	if got := l.shown(t); !slices.Equal(got, []bool{true, true, true}) {
		t.Fatalf("label mode hid photos: %v", got)
	}
	// Photos 1 and 2 stay in the library, but Ali is no longer in them.
	if got := l.gridOf(t, core.View{Person: ali}); !slices.Equal(got, []int{3}) {
		t.Fatalf("Ali's grid = %v, want only the photo whose face is not covered", got)
	}
	if got := l.gridOf(t, core.View{Album: album}); !slices.Equal(got, []int{3}) {
		t.Fatalf("the smart album for Ali = %v, want only the photo whose face is not covered", got)
	}
	if got := l.gridOf(t, core.View{}); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("the whole library = %v", got)
	}
}

func TestAPersonWhoseEveryFaceIsCoveredIsNotSuggestedSearchedOrGivenAPage(t *testing.T) {
	l := newFaceLibrary(t, 3)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 10))
	l.save(t, "m1", 2, faceAt(0.1, 170))
	ali, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 0, 0.1), l.faceID(t, 2, 0.1)}, "Alix")
	check(t, err)
	bo, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 1, 0.1)}, "Box")
	check(t, err)
	names := func() (suggested, named []string) {
		got, err := l.st.SearchPeople(l.ctx, "x", 10)
		check(t, err)
		for _, p := range got {
			suggested = append(suggested, p.Name)
		}
		all, err := l.st.NamedPeople(l.ctx)
		check(t, err)
		for _, p := range all {
			named = append(named, p.Name)
		}
		return suggested, named
	}
	if sug, named := names(); len(sug) != 2 || len(named) != 2 {
		t.Fatalf("before the block: suggested %v, named %v", sug, named)
	}
	_, _, err = l.st.SetFaceBlock(l.ctx, l.faceID(t, 1, 0.1), core.BlockLabel, false, false)
	check(t, err)
	if sug, named := names(); !slices.Equal(sug, []string{"Alix"}) || !slices.Equal(named, []string{"Alix"}) {
		t.Fatalf("after the block: suggested %v, named %v; want only Alix", sug, named)
	}
	if _, err := l.st.Person(l.ctx, bo); !errors.Is(err, ErrNotFound) {
		t.Fatalf("page of a person every face of whom is covered: %v", err)
	}
	// One face covered is not all of them: Ali still has a page and a count.
	_, _, err = l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockLabel, false, false)
	check(t, err)
	if p, err := l.st.Person(l.ctx, ali); err != nil || p.Count != 1 {
		t.Fatalf("Ali's page = %+v, %v; want one photo", p, err)
	}
}

func TestNarrowingAFaceBlockToTheFaceAloneNeedsThePIN(t *testing.T) {
	l := newFaceLibrary(t, 3)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 20))
	l.save(t, "m1", 2, faceAt(0.1, 170))
	face := l.faceID(t, 0, 0.1)
	id, _, err := l.st.SetFaceBlock(l.ctx, face, core.BlockPhotos, true, false)
	check(t, err)
	if _, _, err := l.st.SetFaceBlock(l.ctx, face, core.BlockPhotos, false, false); !errors.Is(err, ErrBlockChangeLocked) {
		t.Fatalf("narrowing while locked: %v", err)
	}
	if got := l.shown(t); !slices.Equal(got, []bool{false, false, true}) {
		t.Fatalf("a refused narrowing moved the block: %v", got)
	}
	again, created, err := l.st.SetFaceBlock(l.ctx, face, core.BlockPhotos, false, true)
	if err != nil || created || again != id {
		t.Fatalf("narrowing unlocked = %d, %v, %v", again, created, err)
	}
	if got := l.shown(t); !slices.Equal(got, []bool{false, true, true}) {
		t.Fatalf("shown after narrowing = %v, want only the face's own photo hidden", got)
	}
	// Narrowed, it stays narrow as new look-alikes arrive, and widening
	// again is allowed while locked.
	l.save(t, "m1", 2, faceAt(0.1, 5))
	if got := l.shown(t); !slices.Equal(got, []bool{false, true, true}) {
		t.Fatalf("a narrowed block covered a new look-alike: %v", got)
	}
	_, _, err = l.st.SetFaceBlock(l.ctx, face, core.BlockPhotos, true, false)
	check(t, err)
	if got := l.shown(t); !slices.Equal(got, []bool{false, false, false}) {
		t.Fatalf("shown after widening again = %v", got)
	}
}

func TestReanalysingTheAnchorWithTheSameVectorDoesNotScanTheModelAgain(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	block := l.blockOf(t, id)
	// A face that looks like the block and was never judged, as if it had
	// been written by something other than a face save: only a scan finds it.
	check(t, errOf(l.st.w.ExecContext(l.ctx, `INSERT INTO faces (asset_id, at_ms, x, y, w, h, score, sharpness, yaw, model, vec)
		VALUES (?, 0, 0.1, 0.2, 0.2, 0.3, 0.9, 50, 0, 'm1', ?)`, l.assets[1], float32Bytes(vecAt(4, 5)))))

	l.save(t, "m1", 0, faceAt(0.1, 0)) // found again, unchanged
	if got := l.covered(t, block); !slices.Equal(got, []string{"p1@0.1/m1"}) {
		t.Fatalf("an unchanged anchor scanned the model's faces: covered %v", got)
	}
	l.save(t, "m1", 0, faceAt(0.1, 1)) // found again, with a different vector
	if got := l.covered(t, block); !slices.Equal(got, []string{"p1@0.1/m1", "p2@0.1/m1"}) {
		t.Fatalf("a changed anchor did not scan the model's faces: covered %v", got)
	}
}

func TestACoveredFaceLeavesAPersonsCover(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 170))
	check(t, l.st.SaveThumbnails(l.ctx, "t1", []ThumbResult{{AssetID: l.assets[0]}, {AssetID: l.assets[1]}}))
	person, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 0, 0.1), l.faceID(t, 1, 0.1)}, "Ali")
	check(t, err)
	check(t, l.st.SetPersonCover(l.ctx, person, l.faceID(t, 0, 0.1)))
	_, _, err = l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockLabel, false, false)
	check(t, err)
	got, err := l.st.Person(l.ctx, person)
	check(t, err)
	if got.Cover.AssetID != l.assets[1] || got.Count != 1 {
		t.Fatalf("person after covering one face = %+v", got)
	}
}

func TestNewPhotoOfALookalikeIsHiddenByTheTransactionThatSavesItsFaces(t *testing.T) {
	l := newFaceLibrary(t, 4)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	_, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	before, err := l.st.VisibilityRevision(l.ctx)
	check(t, err)

	// One batch: a look-alike, an unrelated face, and a photo with both.
	check(t, l.st.SaveFaces(l.ctx, "m1", "m1", []FaceResult{
		{AssetID: l.assets[1], Faces: []DetectedFace{faceAt(0.4, 30)}},
		{AssetID: l.assets[2], Faces: []DetectedFace{faceAt(0.4, 100)}},
		{AssetID: l.assets[3], Faces: []DetectedFace{faceAt(0.2, 100), faceAt(0.6, 45)}},
	}))
	if got, want := l.shown(t), []bool{false, false, true, false}; !slices.Equal(got, want) {
		t.Fatalf("photos shown right after the save = %v, want %v", got, want)
	}
	if after, err := l.st.VisibilityRevision(l.ctx); err != nil || after == before {
		t.Fatalf("the save did not move the visibility revision: %d -> %d, %v", before, after, err)
	}
}

func TestAFaceChangedByAReanalysisIsUncoveredWhenItNoLongerLooksLikeTheBlock(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 20))
	id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	if got := l.shown(t); got[1] {
		t.Fatal("the look-alike was not hidden")
	}
	l.save(t, "m1", 1, faceAt(0.1, 120)) // found again, at the same spot, with another vector
	if got := l.shown(t); !got[1] {
		t.Fatal("a face that stopped looking like the block kept its photo hidden")
	}
	if got := l.covered(t, l.blockOf(t, id)); !slices.Equal(got, []string{"p1@0.1/m1"}) {
		t.Fatalf("covered faces = %v", got)
	}
}

func TestAFaceBlockSurvivesAFaceModelUpgradeAndMovedBoxes(t *testing.T) {
	l := newFaceLibrary(t, 5)
	// Model m1 (4 values per vector).
	l.save(t, "m1", 0, faceAt(0.1, 0), faceAt(0.6, 150)) // the blocked face, and a bystander
	l.save(t, "m1", 1, faceAt(0.1, 20))                  // a look-alike
	l.save(t, "m1", 2, faceAt(0.1, 130))
	id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	block := l.blockOf(t, id)
	anchor := l.faceID(t, 0, 0.1)
	if got, want := l.shown(t), []bool{false, false, true, true, true}; !slices.Equal(got, want) {
		t.Fatalf("before the upgrade: shown = %v, want %v", got, want)
	}

	// The upgrade starts with the anchor's photo: model m2 has 3 values per
	// vector, and finds the faces a little differently placed (the boxes
	// still overlap by at least half).
	moved := func(x float32, deg float64) DetectedFace {
		return DetectedFace{X: x, Y: 0.25, W: 0.18, H: 0.28, Score: 0.9, Sharpness: 50, Vec: vecAt(3, deg)}
	}
	check(t, l.st.SaveFaces(l.ctx, "m2", "m2", []FaceResult{{AssetID: l.assets[0], Faces: []DetectedFace{moved(0.12, 90), moved(0.62, 240)}}}))
	var face sql.NullInt64
	var x float64
	check(t, l.st.r.QueryRowContext(l.ctx, "SELECT face_id, x FROM face_blocks WHERE id = ?", block).Scan(&face, &x))
	if face.Int64 != anchor || math.Abs(x-0.12) > 1e-6 {
		t.Fatalf("anchor = face %v at x %v, want face %d moved to 0.12", face, x, anchor)
	}
	models, err := queryAll(l.ctx, l.st.r, func(r *sql.Rows) (m string, err error) { err = r.Scan(&m); return m, err },
		"SELECT model FROM face_block_vectors WHERE face_block_id = ? ORDER BY model", block)
	check(t, err)
	if !slices.Equal(models, []string{"m1", "m2"}) {
		t.Fatalf("block vectors for models %v", models)
	}
	// The photos still at m1 stay covered by the m1 vector meanwhile.
	if got, want := l.shown(t), []bool{false, false, true, true, true}; !slices.Equal(got, want) {
		t.Fatalf("mid-upgrade: shown = %v, want %v", got, want)
	}

	// The rest of the library follows. At m2 the look-alike is 15 degrees
	// from the anchor in the new space, the other photos are far.
	check(t, l.st.SaveFaces(l.ctx, "m2", "m2", []FaceResult{
		{AssetID: l.assets[1], Faces: []DetectedFace{moved(0.1, 105)}},
		{AssetID: l.assets[2], Faces: []DetectedFace{moved(0.1, 200)}},
	}))
	l.save(t, "m2", 3, moved(0.3, 80)) // a new photo, like it
	l.save(t, "m2", 4, moved(0.3, 250))
	if got, want := l.shown(t), []bool{false, false, true, false, true}; !slices.Equal(got, want) {
		t.Fatalf("after the upgrade: shown = %v, want %v", got, want)
	}
	if got := l.covered(t, block); !slices.Equal(got, []string{"p1@0.1/m2", "p2@0.1/m2", "p4@0.3/m2"}) {
		t.Fatalf("covered faces = %v", got)
	}

	// The detector misses the blocked face once: the anchor face goes, the
	// block (and its look-alikes) stay.
	check(t, l.st.SaveFaces(l.ctx, "m2", "m2", []FaceResult{{AssetID: l.assets[0], Faces: []DetectedFace{moved(0.62, 240)}}}))
	check(t, l.st.r.QueryRowContext(l.ctx, "SELECT face_id FROM face_blocks WHERE id = ?", block).Scan(&face))
	if face.Valid {
		t.Fatalf("anchor face after it was deleted = %v, want none", face)
	}
	if got, want := l.shown(t), []bool{true, false, true, false, true}; !slices.Equal(got, want) {
		t.Fatalf("with the anchor gone: shown = %v, want %v", got, want)
	}
	// It is found again near where it was, with a vector a long way from the
	// old one (160 degrees): the look-alikes are judged by it, so the photo
	// 80 degrees from the old vector is let go and the one at 200 is covered.
	check(t, l.st.SaveFaces(l.ctx, "m2", "m2", []FaceResult{{AssetID: l.assets[0], Faces: []DetectedFace{moved(0.14, 160), moved(0.62, 240)}}}))
	check(t, l.st.r.QueryRowContext(l.ctx, "SELECT face_id, x FROM face_blocks WHERE id = ?", block).Scan(&face, &x))
	if !face.Valid || math.Abs(x-0.14) > 1e-6 {
		t.Fatalf("anchor found again = %v at x %v", face, x)
	}
	if got, want := l.shown(t), []bool{false, false, false, true, true}; !slices.Equal(got, want) {
		t.Fatalf("with the anchor found again: shown = %v, want %v", got, want)
	}
	if got := l.covered(t, block); !slices.Equal(got, []string{"p1@0.1/m2", "p2@0.1/m2", "p3@0.1/m2"}) {
		t.Fatalf("covered faces = %v", got)
	}
}

func TestAFaceIsOnlyComparedWithABlockVectorOfItsOwnModel(t *testing.T) {
	l := newFaceLibrary(t, 3)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	_, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	// An identical vector under another model means nothing: its anchor
	// has not been found at that model, so the face is judged by none.
	l.save(t, "m2", 1, faceAt(0.1, 0))
	if got := l.shown(t); !got[1] {
		t.Fatal("a face of another model was compared with the block's vector")
	}
	l.save(t, "m1", 2, faceAt(0.1, 0))
	if got := l.shown(t); got[2] {
		t.Fatal("a face of the block's model was not compared with it")
	}
}

func TestALookalikeKeepsItsCoverWhenReanalysedBeforeTheAnchorIsFoundAtTheNewModel(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 20))
	id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	block := l.blockOf(t, id)

	// The look-alike goes to model m2 first: nothing says yet whether it
	// still looks like the block, so it stays covered.
	l.save(t, "m2", 1, DetectedFace{X: 0.1, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: vecAt(3, 170)})
	if got := l.shown(t); got[1] {
		t.Fatal("a face re-analysed before its block's anchor was let go")
	}
	// The anchor follows, and m2 puts it far from that face: now it is judged.
	l.save(t, "m2", 0, DetectedFace{X: 0.1, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: vecAt(3, 0)})
	if got := l.shown(t); !slices.Equal(got, []bool{false, true}) {
		t.Fatalf("shown = %v, want the anchor's photo hidden and the other let go", got)
	}
	if got := l.covered(t, block); !slices.Equal(got, []string{"p1@0.1/m2"}) {
		t.Fatalf("covered faces = %v", got)
	}
}

func TestRemovingAFaceBlockUncoversEverything(t *testing.T) {
	l := newFaceLibrary(t, 2)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 10))
	id, _, err := l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockPhotos, true, false)
	check(t, err)
	check(t, l.st.RemoveBlock(l.ctx, id))
	if got := l.shown(t); !slices.Equal(got, []bool{true, true}) {
		t.Fatalf("shown after removing the block = %v", got)
	}
	for _, table := range []string{"face_blocks", "face_block_vectors", "face_block_faces"} {
		var n int
		check(t, l.st.r.QueryRowContext(l.ctx, "SELECT count(*) FROM "+table).Scan(&n))
		if n != 0 {
			t.Errorf("%s still has %d rows", table, n)
		}
	}
	l.save(t, "m1", 1, faceAt(0.1, 10))
	if got := l.shown(t); !got[1] {
		t.Fatal("a removed block still hid a re-analysed photo")
	}
}

func TestFaceBlockAssetsListsThePhotosHoldingAnAnchor(t *testing.T) {
	l := newFaceLibrary(t, 3)
	l.save(t, "m1", 0, faceAt(0.1, 0), faceAt(0.6, 90))
	l.save(t, "m1", 2, faceAt(0.1, 0))
	for _, f := range []int64{l.faceID(t, 0, 0.1), l.faceID(t, 0, 0.6), l.faceID(t, 2, 0.1)} {
		_, _, err := l.st.SetFaceBlock(l.ctx, f, core.BlockLabel, false, false)
		check(t, err)
	}
	got, err := l.st.FaceBlockAssets(l.ctx)
	check(t, err)
	if want := []int64{l.assets[0], l.assets[2]}; !slices.Equal(got, want) {
		t.Fatalf("anchor photos = %v, want %v", got, want)
	}
}

func TestADotOverEncodedBytesMatchesTheDotOfTheVectors(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{1, 3, 4, 7, 512} {
		a, b := make([]float32, n), make([]float32, n)
		for i := range a {
			a[i], b[i] = rng.Float32()-0.5, rng.Float32()-0.5
		}
		got, want := dotBytes(a, float32Bytes(b)), dot(a, b)
		if math.Abs(float64(got-want)) > 1e-4 {
			t.Errorf("%d values: %v, want %v", n, got, want)
		}
	}
}

// randomUnit returns a random unit vector of 512 values (the ArcFace size).
func randomUnit(rng *rand.Rand) []float32 {
	v := make([]float32, 512)
	var norm float64
	for i := range v {
		v[i] = float32(rng.NormFloat64())
		norm += float64(v[i]) * float64(v[i])
	}
	for i := range v {
		v[i] /= float32(math.Sqrt(norm))
	}
	return v
}

// bigFaceLibrary is perAsset faces in each of n photos, all of model m1 with
// random 512-value unit vectors (the ArcFace size).
func bigFaceLibrary(b *testing.B, n, perAsset int) (st *Store, assets []int64) {
	b.Helper()
	st, ctx := openBench(b), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		b.Fatal(err)
	}
	items := make([]IngestItem, n)
	for i := range items {
		items[i] = item(src.ID, fmt.Sprintf(`C:\Photos\p%d.jpg`, i+1), fmt.Sprint("p", i+1))
	}
	if _, err := st.Ingest(ctx, items); err != nil {
		b.Fatal(err)
	}
	assets, err = queryAll(ctx, st.r, scanID, "SELECT id FROM assets ORDER BY id")
	if err != nil {
		b.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(7, 8))
	err = st.tx(ctx, func(tx *sql.Tx) error {
		ins, err := tx.PrepareContext(ctx, `INSERT INTO faces (asset_id, at_ms, x, y, w, h, score, sharpness, yaw, model, vec)
			VALUES (?, 0, ?, 0.2, 0.2, 0.3, 0.9, 50, 0, 'm1', ?)`)
		if err != nil {
			return err
		}
		defer ins.Close()
		for _, a := range assets {
			for k := range perAsset {
				if _, err := ins.ExecContext(ctx, a, float32(k)*0.1, float32Bytes(randomUnit(rng))); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	// A library that has been in use has its write-ahead log folded in.
	if _, err := st.w.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		b.Fatal(err)
	}
	return st, assets
}

func openBench(b *testing.B) *Store {
	b.Helper()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { st.Close() })
	return st
}

// BenchmarkBlockingAFaceWithLookalikesAt300000Faces is block creation: the
// anchor's vector against every face of its model.
func BenchmarkBlockingAFaceWithLookalikesAt300000Faces(b *testing.B) {
	st, assets := bigFaceLibrary(b, 3000, 100)
	ctx := context.Background()
	var face int64
	if err := st.r.QueryRowContext(ctx, "SELECT min(id) FROM faces WHERE asset_id = ?", assets[0]).Scan(&face); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		id, _, err := st.SetFaceBlock(ctx, face, core.BlockPhotos, true, true)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := st.RemoveBlock(ctx, id); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// BenchmarkSavingAFaceBatchWithFiveLookalikeBlocks is the faces stage's save
// of 32 photos (3 faces each) while five look-alike blocks exist, over a
// library of 30,000 faces.
func BenchmarkSavingAFaceBatchWithFiveLookalikeBlocks(b *testing.B) {
	st, assets := bigFaceLibrary(b, 300, 100)
	ctx := context.Background()
	for k := range 5 {
		var face int64
		if err := st.r.QueryRowContext(ctx, "SELECT min(id) FROM faces WHERE asset_id = ?", assets[k]).Scan(&face); err != nil {
			b.Fatal(err)
		}
		if _, _, err := st.SetFaceBlock(ctx, face, core.BlockPhotos, true, true); err != nil {
			b.Fatal(err)
		}
	}
	rng := rand.New(rand.NewPCG(9, 10))
	batch := make([]FaceResult, 32)
	for i := range batch {
		batch[i].AssetID = assets[10+i]
	}
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		for i := range batch {
			batch[i].Faces = batch[i].Faces[:0]
			for k := range 3 {
				batch[i].Faces = append(batch[i].Faces, DetectedFace{X: float32(k) * 0.3, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: randomUnit(rng)})
			}
		}
		b.StartTimer()
		if err := st.SaveFaces(ctx, "m1", "m1", batch); err != nil {
			b.Fatal(err)
		}
	}
}

// benchAnchorBatch is the faces stage saving 32 photos while 5 look-alike
// blocks exist over a library of 300,000 faces: the 5 anchor photos, found
// again as they were (or with changed vectors, which forces a rescan of the
// model), and 27 others with 3 new faces each.
func benchAnchorBatch(b *testing.B, changed bool) {
	b.Helper()
	st, assets := bigFaceLibrary(b, 3000, 100)
	ctx := context.Background()
	var batch []FaceResult
	for k := range 5 {
		faces, err := queryAll(ctx, st.r, func(r *sql.Rows) (f DetectedFace, err error) {
			var vec []byte
			err = r.Scan(&f.X, &f.Y, &f.W, &f.H, &f.Score, &f.Sharpness, &f.Yaw, &vec)
			f.Vec = bytesFloat32(vec)
			return f, err
		}, "SELECT x, y, w, h, score, sharpness, yaw, vec FROM faces WHERE asset_id = ? ORDER BY id", assets[k])
		if err != nil {
			b.Fatal(err)
		}
		var face int64
		if err := st.r.QueryRowContext(ctx, "SELECT min(id) FROM faces WHERE asset_id = ?", assets[k]).Scan(&face); err != nil {
			b.Fatal(err)
		}
		if _, _, err := st.SetFaceBlock(ctx, face, core.BlockPhotos, true, true); err != nil {
			b.Fatal(err)
		}
		batch = append(batch, FaceResult{AssetID: assets[k], Faces: faces})
	}
	for i := range 27 {
		batch = append(batch, FaceResult{AssetID: assets[10+i]})
	}
	rng := rand.New(rand.NewPCG(11, 12))
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		for i := 5; i < len(batch); i++ {
			batch[i].Faces = batch[i].Faces[:0]
			for k := range 3 {
				batch[i].Faces = append(batch[i].Faces, DetectedFace{X: float32(k) * 0.3, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: randomUnit(rng)})
			}
		}
		if changed {
			for i := range 5 {
				batch[i].Faces[0].Vec = randomUnit(rng)
			}
		}
		b.StartTimer()
		if err := st.SaveFaces(ctx, "m1", "m1", batch); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSavingAFaceBatchWithTheAnchorsUnchangedAt300000Faces is the usual
// re-analysis: the anchors come out as they were, so no model scan is made.
func BenchmarkSavingAFaceBatchWithTheAnchorsUnchangedAt300000Faces(b *testing.B) {
	benchAnchorBatch(b, false)
}

// BenchmarkSavingAFaceBatchWithTheAnchorsChangedAt300000Faces is the worst
// case: all five anchors have new vectors, so five scans of the model are made.
func BenchmarkSavingAFaceBatchWithTheAnchorsChangedAt300000Faces(b *testing.B) {
	benchAnchorBatch(b, true)
}

func TestABlockedPersonRuleAlbumDoesNotHideAPhotoOnlyAFaceCoveredByLabelModeLinksToThePerson(t *testing.T) {
	l := newFaceLibrary(t, 3)
	l.save(t, "m1", 0, faceAt(0.1, 0))
	l.save(t, "m1", 1, faceAt(0.1, 170))
	l.save(t, "m1", 2, faceAt(0.1, 100))
	ali, err := l.st.NameFaces(l.ctx, []int64{l.faceID(t, 0, 0.1), l.faceID(t, 1, 0.1)}, "Ali")
	check(t, err)
	album, err := l.st.CreateAlbum(l.ctx, "Ali", core.AlbumSmart, core.AlbumRule{Person: ali}, nil)
	check(t, err)
	_, _, err = l.st.SetFaceBlock(l.ctx, l.faceID(t, 0, 0.1), core.BlockLabel, false, false)
	check(t, err)
	_, err = l.st.SetBlock(l.ctx, core.BlockAlbum, album, "", core.BlockPhotos)
	check(t, err)
	// Photo 0's only link to Ali is the covered face, so the album no longer
	// holds it and its block no longer hides it; photo 1 is still Ali's.
	if got := l.shown(t); !slices.Equal(got, []bool{true, false, true}) {
		t.Fatalf("shown = %v, want [true false true]", got)
	}
}
