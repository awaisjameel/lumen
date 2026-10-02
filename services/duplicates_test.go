package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

// copiesFixture is a library with one photo in three identical files (a1, a2
// and a3) and another with one file (b), each a real file under a temporary
// folder whose size and time the library records, as the scanner would.
type copiesFixture struct {
	svc     *LibraryService
	st      *store.Store
	rec     *fakeRecycler
	id      int64 // the photo with three files
	otherID int64
	a       [3]string
	b       string
}

func newCopiesFixture(t *testing.T) copiesFixture {
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
	f := copiesFixture{st: st, rec: &fakeRecycler{}}
	var items []store.IngestItem
	add := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, store.IngestItem{SourceID: src.ID, Path: p, Size: info.Size(), MTime: info.ModTime().UnixNano(),
			Hash: []byte(content), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile})
		return p
	}
	for i := range f.a {
		f.a[i] = add("a"+string(rune('1'+i))+".jpg", "a")
	}
	f.b = add("b.jpg", "b")
	if _, err := st.Ingest(ctx, items); err != nil {
		t.Fatal(err)
	}
	if f.id, err = assetAt(ctx, st, f.a[0]); err != nil {
		t.Fatal(err)
	}
	if f.otherID, err = assetAt(ctx, st, f.b); err != nil {
		t.Fatal(err)
	}
	f.svc = NewLibraryService(st, nil, nil)
	f.svc.recycle = f.rec.recycle
	return f
}

// pathsOf is the files the library still holds for the photo.
func (f copiesFixture) pathsOf(t *testing.T, id int64) []string {
	t.Helper()
	d, err := f.st.AssetDetail(t.Context(), id, false)
	if err != nil {
		t.Fatal(err)
	}
	return d.Paths
}

func (f copiesFixture) untouched(t *testing.T) {
	t.Helper()
	if f.rec.called() != 0 {
		t.Fatalf("the recycler was called: %v", f.rec.calls)
	}
	if got := f.pathsOf(t, f.id); len(got) != 3 {
		t.Fatalf("the library holds %v for a, want all three files", got)
	}
}

func TestDeleteCopiesRecyclesOnlyTheFilesListedAndKeepsTheOneChosen(t *testing.T) {
	f := newCopiesFixture(t)
	result, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[0], []string{f.a[1]})
	if err != nil {
		t.Fatal(err)
	}
	if result.Recycled != 1 || result.Kept == nil || len(result.Kept) != 0 {
		t.Fatalf("result = %+v, want the one listed copy recycled and an empty Kept list", result)
	}
	if len(f.rec.calls) != 1 || !slices.Equal(f.rec.calls[0], []string{f.a[1]}) {
		t.Fatalf("recycler calls = %v, want one call with a2 only", f.rec.calls)
	}
	// a3 was not listed (it could be a copy the scanner found after the review): it stays.
	if got := f.pathsOf(t, f.id); !slices.Equal(got, []string{f.a[0], f.a[2]}) {
		t.Fatalf("the library holds %v for a, want a1 and a3", got)
	}
	if n, err := f.st.CountVisible(t.Context()); err != nil || n != 2 {
		t.Fatalf("visible = %d, %v, want both photos", n, err)
	}
}

func TestDeleteCopiesKeepsWhicheverCopyTheUserChoseAndRecyclesSeveral(t *testing.T) {
	f := newCopiesFixture(t)
	// Paths compare without case, as the files table does.
	upper := []string{filepath.Join(filepath.Dir(f.a[0]), "A1.JPG"), f.a[1]}
	if _, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[2], upper); err != nil {
		t.Fatal(err)
	}
	if len(f.rec.calls) != 1 || len(f.rec.calls[0]) != 2 {
		t.Fatalf("recycler calls = %v, want one call with two files", f.rec.calls)
	}
	if got := f.pathsOf(t, f.id); !slices.Equal(got, []string{f.a[2]}) {
		t.Fatalf("the library holds %v for a, want only the kept copy", got)
	}
}

