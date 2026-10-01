package services

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/search"
	"lumen/internal/store"
)

// Without the AI, a search can still find named people; the description it
// can't search is said, not failed.
func TestSearchWithoutAI(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	photo := func(name string, at int64) store.IngestItem {
		return store.IngestItem{SourceID: src.ID, Path: `C:\Photos\` + name, Size: 1, Hash: []byte(name),
			Kind: core.KindPhoto, Ext: ".jpg", TakenAt: at}
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{photo("ali.jpg", 100), photo("beach.jpg", 200)}); err != nil {
		t.Fatal(err)
	}
	days, err := st.DayItems(ctx, "1970-01-01", "1970-01-01", core.View{}, 0)
	if err != nil || len(days) != 1 || len(days[0].Items) != 2 {
		t.Fatalf("days: %+v, %v", days, err)
	}
	ali, beach := days[0].Items[1].ID, days[0].Items[0].ID // newest first: beach, then Ali
	if err := st.SaveFaces(ctx, "f", "f", []store.FaceResult{{AssetID: ali, Faces: []store.DetectedFace{
		{X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Sharpness: 50, Vec: []float32{1, 0}},
	}}, {AssetID: beach, Faces: []store.DetectedFace{
		{X: 0.5, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Sharpness: 50, Vec: []float32{0, 1}},
	}}}); err != nil {
		t.Fatal(err)
	}
	faces, err := st.AssetFaces(ctx, ali)
	if err != nil || len(faces) != 1 {
		t.Fatalf("faces: %v, %v", faces, err)
	}
	person, err := st.NameFaces(ctx, []int64{faces[0].ID}, "Ali")
	if err != nil {
		t.Fatal(err)
	}
	if faces, err = st.AssetFaces(ctx, beach); err != nil || len(faces) != 1 {
		t.Fatalf("faces: %v, %v", faces, err)
	}
	if _, err := st.NameFaces(ctx, []int64{faces[0].ID}, "Ali Khan"); err != nil {
		t.Fatal(err)
	}
	s := NewSearchService(st, nil, true)
	for _, name := range []string{"Ali", "Ali Khan"} {
		query := strings.Repeat(name+" ", 8000)
		prefix := string([]rune(query)[:search.MaxQueryRunes])
		want, err := s.Search(ctx, prefix)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		got, err := s.Search(ctx, query)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("long repeated name %q: %+v, %v; want %+v", name, got, err, want)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Errorf("long repeated name %q took %s", name, elapsed)
		}
	}
	for _, c := range []struct {
		query string
		ids   []int64
		noAI  bool
	}{
		{"", nil, false},
		{"ali", []int64{ali}, false},
		{"Ali Khan", []int64{beach}, false},      // the longer name wins
		{"Ali at the beach", []int64{ali}, true}, // only who is in them could be searched
		{"the beach", nil, true},
	} {
		r, err := s.Search(ctx, c.query)
		if err != nil {
			t.Errorf("%q: %v", c.query, err)
			continue
		}
		var ids []int64
		for _, it := range r.Items {
			ids = append(ids, it.ID)
		}
		if len(ids) != len(c.ids) || (len(ids) > 0 && ids[0] != c.ids[0]) || r.NoAI != c.noAI {
			t.Errorf("%q: items %v, noAI %v; want %v, %v", c.query, ids, r.NoAI, c.ids, c.noAI)
		}
	}
	if _, err := st.SetBlock(ctx, core.BlockPerson, person, "", core.BlockLabel); err != nil {
		t.Fatal(err)
	}
	blocked, err := s.Search(ctx, "Ali at the beach")
	if err != nil || !blocked.Blocked || len(blocked.Items) != 0 {
		t.Errorf("blocked name search = %+v, %v", blocked, err)
	}
	// A blocked shorter name doesn't hide a visible longer one.
	if r, err := s.Search(ctx, "Ali Khan"); err != nil || r.Blocked || len(r.Items) != 1 || r.Items[0].ID != beach {
		t.Errorf("visible longer name beside a blocked one = %+v, %v", r, err)
	}
	thing, err := st.CreateTag(ctx, "Été", core.TagManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	album, err := st.CreateAlbum(ctx, "Family Trip", core.AlbumManual, core.AlbumRule{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		kind  core.BlockKind
		id    int64
		query string
	}{
		{core.BlockTag, thing, "photos of ÉTÉ"},
		{core.BlockAlbum, album, "family trip at the beach"},
	} {
		if _, err := st.SetBlock(ctx, target.kind, target.id, "", core.BlockLabel); err != nil {
			t.Fatal(err)
		}
		if r, err := s.Search(ctx, target.query); err != nil || !r.Blocked || r.NoAI || len(r.Items) != 0 || len(r.People) != 0 {
			t.Errorf("%s name search = %+v, %v", target.kind, r, err)
		}
	}
	if r, err := s.Search(ctx, "Étéland"); err != nil || r.Blocked {
		t.Errorf("partial Thing name = %+v, %v", r, err)
	}
	if r, err := s.Search(ctx, "Ali Khan and Ali Khan"); err != nil || r.Blocked || len(r.Items) != 1 {
		t.Errorf("repeated longer name = %+v, %v", r, err)
	}
	prefix := strings.Repeat("🌊", search.MaxQueryRunes)
	want, err := s.Search(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Search(ctx, prefix+" Family Trip"); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("blocked name after Unicode query limit = %+v, %v; want %+v", got, err, want)
	}
	if _, err := s.CreateLabel(ctx, "Eiffel Tower"); !errors.Is(err, ErrNoAI) {
		t.Errorf("CreateLabel without AI: %v, want ErrNoAI", err)
	}
	if code := MarshalError(ErrNoAI); string(code) != `{"code":"no_ai"}` {
		t.Errorf("MarshalError(ErrNoAI) = %s", code)
	}
	if code := MarshalError(errors.New("disk full")); code != nil {
		t.Errorf("MarshalError(other) = %s, want Wails' default", code)
	}
}

func TestVisibleAssetsPrunesResultsInOrder(t *testing.T) {
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "library.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"a", "b", "blocked", "hidden"} {
		if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\` + name + ".jpg",
			Hash: []byte(name), Kind: core.KindPhoto, TakenAt: int64(i + 1)}}); err != nil {
			t.Fatal(err)
		}
	}
	days, err := st.DayItems(ctx, "1970-01-01", "1970-01-01", core.View{}, 0)
	if err != nil || len(days) != 1 || len(days[0].Items) != 4 {
		t.Fatalf("fixture: %+v, %v", days, err)
	}
	items := days[0].Items
	a, b, blocked, hidden := items[3].ID, items[2].ID, items[1].ID, items[0].ID
	if _, err := st.SetBlock(ctx, core.BlockAsset, blocked, "", core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite3", db)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", hidden); err != nil {
		t.Fatal(err)
	}
	s := NewSearchService(st, nil, false)
	for _, read := range []struct {
		name string
		fn   func(context.Context, []int64) ([]int64, error)
	}{{"store", st.VisibleAssets}, {"service", s.VisibleAssets}} {
		t.Run(read.name, func(t *testing.T) {
			for _, c := range []struct {
				ids, want []int64
			}{
				{[]int64{b, blocked, a, hidden, b, 999}, []int64{b, a, b}},
				{[]int64{hidden, blocked, 999}, []int64{}},
				{nil, []int64{}},
			} {
				got, err := read.fn(ctx, c.ids)
				if err != nil || got == nil || !slices.Equal(got, c.want) {
					t.Errorf("visible IDs %v = %v, %v; want %v", c.ids, got, err, c.want)
				}
			}
		})
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := s.VisibleAssets(cancelled, []int64{a}); got == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled visibility read = %v, %v", got, err)
	}
}

// A folder that doesn't exist fails AddSources before any folder is added.
func TestAddSourcesIsAllOrNone(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewLibraryService(st, nil)
	dir := t.TempDir()
	if _, err := s.AddSources(ctx, []string{dir, filepath.Join(dir, "missing")}); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("AddSources with a missing folder: %v", err)
	}
	if srcs, err := st.ListSources(ctx); err != nil || len(srcs) != 0 {
		t.Fatalf("sources after a failed add: %v, %v", srcs, err)
	}
	file := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSources(ctx, []string{file}); !errors.Is(err, ErrFolderNotFound) {
		t.Fatalf("AddSources with a file: %v", err)
	}
	if _, err := st.AddSource(ctx, dir); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(dir, "inside")
	if err := os.Mkdir(inside, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSources(ctx, []string{inside}); !errors.Is(err, store.ErrAlreadyCovered) {
		t.Fatalf("AddSources with only a covered folder: %v", err)
	}
}
