package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

// fakeRecycler stands in for fileops.Recycle: it moves nothing, and answers
// each path from status (Recycled for any path not listed).
type fakeRecycler struct {
	mu     sync.Mutex
	calls  [][]string
	status map[string]fileops.Status
	err    error // returned with the outcomes
	short  bool  // answers one outcome too few
}

func (f *fakeRecycler) recycle(_ context.Context, paths []string) ([]fileops.Outcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, slices.Clone(paths))
	out := make([]fileops.Outcome, len(paths))
	for i, p := range paths {
		out[i] = fileops.Outcome{Path: p, Status: fileops.Recycled}
		if s, ok := f.status[p]; ok {
			out[i].Status = s
			if s == fileops.Failed {
				out[i].Err = errors.New("in use")
			}
		}
	}
	if f.short {
		out = out[:len(out)-1]
	}
	return out, f.err
}

func (f *fakeRecycler) called() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// trashFixture is a library of three photos, a, b and c (a also has an exact
// copy), each file a real file under a temporary folder so a test can see
// that nothing touched it.
type trashFixture struct {
	svc     *LibraryService
	st      *store.Store
	rec     *fakeRecycler
	ids     map[string]int64 // asset ids by name
	paths   map[string]string
	dir     string
	copyOfA string
}

func newTrashFixture(t *testing.T) trashFixture {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := t.TempDir()
	src, err := st.AddSource(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	f := trashFixture{st: st, rec: &fakeRecycler{}, ids: map[string]int64{}, paths: map[string]string{}, dir: dir,
		copyOfA: filepath.Join(dir, "a-copy.jpg")}
	var items []store.IngestItem
	for _, name := range []string{"a", "b", "c"} {
		p := filepath.Join(dir, name+".jpg")
		if err := os.WriteFile(p, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		f.paths[name] = p
		items = append(items, store.IngestItem{SourceID: src.ID, Path: p, Size: 1, MTime: 1, Hash: []byte(name),
			Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile})
	}
	if err := os.WriteFile(f.copyOfA, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	items = append(items, store.IngestItem{SourceID: src.ID, Path: f.copyOfA, Size: 1, MTime: 1, Hash: []byte("a"),
		Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile})
	if _, err := st.Ingest(ctx, items); err != nil {
		t.Fatal(err)
	}
	for name, p := range f.paths {
		d, err := assetAt(ctx, st, p)
		if err != nil {
			t.Fatal(err)
		}
		f.ids[name] = d
	}
	f.svc = NewLibraryService(st, nil, nil)
	f.svc.recycle = f.rec.recycle
	return f
}

// assetAt finds the asset a file belongs to, through the grid's own queries.
func assetAt(ctx context.Context, st *store.Store, path string) (int64, error) {
	days, err := st.DayItems(ctx, "2023-11-14", "2023-11-14", core.View{}, core.TagConfidenceDefault)
	if err != nil {
		return 0, err
	}
	for _, d := range days {
		for _, it := range d.Items {
			detail, err := st.AssetDetail(ctx, it.ID, false)
			if err != nil {
				return 0, err
			}
			if slices.Contains(detail.Paths, path) {
				return it.ID, nil
			}
		}
	}
	return 0, store.ErrNotFound
}

func (f trashFixture) inTrash(t *testing.T) int {
	t.Helper()
	n, err := f.svc.TrashCount(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// confirm is what the user agrees to delete: the trashed assets ids, or with
// none, everything in the Trash.
func (f trashFixture) confirm(t *testing.T, ids ...int64) core.TrashSelection {
	t.Helper()
	sel, err := f.svc.TrashSelection(t.Context(), ids)
	if err != nil {
		t.Fatal(err)
	}
	return sel
}

func TestTrashingMovesNothingOnDiskAndNeverCallsTheRecycler(t *testing.T) {
	f := newTrashFixture(t)
	if err := f.svc.TrashAssets(t.Context(), []int64{f.ids["a"], f.ids["b"]}); err != nil {
		t.Fatal(err)
	}
	if n := f.inTrash(t); n != 2 {
		t.Fatalf("TrashCount = %d, want 2", n)
	}
	if f.rec.called() != 0 {
		t.Fatal("trashing called the recycler: soft delete must leave the files alone")
	}
	for name, p := range f.paths {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file %s was touched: %v", name, err)
		}
	}
	if _, err := os.Stat(f.copyOfA); err != nil {
		t.Errorf("a's copy was touched: %v", err)
	}
}

func TestRestoringBringsAssetsBackOneOrAll(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"], f.ids["b"], f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.RestoreAssets(ctx, []int64{f.ids["a"]}); err != nil {
		t.Fatal(err)
	}
	if n := f.inTrash(t); n != 2 {
		t.Fatalf("TrashCount after restoring one = %d, want 2", n)
	}
	if err := f.svc.RestoreAllTrash(ctx); err != nil {
		t.Fatal(err)
	}
	if n := f.inTrash(t); n != 0 {
		t.Fatalf("TrashCount after restoring all = %d, want 0", n)
	}
	if n, err := f.st.CountVisible(ctx); err != nil || n != 3 {
		t.Fatalf("visible = %d, %v, want all three back", n, err)
	}
}

func TestDeleteFromDiskRecyclesEveryCopyAndForgetsTheirFiles(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"], f.ids["b"]}); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.DeleteFromDisk(ctx, f.confirm(t, f.ids["a"]))
	if err != nil {
		t.Fatal(err)
	}
	if result.Recycled != 2 || len(result.Kept) != 0 || result.Kept == nil {
		t.Fatalf("result = %+v, want both copies of a recycled and an empty Kept list", result)
	}
	if len(f.rec.calls) != 1 || !slices.Equal(f.rec.calls[0], []string{f.copyOfA, f.paths["a"]}) {
		t.Fatalf("recycler calls = %v, want one call with both copies of a, and not b", f.rec.calls)
	}
	// a's files are forgotten: it is missing now, but keeps its trashed mark
	// and so does not come back to the library, and b is untouched.
	if n := f.inTrash(t); n != 1 {
		t.Fatalf("TrashCount = %d, want just b", n)
	}
	if _, err := f.st.AssetFile(ctx, f.ids["a"], false); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a is still served: %v", err)
	}
	if n, _ := f.st.CountVisible(ctx); n != 1 {
		t.Fatalf("visible = %d, want only c", n)
	}
	if _, err := f.st.AssetFile(ctx, f.ids["b"], false); err != nil {
		t.Fatalf("b was affected: %v", err)
	}
}

func TestDeleteFromDiskRefusesAssetsThatAreNotInTheTrash(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"]}); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]int64{{f.ids["b"]}, {f.ids["a"], f.ids["b"]}, {9999}} {
		_, err := f.svc.DeleteFromDisk(ctx, core.TrashSelection{Assets: ids, Files: len(ids)})
		if !errors.Is(err, store.ErrNotFound) || !errors.Is(err, ErrTrashChanged) {
			t.Errorf("DeleteFromDisk(%v) = %v, want ErrTrashChanged from ErrNotFound", ids, err)
		}
	}
	if f.rec.called() != 0 {
		t.Fatal("a refused call reached the recycler")
	}
	if n, _ := f.st.CountVisible(ctx); n != 2 {
		t.Fatalf("visible = %d, want b and c untouched", n)
	}
	if n := f.inTrash(t); n != 1 {
		t.Fatalf("TrashCount = %d", n)
	}
}

func TestDeleteFromDiskWithNothingGivenDoesNothing(t *testing.T) {
	f := newTrashFixture(t)
	result, err := f.svc.DeleteFromDisk(t.Context(), core.TrashSelection{})
	if err != nil || result.Recycled != 0 || result.Kept == nil || f.rec.called() != 0 {
		t.Fatalf("result = %+v, %v, calls = %d", result, err, f.rec.called())
	}
}

func TestDeleteFromDiskReportsKeptAndFailedFilesAndForgetsOnlyRecycledOnes(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	f.rec.status = map[string]fileops.Status{f.copyOfA: fileops.Kept, f.paths["b"]: fileops.Failed}
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"], f.ids["b"], f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.DeleteFromDisk(ctx, f.confirm(t, f.ids["a"], f.ids["b"], f.ids["c"]))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(result.Kept)
	want := []string{f.copyOfA, f.paths["b"]}
	slices.Sort(want)
	if result.Recycled != 2 || !slices.Equal(result.Kept, want) {
		t.Fatalf("result = %+v, want a.jpg and c.jpg recycled, the copy and b.jpg kept", result)
	}
	// a lost one file and keeps the other: still on disk, still in the Trash.
	// b lost none. c is gone from the library.
	if n := f.inTrash(t); n != 2 {
		t.Fatalf("TrashCount = %d, want a and b still there", n)
	}
	for _, name := range []string{"a", "b"} {
		if _, err := f.st.AssetFile(ctx, f.ids[name], false); err != nil {
			t.Errorf("%s no longer has its remaining file: %v", name, err)
		}
	}
	if _, err := f.st.AssetFile(ctx, f.ids["c"], false); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("c is still served: %v", err)
	}
	for _, p := range []string{f.copyOfA, f.paths["b"]} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("kept file %s was touched: %v", p, err)
		}
	}
}

