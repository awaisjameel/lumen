package index

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// fakeReader stands in for the OCR models: it answers by the width of the
// image, and fails as it is told to.
type fakeReader struct {
	mu     sync.Mutex
	reads  []int // the widths it was asked to read
	closed int
	text   map[int][]ai.OCRLine
	fail   map[int]error // by width
}

func (f *fakeReader) Read(_ context.Context, pix []byte, w, h int) ([]ai.OCRLine, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, w)
	if len(pix) != w*h*3 {
		return nil, errors.New("pixels are not an RGB image")
	}
	if err := f.fail[w]; err != nil {
		return nil, err
	}
	return f.text[w], nil
}

func (f *fakeReader) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
}

func (f *fakeReader) closes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeReader) widths() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	got := slices.Clone(f.reads)
	slices.Sort(got)
	return got
}

func line(text string) ai.OCRLine { return ai.OCRLine{Text: text, Conf: 0.9} }

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
}

// openFake makes the indexer's OCR models the fake, which counts loads.
func openFake(ix *Indexer, f *fakeReader, loads *atomic.Int32) {
	ix.ocr.open = func() (textReader, error) {
		loads.Add(1)
		return f, nil
	}
}

func TestTheOCRStageReadsOnlyScreenshotsAndDocumentsOnceTheUserTurnsItOn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	photos := t.TempDir()
	// The photos differ in width, which is how the fake tells them apart.
	const shotW, docW, natureW = 300, 310, 320
	for name, w := range map[string]int{"shot": shotW, "doc": docW, "nature": natureW} {
		writePNG(t, filepath.Join(photos, name+".png"), w, 200)
	}
	paths := core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SetBool(ctx, core.SettingAIOnBattery, true); err != nil { // must not depend on the power source
		t.Fatal(err)
	}
	if err := st.SyncVocabulary(ctx, []store.VocabLabel{
		{Name: "Screenshots", Prompts: []string{"a screenshot"}, Exclusive: true},
		{Name: "Documents", Prompts: []string{"a document"}, Exclusive: true},
		{Name: "Nature", Prompts: []string{"nature"}, Exclusive: true},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, photos); err != nil {
		t.Fatal(err)
	}
	reader := &fakeReader{text: map[int][]ai.OCRLine{
		shotW:   {line("Wireless network"), line("Password sunflower42")},
		docW:    {line("Invoice total due Friday")},
		natureW: {line("never read")},
	}}
	var loads atomic.Int32
	ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{}, &fakeEvents{})
	openFake(ix, reader, &loads)
	ix.Start(ctx)
	defer func() {
		cancel()
		ix.Stop()
		if n := reader.closes(); n != 1 {
			t.Errorf("the OCR models were closed %d times by Stop, want once", n)
		}
	}()
	waitIdle(t, ix, 3)

	tagged := func(name string, tag string) store.EmbedResult {
		id := idOfPhoto(t, st, name)
		tagID, _, err := st.TagByName(ctx, tag)
		if err != nil {
			t.Fatal(err)
		}
		return store.EmbedResult{AssetID: id, Vec: []int8{1}, Scale: 1, Tags: []store.TagScore{{TagID: tagID, Score: 0.6}}}
	}
	version := store.StageVersion{Photo: "e", Video: "e"}
	// OCR is off: the embeddings tag the screenshot, and nothing is queued.
	if err := st.SaveEmbeddingVersions(ctx, "m", version, []store.EmbedResult{tagged("shot.png", "Screenshots"), tagged("nature.png", "Nature")}); err != nil {
		t.Fatal(err)
	}
	nudge(ix.wakeOCR)
	time.Sleep(300 * time.Millisecond)
	if n := len(reader.widths()); n != 0 || loads.Load() != 0 {
		t.Fatalf("OCR ran while off: %d reads, %d loads", n, loads.Load())
	}
	if p := ix.Progress(ctx); p.OCR != 0 {
		t.Fatalf("progress counts %d photos for OCR while it is off", p.OCR)
	}

	// Turning it on queues the screenshot that was tagged before.
	if err := ix.SetOCR(ctx, true); err != nil {
		t.Fatal(err)
	}
	waitForText(t, st, "sunflower42", 1)
	// A photo tagged a document afterwards, while it is on, is queued by the
	// save of its tags.
	if err := st.SaveEmbeddingVersions(ctx, "m", version, []store.EmbedResult{tagged("doc.png", "Documents")}); err != nil {
		t.Fatal(err)
	}
	nudge(ix.wakeOCR)
	waitForText(t, st, "invoice friday", 1)
	if got := reader.widths(); !slices.Equal(got, []int{shotW, docW}) {
		t.Fatalf("photos read = %v, want the screenshot and the document, once each", got)
	}
	if loads.Load() != 1 {
		t.Errorf("the OCR models were loaded %d times, want once while there was work", loads.Load())
	}
	if p := ix.Progress(ctx); p.OCR != 0 {
		t.Errorf("progress still counts %d photos waiting for text", p.OCR)
	}
	// Both lines of the screenshot are in its text.
	if got, err := st.TextMatches(ctx, "wireless password", 10); err != nil || len(got) != 1 {
		t.Errorf("both lines of the screenshot: %v, %v", got, err)
	}

	// Turning it off stops it: a photo that becomes a document later waits.
	if err := ix.SetOCR(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveEmbeddingVersions(ctx, "m", version, []store.EmbedResult{tagged("nature.png", "Documents")}); err != nil {
		t.Fatal(err)
	}
	nudge(ix.wakeOCR)
	time.Sleep(300 * time.Millisecond)
	if got := reader.widths(); len(got) != 2 {
		t.Fatalf("photos read after turning it off = %v", got)
	}
}

