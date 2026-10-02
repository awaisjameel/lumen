package index

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
	"lumen/internal/xmp"
)

// originalMTime is the modification time every fixture original is given, so
// a test can see that nothing touched it.
var originalMTime = time.Date(2020, time.March, 4, 5, 6, 7, 0, time.UTC)

// xmpFixture is a library of three photos in a temporary folder, each a real
// file: a (4000 x 3000) has the Things Beaches (AI) and Holiday (the user's)
// and Ali's face; b has the Thing Dogs; c has nothing. XMP export is on.
// The originals are read-only, so even an attempt to write one would fail.
type xmpFixture struct {
	st                           *store.Store
	ix                           *Indexer
	dir                          string
	a, b, c                      int64
	beaches, dogs, holiday       int64
	ali                          int64
	aliFace                      int64
	original                     map[string][]byte // the originals' bytes, by path
	pathA, pathB, pathC          string
	sidecarA, sidecarB, sidecarC string
}

func newXMPFixture(t *testing.T) *xmpFixture {
	t.Helper()
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := t.TempDir()
	src, err := st.AddSource(ctx, dir)
	check(t, err)
	f := &xmpFixture{st: st, dir: dir, original: map[string][]byte{}}
	var items []store.IngestItem
	for _, name := range []string{"a.jpg", "b.jpg", "c.jpg"} {
		path := filepath.Join(dir, name)
		body := []byte("original " + name)
		check(t, os.WriteFile(path, body, 0o600))
		check(t, os.Chtimes(path, originalMTime, originalMTime))
		check(t, os.Chmod(path, 0o400))
		t.Cleanup(func() { _ = os.Chmod(path, 0o600) }) // so the temporary folder can be removed
		f.original[path] = body
		items = append(items, store.IngestItem{SourceID: src.ID, Path: path, Size: int64(len(body)), MTime: originalMTime.UnixNano(),
			Hash: body, Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile})
	}
	_, err = st.Ingest(ctx, items)
	check(t, err)
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	check(t, err)
	id := map[string]int64{}
	for _, j := range jobs {
		id[filepath.Base(j.Path)] = j.AssetID
	}
	f.a, f.b, f.c = id["a.jpg"], id["b.jpg"], id["c.jpg"]
	f.pathA, f.pathB, f.pathC = filepath.Join(dir, "a.jpg"), filepath.Join(dir, "b.jpg"), filepath.Join(dir, "c.jpg")
	f.sidecarA, f.sidecarB, f.sidecarC = f.pathA+".xmp", f.pathB+".xmp", f.pathC+".xmp"
	check(t, st.SaveMetadata(ctx, "v", []store.MetadataResult{
		{AssetID: f.a, Width: 4000, Height: 3000, Orientation: 1},
		{AssetID: f.b, Width: 100, Height: 100, Orientation: 1},
		{AssetID: f.c, Width: 100, Height: 100, Orientation: 1},
	}))
	check(t, st.SyncVocabulary(ctx, []store.VocabLabel{{Name: "Beaches", Prompts: []string{"a beach"}}, {Name: "Dogs", Prompts: []string{"a dog"}}}))
	f.beaches, _, err = st.TagByName(ctx, "Beaches")
	check(t, err)
	f.dogs, _, err = st.TagByName(ctx, "Dogs")
	check(t, err)
	check(t, st.SaveEmbeddings(ctx, "m", "m", []store.EmbedResult{
		{AssetID: f.a, Vec: []int8{1}, Scale: 1, Tags: []store.TagScore{{TagID: f.beaches, Score: 0.4}}},
		{AssetID: f.b, Vec: []int8{1}, Scale: 1, Tags: []store.TagScore{{TagID: f.dogs, Score: 0.4}}},
		{AssetID: f.c, Skipped: true},
	}))
	f.holiday, err = st.CreateTag(ctx, "Holiday", core.TagManual, nil)
	check(t, err)
	check(t, st.TagAsset(ctx, f.a, f.holiday))
	vec := []float32{1, 0, 0, 0}
	check(t, st.SaveFaces(ctx, "f1", "f1", []store.FaceResult{
		{AssetID: f.a, Faces: []store.DetectedFace{{X: 0.1, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: vec}}},
		{AssetID: f.b, Skipped: true}, {AssetID: f.c, Skipped: true},
	}))
	faces, err := st.AssetFaces(ctx, f.a)
	check(t, err)
	f.aliFace = faces[0].ID
	f.ali, err = st.NameFaces(ctx, []int64{f.aliFace}, "Ali")
	check(t, err)
	f.ix = New(st, paths, nil, Models{}, &fakeEvents{})
	check(t, st.SetXMP(ctx, true))
	return f
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// tryRead is the file's text, or "" while it cannot be read (Windows refuses a
// read for an instant while a replacement renames over the file).
func tryRead(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// export runs a full pass.
func (f *xmpFixture) export(t *testing.T) core.XMPExport {
	t.Helper()
	res, err := f.ix.ExportXMPNow(t.Context())
	check(t, err)
	return res
}

// pass runs a background-style pass: one that trusts the recorded hashes.
func (f *xmpFixture) pass(t *testing.T) core.XMPExport {
	t.Helper()
	res, err := f.ix.exportXMP(t.Context(), xmpOptions{})
	check(t, err)
	return res
}

func TestExportWritesTheThingsPeopleAndFavouritesBesideEachPhoto(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetFavourite(t.Context(), f.a, true))
	res := f.export(t)
	if res != (core.XMPExport{Written: 2}) {
		t.Fatalf("result = %+v, want two written (c has nothing to say)", res)
	}
	a := readFile(t, f.sidecarA)
	for _, want := range []string{
		"<rdf:li>Beaches</rdf:li>", "<rdf:li>Holiday</rdf:li>", // Things
		`<rdf:li>Ali</rdf:li>`, `mwg-rs:Name="Ali"`, `stDim:w="4000" stDim:h="3000"`, // the person and her region
		`xmp:Rating="5"`, // the favourite
		`lumen:Exporter="` + xmp.Version + `"`,
	} {
		if !strings.Contains(a, want) {
			t.Errorf("a's sidecar lacks %s:\n%s", want, a)
		}
	}
	b := readFile(t, f.sidecarB)
	if !strings.Contains(b, "<rdf:li>Dogs</rdf:li>") || strings.Contains(b, "Rating") || strings.Contains(b, "PersonInImage") {
		t.Errorf("b's sidecar is not just the Dog Thing:\n%s", b)
	}
	if fileExists(f.sidecarC) {
		t.Error("a photo with nothing to say got a sidecar")
	}
}

func TestExportDoesNothingWhileTheSettingIsOff(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetXMP(t.Context(), false))
	if _, err := f.ix.ExportXMPNow(t.Context()); !errors.Is(err, ErrXMPOff) {
		t.Fatalf("ExportXMPNow while off = %v, want ErrXMPOff", err)
	}
	// Not even the background pass writes: it runs for a while and finds nothing.
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	f.ix.runXMP(ctx, xmpTiming{cooldown: time.Millisecond, poll: 10 * time.Millisecond, retry: time.Millisecond})
	for _, p := range []string{f.sidecarA, f.sidecarB, f.sidecarC} {
		if fileExists(p) {
			t.Fatalf("%s written while XMP export is off", p)
		}
	}
}

