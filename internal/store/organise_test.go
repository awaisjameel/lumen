package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// organiseFixture is a library of virtual paths (the store never touches the
// disk): photos with places, cameras and an event name, and the kinds of
// asset a plan must leave alone.
type organiseFixture struct {
	t   *testing.T
	st  *Store
	ctx context.Context
	src core.Source
	ids map[string]int64 // asset by file name
}

const organiseDay = 1_700_000_000

func newOrganiseFixture(t *testing.T) organiseFixture {
	t.Helper()
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	off, err := st.AddSource(ctx, `C:\Off`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\beach1.jpg`, "beach1"), item(src.ID, `C:\Photos\beach2.jpg`, "beach2"),
		item(src.ID, `C:\Photos\later.jpg`, "later"),
		item(src.ID, `C:\Photos\hidden.jpg`, "hidden"), item(src.ID, `C:\Photos\trashed.jpg`, "trashed"),
		item(src.ID, `C:\Photos\blocked.jpg`, "blocked"), item(src.ID, `C:\Photos\Private\inside.jpg`, "inside"),
		item(off.ID, `C:\Off\off.jpg`, "off"),
	})))
	f := organiseFixture{t: t, st: st, ctx: ctx, src: src, ids: map[string]int64{}}
	for _, name := range []string{"beach1", "beach2", "later", "hidden", "trashed", "blocked", "inside", "off"} {
		var id int64
		check(t, st.r.QueryRowContext(ctx, "SELECT asset_id FROM files WHERE path LIKE ?", `%\`+name+".jpg").Scan(&id))
		f.ids[name] = id
	}
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE sources SET enabled = 0 WHERE id = ?", off.ID)))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET taken_at = ?, camera = 'Pixel 8', place_id = 1 WHERE id IN (?, ?)",
		organiseDay, f.ids["beach1"], f.ids["beach2"])))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET taken_at = ? WHERE id = ?", organiseDay+3600, f.ids["beach2"])))
	check(t, errOf(st.w.ExecContext(ctx, `INSERT INTO places(id, name, region, country, lat, lon)
		VALUES (1, 'Karachi', 'Sindh', 'Pakistan', 24.8, 67.0), (2, 'Lahore', 'Punjab', 'Pakistan', 31.5, 74.3)`)))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET taken_at = ?, place_id = 2 WHERE id = ?", organiseDay+5*86400, f.ids["later"])))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", f.ids["hidden"])))
	check(t, st.TrashAssets(ctx, []int64{f.ids["trashed"]}))
	_, err = st.SetBlock(ctx, core.BlockAsset, f.ids["blocked"], "", core.BlockPhotos)
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)
	check(t, err)
	return f
}

func (f organiseFixture) snapshot(t *testing.T, sourceID int64) OrganiseSnapshot {
	t.Helper()
	snap, err := f.st.OrganiseSnapshot(f.ctx, sourceID, core.EventGapDefault)
	check(t, err)
	return snap
}

func filesByName(snap OrganiseSnapshot) map[string]OrganiseFile {
	out := map[string]OrganiseFile{}
	for _, file := range snap.Files {
		out[file.Path[len(`C:\Photos\`):]] = file
	}
	return out
}

func TestOrganiseSnapshotLeavesOutBlockedTrashedHiddenMissingAndDisabledFiles(t *testing.T) {
	f := newOrganiseFixture(t)
	// A photo whose only file is forgotten is missing, and has nothing to move.
	var gone int64
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT id FROM files WHERE path = ?", `C:\Photos\later.jpg`).Scan(&gone))
	check(t, f.st.RemoveFiles(f.ctx, []int64{gone}))

	snap := f.snapshot(t, 0)
	var names []string
	for _, file := range snap.Files {
		names = append(names, file.Path)
	}
	slices.Sort(names)
	want := []string{`C:\Photos\beach1.jpg`, `C:\Photos\beach2.jpg`}
	if !slices.Equal(names, want) {
		t.Fatalf("planned files = %v, want %v", names, want)
	}
	// Every path the library holds in scope is known, so no destination can
	// take one, whatever its photo's state; the disabled source is out of scope.
	for _, path := range []string{`C:\Photos\hidden.jpg`, `C:\Photos\TRASHED.jpg`, `C:\Photos\Private\inside.jpg`} {
		if !snap.Known[core.Fold(path)] {
			t.Errorf("the library's file %s is not among the known paths", path)
		}
	}
	if snap.Known[core.Fold(`C:\Off\off.jpg`)] {
		t.Error("a disabled source's file is among the known paths")
	}
	if !slices.Equal(snap.BlockedDirs, []string{`C:\Photos\Private`}) {
		t.Errorf("blocked folders = %v", snap.BlockedDirs)
	}
}

func TestOrganiseSnapshotCanBeLimitedToOneSource(t *testing.T) {
	f := newOrganiseFixture(t)
	other, err := f.st.AddSource(f.ctx, `C:\Other`)
	check(t, err)
	check(t, errOf(f.st.Ingest(f.ctx, []IngestItem{item(other.ID, `C:\Other\o.jpg`, "o")})))
	if got := len(f.snapshot(t, other.ID).Files); got != 1 {
		t.Fatalf("files of one source = %d, want 1", got)
	}
	if got := len(f.snapshot(t, 0).Files); got != 4 { // beach1, beach2, later, o.jpg
		t.Fatalf("files of every source = %d, want 4", got)
	}
}

func TestOrganiseSnapshotUsesTheNamesTheViewsShow(t *testing.T) {
	f := newOrganiseFixture(t)
	check(t, f.st.SetEventName(f.ctx, f.ids["beach1"], "Beach day", core.EventGapDefault))
	files := filesByName(f.snapshot(t, 0))
	for _, name := range []string{"beach1.jpg", "beach2.jpg"} {
		file := files[name]
		if file.Event != "Beach day" || file.Place != "Karachi" || file.Camera != "Pixel 8" {
			t.Errorf("%s = %+v, want the named event, its place and camera", name, file)
		}
	}
	// The later photo is in another event, still unnamed: it has the title the
	// Events view generates for it, and its own place.
	later := files["later.jpg"]
	if later.Event == "" || later.Event == "Beach day" || later.Place != "Lahore" {
		t.Errorf("later.jpg = %+v, want its own event title and place", later)
	}
}

func TestOrganiseSnapshotNeverNamesABlockedPlace(t *testing.T) {
	f := newOrganiseFixture(t)
	_, err := f.st.SetBlock(f.ctx, core.BlockPlace, 1, "", core.BlockLabel)
	check(t, err)
	files := filesByName(f.snapshot(t, 0))
	if got := files["beach1.jpg"]; got.Place != "" {
		t.Fatalf("a label-blocked place reached the snapshot: %q", got.Place)
	}
	// The label-blocked place also drops out of the event's title.
	if got := files["beach1.jpg"].Event; got == "" || strings.Contains(core.Fold(got), "karachi") {
		t.Fatalf("the event title %q names a blocked place", got)
	}
	if got := files["later.jpg"].Place; got != "Lahore" {
		t.Fatalf("an unblocked place was lost: %q", got)
	}
}

func movesOf(paths ...string) []OrganiseMove {
	var moves []OrganiseMove
	for i := 0; i+1 < len(paths); i += 2 {
		moves = append(moves, OrganiseMove{FileID: int64(i/2 + 1), AssetID: int64(i/2 + 1), From: paths[i], To: paths[i+1],
			Size: 1, MTime: 1, State: core.MovePlanned})
	}
	return moves
}

func TestSavingAPlanReplacesEarlierPreviewsButNeverARunThatStarted(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	first, err := st.SaveOrganisePlan(ctx, "{Year}/{Name}", movesOf(`C:\a`, `C:\b`))
	check(t, err)
	check(t, st.SetOrganiseRunState(ctx, first, core.OrganiseDone))
	second, err := st.SaveOrganisePlan(ctx, "{Year}/{Name}", movesOf(`C:\c`, `C:\d`, `C:\e`, `C:\f`))
	check(t, err)
	third, err := st.SaveOrganisePlan(ctx, "{Name}", movesOf(`C:\g`, `C:\h`))
	check(t, err)

	if _, err := st.OrganiseRun(ctx, second); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an older preview survived a newer one: %v", err)
	}
	moves, err := st.OrganiseMoves(ctx, second, false)
	check(t, err)
	if len(moves) != 0 {
		t.Fatalf("the replaced preview kept %d moves", len(moves))
	}
	run, err := st.OrganiseRun(ctx, first)
	check(t, err)
	if run.State != core.OrganiseDone || run.Total != 1 {
		t.Fatalf("a finished run was touched: %+v", run)
	}
	runs, err := st.OrganiseRuns(ctx)
	check(t, err)
	if len(runs) != 1 || runs[0].ID != first {
		t.Fatalf("past runs = %+v, want only the finished one (a preview is not a past run)", runs)
	}
	if third == first {
		t.Fatal("ids were reused")
	}
}

func TestSavingAPlanNumbersTheMovesFromOneInOrder(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	moves := movesOf(`C:\a`, `C:\b`, `C:\c`, `C:\d`)
	moves[1].State, moves[1].Error = core.MoveSkipped, "missing"
	moves[0].Renamed = true
	run, err := st.SaveOrganisePlan(ctx, "t", moves)
	check(t, err)
	if moves[0].Seq != 1 || moves[1].Seq != 2 {
		t.Fatalf("seq = %d, %d; want 1, 2", moves[0].Seq, moves[1].Seq)
	}
	got, err := st.OrganiseMoves(ctx, run, false)
	check(t, err)
	if len(got) != 2 || got[0] != moves[0] || got[1] != moves[1] {
		t.Fatalf("stored moves = %+v, want %+v", got, moves)
	}
	back, err := st.OrganiseMoves(ctx, run, false, core.MoveSkipped)
	check(t, err)
	if len(back) != 1 || back[0].Error != "missing" {
		t.Fatalf("skipped moves = %+v", back)
	}
}

func TestAMoveCanOnlyBeJournaledFromTheStateItIsIn(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	run, err := st.SaveOrganisePlan(ctx, "t", movesOf(`C:\a`, `C:\b`))
	check(t, err)
	check(t, st.JournalOrganiseMove(ctx, run, 1, core.MovePlanned, core.MoveMoving, ""))
	if err := st.JournalOrganiseMove(ctx, run, 1, core.MovePlanned, core.MoveMoving, ""); !errors.Is(err, ErrOrganiseState) {
		t.Fatalf("journaling a move twice = %v, want ErrOrganiseState", err)
	}
	if err := st.JournalOrganiseMove(ctx, run, 9, core.MovePlanned, core.MoveMoving, ""); !errors.Is(err, ErrOrganiseState) {
		t.Fatalf("journaling a move that is not there = %v, want ErrOrganiseState", err)
	}
}

func TestFinishingAMovePointsTheFileAtItsNewPlaceAndKeepsEverythingAttachedToTheAsset(t *testing.T) {
	f := newOrganiseFixture(t)
	a := f.ids["beach1"]
	album, err := f.st.CreateAlbum(f.ctx, "Holiday", core.AlbumManual, core.AlbumRule{}, []int64{a})
	check(t, err)
	tag, err := f.st.CreateTag(f.ctx, "Sea", core.TagManual, nil)
	check(t, err)
	check(t, f.st.TagAsset(f.ctx, a, tag))
	check(t, f.st.SaveFaces(f.ctx, "f1", "f1", []FaceResult{{AssetID: a, Faces: []DetectedFace{face(0.1, 1)}}}))
	person, err := f.st.NameFaces(f.ctx, []int64{facesOf(t, f.st, a)[0].ID}, "Alice")
	check(t, err)
	_, err = f.st.SetBlock(f.ctx, core.BlockPerson, person, "", core.BlockLabel)
	check(t, err)

	var fileID int64
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT id FROM files WHERE path = ?", `C:\Photos\beach1.jpg`).Scan(&fileID))
	move := OrganiseMove{FileID: fileID, AssetID: a, From: `C:\Photos\beach1.jpg`, To: `C:\Photos\2023\11\beach1.jpg`,
		Size: 6, MTime: 1, State: core.MovePlanned}
	moves := []OrganiseMove{move}
	run, err := f.st.SaveOrganisePlan(f.ctx, "t", moves)
	check(t, err)
	move.Seq = moves[0].Seq
	check(t, f.st.JournalOrganiseMove(f.ctx, run, move.Seq, core.MovePlanned, core.MoveMoving, ""))
	check(t, f.st.FinishOrganiseMove(f.ctx, run, move, move.From, move.To, core.MoveMoved, OrganiseSidecar{}))

	detail, err := f.st.AssetDetail(f.ctx, a, false)
	check(t, err)
	if !slices.Equal(detail.Paths, []string{`C:\Photos\2023\11\beach1.jpg`}) {
		t.Fatalf("the asset's paths = %v, want the new one only", detail.Paths)
	}
	stored, err := f.st.OrganiseMoves(f.ctx, run, false)
	check(t, err)
	if stored[0].State != core.MoveMoved {
		t.Fatalf("move state = %s, want moved", stored[0].State)
	}
	if albums, err := f.st.AssetAlbums(f.ctx, a); err != nil || !slices.Equal(albums, []int64{album}) {
		t.Fatalf("albums after the move = %+v, %v", albums, err)
	}
	if tags, err := f.st.AssetTags(f.ctx, a, core.TagConfidenceDefault); err != nil || len(tags) == 0 {
		t.Fatalf("tags after the move = %+v, %v", tags, err)
	}
	var named int64
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT COALESCE(person_id, 0) FROM faces WHERE asset_id = ?", a).Scan(&named))
	if named != person {
		t.Fatalf("the face belongs to person %d after the move, want %d", named, person)
	}
	blocks, err := f.st.Blocks(f.ctx)
	check(t, err)
	if !slices.ContainsFunc(blocks, func(b core.Block) bool { return b.Kind == core.BlockPerson && b.TargetID == person }) {
		t.Fatalf("the person's block was lost: %+v", blocks)
	}

	// Putting it back is the same step the other way.
	check(t, f.st.JournalOrganiseMove(f.ctx, run, move.Seq, core.MoveMoved, core.MoveUndoing, ""))
	check(t, f.st.FinishOrganiseMove(f.ctx, run, move, move.To, move.From, core.MoveUndone, OrganiseSidecar{}))
	detail, err = f.st.AssetDetail(f.ctx, a, false)
	check(t, err)
	if !slices.Equal(detail.Paths, []string{`C:\Photos\beach1.jpg`}) {
		t.Fatalf("the asset's paths after undo = %v", detail.Paths)
	}
}

func TestFinishingAMoveForAFileTheLibraryForgotChangesNoOtherFile(t *testing.T) {
	f := newOrganiseFixture(t)
	var fileID int64
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT id FROM files WHERE path = ?", `C:\Photos\beach1.jpg`).Scan(&fileID))
	move := OrganiseMove{FileID: fileID, AssetID: f.ids["beach1"], From: `C:\Photos\elsewhere.jpg`, To: `C:\Photos\x.jpg`, State: core.MovePlanned}
	moves := []OrganiseMove{move}
	run, err := f.st.SaveOrganisePlan(f.ctx, "t", moves)
	check(t, err)
	move.Seq = moves[0].Seq
	// The row for FileID is at another path than the move says: it is left alone.
	check(t, f.st.FinishOrganiseMove(f.ctx, run, move, move.From, move.To, core.MoveMoved, OrganiseSidecar{}))
	var path string
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT path FROM files WHERE id = ?", fileID).Scan(&path))
	if path != `C:\Photos\beach1.jpg` {
		t.Fatalf("a file row that was not at the old path was changed to %s", path)
	}
}

func TestAMoveIsRefusedWhenItsPhotoIsNoLongerVisibleOrItsDestinationIsBlocked(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(f organiseFixture, m *OrganiseMove)
		want  string
	}{
		{"nothing changed", func(organiseFixture, *OrganiseMove) {}, ""},
		{"blocked since the preview", func(f organiseFixture, m *OrganiseMove) {
			_, err := f.st.SetBlock(f.ctx, core.BlockAsset, m.AssetID, "", core.BlockPhotos)
			check(f.t, err)
		}, MoveRefusedNotVisible},
		{"trashed since the preview", func(f organiseFixture, m *OrganiseMove) {
			check(f.t, f.st.TrashAssets(f.ctx, []int64{m.AssetID}))
		}, MoveRefusedNotVisible},
		{"hidden since the preview", func(f organiseFixture, m *OrganiseMove) {
			check(f.t, errOf(f.st.w.ExecContext(f.ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", m.AssetID)))
		}, MoveRefusedNotVisible},
		{"the library lost the file", func(f organiseFixture, m *OrganiseMove) {
			check(f.t, f.st.RemoveFiles(f.ctx, []int64{m.FileID}))
		}, MoveRefusedMissing},
		{"the file is at another path now", func(_ organiseFixture, m *OrganiseMove) {
			m.From = `C:\Photos\moved-by-hand.jpg`
		}, MoveRefusedMissing},
		{"the destination is in a folder blocked since the preview", func(f organiseFixture, _ *OrganiseMove) {
			_, err := f.st.SetBlock(f.ctx, core.BlockFolder, 0, `C:\Photos\2023`, core.BlockPhotos)
			check(f.t, err)
		}, MoveRefusedBlockedFolder},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newOrganiseFixture(t)
			path := `C:\Photos\beach1.jpg`
			m := OrganiseMove{AssetID: f.ids["beach1"], From: path, To: `C:\Photos\2023\11\beach1.jpg`}
			check(t, f.st.r.QueryRowContext(f.ctx, "SELECT id FROM files WHERE path = ?", path).Scan(&m.FileID))
			c.setup(f, &m)
			got, err := f.st.OrganiseMovable(f.ctx, m)
			check(t, err)
			if got != c.want {
				t.Errorf("refusal %q, want %q", got, c.want)
			}
		})
	}
}

func TestFoldersAMadeRunCreatedComeBackDeepestFirstAndCanBeForgotten(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	run, err := st.SaveOrganisePlan(ctx, "t", movesOf(`C:\a`, `C:\b`))
	check(t, err)
	for _, dir := range []string{`C:\Photos\2023`, `C:\Photos\2023\11\Beach`, `C:\Photos\2023\11`} {
		check(t, st.AddOrganiseDir(ctx, run, dir))
	}
	check(t, st.AddOrganiseDir(ctx, run, `C:\Photos\2023`)) // journaling twice is harmless
	dirs, err := st.OrganiseDirs(ctx, run)
	check(t, err)
	if want := []string{`C:\Photos\2023\11\Beach`, `C:\Photos\2023\11`, `C:\Photos\2023`}; !slices.Equal(dirs, want) {
		t.Fatalf("folders = %v, want %v", dirs, want)
	}
	check(t, st.DropOrganiseDir(ctx, run, `C:\Photos\2023\11\Beach`))
	if dirs, err = st.OrganiseDirs(ctx, run); err != nil || len(dirs) != 2 {
		t.Fatalf("folders after forgetting one = %v, %v", dirs, err)
	}
}

func TestPastRunsCountTheirMovedFilesAndTheirTotal(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	moves := movesOf(`C:\a`, `C:\b`, `C:\c`, `C:\d`, `C:\e`, `C:\f`)
	run, err := st.SaveOrganisePlan(ctx, "{Year}", moves)
	check(t, err)
	check(t, st.SetOrganiseRunState(ctx, run, core.OrganiseDone))
	check(t, st.SetOrganiseMove(ctx, run, 1, core.MoveMoved, ""))
	check(t, st.SetOrganiseMove(ctx, run, 2, core.MoveMoved, ""))
	check(t, st.SetOrganiseMove(ctx, run, 3, core.MoveSkipped, "changed"))
	runs, err := st.OrganiseRuns(ctx)
	check(t, err)
	if len(runs) != 1 || runs[0].Moved != 2 || runs[0].Total != 3 || runs[0].Template != "{Year}" || runs[0].State != core.OrganiseDone {
		t.Fatalf("runs = %+v, want one with 2 moved of 3", runs)
	}
	counts, err := st.OrganiseCounts(ctx, run)
	check(t, err)
	if counts[core.MoveMoved] != 2 || counts[core.MoveSkipped] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestInterruptedRunsAreThoseRunningUndoingOrHoldingAMoveInDoubt(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	idle, err := st.SaveOrganisePlan(ctx, "t", movesOf(`C:\a`, `C:\b`))
	check(t, err)
	check(t, st.SetOrganiseRunState(ctx, idle, core.OrganiseDone))
	running, err := st.SaveOrganisePlan(ctx, "t", movesOf(`C:\c`, `C:\d`))
	check(t, err)
	check(t, st.SetOrganiseRunState(ctx, running, core.OrganiseRunning))
	failedInDoubt, err := st.SaveOrganisePlan(ctx, "t", movesOf(`C:\e`, `C:\f`))
	check(t, err)
	check(t, st.SetOrganiseRunState(ctx, failedInDoubt, core.OrganiseFailed))
	check(t, st.JournalOrganiseMove(ctx, failedInDoubt, 1, core.MovePlanned, core.MoveMoving, ""))
	got, err := st.InterruptedOrganiseRuns(ctx)
	check(t, err)
	if want := []int64{running, failedInDoubt}; !slices.Equal(got, want) {
		t.Fatalf("interrupted runs = %v, want %v", got, want)
	}
}

func TestOrganiseTablesFollowTheirRunWhenItIsDeleted(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	run, err := st.SaveOrganisePlan(ctx, "t", movesOf(`C:\a`, `C:\b`))
	check(t, err)
	check(t, st.AddOrganiseDir(ctx, run, `C:\x`))
	// A newer preview deletes the older one: its moves and folders go with it.
	_, err = st.SaveOrganisePlan(ctx, "t", movesOf(`C:\c`, `C:\d`))
	check(t, err)
	var n int
	check(t, st.r.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM organise_moves WHERE run_id = ?) + (SELECT COUNT(*) FROM organise_dirs WHERE run_id = ?)", run, run).Scan(&n))
	if n != 0 {
		t.Fatalf("%d journal rows outlived their run", n)
	}
}

func TestFinishingAMoveDropsAStaleRowAtItsNewPathSoTheCommitNeverWedges(t *testing.T) {
	f := newOrganiseFixture(t)
	// A row for a file that is not on disk holds the path the move is taking.
	check(t, errOf(f.st.Ingest(f.ctx, []IngestItem{item(f.src.ID, `C:\Photos\2023\11\beach1.jpg`, "a ghost")})))
	var ghost, fileID int64
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT asset_id FROM files WHERE path = ?", `C:\Photos\2023\11\beach1.jpg`).Scan(&ghost))
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT id FROM files WHERE path = ?", `C:\Photos\beach1.jpg`).Scan(&fileID))

	move := OrganiseMove{FileID: fileID, AssetID: f.ids["beach1"], From: `C:\Photos\beach1.jpg`, To: `C:\Photos\2023\11\beach1.jpg`,
		Size: 6, MTime: 1, State: core.MovePlanned}
	moves := []OrganiseMove{move}
	run, err := f.st.SaveOrganisePlan(f.ctx, "t", moves)
	check(t, err)
	move.Seq = moves[0].Seq
	check(t, f.st.JournalOrganiseMove(f.ctx, run, move.Seq, core.MovePlanned, core.MoveMoving, ""))
	check(t, f.st.FinishOrganiseMove(f.ctx, run, move, move.From, move.To, core.MoveMoved, OrganiseSidecar{}))

	detail, err := f.st.AssetDetail(f.ctx, f.ids["beach1"], false)
	check(t, err)
	if !slices.Equal(detail.Paths, []string{`C:\Photos\2023\11\beach1.jpg`}) {
		t.Fatalf("the moved asset's paths = %v", detail.Paths)
	}
	var missing bool
	check(t, f.st.r.QueryRowContext(f.ctx, "SELECT missing_since IS NOT NULL FROM assets WHERE id = ?", ghost).Scan(&missing))
	if !missing {
		t.Fatal("the asset of the stale row, which has no other file, was not marked missing")
	}
}

func TestPastRunsCountTheSidecarsThatStayedBehind(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	moves := movesOf(`C:\a`, `C:\b`, `C:\c`, `C:\d`, `C:\e`, `C:\f`)
	run, err := st.SaveOrganisePlan(ctx, "t", moves)
	check(t, err)
	for seq, side := range map[int]OrganiseSidecar{
		1: {State: SidecarMoved, From: `C:\a.xmp`, To: `C:\b.xmp`},
		2: {State: SidecarKept, Reason: "sidecar_taken"},
		3: {State: SidecarKept, Reason: "sidecar_taken"},
	} {
		m := moves[seq-1]
		m.Seq = seq
		check(t, st.JournalOrganiseMove(ctx, run, seq, core.MovePlanned, core.MoveMoving, SidecarPlanned))
		check(t, st.FinishOrganiseMove(ctx, run, m, m.From, m.To, core.MoveMoved, side))
	}
	check(t, st.SetOrganiseRunState(ctx, run, core.OrganiseDone))
	runs, err := st.OrganiseRuns(ctx)
	check(t, err)
	if len(runs) != 1 || runs[0].SidecarsKept != 2 || runs[0].Moved != 3 {
		t.Fatalf("runs = %+v, want 3 moved with 2 sidecars kept", runs)
	}
	assets, err := st.OrganiseMovedAssets(ctx, run)
	check(t, err)
	if !slices.Equal(assets, []int64{1, 2, 3}) {
		t.Fatalf("moved assets = %v, want 1, 2, 3", assets)
	}
}

func TestRetryFailedOfRequeuesTheFailedJobsOfTheGivenAssetsOnly(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Media`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Media\a.jpg`, "a"), item(src.ID, `C:\Media\b.jpg`, "b")})))
	for range MaxAttempts {
		jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
		check(t, err)
		var results []MetadataResult
		for _, j := range jobs {
			results = append(results, MetadataResult{AssetID: j.AssetID, Err: "the file was not there"})
		}
		check(t, st.SaveMetadata(ctx, "v1", results))
	}
	if jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10); err != nil || len(jobs) != 0 {
		t.Fatalf("jobs still pending after %d failures: %+v, %v", MaxAttempts, jobs, err)
	}
	n, err := st.RetryFailedOf(ctx, []int64{1})
	check(t, err)
	if n == 0 {
		t.Fatal("nothing was requeued")
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	check(t, err)
	if len(jobs) != 1 || jobs[0].AssetID != 1 {
		t.Fatalf("pending after the retry = %+v, want only asset 1", jobs)
	}
	if n, err := st.RetryFailedOf(ctx, nil); err != nil || n != 0 {
		t.Fatalf("retry of no assets = %d, %v", n, err)
	}
}