func idOfPhoto(t *testing.T, st *store.Store, name string) int64 {
	t.Helper()
	days, err := st.DayItems(t.Context(), "1970-01-01", "2100-01-01", core.View{}, core.TagConfidenceDefault)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range days {
		for _, it := range d.Items {
			det, err := st.AssetDetail(t.Context(), it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(det.Paths[0]) == name {
				return it.ID
			}
		}
	}
	t.Fatalf("no photo named %s", name)
	return 0
}

func waitForText(t *testing.T, st *store.Store, query string, want int) {
	t.Helper()
	var got []int64
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		var err error
		if got, err = st.TextMatches(t.Context(), query, 10); err != nil {
			t.Fatal(err)
		}
		if len(got) == want {
			return
		}
	}
	t.Fatalf("text %q is in %d photos, want %d", query, len(got), want)
}

// OCR is AI work: it waits for mains power and for the user's pause, and
// frees its models while it waits.
func TestPauseAndBatteryHoldTheOCRStageAndFreeItsModels(t *testing.T) {
	ix := New(nil, core.Paths{}, nil, Models{}, &fakeEvents{})
	if ix.held(core.StageOCR) {
		t.Fatal("OCR is held with nothing holding it")
	}
	ix.onBattery = true
	if !ix.held(core.StageOCR) {
		t.Error("OCR runs on battery")
	}
	ix.aiOnBattery = true
	if ix.held(core.StageOCR) {
		t.Error("OCR is held on battery that the user allowed")
	}
	ix.onBattery, ix.aiOnBattery, ix.paused = false, false, true
	if !ix.held(core.StageOCR) {
		t.Error("OCR runs while paused")
	}

	reader := &fakeReader{}
	var loads atomic.Int32
	openFake(ix, reader, &loads)
	if _, done, err := ix.ocr.acquire(t.Context()); err != nil {
		t.Fatal(err)
	} else {
		done()
	}
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if ix.waitTurn(ctx, core.StageOCR, ix.ocr) {
		t.Fatal("waitTurn returned while paused")
	}
	if reader.closes() != 1 || ix.ocr.loaded {
		t.Errorf("models closed %d times, loaded %v; want them freed while held", reader.closes(), ix.ocr.loaded)
	}
	ix.setHold(func() { ix.paused = false })
	if !ix.waitTurn(t.Context(), core.StageOCR, ix.ocr) {
		t.Error("waitTurn did not return after resuming")
	}
}

func TestOCRKeepsItsOwnVersionAndRequeuesOnlyItself(t *testing.T) {
	ix := New(nil, core.Paths{}, nil, Models{}, &fakeEvents{})
	v, ok := ix.version[core.StageOCR]
	if !ok || v.Photo != ai.OCRVersion || v.Photo == "" {
		t.Fatalf("OCR stage version = %+v, want %q", v, ai.OCRVersion)
	}
	for _, other := range []core.Stage{core.StageMetadata, core.StageThumbnail} {
		if ix.version[other].Photo == v.Photo {
			t.Errorf("OCR shares the version of the %s stage", other)
		}
	}
}

func TestOCRSortsFailuresIntoPhotosAndModels(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	paths := core.PathsIn(t.TempDir())
	ix := New(nil, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{}, &fakeEvents{})
	reader := &fakeReader{text: map[int][]ai.OCRLine{300: {line("fine")}}, fail: map[int]error{
		310: ai.ErrBadOutput, 320: ai.ErrBadImage,
	}}
	var loads atomic.Int32
	openFake(ix, reader, &loads)
	job := func(id int64, w int) store.JobItem {
		path := filepath.Join(dir, fmt.Sprintf("p%d.png", id))
		writePNG(t, path, w, 100)
		return store.JobItem{AssetID: id, Path: path, Kind: core.KindPhoto, Orientation: 1}
	}
	items := []store.JobItem{job(1, 300), job(2, 310), job(3, 320), {AssetID: 4, Path: filepath.Join(dir, "missing.png"), Kind: core.KindPhoto, Orientation: 1}}
	rs, err := ix.doOCR(ctx, items)
	if err != nil || len(rs) != 4 {
		t.Fatalf("doOCR = %+v, %v", rs, err)
	}
	if rs[0].Text != "fine" || rs[0].Err != "" {
		t.Errorf("a readable photo = %+v", rs[0])
	}
	for _, i := range []int{1, 2, 3} {
		if rs[i].Err == "" || rs[i].Text != "" {
			t.Errorf("photo %d, whose own problem it is = %+v, want an error of its own", i+1, rs[i])
		}
	}
	// A model that fails on anything fails the batch: nothing is saved, and
	// the stage backs off, rather than every photo being marked unreadable.
	reader.fail = map[int]error{300: errors.New("session lost")}
	if rs, err := ix.doOCR(ctx, items[:1]); err == nil || rs != nil {
		t.Errorf("a failing model gave %+v, %v; want the batch to fail", rs, err)
	}
	// A cancelled batch is not saved either.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if rs, err := ix.doOCR(cancelled, items[:1]); !errors.Is(err, context.Canceled) || rs != nil {
		t.Errorf("a cancelled batch gave %+v, %v", rs, err)
	}
}