func TestExportNeverModifiesAnOriginal(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetFavourite(t.Context(), f.a, true))
	f.export(t)
	check(t, f.st.TagAsset(t.Context(), f.a, f.dogs))
	f.export(t)
	for path, body := range f.original {
		if got := readFile(t, path); got != string(body) {
			t.Errorf("%s changed to %q", path, got)
		}
		info, err := os.Stat(path)
		check(t, err)
		if !info.ModTime().Equal(originalMTime) {
			t.Errorf("%s modified at %v, want %v", path, info.ModTime(), originalMTime)
		}
	}
	entries, err := os.ReadDir(f.dir)
	check(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"a.jpg", "a.jpg.xmp", "b.jpg", "b.jpg.xmp", "c.jpg"}; !slices.Equal(names, want) {
		t.Fatalf("folder holds %v, want %v (no temporary files, nothing else)", names, want)
	}
}

func TestExportNeverOverwritesASidecarLumenDidNotWrite(t *testing.T) {
	f := newXMPFixture(t)
	check(t, os.WriteFile(f.sidecarA, []byte("<x:xmpmeta>made by another program</x:xmpmeta>"), 0o600))
	res := f.export(t)
	if res != (core.XMPExport{Written: 1, Skipped: 1}) {
		t.Fatalf("result = %+v, want b written and a skipped", res)
	}
	if got := readFile(t, f.sidecarA); got != "<x:xmpmeta>made by another program</x:xmpmeta>" {
		t.Fatalf("a foreign sidecar was changed to %q", got)
	}
	// It stays foreign on every later pass, however the photo changes.
	check(t, f.st.TagAsset(t.Context(), f.a, f.dogs))
	if res := f.export(t); res != (core.XMPExport{Skipped: 1, Unchanged: 1}) {
		t.Fatalf("second result = %+v, want a still skipped and b unchanged", res)
	}
	if got := readFile(t, f.sidecarA); got != "<x:xmpmeta>made by another program</x:xmpmeta>" {
		t.Fatalf("a foreign sidecar was changed later to %q", got)
	}
}

