package services

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
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
	ali := days[0].Items[1].ID // newest first: beach, then Ali
	if err := st.SaveFaces(ctx, "f", "f", []store.FaceResult{{AssetID: ali, Faces: []store.DetectedFace{
		{X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Sharpness: 50, Vec: []float32{1, 0}},
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
	s := NewSearchService(st, nil, true)
	for _, c := range []struct {
		query string
		ids   []int64
		noAI  bool
	}{
		{"", nil, false},
		{"ali", []int64{ali}, false},
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

// A folder that doesn't exist fails AddSources before any folder is added.
func TestAddSourcesIsAllOrNone(t *testing.T) {
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewLibraryService(st, nil, nil)
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
}
