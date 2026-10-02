package fileops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/scanner"
	"lumen/internal/store"
)

// library is a real store over real files under t.TempDir(): the planner and
// the runner read the disk, so their tests move real (tiny) files.
type library struct {
	t    *testing.T
	st   *store.Store
	ctx  context.Context
	root string // the source folder
	src  core.Source
}

func newLibrary(t *testing.T) library {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	l := library{t: t, st: st, ctx: t.Context(), root: filepath.Join(t.TempDir(), "Photos")}
	l.src = l.addSource(l.root)
	return l
}

func (l library) addSource(root string) core.Source {
	l.t.Helper()
	if err := os.MkdirAll(root, 0o750); err != nil {
		l.t.Fatal(err)
	}
	src, err := l.st.AddSource(l.ctx, root)
	if err != nil {
		l.t.Fatal(err)
	}
	return src
}

func (l library) path(rel string) string { return filepath.Join(l.root, filepath.FromSlash(rel)) }

// day is a photo's date for these tests: noon on that day.
func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 12, 0, 0, 0, time.UTC)
}

// add writes a file in the source and has the library ingest it as the photo
// taken at taken. Content makes the asset: the same content in two places is
// one asset with two files.
func (l library) add(rel, content string, taken time.Time) int64 {
	l.t.Helper()
	return l.addTo(l.src, l.root, rel, content, taken)
}

func (l library) addTo(src core.Source, root, rel, content string, taken time.Time) int64 {
	l.t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	l.write(path, content)
	info, err := os.Stat(path)
	if err != nil {
		l.t.Fatal(err)
	}
	if _, err := l.st.Ingest(l.ctx, []store.IngestItem{{
		SourceID: src.ID, Path: path, Size: info.Size(), MTime: info.ModTime().UnixNano(), Hash: []byte(content),
		Kind: core.KindPhoto, Ext: filepath.Ext(path), TakenAt: core.WallClock(taken), DateFrom: core.DateFromFile,
	}}); err != nil {
		l.t.Fatal(err)
	}
	return l.assetAt(path)
}

func (l library) write(path, content string) {
	l.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		l.t.Fatal(err)
	}
}

// assetAt is the asset the library has a file of at path.
func (l library) assetAt(path string) int64 {
	l.t.Helper()
	snap, err := l.st.OrganiseSnapshot(l.ctx, 0, core.EventGapDefault)
	if err != nil {
		l.t.Fatal(err)
	}
	for _, f := range snap.Files {
		if core.Fold(f.Path) == core.Fold(path) {
			return f.AssetID
		}
	}
	// Not visible: ask the details instead (hidden, trashed… are still assets).
	l.t.Fatalf("the library has no visible file at %s", path)
	return 0
}

func (l library) snapshot(sourceID int64) store.OrganiseSnapshot {
	l.t.Helper()
	snap, err := l.st.OrganiseSnapshot(l.ctx, sourceID, core.EventGapDefault)
	if err != nil {
		l.t.Fatal(err)
	}
	return snap
}

func (l library) plan(template string) Planned {
	l.t.Helper()
	p, err := Plan(l.ctx, template, l.snapshot(0))
	if err != nil {
		l.t.Fatal(err)
	}
	return p
}

// savePlan previews the template for every source and stores it as a run.
func (l library) savePlan(template string) (int64, Planned) {
	l.t.Helper()
	p := l.plan(template)
	run, err := l.st.SaveOrganisePlan(l.ctx, template, p.Moves)
	if err != nil {
		l.t.Fatal(err)
	}
	return run, p
}

