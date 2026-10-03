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
	"lumen/internal/search"
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
			a, err := st.AssetDetail(ctx, it.ID, false)
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
	ix := quiet(New(st, paths, cache, Models{}, &fakeEvents{}))
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
	ix = quiet(New(st, paths, cache, Models{}, &fakeEvents{}))
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
	if n, err := st.RequeueVersions(ctx, ix.version); err != nil || n != 0 {
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
	ix := quiet(New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{}, &fakeEvents{}))
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

	ix = quiet(New(st, paths, nil, Models{People: people.NewEngine(st, ai.FaceModel{Version: "sface-test"})}, &fakeEvents{}))
	before := ix.facesBeforeAnalysis()
	var items []store.JobItem
	for name, it := range byName {
		a, err := st.AssetDetail(ctx, it.ID, false)
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

func TestVideoQueuesMergeInCaptureOrder(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, `C:\Videos`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{
		{SourceID: src.ID, Path: `C:\Videos\newer.mp4`, Hash: []byte("newer"), Kind: core.KindVideo, Ext: ".mp4", TakenAt: 200},
		{SourceID: src.ID, Path: `C:\Videos\older.mp4`, Hash: []byte("older"), Kind: core.KindVideo, Ext: ".mp4", TakenAt: 100},
	}); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 2)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %v, %v", jobs, err)
	}
	for _, j := range jobs {
		if err := st.SaveMetadata(ctx, "m", []store.MetadataResult{{AssetID: j.AssetID}}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveThumbnails(ctx, "t", []store.ThumbResult{{AssetID: j.AssetID}}); err != nil {
			t.Fatal(err)
		}
	}
	ix := quiet(New(st, paths, nil, Models{Search: search.NewEngine(st, ai.Model{}), People: people.NewEngine(st, ai.FaceModel{})}, &fakeEvents{}))
	got, err := ix.fetchVideos(ctx, 2)
	if err != nil || len(got) != 2 || got[0].TakenAt != 200 || got[1].TakenAt != 100 {
		t.Fatalf("video order = %v, %v", got, err)
	}
	ix.focus = &store.TimeRange{From: 90, To: 110}
	got, err = ix.fetchVideos(ctx, 2)
	if err != nil || len(got) != 1 || got[0].TakenAt != 100 {
		t.Fatalf("focused video jobs = %v, %v", got, err)
	}
}

func TestMetadataStandingUsesTheKindVersionAndRejectsEpochs(t *testing.T) {
	oldPhoto := "imagemeta-1.1+goexif+vips-3"
	oldVideo := "ffprobe-9.0-1"
	ordinary := core.WallClock(time.Date(1965, time.June, 3, 12, 0, 0, 0, time.UTC))
	unix := core.WallClock(time.Date(1970, time.January, 1, 0, 0, 0, 0, time.UTC))
	quicktime := core.WallClock(time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC))
	for _, c := range []struct {
		name  string
		kind  core.Kind
		prior string
		taken int64
		want  bool
	}{
		{"ordinary photo", core.KindPhoto, oldPhoto + "+" + oldVideo, ordinary, true},
		{"Unix epoch", core.KindPhoto, oldPhoto + "+" + oldVideo, unix, false},
		{"QuickTime epoch", core.KindPhoto, oldPhoto + "+" + oldVideo, quicktime, false},
		{"beside epoch", core.KindPhoto, oldPhoto + "+" + oldVideo, unix + 1, true},
		{"video tag change", core.KindVideo, oldPhoto + "+" + oldVideo, ordinary, false},
		{"photo part changes only", core.KindVideo, oldPhoto + "+" + media.VideoMetadataVersion, ordinary, true},
		{"video part changes only", core.KindPhoto, media.MetadataVersion + "+" + oldVideo, ordinary, true},
		{"older photo fix", core.KindPhoto, "imagemeta-1.1+goexif+vips-2+" + oldVideo, ordinary, false},
		{"unknown", core.KindPhoto, "unknown", ordinary, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := metadataStand(store.JobItem{Kind: c.kind, Prior: c.prior, TakenAt: c.taken}); got != c.want {
				t.Errorf("metadata stands = %v, want %v", got, c.want)
			}
		})
	}
}

func TestThumbnailStandingUsesOnlyTheKindVersion(t *testing.T) {
	for _, c := range []struct {
		name  string
		kind  core.Kind
		prior string
		want  bool
	}{
		{"current photo", core.KindPhoto, media.ThumbnailVersion + "+" + media.VideoThumbnailVersion, true},
		{"video change only", core.KindPhoto, media.ThumbnailVersion + "+ffmpeg-old+vips-old", true},
		{"unchanged photo pixels beside old video", core.KindPhoto, media.HEIFProfilesVersion + "+ffmpeg-old+vips-old", true},
		{"unknown photo change", core.KindPhoto, "vips-unknown+" + media.VideoThumbnailVersion, false},
		{"photo change only", core.KindVideo, "vips-old+" + media.VideoThumbnailVersion, true},
		{"video change", core.KindVideo, media.ThumbnailVersion + "+ffmpeg-old+vips-old", false},
		{"unknown", core.KindPhoto, "unknown", false},
		{"no prior output", core.KindPhoto, "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := thumbnailsStand(store.JobItem{Kind: c.kind, Prior: c.prior, Path: "plain.jpg"}); got != c.want {
				t.Errorf("thumbnails stand = %v, want %v", got, c.want)
			}
		})
	}
}