func TestDeleteFromDiskForgetsFilesThatWereAlreadyGoneWithoutCountingThem(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	f.rec.status = map[string]fileops.Status{f.paths["c"]: fileops.Gone}
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.DeleteFromDisk(ctx, f.confirm(t, f.ids["c"]))
	if err != nil || result.Recycled != 0 || len(result.Kept) != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if n := f.inTrash(t); n != 0 {
		t.Fatalf("TrashCount = %d: the asset has no file left", n)
	}
}

func TestDeletingEverythingTakesOnlyWhatIsInTheTrash(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"], f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.DeleteFromDisk(ctx, f.confirm(t))
	if err != nil || result.Recycled != 3 || len(result.Kept) != 0 {
		t.Fatalf("result = %+v, %v, want a's two copies and c", result, err)
	}
	if len(f.rec.calls) != 1 || slices.Contains(f.rec.calls[0], f.paths["b"]) {
		t.Fatalf("recycler calls = %v: b was never trashed", f.rec.calls)
	}
	if n, _ := f.st.CountVisible(ctx); n != 1 {
		t.Fatalf("visible = %d, want b", n)
	}
	// An empty Trash has nothing to give the recycler.
	before := f.rec.called()
	if result, err := f.svc.DeleteFromDisk(ctx, f.confirm(t)); err != nil || result.Recycled != 0 || f.rec.called() != before {
		t.Fatalf("emptying an empty Trash: %+v, %v, calls %d", result, err, f.rec.called())
	}
}

