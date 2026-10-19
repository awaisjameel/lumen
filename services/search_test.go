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
	"lumen/internal/index"
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
	s := NewSearchService(st, nil, true, nil)
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

func TestHitsPruneResultsInOrderWithCurrentThumbnails(t *testing.T) {
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
	// a's thumbnail is made after the search found it.
	if _, err := w.ExecContext(ctx, "UPDATE assets SET thumb_version = 7 WHERE id = ?", a); err != nil {
		t.Fatal(err)
	}
	s := NewSearchService(st, nil, false, nil)
	for _, c := range []struct {
		ids, want []int64
	}{
		{[]int64{b, blocked, a, hidden, b, 999}, []int64{b, a, b}},
		{[]int64{hidden, blocked, 999}, []int64{}},
		{nil, []int64{}},
	} {
		items, err := s.Hits(ctx, c.ids)
		got := make([]int64, len(items))
		for i, it := range items {
			got[i] = it.ID
		}
		if err != nil || items == nil || !slices.Equal(got, c.want) {
			t.Errorf("hits %v = %v, %v; want %v", c.ids, got, err, c.want)
		}
		if i := slices.Index(got, a); i >= 0 && items[i].Thumb != 7 {
			t.Errorf("a's thumbnail = %d, want the current version 7", items[i].Thumb)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if got, err := s.Hits(cancelled, []int64{a}); got == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled hits read = %v, %v", got, err)
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
	s := NewLibraryService(st, nil, nil, nil)
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

// fakeSemantic stands in for the search engine: it answers with fixed hits
// and notes what it was asked.
type fakeSemantic struct {
	hits  []search.Hit
	all   bool
	err   error
	asked []string
	allow func(int64) bool
}

func (f *fakeSemantic) Search(_ context.Context, query string, allow func(int64) bool) ([]search.Hit, bool, error) {
	f.asked = append(f.asked, query)
	f.allow = allow
	return f.hits, f.all, f.err
}

// textLibrary is a store of photos a, b, c, d (a is the oldest) with some of
// them having text, and a search service over it with a fake engine.
type textLibrary struct {
	db         string
	st         *store.Store
	svc        *SearchService
	engine     *fakeSemantic
	a, b, c, d int64
}

func newTextLibrary(t *testing.T) *textLibrary {
	t.Helper()
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "library.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"a", "b", "c", "d"} {
		if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\` + name + ".png", Size: 1,
			Hash: []byte(name), Kind: core.KindPhoto, Ext: ".png", TakenAt: int64(100 * (i + 1))}}); err != nil {
			t.Fatal(err)
		}
	}
	days, err := st.DayItems(ctx, "1970-01-01", "1970-01-01", core.View{}, 0)
	if err != nil || len(days) != 1 || len(days[0].Items) != 4 {
		t.Fatalf("fixture: %+v, %v", days, err)
	}
	items := days[0].Items // newest first
	l := &textLibrary{db: db, st: st, engine: &fakeSemantic{all: true}, d: items[0].ID, c: items[1].ID, b: items[2].ID, a: items[3].ID}
	l.svc = &SearchService{store: st, semantic: l.engine}
	return l
}

func (l *textLibrary) say(t *testing.T, id int64, text string) {
	t.Helper()
	if err := l.st.SaveOCR(t.Context(), "ocr", []store.OCRResult{{AssetID: id, Text: text}}); err != nil {
		t.Fatal(err)
	}
}

func resultIDs(r core.SearchResult) []int64 {
	ids := []int64{}
	for _, it := range r.Items {
		ids = append(ids, it.ID)
	}
	return ids
}

func TestSearchListsPhotosWhoseTextMatchesBeforeTheMeaningMatches(t *testing.T) {
	l := newTextLibrary(t)
	l.say(t, l.a, "Invoice total due on Friday")
	l.say(t, l.c, "Please pay this invoice today")
	// The engine finds c (also found by its text), b and d by meaning.
	l.engine.hits = []search.Hit{{ID: l.c, Score: 0.9}, {ID: l.b, Score: 0.8}, {ID: l.d, Score: 0.7, At: 4500}}
	r, err := l.svc.Search(t.Context(), "invoice")
	if err != nil {
		t.Fatal(err)
	}
	// c and a by their text, newest first; then b and d, without c again.
	if want := []int64{l.c, l.a, l.b, l.d}; !slices.Equal(resultIDs(r), want) {
		t.Fatalf("results = %v, want %v", resultIDs(r), want)
	}
	if got := r.Items[3].At; got != 4500 {
		t.Errorf("the video moment of a meaning match = %d, want 4500", got)
	}
	if got := r.Items[0].At; got != 0 {
		t.Errorf("a text match has the moment %d, want none", got)
	}
	if !r.Complete || r.NoAI {
		t.Errorf("result = %+v, want complete and with the AI", r)
	}
	if !slices.Equal(l.engine.asked, []string{"invoice"}) {
		t.Errorf("the engine was asked %q", l.engine.asked)
	}
}

func TestSearchFindsTextWithoutTheAI(t *testing.T) {
	l := newTextLibrary(t)
	l.say(t, l.b, "Boarding pass gate B12")
	for name, semantic := range map[string]semanticSearch{
		"no engine":          nil,
		"engine not started": &fakeSemantic{err: search.ErrNotLoaded},
	} {
		l.svc.semantic = semantic
		r, err := l.svc.Search(t.Context(), "gate b12")
		if err != nil || !r.NoAI || !slices.Equal(resultIDs(r), []int64{l.b}) {
			t.Errorf("%s: result = %+v, %v; want the text match, and noAI", name, r, err)
		}
	}
	if r, err := l.svc.Search(t.Context(), "nothing like it"); err != nil || len(r.Items) != 0 {
		t.Errorf("search without a match = %+v, %v", r, err)
	}
}

func TestSearchNeverReturnsTextOfPhotosTheUserCannotSee(t *testing.T) {
	l := newTextLibrary(t)
	for _, id := range []int64{l.a, l.b, l.c, l.d} {
		l.say(t, id, "secret passphrase")
	}
	if _, err := l.st.SetBlock(t.Context(), core.BlockAsset, l.a, "", core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite3", l.db)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.ExecContext(t.Context(), "UPDATE assets SET trashed_at = 1 WHERE id = ?", l.b); err != nil {
		t.Fatal(err)
	}
	r, err := l.svc.Search(t.Context(), "passphrase")
	if err != nil || !slices.Equal(resultIDs(r), []int64{l.d, l.c}) {
		t.Fatalf("results = %v, %v; want the two photos that can be seen", resultIDs(r), err)
	}
}

// nameAli names the faces of the given photos Ali, and returns who she is.
func (l *textLibrary) nameAli(t *testing.T, ids ...int64) int64 {
	t.Helper()
	ctx := t.Context()
	var results []store.FaceResult
	for _, id := range ids {
		results = append(results, store.FaceResult{AssetID: id, Faces: []store.DetectedFace{
			{X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Sharpness: 50, Vec: []float32{1, 0}},
		}})
	}
	if err := l.st.SaveFaces(ctx, "f", "f", results); err != nil {
		t.Fatal(err)
	}
	var faces []int64
	for _, id := range ids {
		fs, err := l.st.AssetFaces(ctx, id)
		if err != nil || len(fs) != 1 {
			t.Fatalf("faces of %d: %v, %v", id, fs, err)
		}
		faces = append(faces, fs[0].ID)
	}
	ali, err := l.st.NameFaces(ctx, faces, "Ali")
	if err != nil {
		t.Fatal(err)
	}
	return ali
}

func TestSearchNarrowsTextMatchesToTheNamedPeople(t *testing.T) {
	l := newTextLibrary(t)
	ctx := t.Context()
	ali := l.nameAli(t, l.a, l.c)
	l.say(t, l.a, "Gate B12 boarding")
	l.say(t, l.b, "Gate B13 boarding") // not Ali's
	l.engine.hits = []search.Hit{{ID: l.c, Score: 0.9}}
	r, err := l.svc.Search(ctx, "Ali boarding")
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{l.a, l.c}; !slices.Equal(resultIDs(r), want) || !slices.Equal(r.People, []core.SearchPerson{{ID: ali, Name: "Ali"}}) || r.Description != "boarding" {
		t.Fatalf("results = %v for %v, want Ali's photo with the text, then her other that the engine found, Ali and \"boarding\" searched", resultIDs(r), r.People)
	}
	// The engine searches only Ali's photos.
	if l.engine.allow == nil || !l.engine.allow(l.a) || !l.engine.allow(l.c) || l.engine.allow(l.b) || l.engine.allow(l.d) {
		t.Error("the engine was not narrowed to Ali's photos")
	}
	// A name alone gives the person's photos, as before, without a text search.
	l.engine.asked = nil
	if r, err := l.svc.Search(ctx, "Ali"); err != nil || !slices.Equal(resultIDs(r), []int64{l.c, l.a}) || len(l.engine.asked) != 0 || r.Description != "" {
		t.Errorf("a name alone = %v, %v (engine asked %q, description %q)", resultIDs(r), err, l.engine.asked, r.Description)
	}
}

func TestSearchFallsBackToThePeoplesPhotosAfterTheirTextWithoutTheAI(t *testing.T) {
	l := newTextLibrary(t)
	l.nameAli(t, l.a, l.c)
	l.say(t, l.c, "boarding pass")
	l.svc.semantic = nil
	r, err := l.svc.Search(t.Context(), "Ali boarding")
	if err != nil || !r.NoAI || !slices.Equal(resultIDs(r), []int64{l.c, l.a}) {
		t.Fatalf("result = %v (noAI %v), %v; want Ali's photo with the text, then her other", resultIDs(r), r.NoAI, err)
	}
}

func TestSearchAsksForResultsToGrowWhileTextIsStillBeingRead(t *testing.T) {
	l := newTextLibrary(t)
	ctx := t.Context()
	w, err := sql.Open("sqlite3", l.db)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Everything else is indexed, so only the text can be waited for.
	if _, err := w.ExecContext(ctx, "UPDATE asset_jobs SET status = 'done' WHERE stage IN ('metadata', 'thumbnail', 'embedding')"); err != nil {
		t.Fatal(err)
	}
	if r, err := l.svc.Search(ctx, "invoice"); err != nil || r.Pending != 0 {
		t.Fatalf("search with OCR off: pending %d, %v", r.Pending, err)
	}
	if err := l.st.SyncVocabulary(ctx, []store.VocabLabel{{Name: "Screenshots", Prompts: []string{"a screenshot"}, Exclusive: true}}); err != nil {
		t.Fatal(err)
	}
	shots, _, err := l.st.TagByName(ctx, "Screenshots")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{l.a, l.b} {
		if err := l.st.TagAsset(ctx, id, shots); err != nil {
			t.Fatal(err)
		}
	}
	// Turning it on queues the two screenshots, which are not read yet.
	if err := l.st.SetOCR(ctx, true); err != nil {
		t.Fatal(err)
	}
	r, err := l.svc.Search(ctx, "invoice")
	if err != nil || r.Pending != 2 {
		t.Fatalf("search while two screenshots wait for their text: pending %d, %v", r.Pending, err)
	}
	// An empty search waits for nothing.
	if r, err := l.svc.Search(ctx, ""); err != nil || r.Pending != 0 {
		t.Fatalf("empty search: pending %d, %v", r.Pending, err)
	}
}

func TestTurningTextReadingOnAndOffThroughTheLibraryService(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ix := index.New(st, paths, nil, index.Models{}, &quietEvents{})
	s := NewLibraryService(st, ix, nil, nil)
	if got, err := s.Settings(ctx); err != nil || got.OCR {
		t.Fatalf("settings of a new library: OCR %v, %v; want it off", got.OCR, err)
	}
	if err := s.SetOCR(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Settings(ctx); err != nil || !got.OCR {
		t.Fatalf("settings after turning it on: OCR %v, %v", got.OCR, err)
	}
	if err := s.SetOCR(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Settings(ctx); err != nil || got.OCR {
		t.Fatalf("settings after turning it off: OCR %v, %v", got.OCR, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.SetOCR(cancelled, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("SetOCR with a cancelled context: %v", err)
	}
}

// screenshotLibrary is a text library whose photos are indexed except for
// their text, with a Screenshots Thing.
func screenshotLibrary(t *testing.T) (lib *textLibrary, screenshots int64) {
	t.Helper()
	l := newTextLibrary(t)
	ctx := t.Context()
	if err := l.st.SyncVocabulary(ctx, []store.VocabLabel{{Name: "Screenshots", Prompts: []string{"a screenshot"}, Exclusive: true}}); err != nil {
		t.Fatal(err)
	}
	shots, _, err := l.st.TagByName(ctx, "Screenshots")
	if err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite3", l.db)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err := w.ExecContext(ctx, "UPDATE asset_jobs SET status = 'done' WHERE stage IN ('metadata', 'thumbnail', 'embedding')"); err != nil {
		t.Fatal(err)
	}
	return l, shots
}

func TestTaggingAPhotoAScreenshotQueuesItsTextWhileOCRIsOn(t *testing.T) {
	l, _ := screenshotLibrary(t)
	ctx := t.Context()
	if err := l.st.SetOCR(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := l.svc.TagAsset(ctx, l.a, "Screenshots"); err != nil {
		t.Fatal(err)
	}
	r, err := l.svc.Search(ctx, "invoice")
	if err != nil || r.Pending != 1 {
		t.Fatalf("pending after tagging a screenshot = %d, %v; want its text queued", r.Pending, err)
	}
}

func TestBatchTaggingKeepsCommittedOutcomesWhenOCRQueueingFails(t *testing.T) {
	l, _ := screenshotLibrary(t)
	ctx := t.Context()
	check(t, l.st.SetOCR(ctx, true))
	w, err := sql.Open("sqlite3", l.db)
	check(t, err)
	defer w.Close()
	// Fail only the background queue write, after the tag edit has committed.
	_, err = w.ExecContext(ctx, `CREATE TRIGGER refuse_ocr_queue BEFORE INSERT ON asset_jobs
		WHEN NEW.stage = 'ocr' BEGIN SELECT RAISE(ABORT, 'queue unavailable'); END`)
	check(t, err)
	results, err := l.svc.TagAssets(ctx, []int64{l.a}, "Screenshots")
	check(t, err)
	if len(results) != 1 || results[0].Error != "" {
		t.Fatalf("committed tag reported as failed: %+v", results)
	}
	tags, err := l.st.AssetTags(ctx, l.a, core.TagConfidenceDefault)
	check(t, err)
	if len(tags) != 1 || tags[0].Name != "Screenshots" {
		t.Fatalf("tag was not committed: %+v", tags)
	}
}

func TestLoweringTheTagConfidenceQueuesPhotosThatBecomeScreenshots(t *testing.T) {
	l, shots := screenshotLibrary(t)
	ctx := t.Context()
	if err := l.st.ReplaceAITags(ctx, []store.AssetTagSet{{AssetID: l.a, Tags: []store.TagScore{{TagID: shots, Score: 0.05}}}}); err != nil {
		t.Fatal(err)
	}
	ix := index.New(l.st, core.PathsIn(t.TempDir()), nil, index.Models{}, &quietEvents{})
	lib := NewLibraryService(l.st, ix, nil, nil)
	if err := lib.SetTagConfidence(ctx, 0.2); err != nil {
		t.Fatal(err)
	}
	if err := lib.SetOCR(ctx, true); err != nil {
		t.Fatal(err)
	}
	if n, err := l.st.CountPending(ctx, core.StageOCR); err != nil || n != 0 {
		t.Fatalf("pending with the photo below the confidence = %d, %v", n, err)
	}
	if err := lib.SetTagConfidence(ctx, 0.01); err != nil {
		t.Fatal(err)
	}
	if n, err := l.st.CountPending(ctx, core.StageOCR); err != nil || n != 1 {
		t.Fatalf("pending after lowering the confidence = %d, %v; want the photo queued", n, err)
	}
}

func TestRecentSearchesLeaveOutThoseThatNameSomethingBlocked(t *testing.T) {
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
	if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\ali.jpg`, Size: 1, Hash: []byte("ali"),
		Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 100}}); err != nil {
		t.Fatal(err)
	}
	days, err := st.DayItems(ctx, "1970-01-01", "1970-01-01", core.View{}, 0)
	if err != nil || len(days) != 1 {
		t.Fatalf("days: %+v, %v", days, err)
	}
	if err := st.SaveFaces(ctx, "f", "f", []store.FaceResult{{AssetID: days[0].Items[0].ID, Faces: []store.DetectedFace{
		{X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Sharpness: 50, Vec: []float32{1, 0}},
	}}}); err != nil {
		t.Fatal(err)
	}
	faces, err := st.AssetFaces(ctx, days[0].Items[0].ID)
	if err != nil || len(faces) != 1 {
		t.Fatalf("faces: %v, %v", faces, err)
	}
	person, err := st.NameFaces(ctx, []int64{faces[0].ID}, "Ali")
	if err != nil {
		t.Fatal(err)
	}
	s := NewSearchService(st, nil, true, nil)
	for _, q := range []string{"sunset", "Ali at the beach"} {
		if _, err := s.RememberSearch(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.RecentSearches(ctx); err != nil || !slices.Equal(got, []string{"Ali at the beach", "sunset"}) {
		t.Fatalf("recent = %q, %v", got, err)
	}
	block, err := st.SetBlock(ctx, core.BlockPerson, person, "", core.BlockLabel)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.RecentSearches(ctx); err != nil || !slices.Equal(got, []string{"sunset"}) {
		t.Fatalf("recent while Ali is blocked = %q, %v", got, err)
	}
	if got, err := s.RememberSearch(ctx, "beach"); err != nil || !slices.Equal(got, []string{"beach", "sunset"}) {
		t.Fatalf("recent after another search = %q, %v", got, err)
	}
	// Unblocking brings the search back: it was hidden, not forgotten.
	if err := st.RemoveBlock(ctx, block); err != nil {
		t.Fatal(err)
	}
	if got, err := s.RecentSearches(ctx); err != nil || !slices.Equal(got, []string{"beach", "Ali at the beach", "sunset"}) {
		t.Fatalf("recent after unblocking = %q, %v", got, err)
	}
	if err := s.ClearRecentSearches(ctx); err != nil {
		t.Fatal(err)
	}
	if got, err := s.RecentSearches(ctx); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("recent after clearing = %#v, %v", got, err)
	}
}
