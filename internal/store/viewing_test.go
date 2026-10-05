package store

import (
	"errors"
	"reflect"
	"testing"

	"lumen/internal/core"
)

func viewingFixture(t *testing.T) (st *Store, ids []int64) {
	t.Helper()
	st = openTest(t)
	src, err := st.AddSource(t.Context(), `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(t.Context(), []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
	sel, err := st.Selection(t.Context(), core.View{}, nil, false)
	check(t, err)
	return st, sel.IDs
}

func TestPlaybackWritesRequireTheCurrentOptInAndVisibleVideo(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Videos`)
	check(t, err)
	in := item(src.ID, `C:\Videos\clip.mp4`, "resume-video")
	in.Kind = core.KindVideo
	check(t, errOf(st.Ingest(ctx, []IngestItem{in})))
	selection, err := st.Selection(ctx, core.View{}, nil, false)
	check(t, err)
	id := selection.IDs[0]
	check(t, st.SaveMetadata(ctx, "fixture", []MetadataResult{{AssetID: id, Duration: 12_000}}))
	check(t, st.SetPlaybackPosition(ctx, id, 5000))
	position, err := st.PlaybackPosition(ctx, id)
	check(t, err)
	if position != 0 {
		t.Fatal("recorded history before opting in")
	}
	p := core.DefaultPreferences()
	p.Viewer.Resume = true
	_, err = st.SetPreferences(ctx, p)
	check(t, err)
	check(t, st.SetPlaybackPosition(ctx, id, 50_000))
	position, err = st.PlaybackPosition(ctx, id)
	check(t, err)
	if position != 12_000 {
		t.Fatalf("unbounded playback position: %d", position)
	}
	p.Viewer.Resume = false
	_, err = st.SetPreferences(ctx, p)
	check(t, err)
	check(t, st.ClearPlaybackHistory(ctx))
	check(t, st.SetPlaybackPosition(ctx, id, 8000))
	position, err = st.PlaybackPosition(ctx, id)
	check(t, err)
	if position != 0 {
		t.Fatal("a late save recreated history after disabling it")
	}
	p.Viewer.Resume = true
	_, err = st.SetPreferences(ctx, p)
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockAsset, id, "", core.BlockPhotos)
	check(t, err)
	if !errors.Is(st.SetPlaybackPosition(ctx, id, 1000), ErrNotFound) {
		t.Fatal("recorded history for a covered video")
	}
}

func TestSelectionRetainsSearchOrderAndRefusesMembershipDrift(t *testing.T) {
	st, ids := viewingFixture(t)
	ordered := []int64{ids[1], ids[0]}
	sel, err := st.Selection(t.Context(), core.View{}, ordered, true)
	check(t, err)
	if !reflect.DeepEqual(sel.IDs, ordered) {
		t.Fatalf("lost result rank: %v", sel.IDs)
	}
	check(t, st.ValidateSelection(t.Context(), sel))
	check(t, st.SetFavourite(t.Context(), ids[0], true))
	if !errors.Is(st.ValidateSelection(t.Context(), sel), ErrSelectionChanged) {
		t.Fatal("accepted a stale membership revision")
	}
	sel, err = st.Selection(t.Context(), core.View{Filter: core.FilterFavourites}, nil, false)
	check(t, err)
	if !reflect.DeepEqual(sel.IDs, []int64{ids[0]}) {
		t.Fatalf("favourite snapshot: %v", sel.IDs)
	}
	_, err = st.SetBlock(t.Context(), core.BlockAsset, ids[0], "", core.BlockPhotos)
	check(t, err)
	if !errors.Is(st.ValidateSelection(t.Context(), sel), ErrSelectionChanged) {
		t.Fatal("accepted a pre-block snapshot")
	}
	sel, err = st.Selection(t.Context(), core.View{}, ordered, true)
	check(t, err)
	if !reflect.DeepEqual(sel.IDs, []int64{ids[1]}) {
		t.Fatalf("covered asset entered selection: %v", sel.IDs)
	}
}