func TestDeleteFromDiskTakesOnlyWhatWasConfirmed(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	sel := f.confirm(t)
	// A trash finishing while the user reads the question is not part of it.
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["b"]}); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.DeleteFromDisk(ctx, sel)
	if err != nil || result.Recycled != 1 || len(f.rec.calls) != 1 || !slices.Equal(f.rec.calls[0], []string{f.paths["c"]}) {
		t.Fatalf("result = %+v, %v, calls %v, want c alone", result, err, f.rec.calls)
	}
	if n := f.inTrash(t); n != 1 {
		t.Fatalf("TrashCount = %d, want b still there", n)
	}
}

func TestDeleteFromDiskRefusesASelectionThatChanged(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"], f.ids["b"]}); err != nil {
		t.Fatal(err)
	}
	restored := f.confirm(t)
	if err := f.svc.RestoreAssets(ctx, []int64{f.ids["b"]}); err != nil {
		t.Fatal(err)
	}
	lost := f.confirm(t, f.ids["a"])
	lost.Files++ // as if a copy of a was forgotten since
	for _, sel := range []core.TrashSelection{restored, lost} {
		_, err := f.svc.DeleteFromDisk(ctx, sel)
		var failure Failure
		if !errors.Is(err, ErrTrashChanged) || json.Unmarshal(MarshalError(err), &failure) != nil || failure.Code != CodeTrashChanged {
			t.Errorf("DeleteFromDisk(%+v) = %v, the UI sees %+v, want trash_changed", sel, err, failure)
		}
	}
	if f.rec.called() != 0 {
		t.Fatal("a changed selection reached the recycler")
	}
}

func TestDeleteFromDiskWhenTheRecyclerFailsKeepsWhatItDidRecycle(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	f.rec.status = map[string]fileops.Status{f.paths["b"]: fileops.Failed}
	f.rec.err = errors.New("start COM")
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["b"], f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	_, err := f.svc.DeleteFromDisk(ctx, f.confirm(t, f.ids["b"], f.ids["c"]))
	if !errors.Is(err, ErrDeleteFailed) {
		t.Fatalf("err = %v, want ErrDeleteFailed", err)
	}
	var failure Failure
	if err := json.Unmarshal(MarshalError(err), &failure); err != nil || failure.Code != CodeDeleteFailed {
		t.Fatalf("the UI sees %+v, %v, want the delete_failed code", failure, err)
	}
	// c did reach the bin before the failure, so the library forgot it.
	if n := f.inTrash(t); n != 1 {
		t.Fatalf("TrashCount = %d, want b alone", n)
	}
}

func TestDeleteFromDiskForgetsNothingWhenTheRecyclerLosesTrack(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	f.rec.short = true
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["b"], f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteFromDisk(ctx, f.confirm(t, f.ids["b"], f.ids["c"])); !errors.Is(err, ErrDeleteFailed) {
		t.Fatalf("err = %v, want ErrDeleteFailed", err)
	}
	if n := f.inTrash(t); n != 2 {
		t.Fatalf("TrashCount = %d, want both still there", n)
	}
}

