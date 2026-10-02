package services

import (
	"errors"
	"os"
	"slices"
	"testing"

	"lumen/internal/fileops"
	"lumen/internal/store"
)

// pathsOf is the files the library still holds for the asset.
func (f trashFixture) pathsOf(t *testing.T, id int64) []string {
	t.Helper()
	d, err := f.st.AssetDetail(t.Context(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	return d.Paths
}

func TestDeleteCopiesRecyclesTheOtherFilesAndKeepsTheOneChosen(t *testing.T) {
	f := newTrashFixture(t)
	result, err := f.svc.DeleteCopies(t.Context(), f.ids["a"], f.paths["a"])
	if err != nil {
		t.Fatal(err)
	}
	if result.Recycled != 1 || result.Kept == nil || len(result.Kept) != 0 {
		t.Fatalf("result = %+v, want the one extra copy recycled and an empty Kept list", result)
	}
	if len(f.rec.calls) != 1 || !slices.Equal(f.rec.calls[0], []string{f.copyOfA}) {
		t.Fatalf("recycler calls = %v, want one call with the copy only", f.rec.calls)
	}
	if got := f.pathsOf(t, f.ids["a"]); !slices.Equal(got, []string{f.paths["a"]}) {
		t.Fatalf("the library holds %v for a, want only the file kept", got)
	}
	// The photo stays in the library, and no other photo was touched.
	if n, err := f.st.CountVisible(t.Context()); err != nil || n != 3 {
		t.Fatalf("visible = %d, %v, want all three photos", n, err)
	}
	if _, err := os.Stat(f.paths["a"]); err != nil {
		t.Fatalf("the file to keep was touched: %v", err)
	}
}

func TestDeleteCopiesKeepsWhicheverCopyTheUserChose(t *testing.T) {
	f := newTrashFixture(t)
	if _, err := f.svc.DeleteCopies(t.Context(), f.ids["a"], f.copyOfA); err != nil {
		t.Fatal(err)
	}
	if len(f.rec.calls) != 1 || !slices.Equal(f.rec.calls[0], []string{f.paths["a"]}) {
		t.Fatalf("recycler calls = %v, want the other copy", f.rec.calls)
	}
	if got := f.pathsOf(t, f.ids["a"]); !slices.Equal(got, []string{f.copyOfA}) {
		t.Fatalf("the library holds %v for a, want the chosen copy", got)
	}
}

func TestDeleteCopiesDoesNothingWhenTheFileToKeepIsNotOnDisk(t *testing.T) {
	f := newTrashFixture(t)
	if err := os.Remove(f.paths["a"]); err != nil { // a temporary file of this test
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteCopies(t.Context(), f.ids["a"], f.paths["a"]); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if f.rec.called() != 0 {
		t.Fatal("the other copy was recycled although the one to keep is gone")
	}
	if got := f.pathsOf(t, f.ids["a"]); len(got) != 2 {
		t.Fatalf("the library holds %v for a, want both files", got)
	}
}

func TestDeleteCopiesDoesNothingWhenKeepIsNotAFileOfTheAsset(t *testing.T) {
	f := newTrashFixture(t)
	for _, keep := range []string{f.paths["b"], f.paths["a"] + ".missing", ""} {
		if _, err := f.svc.DeleteCopies(t.Context(), f.ids["a"], keep); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("keep %q: err = %v, want ErrNotFound", keep, err)
		}
	}
	if _, err := f.svc.DeleteCopies(t.Context(), 9999, f.paths["a"]); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown asset: err = %v, want ErrNotFound", err)
	}
	if f.rec.called() != 0 {
		t.Fatal("a refused call reached the recycler")
	}
	if got := f.pathsOf(t, f.ids["a"]); len(got) != 2 {
		t.Fatalf("the library holds %v for a, want both files", got)
	}
}

func TestDeleteCopiesRefusesAPhotoTheLibraryDoesNotShow(t *testing.T) {
	f := newTrashFixture(t)
	if err := f.svc.TrashAssets(t.Context(), []int64{f.ids["a"]}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteCopies(t.Context(), f.ids["a"], f.paths["a"]); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for a trashed photo", err)
	}
	if f.rec.called() != 0 {
		t.Fatal("a trashed photo's copies reached the recycler")
	}
}

func TestDeleteCopiesReportsTheFilesTheShellWouldNotRecycle(t *testing.T) {
	f := newTrashFixture(t)
	f.rec.status = map[string]fileops.Status{f.copyOfA: fileops.Kept}
	result, err := f.svc.DeleteCopies(t.Context(), f.ids["a"], f.paths["a"])
	if err != nil {
		t.Fatal(err)
	}
	if result.Recycled != 0 || !slices.Equal(result.Kept, []string{f.copyOfA}) {
		t.Fatalf("result = %+v, want the copy reported as kept", result)
	}
	if _, err := os.Stat(f.copyOfA); err != nil {
		t.Fatalf("the unrecyclable copy was touched: %v", err)
	}
	if got := f.pathsOf(t, f.ids["a"]); len(got) != 2 {
		t.Fatalf("the library holds %v for a, want both files still", got)
	}
}

func TestDeleteCopiesOfAPhotoWithOneFileHasNothingToRecycle(t *testing.T) {
	f := newTrashFixture(t)
	result, err := f.svc.DeleteCopies(t.Context(), f.ids["b"], f.paths["b"])
	if err != nil || result.Recycled != 0 || f.rec.called() != 0 {
		t.Fatalf("result = %+v, %v, calls = %d", result, err, f.rec.called())
	}
}