func TestExportLeavesASidecarSomeoneEditedAfterLumenWroteIt(t *testing.T) {
	f := newXMPFixture(t)
	f.export(t)
	check(t, os.WriteFile(f.sidecarB, []byte("edited by hand"), 0o600))
	check(t, f.st.TagAsset(t.Context(), f.b, f.holiday)) // b's packet would change
	if res := f.export(t); res != (core.XMPExport{Skipped: 1, Unchanged: 1}) {
		t.Fatalf("result = %+v, want b skipped and a unchanged", res)
	}
	if got := readFile(t, f.sidecarB); got != "edited by hand" {
		t.Fatalf("an edited sidecar was overwritten with %q", got)
	}
}

func TestExportRewritesItsOwnSidecarOnlyWhenWhatItSaysChanges(t *testing.T) {
	f := newXMPFixture(t)
	f.export(t)
	before, err := os.Stat(f.sidecarA)
	check(t, err)
	// Nothing changed: nothing is written, whether or not the disk is read.
	if res := f.pass(t); res != (core.XMPExport{Unchanged: 2}) {
		t.Fatalf("background pass over an unchanged library = %+v, want two unchanged", res)
	}
	if res := f.export(t); res != (core.XMPExport{Unchanged: 2}) {
		t.Fatalf("full pass over an unchanged library = %+v, want two unchanged", res)
	}
	after, err := os.Stat(f.sidecarA)
	check(t, err)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an up-to-date sidecar was rewritten")
	}

	// A tag changes: that photo's sidecar alone is rewritten, with the tag.
	check(t, f.st.TagAsset(t.Context(), f.a, f.dogs))
	if res := f.pass(t); res != (core.XMPExport{Written: 1, Unchanged: 1}) {
		t.Fatalf("after a tag change = %+v, want a written and b unchanged", res)
	}
	if got := readFile(t, f.sidecarA); !strings.Contains(got, "<rdf:li>Dogs</rdf:li>") {
		t.Fatalf("the new tag is not in the sidecar:\n%s", got)
	}
	// Taking it off again brings it back to the first content, and the tag is gone.
	check(t, f.st.UntagAsset(t.Context(), f.a, f.dogs))
	f.pass(t)
	if got := readFile(t, f.sidecarA); strings.Contains(got, "Dogs") {
		t.Fatalf("a removed tag is still in the sidecar:\n%s", got)
	}
}

func TestExportBringsASidecarBackToTheMarkerWhenItsPhotoHasNothingLeftToSay(t *testing.T) {
	f := newXMPFixture(t)
	f.export(t)
	check(t, f.st.UntagAsset(t.Context(), f.b, f.dogs))
	f.pass(t)
	got := readFile(t, f.sidecarB)
	if strings.Contains(got, "Dogs") || !strings.Contains(got, "lumen:Exporter") {
		t.Fatalf("b's sidecar should now hold only the marker:\n%s", got)
	}
}

func TestExportNeverWritesABlockedLabelOrBlockedPhoto(t *testing.T) {
	f := newXMPFixture(t)
	ctx := t.Context()
	// Blocked before the first export: the labels never reach a file.
	_, err := f.st.SetBlock(ctx, core.BlockPerson, f.ali, "", core.BlockLabel)
	check(t, err)
	_, err = f.st.SetBlock(ctx, core.BlockTag, f.beaches, "", core.BlockLabel)
	check(t, err)
	_, err = f.st.SetBlock(ctx, core.BlockAsset, f.b, "", core.BlockPhotos)
	check(t, err)
	f.export(t)
	a := readFile(t, f.sidecarA)
	if strings.Contains(a, "Ali") || strings.Contains(a, "Beaches") || strings.Contains(a, "mwg-rs:Regions") {
		t.Fatalf("a blocked label reached a's sidecar:\n%s", a)
	}
	if !strings.Contains(a, "<rdf:li>Holiday</rdf:li>") {
		t.Fatalf("the unblocked Thing is missing:\n%s", a)
	}
	if fileExists(f.sidecarB) {
		t.Fatal("a blocked photo got a sidecar")
	}
}

func TestExportRemovesALabelFromASidecarOnceItIsBlockedAndBlankedPhotosKeepNothing(t *testing.T) {
	f := newXMPFixture(t)
	ctx := t.Context()
	f.export(t)
	if !strings.Contains(readFile(t, f.sidecarA), "Ali") || !strings.Contains(readFile(t, f.sidecarB), "Dogs") {
		t.Fatal("setup: the first export lacks Ali or Dogs")
	}
	_, err := f.st.SetBlock(ctx, core.BlockPerson, f.ali, "", core.BlockLabel)
	check(t, err)
	_, err = f.st.SetBlock(ctx, core.BlockAsset, f.b, "", core.BlockPhotos)
	check(t, err)
	f.pass(t)
	a, b := readFile(t, f.sidecarA), readFile(t, f.sidecarB)
	if strings.Contains(a, "Ali") {
		t.Fatalf("a blocked person's name stayed in a's sidecar:\n%s", a)
	}
	if strings.Contains(b, "Dogs") || !strings.Contains(b, "lumen:Exporter") {
		t.Fatalf("a blocked photo's sidecar still says what it did, or is gone:\n%s", b)
	}
}