func TestDisplayAdjustmentsLeaveCanonicalMetadataAndInvalidateTileURLs(t *testing.T) {
	st, ids := viewingFixture(t)
	id := ids[0]
	check(t, st.SaveMetadata(t.Context(), "fixture", []MetadataResult{{AssetID: id, Width: 600, Height: 400, Orientation: 1}}))
	check(t, st.SaveThumbnails(t.Context(), "fixture", []ThumbResult{{AssetID: id}}))
	before, err := st.GridItems(t.Context(), []int64{id})
	check(t, err)
	a, err := st.SetAdjustment(t.Context(), id, 5, true)
	check(t, err)
	if a.Rotation != 1 || !a.Flip {
		t.Fatalf("adjustment: %+v", a)
	}
	canonical, err := st.AssetDetail(t.Context(), id, false)
	check(t, err)
	if canonical.Width != 600 || canonical.Height != 400 {
		t.Fatal("canonical dimensions were changed")
	}
	after, err := st.GridItems(t.Context(), []int64{id})
	check(t, err)
	if after[0].W != 400 || after[0].H != 600 || after[0].Thumb == before[0].Thumb {
		t.Fatalf("presentation not refreshed: %+v", after)
	}
}

func TestFileMoveOutcomesDoNotDiscloseCoveredPaths(t *testing.T) {
	st, ids := viewingFixture(t)
	snap, err := st.SelectedFiles(t.Context(), ids)
	check(t, err)
	f := snap.Files[0]
	known, err := st.KnownDestinations(t.Context(), []string{f.Path, `C:\Photos\unindexed.jpg`})
	check(t, err)
	if !known[core.Fold(f.Path)] || known[core.Fold(`C:\Photos\unindexed.jpg`)] {
		t.Fatalf("destination membership: %v", known)
	}
	run, err := st.SaveOrganisePlan(t.Context(), "Selected files", []OrganiseMove{{FileID: f.FileID, AssetID: f.AssetID, Root: f.Root, From: f.Path, To: f.Path + ".renamed", Size: f.Size, MTime: f.MTime, State: core.MovePlanned}})
	check(t, err)
	moves, err := st.VisibleOrganiseMoves(t.Context(), run)
	check(t, err)
	if len(moves) != 1 || moves[0].From != f.Path {
		t.Fatalf("missing public outcome: %+v", moves)
	}
	_, err = st.SetBlock(t.Context(), core.BlockAsset, f.AssetID, "", core.BlockPhotos)
	check(t, err)
	moves, err = st.VisibleOrganiseMoves(t.Context(), run)
	check(t, err)
	if len(moves) != 0 {
		t.Fatalf("covered paths exposed: %+v", moves)
	}
}

func TestANewExportPlanReplacesUnrunOnesAndKeepsOnlyRecentOutcomes(t *testing.T) {
	st, ids := viewingFixture(t)
	ctx := t.Context()
	plan := func(n int) int64 {
		t.Helper()
		jobs := make([]core.ExportJob, n)
		for i := range jobs {
			jobs[i] = core.ExportJob{AssetID: ids[0], From: `C:\Photos\a.jpg`, To: `D:\Out\a.jpg`, Options: core.ExportOptions{Format: core.ExportOriginal}}
		}
		batch, err := st.SaveExportPlan(ctx, jobs)
		check(t, err)
		return batch
	}
	settled := plan(exportHistory + 5)
	jobs, err := st.ExportJobs(ctx, settled)
	check(t, err)
	for i, j := range jobs {
		state := core.ExportDone
		if i == 0 {
			state = core.ExportReady // still owed to recovery
		}
		check(t, st.SetExportJob(ctx, j.ID, state, "", "", ""))
	}
	abandoned := plan(3)
	current := plan(2)
	if jobs, err = st.ExportJobs(ctx, abandoned); err != nil || len(jobs) != 0 {
		t.Fatalf("a plan never run was kept: %d jobs, %v", len(jobs), err)
	}
	if jobs, err = st.ExportJobs(ctx, current); err != nil || len(jobs) != 2 {
		t.Fatalf("the current plan: %d jobs, %v", len(jobs), err)
	}
	jobs, err = st.ExportJobs(ctx, settled)
	check(t, err)
	if len(jobs) != exportHistory+1 || jobs[0].State != core.ExportReady {
		t.Fatalf("kept %d settled outcomes, first %q; want the interrupted one and the newest %d", len(jobs), jobs[0].State, exportHistory)
	}
	var batches int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM export_batches").Scan(&batches))
	if batches != 2 {
		t.Fatalf("%d batches remain, want the settled and the current one", batches)
	}
}