func TestDeleteFromDiskForgetsRecycledFilesEvenIfTheCallWasCancelled(t *testing.T) {
	f := newTrashFixture(t)
	if err := f.svc.TrashAssets(t.Context(), []int64{f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	sel := f.confirm(t, f.ids["c"])
	ctx, cancel := context.WithCancel(t.Context())
	f.svc.recycle = func(_ context.Context, paths []string) ([]fileops.Outcome, error) {
		cancel() // the user quits after the shell has taken the file
		return []fileops.Outcome{{Path: paths[0], Status: fileops.Recycled}}, ctx.Err()
	}
	if _, err := f.svc.DeleteFromDisk(ctx, sel); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
	if n := f.inTrash(t); n != 0 {
		t.Fatalf("TrashCount = %d: c is in the Recycle Bin and must not linger", n)
	}
}

func TestTheRecycleBinFailureCodeIsDeclaredForTheUI(t *testing.T) {
	if got := MarshalError(ErrDeleteFailed); string(got) != `{"code":"delete_failed"}` {
		t.Fatalf("MarshalError = %s", got)
	}
}

func TestAFailedRecycleLogsNoFilePaths(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f.svc.recycle = func(_ context.Context, paths []string) ([]fileops.Outcome, error) {
		return []fileops.Outcome{{Path: paths[0], Status: fileops.Failed, Err: fmt.Errorf("in use: %s", paths[0])}}, nil
	}
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["b"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteFromDisk(ctx, f.confirm(t, f.ids["b"])); err != nil {
		t.Fatal(err)
	}
	if logs.Len() == 0 {
		t.Fatal("the failure was not logged at all")
	}
	// The text handler escapes backslashes, so look for the file's name, which
	// survives quoting, rather than for the whole path.
	if strings.Contains(logs.String(), filepath.Base(f.paths["b"])) {
		t.Fatalf("a file path reached the log: %s", logs.String())
	}
}

func TestTrashSelectionCountsEveryCopyAndRefusesAssetsOutsideTheTrash(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"], f.ids["c"]}); err != nil {
		t.Fatal(err)
	}
	both := []int64{f.ids["a"], f.ids["c"]}
	slices.Sort(both)
	for _, c := range []struct {
		ids    []int64
		assets []int64
		files  int
	}{{[]int64{f.ids["a"]}, []int64{f.ids["a"]}, 2}, {[]int64{f.ids["c"]}, []int64{f.ids["c"]}, 1}, {both, both, 3}, {nil, both, 3}} {
		sel, err := f.svc.TrashSelection(ctx, c.ids)
		slices.Sort(sel.Assets)
		if err != nil || sel.Files != c.files || !slices.Equal(sel.Assets, c.assets) {
			t.Errorf("TrashSelection(%v) = %+v, %v, want %v with %d files", c.ids, sel, err, c.assets, c.files)
		}
	}
	if _, err := f.svc.TrashSelection(ctx, []int64{f.ids["b"]}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a photo outside the Trash: %v", err)
	}
	if sel, err := f.svc.TrashSelection(ctx, nil); err != nil || sel.Assets == nil {
		t.Errorf("TrashSelection(nil) = %+v, %v: the UI needs a list", sel, err)
	}
}

func TestDeleteFromDiskLeavesAFileThatTookOverARecycledFilesRowId(t *testing.T) {
	f := newTrashFixture(t)
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.ids["a"]}); err != nil {
		t.Fatal(err)
	}
	sources, err := f.st.ListSources(ctx)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources = %v, %v", sources, err)
	}
	newcomer := filepath.Join(f.dir, "newcomer.jpg")
	if err := os.WriteFile(newcomer, []byte("n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sel := f.confirm(t, f.ids["a"])
	// While the shell works, the scanner forgets the copy's row (the highest
	// id) and then finds an unrelated file, which SQLite gives the same id.
	f.svc.recycle = func(ctx context.Context, paths []string) ([]fileops.Outcome, error) {
		files, err := f.st.TrashedFiles(ctx, []int64{f.ids["a"]})
		if err != nil {
			t.Error(err)
		}
		i := slices.IndexFunc(files, func(x store.TrashedFile) bool { return x.Path == f.copyOfA })
		if i < 0 {
			t.Fatal("the copy of a has no row")
		}
		if err := f.st.RemoveFiles(ctx, []int64{files[i].ID}); err != nil {
			t.Error(err)
		}
		if _, err := f.st.Ingest(ctx, []store.IngestItem{{SourceID: sources[0].ID, Path: newcomer, Size: 1, MTime: 1,
			Hash: []byte("n"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_100, DateFrom: core.DateFromFile}}); err != nil {
			t.Error(err)
		}
		return f.rec.recycle(ctx, paths)
	}
	if _, err := f.svc.DeleteFromDisk(ctx, sel); err != nil {
		t.Fatal(err)
	}
	id, err := assetAt(ctx, f.st, newcomer)
	if err != nil {
		t.Fatalf("the newcomer's asset is gone: %v", err)
	}
	if ok, err := f.st.AssetVisible(ctx, id, false); err != nil || !ok {
		t.Fatalf("the newcomer is not visible (%v, %v): its row was removed by a stale id", ok, err)
	}
}