func TestOCRModelsThatWontLoadFailTheBatchWithoutMarkingPhotos(t *testing.T) {
	ix := New(nil, core.Paths{}, nil, Models{}, &fakeEvents{})
	// The default opener, on a runtime without the OCR models.
	rs, err := ix.doOCR(t.Context(), []store.JobItem{{AssetID: 1, Path: "x.png", Kind: core.KindPhoto}})
	if err == nil || rs != nil {
		t.Fatalf("doOCR without models = %+v, %v; want the stage to fail", rs, err)
	}
}

func TestJoinLinesPutsOneLineOnEachRow(t *testing.T) {
	if got := joinLines([]ai.OCRLine{line("one"), line("two")}); got != "one\ntwo" {
		t.Errorf("joinLines = %q", got)
	}
	if got := joinLines(nil); got != "" {
		t.Errorf("joinLines(nil) = %q", got)
	}
}

// ocrIndexerLibrary is a store with the Screenshots Thing and photos named
// by the test, each with metadata and thumbnail done.
func ocrIndexerLibrary(t *testing.T, names ...string) (*store.Store, core.Paths, []store.JobItem, int64) {
	t.Helper()
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SyncVocabulary(ctx, []store.VocabLabel{{Name: "Screenshots", Prompts: []string{"a screenshot"}, Exclusive: true}}); err != nil {
		t.Fatal(err)
	}
	shots, _, err := st.TagByName(ctx, "Screenshots")
	if err != nil {
		t.Fatal(err)
	}
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	var items []store.IngestItem
	for i, name := range names {
		items = append(items, store.IngestItem{SourceID: src.ID, Path: `C:\Photos\` + name + ".png", Size: 1, Hash: []byte(name), Kind: core.KindPhoto, Ext: ".png", TakenAt: int64(10 * (i + 1))})
	}
	if _, err := st.Ingest(ctx, items); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10) // the newest first
	if err != nil || len(jobs) != len(names) {
		t.Fatalf("jobs = %v, %v", jobs, err)
	}
	for _, j := range jobs {
		if err := st.SaveMetadata(ctx, "m", []store.MetadataResult{{AssetID: j.AssetID, Orientation: 1}}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveThumbnails(ctx, "t", []store.ThumbResult{{AssetID: j.AssetID}}); err != nil {
			t.Fatal(err)
		}
	}
	return st, paths, jobs, shots
}

func TestProgressSaysOCRWaitsForMainsPowerEvenWithoutOtherAI(t *testing.T) {
	ctx := t.Context()
	st, paths, jobs, shots := ocrIndexerLibrary(t, "a")
	ix := New(st, paths, nil, Models{}, &fakeEvents{})
	ix.onBattery = true
	if p := ix.Progress(ctx); p.OnBattery {
		t.Fatal("no AI is waiting, yet progress says it waits for mains power")
	}
	if err := st.TagAsset(ctx, jobs[0].AssetID, shots); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveEmbeddings(ctx, "m", "e", []store.EmbedResult{{AssetID: jobs[0].AssetID, Vec: []int8{1}, Scale: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOCR(ctx, true); err != nil {
		t.Fatal(err)
	}
	if p := ix.Progress(ctx); !p.OnBattery || p.OCR != 1 {
		t.Fatalf("progress with OCR waiting on battery = %+v, want OnBattery and one photo", p)
	}
}

func TestFetchSkipsPhotosThatStoppedBeingScreenshotsAndFindsTheNext(t *testing.T) {
	ctx := t.Context()
	st, paths, jobs, shots := ocrIndexerLibrary(t, "old", "new")
	newest, older := jobs[0].AssetID, jobs[1].AssetID
	for _, id := range []int64{newest, older} {
		if err := st.TagAsset(ctx, id, shots); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveEmbeddings(ctx, "m", "e", []store.EmbedResult{{AssetID: newest, Vec: []int8{1}, Scale: 1}, {AssetID: older, Vec: []int8{1}, Scale: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOCR(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := st.UntagAsset(ctx, newest, shots); err != nil { // the user says it is not one
		t.Fatal(err)
	}
	ix := New(st, paths, nil, Models{}, &fakeEvents{})
	got, err := ix.fetchOCR(ctx, 1)
	if err != nil || len(got) != 1 || got[0].AssetID != older {
		t.Fatalf("fetch = %+v, %v; want the older photo, after the rejected one was dropped", got, err)
	}
}
