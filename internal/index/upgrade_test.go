package index

import (
	"context"
	"database/sql"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/people"
	"lumen/internal/store"
)

// asMadeBy sets the version every finished job of stage records, as if an
// earlier build had made it.
func asMadeBy(t *testing.T, db string, stage core.Stage, version string) {
	t.Helper()
	raw, err := sql.Open("sqlite3", db)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.ExecContext(t.Context(), "UPDATE asset_jobs SET model_version = ? WHERE stage = ? AND status = 'done'", version, stage); err != nil {
		t.Fatal(err)
	}
}

// photosByName returns every photo's asset by file name.
func photosByName(t *testing.T, st *store.Store) map[string]core.GridItem {
	t.Helper()
	ctx := context.Background()
	days, err := st.DayItems(ctx, "1900-01-01", "2999-12-31", core.View{}, core.TagConfidenceDefault)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]core.GridItem{}
	for _, d := range days {
		for _, it := range d.Items {
			a, err := st.AssetDetail(ctx, it.ID)
			if err != nil {
				t.Fatal(err)
			}
			out[filepath.Base(a.Paths[0])] = it
		}
	}
	return out
}

// waitFor polls cond for up to 30 seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); !cond(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
	}
}

// upgradeLibrary is a folder of three photos, one of them in Display P3.
func upgradeLibrary(t *testing.T) (photos string, paths core.Paths) {
	t.Helper()
	photos = t.TempDir()
	paths = core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	writeJPEG(t, filepath.Join(photos, "IMG_20240101_090000.jpg"), 400, 300, color.RGBA{R: 10})
	writeJPEG(t, filepath.Join(photos, "Screenshot_2024-01-02-10-00-00.png"), 300, 200, color.RGBA{R: 30})
	p3, err := os.ReadFile(filepath.Join("..", "media", "testdata", "red-p3.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(photos, "p3.jpg"), p3)
	return photos, paths
}

func TestUpgradeRemakesOnlyThumbnailsThatChange(t *testing.T) {
	photos, paths := upgradeLibrary(t)
	cache := media.NewCache(paths.ThumbDir, media.Decoders{})
	ctx := context.Background()
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, photos); err != nil {
		t.Fatal(err)
	}
	ix := New(st, paths, cache, Models{}, &fakeEvents{})
	ix.Start(ctx)
	waitIdle(t, ix, 3)
	before := photosByName(t, st)
	ix.Stop()
	st.Close()

	// As the build before colour profiles were converted left it, and
	// before the metadata fixes: metadata re-runs first, as in the real
	// upgrade.
	asMadeBy(t, paths.DBPath, core.StageMetadata, "imagemeta-1.1+goexif+vips-2+"+media.VideoMetadataVersion)
	asMadeBy(t, paths.DBPath, core.StageThumbnail, media.UnmanagedThumbnailVersion+"+"+media.VideoThumbnailVersion)
	st, err = store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ix = New(st, paths, cache, Models{}, &fakeEvents{})
	ix.Start(ctx)
	defer ix.Stop()
	waitFor(t, "the P3 photo is remade", func() bool { return photosByName(t, st)["p3.jpg"].Thumb > before["p3.jpg"].Thumb })
	waitIdle(t, ix, 3)
	after := photosByName(t, st)
	for name, it := range before {
		if name != "p3.jpg" && after[name].Thumb != it.Thumb {
			t.Errorf("%s was remade (thumbnail version %d, then %d): its pixels are the same", name, it.Thumb, after[name].Thumb)
		}
	}
	if n, err := st.RequeueOutdated(ctx, ix.version); err != nil || n != 0 {
		t.Errorf("after the upgrade %d jobs are still outdated (%v)", n, err)
	}
}

func TestFacesFoundBeforeTheAnalysisChangeStandWhereNoneWere(t *testing.T) {
	photos, paths := upgradeLibrary(t)
	ctx := context.Background()
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSource(ctx, photos); err != nil {
		t.Fatal(err)
	}
	ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{}, &fakeEvents{})
	ix.Start(ctx)
	waitIdle(t, ix, 3)
	ix.Stop()
	byName := photosByName(t, st)
	withFace := byName["Screenshot_2024-01-02-10-00-00.png"].ID
	if err := st.SaveFaces(ctx, "sface-test", "old", []store.FaceResult{{AssetID: withFace, Faces: []store.DetectedFace{
		{X: 0.1, Y: 0.1, W: 0.3, H: 0.3, Score: 0.9, Vec: []float32{1, 0}},
	}}}); err != nil {
		t.Fatal(err)
	}

	ix = New(st, paths, nil, Models{People: people.NewEngine(st, ai.FaceModel{Version: "sface-test"})}, &fakeEvents{})
	before := ix.facesBeforeAnalysis()
	var items []store.JobItem
	for name, it := range byName {
		a, err := st.AssetDetail(ctx, it.ID)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, store.JobItem{AssetID: it.ID, Path: a.Paths[0], Kind: core.KindPhoto, Prior: before})
		if name == "IMG_20240101_090000.jpg" { // and once from a version no rule knows
			items = append(items, store.JobItem{AssetID: -it.ID, Path: a.Paths[0], Kind: core.KindPhoto, Prior: "sface-other"})
		}
	}
	stand, err := ix.facesStanding(ctx, items)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int64]bool{byName["IMG_20240101_090000.jpg"].ID: true} // no faces, no colour profile
	for _, j := range items {
		if stand[j.AssetID] != want[j.AssetID] {
			t.Errorf("%s (prior %s): stands = %v, want %v", filepath.Base(j.Path), j.Prior, stand[j.AssetID], want[j.AssetID])
		}
	}
}