func TestUpgradeRequeuesVideoMetadata(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, `C:\Videos`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Videos\1965.mov`, Hash: []byte("1965"), Kind: core.KindVideo, Ext: ".mov", TakenAt: core.WallClock(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC))}}); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("metadata = %v, %v", jobs, err)
	}
	old := "imagemeta-1.1+goexif+vips-3+ffprobe-9.0-1"
	if err := st.SaveMetadata(ctx, old, []store.MetadataResult{{AssetID: jobs[0].AssetID}}); err != nil {
		t.Fatal(err)
	}
	ix := quiet(New(st, paths, nil, Models{}, &fakeEvents{}))
	if n, err := st.RequeueVersions(ctx, map[core.Stage]store.StageVersion{core.StageMetadata: ix.version[core.StageMetadata]}); err != nil || n != 1 {
		t.Fatalf("requeued metadata = %d, %v", n, err)
	}
	jobs, err = st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	if err != nil || len(jobs) != 1 || jobs[0].Prior != old || metadataStand(jobs[0]) {
		t.Fatalf("video metadata must run in full: %v, %v", jobs, err)
	}
}

func TestAnalysisTwoFacesStandOnlyForUnchangedLargePhotos(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	dir := t.TempDir()
	src, err := st.AddSource(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	m := ai.FaceModel{Version: "sface-test", DetectSize: 640}
	ix := quiet(New(st, paths, nil, Models{People: people.NewEngine(st, m)}, &fakeEvents{}))
	if ix.version[core.StageFaces].LegacyPhoto != "" {
		t.Fatal("face upgrades must check pixels before adopting old output")
	}
	for _, c := range []struct {
		name   string
		width  int
		height int
		suffix string
		want   bool
	}{
		{"large", 1000, 800, "", true},
		{"large legacy", 800, 1000, "+video-frames-1", true},
		{"small", 400, 300, "", false},
		{"small legacy", 300, 400, "+video-frames-1", false},
		{"blocked", 1000, 800, "", true}, // judged by the job's own size, which a block doesn't hide
	} {
		path := filepath.Join(dir, c.name+".jpg")
		writeJPEG(t, path, c.width, c.height, color.RGBA{R: 10})
		if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: path, Hash: []byte(c.name), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 100}}); err != nil {
			t.Fatal(err)
		}
		jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("metadata = %v, %v", jobs, err)
		}
		id := jobs[0].AssetID
		if err := st.SaveMetadata(ctx, "m", []store.MetadataResult{{AssetID: id, Width: c.width, Height: c.height, Orientation: 1}}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveThumbnails(ctx, "t", []store.ThumbResult{{AssetID: id}}); err != nil {
			t.Fatal(err)
		}
		if c.name == "blocked" {
			if _, err := st.SetBlock(ctx, core.BlockAsset, id, "", core.BlockPhotos); err != nil {
				t.Fatal(err)
			}
		}
		prior := m.Version + "+analysis-2" + c.suffix
		if err := st.SaveFaces(ctx, m.Version, prior, []store.FaceResult{{AssetID: id, Faces: []store.DetectedFace{{X: 0.1, Y: 0.1, W: 0.3, H: 0.3, Score: 0.9, Vec: []float32{1, 0}}}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.RequeueVersions(ctx, map[core.Stage]store.StageVersion{core.StageFaces: ix.version[core.StageFaces]}); err != nil {
			t.Fatal(err)
		}
		items, err := st.PendingJobsOf(ctx, core.StageFaces, []int64{id})
		if err != nil || len(items) != 1 || items[0].Prior != prior {
			t.Fatalf("face job = %v, %v", items, err)
		}
		standing, err := ix.facesStanding(ctx, items)
		if err != nil || standing[id] != c.want {
			t.Errorf("%s: standing = %v, want %v (%v)", c.name, standing[id], c.want, err)
		}
	}
}

func TestFacePixelsStandingRejectsChangedHEIC(t *testing.T) {
	decodes := func(path, version string) bool {
		if version != media.HEIFProfilesVersion {
			t.Fatalf("decode version = %s", version)
		}
		return path != "p3.heic"
	}
	for _, c := range []struct {
		path   string
		width  int
		height int
		want   bool
	}{
		{"p3.heic", 2000, 1500, false},
		{"srgb.heic", 2000, 1500, true},
		{"plain.jpg", 640, 480, true},
		{"plain.jpg", 639, 480, false},
		{"plain.jpg", 0, 1000, false},
	} {
		if got := facePixelsStand(c.path, c.width, c.height, 640, decodes); got != c.want {
			t.Errorf("%s %dx%d: stands = %v, want %v", c.path, c.width, c.height, got, c.want)
		}
	}
}