// destinations are the To paths of the moves, relative to the source folder,
// with forward slashes, in order.
func (l library) destinations(moves []store.OrganiseMove) []string {
	var out []string
	for _, m := range moves {
		rel, err := filepath.Rel(l.root, m.To)
		if err != nil {
			l.t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func (l library) setEventName(asset int64, name string) {
	l.t.Helper()
	if err := l.st.SetEventName(l.ctx, asset, name, core.EventGapDefault); err != nil {
		l.t.Fatal(err)
	}
}

func (l library) setCamera(asset int64, camera string) {
	l.t.Helper()
	if err := l.st.SaveMetadata(l.ctx, "test", []store.MetadataResult{{AssetID: asset, Camera: camera}}); err != nil {
		l.t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exist(path string) bool { _, err := os.Lstat(path); return err == nil }

func equalStrings(a, b []string) bool { return slices.Equal(a, b) }

func TestPlanPutsEachFileInItsSourceUnderTheSchemeWithTheNamesTheViewsShow(t *testing.T) {
	l := newLibrary(t)
	a := l.add("IMG_1.jpg", "one", day(2024, time.August, 5))
	l.add("IMG_2.jpg", "two", day(2024, time.August, 5).Add(time.Hour))
	l.add("raw/IMG_3.JPG", "three", day(2023, time.December, 25))
	l.setEventName(a, "Beach day")
	l.setCamera(a, "Pixel 8")

	p := l.plan("{Year}/{MonthName}/{Day}/{Event}/{Camera}/{Name}")
	got := l.destinations(p.Moves)
	if len(got) != 3 || got[0] != "2023/December/25/"+eventTitle(t, l, "raw/IMG_3.JPG")+"/Unknown camera/IMG_3.JPG" ||
		got[1] != "2024/August/05/Beach day/Pixel 8/IMG_1.jpg" ||
		got[2] != "2024/August/05/Beach day/Unknown camera/IMG_2.jpg" {
		t.Fatalf("destinations = %q", got)
	}
	for _, m := range p.Moves {
		if m.State != core.MovePlanned || m.Size == 0 || m.MTime == 0 || m.FileID == 0 {
			t.Errorf("move %+v is missing what the run checks later (state, size, mtime)", m)
		}
	}
}

func eventTitle(t *testing.T, l library, rel string) string {
	t.Helper()
	for _, f := range l.snapshot(0).Files {
		if core.Fold(f.Path) == core.Fold(l.path(rel)) {
			return f.Event
		}
	}
	t.Fatalf("no file %s", rel)
	return ""
}

func TestPlanUsesTheDefaultSchemeAndFallsBackWhereAPhotoHasNoValue(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2022, time.March, 9))
	p := l.plan("{Year}/{Month}/{Place}/{Camera}/{Name}")
	if got := l.destinations(p.Moves); !equalStrings(got, []string{"2022/03/Unknown place/Unknown camera/a.jpg"}) {
		t.Fatalf("destinations = %q", got)
	}
	d := l.plan(DefaultTemplate)
	if got := l.destinations(d.Moves); len(got) != 1 || !strings.HasPrefix(got[0], "2022/03/") || !strings.HasSuffix(got[0], "/a.jpg") {
		t.Fatalf("default scheme destinations = %q", got)
	}
}

func TestPlanNeverMovesAFileOutOfItsOwnSource(t *testing.T) {
	l := newLibrary(t)
	other := filepath.Join(t.TempDir(), "Other")
	src2 := l.addSource(other)
	l.add("a.jpg", "a", day(2024, time.January, 1))
	l.addTo(src2, other, "b.jpg", "b", day(2024, time.January, 1))
	p := l.plan("{Year}/{Name}")
	if len(p.Moves) != 2 {
		t.Fatalf("moves = %+v", p.Moves)
	}
	for _, m := range p.Moves {
		root := l.root
		if filepath.Base(m.From) == "b.jpg" {
			root = other
		}
		if !core.IsWithin(m.To, root) {
			t.Errorf("%s would move to %s, outside its source %s", m.From, m.To, root)
		}
	}
	// One source only.
	only, err := Plan(l.ctx, "{Year}/{Name}", l.snapshot(src2.ID))
	if err != nil || len(only.Moves) != 1 || filepath.Base(only.Moves[0].From) != "b.jpg" {
		t.Fatalf("plan for one source = %+v, %v", only.Moves, err)
	}
}

func TestPlanLeavesOutFilesAlreadyWhereTheSchemePutsThemAndIsStableWhenRepeated(t *testing.T) {
	l := newLibrary(t)
	l.add("2024/a.jpg", "a", day(2024, time.May, 1))
	l.add("b.jpg", "b", day(2024, time.May, 1))
	p := l.plan("{Year}/{Name}")
	if p.InPlace != 1 || len(p.Moves) != 1 || filepath.Base(p.Moves[0].From) != "b.jpg" {
		t.Fatalf("in place %d, moves %+v; want a.jpg in place and b.jpg to move", p.InPlace, p.Moves)
	}
}

func TestPlanGivesCollidingNamesASuffixAndNeverTakesAFreeLookingPathTwice(t *testing.T) {
	l := newLibrary(t)
	l.add("x/IMG_1.jpg", "first", day(2024, time.May, 1))
	l.add("y/IMG_1.jpg", "second", day(2024, time.May, 1))
	l.add("z/img_1.JPG", "third", day(2024, time.May, 1)) // a twin in another case and extension case
	l.add("w/IMG_1.jpg", "fourth", day(2024, time.May, 1))
	p := l.plan("{Year}/{Name}")
	got := l.destinations(p.Moves)
	want := []string{"2024/IMG_1.jpg", "2024/IMG_1 (2).jpg", "2024/img_1 (3).JPG", "2024/IMG_1 (4).jpg"}
	// The moves come in capture order, then by file, which is the order added.
	if !equalStrings(got, want) {
		t.Fatalf("destinations = %q, want %q", got, want)
	}
	if p.Renamed != 3 {
		t.Fatalf("renamed = %d, want 3", p.Renamed)
	}
	for i, m := range p.Moves {
		if m.Renamed != (i > 0) {
			t.Errorf("move %d renamed = %v", i, m.Renamed)
		}
	}
}

func TestPlanAvoidsFilesOnDiskThatTheLibraryDoesNotKnowAndPathsItDoesKnow(t *testing.T) {
	l := newLibrary(t)
	l.add("a/IMG_1.jpg", "a", day(2024, time.May, 1))
	l.add("b/IMG_2.jpg", "b", day(2024, time.May, 1))
	l.add("c/IMG_4.jpg", "c", day(2024, time.May, 1))
	l.write(l.path("2024/IMG_1.jpg"), "someone else's file") // on disk, not in the library
	l.write(l.path("2024/IMG_2.JPG"), "another case")        // a case-folded twin on disk
	// A trashed photo's file is not moved, and a path the library holds is not
	// free even if the file has gone from the disk since.
	trashed := l.add("2024/IMG_4.jpg", "trashed", day(2024, time.May, 1))
	if err := l.st.TrashAssets(l.ctx, []int64{trashed}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(l.path("2024/IMG_4.jpg")); err != nil {
		t.Fatal(err)
	}
	p := l.plan("{Year}/{Name}")
	got := l.destinations(p.Moves)
	want := []string{"2024/IMG_1 (2).jpg", "2024/IMG_2 (2).jpg", "2024/IMG_4 (2).jpg"}
	if !equalStrings(got, want) {
		t.Fatalf("destinations = %q, want %q", got, want)
	}
	if readFile(t, l.path("2024/IMG_1.jpg")) != "someone else's file" {
		t.Fatal("planning touched a file")
	}
}

func TestPlanNeverPlansBlockedTrashedHiddenOrMissingAssets(t *testing.T) {
	l := newLibrary(t)
	l.add("keep.jpg", "keep", day(2024, time.May, 1))
	blocked := l.add("blocked.jpg", "blocked", day(2024, time.May, 1))
	trashed := l.add("trashed.jpg", "trashed", day(2024, time.May, 1))
	l.add("Private/inside.jpg", "inside", day(2024, time.May, 1))
	l.add("gone.jpg", "gone", day(2024, time.May, 1))
	if _, err := l.st.SetBlock(l.ctx, core.BlockAsset, blocked, "", core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	if err := l.st.TrashAssets(l.ctx, []int64{trashed}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.st.SetBlock(l.ctx, core.BlockFolder, 0, l.path("Private"), core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	for _, f := range l.snapshot(0).Files {
		if filepath.Base(f.Path) == "gone.jpg" {
			if err := l.st.RemoveFiles(l.ctx, []int64{f.FileID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	p := l.plan("{Year}/{Name}")
	if got := l.destinations(p.Moves); !equalStrings(got, []string{"2024/keep.jpg"}) {
		t.Fatalf("destinations = %q, want only keep.jpg", got)
	}
}

func TestPlanNeverPutsAFileInsideABlockedFolder(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	l.add("b.jpg", "b", day(2025, time.May, 1))
	if _, err := l.st.SetBlock(l.ctx, core.BlockFolder, 0, l.path("2024"), core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	p := l.plan("{Year}/{Name}")
	if len(p.Moves) != 2 {
		t.Fatalf("moves = %+v", p.Moves)
	}
	if m := p.Moves[0]; m.State != core.MoveSkipped || m.Error != ReasonBlockedFolder {
		t.Errorf("the move into the blocked folder = %+v, want skipped %q", m, ReasonBlockedFolder)
	}
	if m := p.Moves[1]; m.State != core.MovePlanned {
		t.Errorf("the other move = %+v, want planned", m)
	}
}

func TestPlanNeverWritesALabelBlockedPlaceNameToDisk(t *testing.T) {
	l := newLibrary(t)
	a := l.add("a.jpg", "a", day(2024, time.May, 1))
	lat, lon := 24.86, 67.01 // Karachi
	if err := l.st.SaveMetadata(l.ctx, "test", []store.MetadataResult{{AssetID: a, Lat: &lat, Lon: &lon}}); err != nil {
		t.Fatal(err)
	}
	detail, err := l.st.AssetDetail(l.ctx, a, false)
	if err != nil || detail.PlaceID == 0 {
		t.Fatalf("the photo has no place: %+v, %v", detail, err)
	}
	named := l.destinations(l.plan("{Place}/{Name}").Moves)
	if len(named) != 1 || strings.HasPrefix(named[0], FallbackPlace) {
		t.Fatalf("destinations = %q, want the place's name", named)
	}
	if _, err := l.st.SetBlock(l.ctx, core.BlockPlace, detail.PlaceID, "", core.BlockLabel); err != nil {
		t.Fatal(err)
	}
	if got := l.destinations(l.plan("{Place}/{Name}").Moves); !equalStrings(got, []string{FallbackPlace + "/a.jpg"}) {
		t.Fatalf("destinations after blocking the place = %q, want the fallback", got)
	}
	// The event's title is made from the place too.
	if got := l.destinations(l.plan("{Event}/{Name}").Moves); len(got) != 1 || strings.Contains(got[0], named[0][:strings.IndexByte(named[0], '/')]) {
		t.Fatalf("the event folder %q names the blocked place", got)
	}
}

func TestPlanSkipsAFileWhenSomethingThatIsNotAFolderIsInTheWayOfItsFolder(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	l.write(l.path("2024"), "a file where the folder would go")
	p := l.plan("{Year}/{Name}")
	if len(p.Moves) != 1 || p.Moves[0].State != core.MoveSkipped || p.Moves[0].Error != ReasonNotAFolder {
		t.Fatalf("moves = %+v, want one skipped %q", p.Moves, ReasonNotAFolder)
	}
}

func TestPlanSaysWhenAFileIsMissingOrChangedSinceTheLibraryLastSawIt(t *testing.T) {
	l := newLibrary(t)
	l.add("gone.jpg", "gone", day(2024, time.May, 1))
	l.add("changed.jpg", "changed", day(2024, time.May, 1))
	l.add("fine.jpg", "fine", day(2024, time.May, 1))
	if err := os.Remove(l.path("gone.jpg")); err != nil {
		t.Fatal(err)
	}
	l.write(l.path("changed.jpg"), "changed on disk after the scan, longer")
	p := l.plan("{Year}/{Name}")
	reasons := map[string]core.OrganiseMoveState{}
	why := map[string]string{}
	for _, m := range p.Moves {
		reasons[filepath.Base(m.From)] = m.State
		why[filepath.Base(m.From)] = m.Error
	}
	if why["gone.jpg"] != ReasonMissing || why["changed.jpg"] != ReasonChanged || reasons["fine.jpg"] != core.MovePlanned {
		t.Fatalf("outcomes = %v / %v", reasons, why)
	}
}

func TestPlanSkipsAPathThatWouldBeTooLongForWindows(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	long := strings.Repeat("{Event}/", 4) + "{Name}"
	a := l.assetAt(l.path("a.jpg"))
	l.setEventName(a, strings.Repeat("e", 78))
	p := l.plan(long)
	if len(p.Moves) != 1 || p.Moves[0].State != core.MoveSkipped || p.Moves[0].Error != ReasonTooLong {
		t.Fatalf("moves = %+v, want one skipped %q", p.Moves, ReasonTooLong)
	}
}

func TestPlanMakesNamesWindowsAccepts(t *testing.T) {
	l := newLibrary(t)
	a := l.add("a.jpg", "a", day(2024, time.May, 1))
	l.setEventName(a, `Eid: "Mubarak"? <3> | a/b\c...`)
	l.setCamera(a, "CON")
	p := l.plan("{Event}/{Camera}/{Name}")
	got := l.destinations(p.Moves)
	if len(got) != 1 || got[0] != `Eid_ _Mubarak__ _3_ _ a_b_c/_CON/a.jpg` {
		t.Fatalf("destinations = %q", got)
	}
}

func TestPlanWithABadTemplateFailsBeforeLookingAtAnyFile(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	if _, err := Plan(l.ctx, "{Year}/{Nonsense}", l.snapshot(0)); !errors.Is(err, ErrInvalidTemplate) {
		t.Fatalf("a bad scheme gave %v, want ErrInvalidTemplate", err)
	}
}

func TestPlanStopsWhenItsContextEnds(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Plan(ctx, "{Name}", l.snapshot(0)); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled plan gave %v", err)
	}
}

func TestPlanMovesEveryFileOfAnAssetWithExactCopies(t *testing.T) {
	l := newLibrary(t)
	l.add("one/a.jpg", "same", day(2024, time.May, 1))
	l.add("two/a.jpg", "same", day(2024, time.May, 1))
	p := l.plan("{Year}/{Name}")
	if got := l.destinations(p.Moves); !equalStrings(got, []string{"2024/a.jpg", "2024/a (2).jpg"}) {
		t.Fatalf("destinations = %q", got)
	}
	if p.Moves[0].AssetID != p.Moves[1].AssetID {
		t.Fatal("the copies are not one asset")
	}
}

func TestPlanOnlyReadsTheDisk(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	before := readFile(t, l.path("a.jpg"))
	l.plan("{Year}/{Name}")
	if exist(l.path("2024")) || readFile(t, l.path("a.jpg")) != before {
		t.Fatal("a plan changed the disk")
	}
}

func TestPlanCountsTheSidecarsThatMoveWithTheirPhotos(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	l.add("b.jpg", "b", day(2024, time.May, 2))
	l.add("d.jpg", "d", day(2024, time.May, 3))
	l.write(l.path("a.jpg.xmp"), "a's sidecar")
	l.write(l.path("b.jpg.XMP"), "b's sidecar, in capitals") // the file system folds case
	l.write(l.path("d.xmp"), "not d's sidecar: its name lacks the extension")
	p := l.plan("{Year}/{Name}")
	if p.Sidecars != 2 {
		t.Fatalf("sidecars = %d, want 2 (a's and b's)", p.Sidecars)
	}
	want := []string{store.SidecarPlanned, store.SidecarPlanned, ""}
	for i, m := range p.Moves {
		if m.Sidecar != want[i] {
			t.Errorf("move %d sidecar = %q, want %q", i, m.Sidecar, want[i])
		}
	}
}

func TestPlanNeverPutsAPhotoBesideASidecarThatIsNotItsOwnNorTakesItsSidecarsName(t *testing.T) {
	l := newLibrary(t)
	l.add("c.jpg", "c", day(2024, time.May, 1))
	l.add("d.jpg", "d", day(2024, time.May, 2))
	l.write(l.path("c.jpg.xmp"), "c's sidecar")
	l.write(l.path("2024/c.jpg.xmp"), "someone else's sidecar, with no photo beside it")
	l.write(l.path("2024/d.JPG.xmp"), "a foreign sidecar in another case, for a photo that is not here")
	p := l.plan("{Year}/{Name}")
	got := l.destinations(p.Moves)
	if want := []string{"2024/c (2).jpg", "2024/d (2).jpg"}; !equalStrings(got, want) {
		t.Fatalf("destinations = %q, want %q", got, want)
	}
	if p.Moves[0].Sidecar != store.SidecarPlanned || p.Moves[1].Sidecar != "" {
		t.Errorf("sidecars = %q, %q; want c's planned and d without", p.Moves[0].Sidecar, p.Moves[1].Sidecar)
	}
	if p.Sidecars != 1 || p.Renamed != 2 {
		t.Errorf("sidecars %d, renamed %d; want 1 and 2", p.Sidecars, p.Renamed)
	}
}

func TestPlanReservesTheSidecarNameOfEveryPlannedPhoto(t *testing.T) {
	l := newLibrary(t)
	// Two photos that would both be IMG_1.jpg: the second must not take the
	// first's sidecar name either, nor may it be a name whose sidecar the
	// first has reserved.
	l.add("x/IMG_1.jpg", "one", day(2024, time.May, 1))
	l.add("y/IMG_1.jpg", "two", day(2024, time.May, 2))
	l.write(l.path("x/IMG_1.jpg.xmp"), "one's sidecar")
	l.write(l.path("y/IMG_1.jpg.xmp"), "two's sidecar")
	p := l.plan("{Year}/{Name}")
	if got := l.destinations(p.Moves); !equalStrings(got, []string{"2024/IMG_1.jpg", "2024/IMG_1 (2).jpg"}) || p.Sidecars != 2 {
		t.Fatalf("destinations = %q with %d sidecars", got, p.Sidecars)
	}
}

func TestPlanCountsTheSidecarsNameInTheLengthOfAPath(t *testing.T) {
	l := newLibrary(t)
	long := strings.Repeat("e", 70)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	l.add("b.jpg", "b", day(2024, time.May, 2))
	// root\pad\long\a.jpg is exactly the longest path Windows allows here.
	room := maxPathLen - (len(l.root) + 1 + 1 + len(long) + 1 + len("a.jpg"))
	pad := strings.Repeat("p", max(room, 0))
	template := pad + "/" + long + "/{Name}"
	l.write(l.path("a.jpg.xmp"), "a's sidecar")
	p := l.plan(template)
	if len(p.Moves) != 2 {
		t.Fatalf("moves = %+v", p.Moves)
	}
	if p.Moves[0].State != core.MoveSkipped || p.Moves[0].Error != ReasonTooLong {
		t.Errorf("the photo whose sidecar name passes the limit = %+v, want skipped %q", p.Moves[0], ReasonTooLong)
	}
	if p.Moves[1].State != core.MovePlanned {
		t.Errorf("the photo without a sidecar fits and = %+v, want planned", p.Moves[1])
	}
}

func TestPlanGivesNoFolderOrFileANameTheScannerSkips(t *testing.T) {
	l := newLibrary(t)
	a := l.add("a.jpg", "a", day(2024, time.May, 1))
	l.setEventName(a, "$Recycle.Bin")
	l.setCamera(a, "System Volume Information")
	b := l.add("b.jpg", "b", day(2025, time.May, 1))
	l.setEventName(b, ".hidden trip")
	c := l.add("c.jpg", "c", day(2026, time.May, 1))
	l.setEventName(c, "..")
	p := l.plan("{Event}/{Camera}/{Name}")
	got := l.destinations(p.Moves)
	want := []string{
		"_$Recycle.Bin/_System Volume Information/a.jpg",
		"hidden trip/Unknown camera/b.jpg",
		"Unknown event/Unknown camera/c.jpg",
	}
	if !equalStrings(got, want) {
		t.Fatalf("destinations = %q, want %q", got, want)
	}
	for _, m := range p.Moves {
		if rel, _ := filepath.Rel(l.root, m.To); scannerSkips(rel) {
			t.Errorf("%s would lose the file at the next scan", rel)
		}
	}
}

// scannerSkips reports whether the scanner would leave out any part of the
// relative path.
func scannerSkips(rel string) bool {
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if scanner.SkipName(part) {
			return true
		}
	}
	return false
}