func TestExportNeverExportsTrashedOrHiddenPhotos(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.TrashAssets(t.Context(), []int64{f.a}))
	check(t, f.st.SetFavourite(t.Context(), f.b, true))
	_, err := f.st.SetBlock(t.Context(), core.BlockAsset, f.b, "", core.BlockPhotos)
	check(t, err)
	if res := f.export(t); res != (core.XMPExport{}) {
		t.Fatalf("result = %+v, want nothing: a is in the Trash and b is blocked", res)
	}
	for _, p := range []string{f.sidecarA, f.sidecarB} {
		if fileExists(p) {
			t.Fatalf("%s written for a photo that is not shown", p)
		}
	}
}

func TestFullExportMakesADeletedSidecarAgainButABackgroundPassTrustsItsRecords(t *testing.T) {
	f := newXMPFixture(t)
	f.export(t)
	check(t, os.Remove(f.sidecarB)) // the test removes its own file; Lumen never would
	if res := f.pass(t); res != (core.XMPExport{Unchanged: 2}) || fileExists(f.sidecarB) {
		t.Fatalf("background pass = %+v (b exists: %v), want it to trust the record", res, fileExists(f.sidecarB))
	}
	if res := f.export(t); res != (core.XMPExport{Written: 1, Unchanged: 1}) || !fileExists(f.sidecarB) {
		t.Fatalf("Export now = %+v (b exists: %v), want b made again", res, fileExists(f.sidecarB))
	}
}

func TestExportWritesBesideEachFileOfAnAssetWithExactCopies(t *testing.T) {
	f := newXMPFixture(t)
	src, err := f.st.ListSources(t.Context())
	check(t, err)
	copyPath := filepath.Join(f.dir, "a copy.jpg")
	body := f.original[f.pathA]
	check(t, os.WriteFile(copyPath, body, 0o600))
	_, err = f.st.Ingest(t.Context(), []store.IngestItem{{SourceID: src[0].ID, Path: copyPath, Size: int64(len(body)), MTime: 1,
		Hash: body, Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}})
	check(t, err)
	f.export(t)
	if !fileExists(f.sidecarA) || !fileExists(copyPath+".xmp") {
		t.Fatal("the copy's sidecar was not written beside the copy")
	}
	if readFile(t, f.sidecarA) != readFile(t, copyPath+".xmp") {
		t.Fatal("the two files of one asset got different sidecars")
	}
}

// eventually fails the test unless cond holds within a few seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

var fastXMP = xmpTiming{cooldown: 5 * time.Millisecond, poll: 10 * time.Millisecond, retry: 10 * time.Millisecond}