func TestDeleteCopiesDoesNothingWhenTheRequestNamesAFileThatIsNotACopy(t *testing.T) {
	f := newCopiesFixture(t)
	ctx := t.Context()
	for name, call := range map[string]func() error{
		"nothing to recycle":   func() error { _, err := f.svc.DeleteCopies(ctx, f.id, f.a[0], nil); return err },
		"an empty list":        func() error { _, err := f.svc.DeleteCopies(ctx, f.id, f.a[0], []string{}); return err },
		"the kept file listed": func() error { _, err := f.svc.DeleteCopies(ctx, f.id, f.a[0], []string{f.a[1], f.a[0]}); return err },
		"another photo's file": func() error { _, err := f.svc.DeleteCopies(ctx, f.id, f.a[0], []string{f.a[1], f.b}); return err },
		"an unknown file": func() error {
			_, err := f.svc.DeleteCopies(ctx, f.id, f.a[0], []string{f.a[1] + ".missing"})
			return err
		},
		"a keeper of another one": func() error { _, err := f.svc.DeleteCopies(ctx, f.id, f.b, []string{f.a[1]}); return err },
		"an unknown keeper":       func() error { _, err := f.svc.DeleteCopies(ctx, f.id, "", []string{f.a[1]}); return err },
		"an unknown photo":        func() error { _, err := f.svc.DeleteCopies(ctx, 9999, f.a[0], []string{f.a[1]}); return err },
	} {
		if err := call(); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
	f.untouched(t)
}

func TestDeleteCopiesDoesNothingWhenTheFileToKeepIsNotOnDisk(t *testing.T) {
	f := newCopiesFixture(t)
	if err := os.Remove(f.a[0]); err != nil { // a temporary file of this test
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[0], []string{f.a[1]}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	f.untouched(t)
}

func TestDeleteCopiesDoesNothingWhenTheFileToKeepWasEditedSinceItWasIndexed(t *testing.T) {
	f := newCopiesFixture(t)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(f.a[0], later, later); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[0], []string{f.a[1]}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound: the others are no longer known to match the keeper", err)
	}
	f.untouched(t)
}

func TestDeleteCopiesLeavesAFileEditedSinceItWasIndexedAndReportsIt(t *testing.T) {
	f := newCopiesFixture(t)
	// a2 got new content of another size; a3 only a new time.
	if err := os.WriteFile(f.a[1], []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(f.a[2], later, later); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[0], []string{f.a[1], f.a[2]})
	if err != nil {
		t.Fatal(err)
	}
	if result.Recycled != 0 || !slices.Equal(result.Kept, []string{f.a[1], f.a[2]}) {
		t.Fatalf("result = %+v, want both edited files reported as kept", result)
	}
	if f.rec.called() != 0 {
		t.Fatalf("an edited file reached the recycler: %v", f.rec.calls)
	}
	if got := f.pathsOf(t, f.id); len(got) != 3 {
		t.Fatalf("the library holds %v for a, want all three files", got)
	}

	// The unedited ones listed with an edited one are still recycled.
	g := newCopiesFixture(t)
	if err := os.WriteFile(g.a[1], []byte("edited"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = g.svc.DeleteCopies(t.Context(), g.id, g.a[0], []string{g.a[1], g.a[2]})
	if err != nil {
		t.Fatal(err)
	}
	if result.Recycled != 1 || !slices.Equal(result.Kept, []string{g.a[1]}) ||
		len(g.rec.calls) != 1 || !slices.Equal(g.rec.calls[0], []string{g.a[2]}) {
		t.Fatalf("result = %+v, calls = %v, want only a3 recycled and a2 reported", result, g.rec.calls)
	}
}

func TestDeleteCopiesForgetsAListedFileThatIsAlreadyGone(t *testing.T) {
	f := newCopiesFixture(t)
	if err := os.Remove(f.a[1]); err != nil { // a temporary file of this test
		t.Fatal(err)
	}
	f.rec.status = map[string]fileops.Status{f.a[1]: fileops.Gone}
	result, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[0], []string{f.a[1]})
	if err != nil || result.Recycled != 0 || len(result.Kept) != 0 {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if got := f.pathsOf(t, f.id); !slices.Equal(got, []string{f.a[0], f.a[2]}) {
		t.Fatalf("the library holds %v for a, want the missing file forgotten", got)
	}
}

func TestDeleteCopiesRefusesAPhotoTheLibraryDoesNotShow(t *testing.T) {
	f := newCopiesFixture(t)
	if err := f.svc.TrashAssets(t.Context(), []int64{f.id}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[0], []string{f.a[1]}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for a trashed photo", err)
	}
	if f.rec.called() != 0 {
		t.Fatal("a trashed photo's copies reached the recycler")
	}
}

func TestDeleteCopiesReportsTheFilesTheShellWouldNotRecycle(t *testing.T) {
	f := newCopiesFixture(t)
	f.rec.status = map[string]fileops.Status{f.a[1]: fileops.Kept}
	result, err := f.svc.DeleteCopies(t.Context(), f.id, f.a[0], []string{f.a[1], f.a[2]})
	if err != nil {
		t.Fatal(err)
	}
	if result.Recycled != 1 || !slices.Equal(result.Kept, []string{f.a[1]}) {
		t.Fatalf("result = %+v, want a3 recycled and a2 reported as kept", result)
	}
	if _, err := os.Stat(f.a[1]); err != nil {
		t.Fatalf("the unrecyclable copy was touched: %v", err)
	}
	if got := f.pathsOf(t, f.id); !slices.Equal(got, []string{f.a[0], f.a[1]}) {
		t.Fatalf("the library holds %v for a, want a1 and the unrecycled a2", got)
	}
}

// blockingRecycler holds the first recycle until release is closed, and says
// when it is inside.
type blockingRecycler struct {
	*fakeRecycler
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingRecycler() *blockingRecycler {
	return &blockingRecycler{fakeRecycler: &fakeRecycler{}, entered: make(chan struct{}), release: make(chan struct{})}
}

func (b *blockingRecycler) recycleBlocking(ctx context.Context, paths []string) ([]fileops.Outcome, error) {
	first := false
	b.once.Do(func() { first = true })
	if first {
		close(b.entered)
		<-b.release
	}
	return b.recycle(ctx, paths)
}

func TestConcurrentDeleteCopiesNeverRecycleEveryCopy(t *testing.T) {
	f := newCopiesFixture(t)
	slow := newBlockingRecycler()
	f.svc.recycle = slow.recycleBlocking
	ctx := t.Context()
	// Two requests, each promising a different file stays: a1 for the first, a2 for the second.
	var wg sync.WaitGroup
	var errFirst, errSecond error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errFirst = f.svc.DeleteCopies(ctx, f.id, f.a[0], []string{f.a[1], f.a[2]})
	}()
	<-slow.entered
	go func() {
		defer wg.Done()
		_, errSecond = f.svc.DeleteCopies(ctx, f.id, f.a[1], []string{f.a[0], f.a[2]})
	}()
	// Unserialised, the second request validates and reaches the recycler while the first is inside it.
	time.Sleep(200 * time.Millisecond)
	if n := slow.called(); n != 0 {
		t.Errorf("a second request reached the recycler while the first was recycling (%d calls so far)", n)
	}
	close(slow.release)
	wg.Wait()
	if errFirst != nil {
		t.Errorf("first request: %v", errFirst)
	}
	if !errors.Is(errSecond, store.ErrNotFound) {
		t.Errorf("second request: err = %v, want ErrNotFound: its keeper was recycled by the first", errSecond)
	}
	if slow.called() != 1 {
		t.Errorf("recycler calls = %v, want exactly one", slow.calls)
	}
	if got := f.pathsOf(t, f.id); !slices.Equal(got, []string{f.a[0]}) {
		t.Errorf("the library holds %v for a, want a1 kept", got)
	}
}

func TestDeleteFromDiskWaitsForACopyDeletionInProgress(t *testing.T) {
	f := newCopiesFixture(t)
	slow := newBlockingRecycler()
	f.svc.recycle = slow.recycleBlocking
	ctx := t.Context()
	if err := f.svc.TrashAssets(ctx, []int64{f.otherID}); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := f.svc.DeleteCopies(ctx, f.id, f.a[0], []string{f.a[1]}); err != nil {
			t.Errorf("DeleteCopies: %v", err)
		}
	}()
	<-slow.entered
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		if _, err := f.svc.DeleteAllTrashFromDisk(ctx); err != nil {
			t.Errorf("DeleteAllTrashFromDisk: %v", err)
		}
	}()
	time.Sleep(200 * time.Millisecond)
	if n := slow.called(); n != 0 {
		t.Errorf("the Trash's deletion reached the recycler during a copy deletion (%d calls so far)", n)
	}
	close(slow.release)
	<-done
	<-finished
	if slow.called() != 2 {
		t.Errorf("recycler calls = %v, want one for the copies and one for the Trash", slow.calls)
	}
}
