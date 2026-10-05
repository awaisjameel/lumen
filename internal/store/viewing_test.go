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
	if a.Rotation != 1 || !a.Flip || a.Version != 1 {
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