func TestTheBackgroundPassExportsChangesAndStopsWhenTurnedOff(t *testing.T) {
	f := newXMPFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { f.ix.runXMP(ctx, fastXMP); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	eventually(t, "the first sidecars", func() bool { return tryRead(f.sidecarA) != "" && tryRead(f.sidecarB) != "" })

	// A tag, a favourite and a person's name each reach the sidecar unprompted.
	check(t, f.st.TagAsset(ctx, f.c, f.holiday))
	eventually(t, "a new tag", func() bool { return strings.Contains(tryRead(f.sidecarC), "Holiday") })
	check(t, f.st.SetFavourite(ctx, f.c, true))
	eventually(t, "a favourite", func() bool { return strings.Contains(tryRead(f.sidecarC), `xmp:Rating="5"`) })
	check(t, f.st.NamePerson(ctx, f.ali, "Alison"))
	eventually(t, "a renamed person", func() bool { return strings.Contains(tryRead(f.sidecarA), "Alison") })

	// Turned off, the pass stops: later changes are not written. The sidecars stay.
	check(t, f.ix.SetXMP(ctx, false))
	time.Sleep(100 * time.Millisecond) // the loop sees the setting
	before := readFile(t, f.sidecarB)
	check(t, f.st.TagAsset(ctx, f.b, f.holiday))
	time.Sleep(300 * time.Millisecond)
	if got := readFile(t, f.sidecarB); got != before {
		t.Fatalf("b's sidecar changed after export was turned off:\n%s", got)
	}
	if !fileExists(f.sidecarA) {
		t.Fatal("turning export off removed a sidecar")
	}
}

func TestTheBackgroundPassWaitsWhileIndexingIsPausedButNotOnBattery(t *testing.T) {
	f := newXMPFixture(t)
	f.ix.SetPaused(true)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { f.ix.runXMP(ctx, fastXMP); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	time.Sleep(300 * time.Millisecond)
	if fileExists(f.sidecarA) {
		t.Fatal("a sidecar was written while indexing is paused")
	}
	// On battery the AI would wait; this is not AI work.
	f.ix.mu.Lock()
	f.ix.onBattery = true
	f.ix.mu.Unlock()
	f.ix.SetPaused(false)
	eventually(t, "the sidecar once resumed", func() bool { return fileExists(f.sidecarA) })
}

func TestExportNowRunsWhileIndexingIsPaused(t *testing.T) {
	f := newXMPFixture(t)
	f.ix.SetPaused(true)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	res, err := f.ix.ExportXMPNow(ctx)
	if err != nil || res.Written != 2 {
		t.Fatalf("Export now while paused = %+v, %v", res, err)
	}
}

func TestExportWritesNothingOnceItsContextHasEnded(t *testing.T) {
	f := newXMPFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.ix.exportXMP(ctx, xmpOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	if fileExists(f.sidecarA) {
		t.Fatal("a sidecar was written after the context ended")
	}
}

func TestXMPDocKeepsAVideosPeopleAsNamesWithoutBoxes(t *testing.T) {
	d := xmpDoc(store.XMPAsset{Exportable: true, Video: true, Width: 10, Height: 10,
		Faces: []store.XMPFace{{Name: "Ali", X: 0.1, Y: 0.1, W: 0.1, H: 0.1}}})
	if len(d.Faces) != 0 || !slices.Equal(d.Names, []string{"Ali"}) {
		t.Fatalf("doc = %+v, want Ali by name only", d)
	}
	if !xmpDoc(store.XMPAsset{Tags: []string{"x"}, Favourite: true}).Empty() {
		t.Fatal("an asset that may not be exported has content")
	}
}

func TestExportedPacketsAreThoseTheXMPPackageWrites(t *testing.T) {
	f := newXMPFixture(t)
	f.export(t)
	want := xmp.Marshal(xmp.Doc{Subjects: []string{"Beaches", "Holiday"}, Width: 4000, Height: 3000,
		Faces: []xmp.Face{{Name: "Ali", X: 0.1, Y: 0.2, W: 0.2, H: 0.3}}})
	if got := readFile(t, f.sidecarA); got != string(want) {
		t.Fatalf("a's sidecar is not the packet for its content:\n%s\n----\n%s", got, want)
	}
	batch, err := f.st.XMPAssets(t.Context(), 0, 10)
	check(t, err)
	for _, a := range batch.Assets {
		for _, file := range a.Files {
			data, err := os.ReadFile(file.Path + ".xmp")
			check(t, err)
			if !bytes.Equal(file.Wrote, fileops.SidecarHash(data)) {
				t.Errorf("the record for %s is not the hash of the file", file.Path)
			}
		}
	}
}

func TestExportCreatesNoSidecarForAPhotoWhoseOnlyThingIsBlocked(t *testing.T) {
	f := newXMPFixture(t)
	// b has only the Dogs Thing; with it blocked there is nothing left to say.
	_, err := f.st.SetBlock(t.Context(), core.BlockTag, f.dogs, "", core.BlockLabel)
	check(t, err)
	f.export(t)
	if fileExists(f.sidecarB) {
		t.Fatalf("a sidecar was made for a photo with nothing left to say:\n%s", readFile(t, f.sidecarB))
	}
}

// recordedHash is the hash Lumen has recorded for the sidecar beside path, nil if none.
func (f *xmpFixture) recordedHash(t *testing.T, path string) []byte {
	t.Helper()
	for after := int64(0); ; {
		batch, err := f.st.XMPAssets(t.Context(), after, 100)
		check(t, err)
		if len(batch.Assets) == 0 {
			return nil
		}
		for _, a := range batch.Assets {
			for _, file := range a.Files {
				if file.Path == path {
					return file.Wrote
				}
			}
			after = a.ID
		}
	}
}

func TestExportRecordsASidecarItWroteEvenWhenTheContextEndsRightAfter(t *testing.T) {
	f := newXMPFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	write := func(c context.Context, path string, data, wrote []byte) (fileops.Sidecar, error) {
		out, err := fileops.WriteSidecar(c, path, data, wrote)
		cancel() // shutdown arrives just after the file is made
		return out, err
	}
	res, err := f.ix.exportXMP(ctx, xmpOptions{write: write})
	if !errors.Is(err, context.Canceled) || res.Written != 1 {
		t.Fatalf("pass = %+v, %v; want one sidecar written and the cancellation", res, err)
	}
	if got := f.recordedHash(t, f.pathA); !bytes.Equal(got, fileops.SidecarHash([]byte(readFile(t, f.sidecarA)))) {
		t.Fatal("a sidecar was written but not recorded, so the next pass would leave it alone as someone else's")
	}
	if fileExists(f.sidecarB) {
		t.Fatal("the pass went on writing after its context ended")
	}
}

func TestExportNowCompletesWhileTheBackgroundPassIsPausedBetweenBatches(t *testing.T) {
	f := newXMPFixture(t)
	paused := make(chan struct{})
	var once sync.Once
	write := func(c context.Context, path string, data, wrote []byte) (fileops.Sidecar, error) {
		out, err := fileops.WriteSidecar(c, path, data, wrote)
		once.Do(func() { f.ix.SetPaused(true); close(paused) }) // the user pauses mid-pass
		return out, err
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		_, _ = f.ix.exportXMP(ctx, xmpOptions{held: true, batch: 1, write: write})
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	<-paused

	asked, stop := context.WithTimeout(t.Context(), 10*time.Second)
	defer stop()
	res, err := f.ix.ExportXMPNow(asked)
	if err != nil || res != (core.XMPExport{Written: 1, Unchanged: 1}) {
		t.Fatalf("Export now beside a paused pass = %+v, %v; want b written and a already done", res, err)
	}
}

func TestExportNowStopsWaitingForItsTurnWhenItsContextEnds(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.ix.xmp.acquire(t.Context())) // a batch is being exported
	defer f.ix.xmp.release()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := f.ix.ExportXMPNow(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's deadline", err)
	}
}

func TestExportNowEndsAndIsWaitedForWhenTheIndexerStops(t *testing.T) {
	f := newXMPFixture(t)
	f.ix.rootCtx, f.ix.cancel = context.WithCancel(t.Context())
	written := make(chan struct{})
	write := func(c context.Context, path string, data, wrote []byte) (fileops.Sidecar, error) {
		out, err := fileops.WriteSidecar(c, path, data, wrote)
		close(written)
		<-c.Done() // the pass is working when Stop comes
		return out, err
	}
	finished := make(chan error, 1)
	go func() {
		_, err := f.ix.exportNow(t.Context(), xmpOptions{full: true, write: write})
		finished <- err
	}()
	<-written
	stopped := make(chan struct{})
	go func() { f.ix.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return")
	}
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Export now ended with %v, want the cancellation", err)
		}
	default:
		t.Fatal("Stop returned before Export now finished")
	}
	if f.recordedHash(t, f.pathA) == nil {
		t.Fatal("the sidecar written before Stop was not recorded")
	}
	if _, err := f.ix.ExportXMPNow(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Export now after Stop = %v, want a refusal", err)
	}
}

func TestExportClaimsAFileThatIsExactlyWhatItWouldWriteAndNothingElse(t *testing.T) {
	f := newXMPFixture(t)
	// A sidecar of Lumen's that lost its record (the user moved photo and
	// sidecar in Explorer): byte for byte today's packet.
	check(t, os.WriteFile(f.sidecarA, xmp.Marshal(xmp.Doc{Subjects: []string{"Beaches", "Holiday"}, Width: 4000, Height: 3000,
		Faces: []xmp.Face{{Name: "Ali", X: 0.1, Y: 0.2, W: 0.2, H: 0.3}}}), 0o600))
	// A packet that says it is Lumen's but says something else: not ours to touch.
	other := xmp.Marshal(xmp.Doc{Subjects: []string{"Cats"}})
	check(t, os.WriteFile(f.sidecarB, other, 0o600))

	if res := f.export(t); res != (core.XMPExport{Unchanged: 1, Skipped: 1}) {
		t.Fatalf("result = %+v, want a unchanged and b skipped", res)
	}
	if f.recordedHash(t, f.pathA) == nil {
		t.Fatal("an identical Lumen sidecar was not claimed")
	}
	if f.recordedHash(t, f.pathB) != nil || readFile(t, f.sidecarB) != string(other) {
		t.Fatal("a sidecar that is not what Lumen would write was claimed or changed")
	}
	// Claimed, it is rewritten when its photo changes.
	check(t, f.st.TagAsset(t.Context(), f.a, f.dogs))
	if res := f.pass(t); res.Written != 1 || !strings.Contains(readFile(t, f.sidecarA), "Dogs") {
		t.Fatalf("after a tag change = %+v; the claimed sidecar was not brought up to date", res)
	}
}

// moveLibrary gives every photo of the fixture a copy in a second folder and
// removes the first folder from the library, as if the user moved the photos in
// Explorer and the scanner followed. Their sidecars stay behind in the old one.
func (f *xmpFixture) moveLibrary(t *testing.T) (newDir string) {
	t.Helper()
	ctx := t.Context()
	newDir = t.TempDir()
	dst, err := f.st.AddSource(ctx, newDir)
	check(t, err)
	var items []store.IngestItem
	for path, body := range f.original {
		moved := filepath.Join(newDir, filepath.Base(path))
		check(t, os.WriteFile(moved, body, 0o600))
		items = append(items, store.IngestItem{SourceID: dst.ID, Path: moved, Size: int64(len(body)), MTime: 1,
			Hash: body, Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile})
	}
	_, err = f.st.Ingest(ctx, items)
	check(t, err)
	sources, err := f.st.ListSources(ctx)
	check(t, err)
	for _, s := range sources {
		if s.ID != dst.ID {
			check(t, f.st.RemoveSource(ctx, s.ID))
		}
	}
	return newDir
}

func TestExportBringsAnAbandonedSidecarBackToTheMarkerOnceItsPhotoIsBlockedAndForgetsOnesItCannotTouch(t *testing.T) {
	f := newXMPFixture(t)
	check(t, f.st.SetFavourite(t.Context(), f.c, true))
	f.export(t) // a, b and c have sidecars
	oldA, oldB, oldC := readFile(t, f.sidecarA), readFile(t, f.sidecarB), readFile(t, f.sidecarC)
	newDir := f.moveLibrary(t)

	// Moved, the photos are still shown: the old sidecars are stale, and left as they are.
	f.export(t)
	if readFile(t, f.sidecarA) != oldA || readFile(t, f.sidecarB) != oldB || readFile(t, f.sidecarC) != oldC {
		t.Fatal("the sidecars of photos that merely moved were changed")
	}
	if !fileExists(filepath.Join(newDir, "a.jpg.xmp")) {
		t.Fatal("the moved photo got no sidecar beside it")
	}

	// a's old sidecar is edited by hand; b's is deleted; c's is untouched.
	check(t, os.WriteFile(f.sidecarA, []byte("edited by hand"), 0o600))
	check(t, os.Remove(f.sidecarB))
	for _, id := range []int64{f.a, f.b, f.c} {
		_, err := f.st.SetBlock(t.Context(), core.BlockAsset, id, "", core.BlockPhotos)
		check(t, err)
	}
	f.pass(t)
	if got := readFile(t, f.sidecarC); strings.Contains(got, "Rating") || !strings.Contains(got, "lumen:Exporter") {
		t.Fatalf("a blocked photo's abandoned sidecar still says what it did:\n%s", got)
	}
	if got := readFile(t, f.sidecarA); got != "edited by hand" {
		t.Fatalf("an edited abandoned sidecar was changed to %q", got)
	}
	if fileExists(f.sidecarB) {
		t.Fatal("a deleted abandoned sidecar was made again")
	}
	orphans, err := f.st.XMPOrphans(t.Context(), 0, 10)
	check(t, err)
	if len(orphans) != 0 {
		t.Fatalf("%d abandoned sidecars still recorded: the edited and the deleted one are not Lumen's to manage", len(orphans))
	}
	// And the scrubbed ones beside the moved photos are the marker too.
	for _, name := range []string{"a.jpg.xmp", "b.jpg.xmp", "c.jpg.xmp"} {
		if got := readFile(t, filepath.Join(newDir, name)); strings.Contains(got, "<rdf:li>") {
			t.Errorf("%s beside a blocked photo still lists things:\n%s", name, got)
		}
	}
}

// writeCalls is a sidecar writer that counts its calls by path.
type writeCalls map[string]int

func (c writeCalls) write(ctx context.Context, path string, data, wrote []byte) (fileops.Sidecar, error) {
	c[path]++
	return fileops.WriteSidecar(ctx, path, data, wrote)
}

func TestABackgroundPassDoesNotTryAForeignSidecarAgainUntilItChangesOrExportNowIsPressed(t *testing.T) {
	f := newXMPFixture(t)
	check(t, os.WriteFile(f.sidecarA, []byte("theirs"), 0o600))
	calls := writeCalls{}
	run := func(full bool) core.XMPExport {
		res, err := f.ix.exportXMP(t.Context(), xmpOptions{full: full, write: calls.write})
		check(t, err)
		return res
	}

	if res := run(false); res != (core.XMPExport{Written: 1, Skipped: 1}) || calls[f.sidecarA] != 1 {
		t.Fatalf("first pass = %+v, %d tries of a; want a looked at once", res, calls[f.sidecarA])
	}
	check(t, f.st.TagAsset(t.Context(), f.b, f.holiday)) // something to export again
	if res := run(false); res != (core.XMPExport{Written: 1, Skipped: 1}) || calls[f.sidecarA] != 1 {
		t.Fatalf("second pass = %+v, %d tries of a; want a not tried again", res, calls[f.sidecarA])
	}
	// A new Lumen process keeps what was remembered.
	again := New(f.st, core.PathsIn(t.TempDir()), nil, Models{}, &fakeEvents{})
	if _, err := again.exportXMP(t.Context(), xmpOptions{write: calls.write}); err != nil || calls[f.sidecarA] != 1 {
		t.Fatalf("after a restart: %d tries of a, %v; want still one", calls[f.sidecarA], err)
	}
	// The file changes: it is looked at again.
	check(t, os.WriteFile(f.sidecarA, []byte("theirs, edited"), 0o600))
	run(false)
	if calls[f.sidecarA] != 2 {
		t.Fatalf("a changed file was tried %d times, want 2", calls[f.sidecarA])
	}
	// Export now looks at everything.
	if res := run(true); calls[f.sidecarA] != 3 || res.Skipped != 1 {
		t.Fatalf("Export now = %+v, %d tries of a; want it looked at again", res, calls[f.sidecarA])
	}
	// The foreign file is gone: the next pass makes the sidecar, and forgets the skip.
	check(t, os.Remove(f.sidecarA))
	check(t, f.st.TagAsset(t.Context(), f.b, f.dogs))
	if res := run(false); res.Written < 1 || !fileExists(f.sidecarA) {
		t.Fatalf("after the file went: %+v, a exists %v", res, fileExists(f.sidecarA))
	}
}

func TestAnUnwritableSidecarIsNotTriedAgainUntilItChangesOrExportNowIsPressed(t *testing.T) {
	f := newXMPFixture(t)
	f.export(t)
	check(t, os.Chmod(f.sidecarA, 0o400))
	t.Cleanup(func() { _ = os.Chmod(f.sidecarA, 0o600) })
	check(t, f.st.TagAsset(t.Context(), f.a, f.dogs))
	calls := writeCalls{}
	run := func(full bool) core.XMPExport {
		res, err := f.ix.exportXMP(t.Context(), xmpOptions{full: full, write: calls.write})
		check(t, err)
		return res
	}
	if res := run(false); res.Skipped != 1 || calls[f.sidecarA] != 1 || strings.Contains(readFile(t, f.sidecarA), "Dogs") {
		t.Fatalf("first pass = %+v, %d tries; want a read-only sidecar left alone", res, calls[f.sidecarA])
	}
	check(t, f.st.TagAsset(t.Context(), f.a, f.holiday)) // moves the revision again
	check(t, f.st.UntagAsset(t.Context(), f.a, f.holiday))
	if res := run(false); res.Skipped != 1 || calls[f.sidecarA] != 1 {
		t.Fatalf("second pass = %+v, %d tries; want it not tried again", res, calls[f.sidecarA])
	}
	check(t, os.Chmod(f.sidecarA, 0o600)) // the user fixes it; the file itself does not change
	if res := run(true); res.Written != 1 || !strings.Contains(readFile(t, f.sidecarA), "Dogs") {
		t.Fatalf("Export now = %+v; want the sidecar written", res)
	}
}

func TestExportRespectsABlockThatCommitsWhileABatchIsBeingWritten(t *testing.T) {
	f := newXMPFixture(t)
	ctx := t.Context()
	write := func(c context.Context, path string, data, wrote []byte) (fileops.Sidecar, error) {
		out, err := fileops.WriteSidecar(c, path, data, wrote)
		if path == f.sidecarA {
			// a is written first; b was read with its Dogs Thing before this.
			_, berr := f.st.SetBlock(ctx, core.BlockTag, f.dogs, "", core.BlockLabel)
			check(t, berr)
		}
		return out, err
	}
	if _, err := f.ix.exportXMP(ctx, xmpOptions{write: write}); err != nil {
		t.Fatal(err)
	}
	if fileExists(f.sidecarB) {
		t.Fatalf("a blocked Thing's name was written from a stale read:\n%s", readFile(t, f.sidecarB))
	}
	if !fileExists(f.sidecarA) {
		t.Fatal("the first sidecar is missing")
	}
}

func TestLumensTemporarySidecarNamesDoNotStartAScan(t *testing.T) {
	ix := New(nil, core.PathsIn(t.TempDir()), nil, Models{}, &fakeEvents{})
	ix.rootCtx = t.Context()
	dir := t.TempDir()
	ix.onFSChange(filepath.Join(dir, "IMG_1.jpg.xmp.0a1b2c3d4e5f.lumen-tmp")) // renamed away already: it no longer exists
	if len(ix.pendingFS) != 0 {
		t.Fatalf("a temporary sidecar queued %v for a scan", ix.pendingFS)
	}
	// Any other vanished name still does (it may be a deleted folder).
	ix.onFSChange(filepath.Join(dir, "IMG_1.jpg.xmp.0a1b2c3d4e5f.bak"))
	if len(ix.pendingFS) != 1 {
		t.Fatalf("queued %v, want the other name", ix.pendingFS)
	}
}
