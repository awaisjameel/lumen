package index

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/people"
	"lumen/internal/search"
	"lumen/internal/store"
	"lumen/internal/testenv"
)

func writeSolid(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, 255
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}

// The whole Phase 2 path with the real model: scan, thumbnail, embed, then
// find photos by description and teach it a new Thing.
func TestIndexerFindsPhotosByDescription(t *testing.T) {
	rt := testenv.Runtime(t)
	if err := ai.Init(rt); err != nil {
		t.Fatal(err)
	}
	m, err := ai.LoadModel(filepath.Join(rt, "models", "siglip2"))
	if err != nil {
		t.Fatal(err)
	}
	photos := t.TempDir()
	paths := core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	colours := map[string]color.RGBA{"red": {220, 20, 20, 255}, "blue": {20, 40, 220, 255}, "green": {20, 180, 40, 255}}
	for name, c := range colours {
		writeSolid(t, filepath.Join(photos, name+".jpg"), c)
	}

	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Skip learning the ~200 built-in Things; this test teaches its own.
	if err := st.SetInt(ctx, core.SettingVocabulary, search.VocabularyVersion); err != nil {
		t.Fatal(err)
	}
	// The AI waits for mains power by default; this test must not depend on it.
	if err := st.SetBool(ctx, core.SettingAIOnBattery, true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, photos); err != nil {
		t.Fatal(err)
	}
	engine := search.NewEngine(st, m)
	engine.Start(ctx)
	ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{Search: engine}, &fakeEvents{})
	ix.Start(ctx)
	defer func() {
		cancel()
		ix.Stop()
		engine.Close()
	}()
	waitIdle(t, ix, 3)
	if n := engine.Indexed(); n != 3 {
		t.Fatalf("%d images searchable, want 3", n)
	}

	byName := map[string]int64{}
	days, _ := st.DayItems(ctx, "1970-01-01", "2100-01-01", core.View{}, core.TagConfidenceDefault)
	for _, d := range days {
		for _, it := range d.Items {
			det, _ := st.AssetDetail(ctx, it.ID)
			name := filepath.Base(det.Paths[0])
			byName[name[:len(name)-4]] = it.ID
		}
	}
	for name := range colours {
		hits, _, err := engine.Search(ctx, "a plain "+name+" image", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 || hits[0].ID != byName[name] {
			t.Errorf("%s: hits %+v, want asset %d first", name, hits, byName[name])
		}
	}
	if hits, _, _ := engine.Search(ctx, "a crowded beach at sunset", nil); len(hits) != 0 {
		t.Errorf("unrelated query found %d photos", len(hits))
	}

	// A new Thing is found across the library at once, surest match first.
	id, err := engine.CreateLabel(ctx, "blue")
	if err != nil {
		t.Fatal(err)
	}
	things, _ := st.Things(ctx, core.TagConfidenceMin)
	i := slices.IndexFunc(things, func(tg core.Tag) bool { return tg.ID == id })
	if i < 0 || things[i].Cover != byName["blue"] {
		t.Errorf("thing %d not found or wrong cover: %+v", id, things)
	}
}

func TestBatteryHoldsOnlyTheAI(t *testing.T) {
	ix := &Indexer{onBattery: true}
	if !ix.held(core.StageEmbedding) || !ix.held(core.StageFaces) || ix.held(core.StageThumbnail) {
		t.Error("on battery: the AI must wait and thumbnails must not")
	}
	ix.aiOnBattery = true
	if ix.held(core.StageEmbedding) {
		t.Error("allowed on battery, yet held")
	}
	ix.paused = true
	if !ix.held(core.StageMetadata) {
		t.Error("paused, yet metadata runs")
	}
}

func TestVideoAIUsesOnePassAndRetriesOnlyTheFailedStage(t *testing.T) {
	rt := testenv.Runtime(t)
	if err := ai.Init(rt); err != nil {
		t.Fatal(err)
	}
	m, err := ai.LoadModel(filepath.Join(rt, "models", "siglip2"))
	if err != nil {
		t.Fatal(err)
	}
	fm, err := ai.LoadFaceModel(filepath.Join(rt, "models", "faces"))
	if err != nil {
		t.Fatal(err)
	}
	for _, failFaces := range []bool{false, true} {
		t.Run(map[bool]string{false: "both", true: "face-model-failure"}[failFaces], func(t *testing.T) {
			ctx := t.Context()
			paths := core.PathsIn(t.TempDir())
			st, err := store.Open(paths.DBPath)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if err := st.SetInt(ctx, core.SettingVocabulary, search.VocabularyVersion); err != nil {
				t.Fatal(err)
			}
			src, err := st.AddSource(ctx, `C:\Videos`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Videos\clip.mp4`, Size: 1, Hash: []byte("clip"), Kind: core.KindVideo, Ext: ".mp4", TakenAt: 100}}); err != nil {
				t.Fatal(err)
			}
			jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("metadata = %v, %v", jobs, err)
			}
			id := jobs[0].AssetID
			if err := st.SaveMetadata(ctx, "m", []store.MetadataResult{{AssetID: id, Duration: 10000, Orientation: 1}}); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveThumbnails(ctx, "t", []store.ThumbResult{{AssetID: id}}); err != nil {
				t.Fatal(err)
			}
			engine := search.NewEngine(st, m)
			engine.Start(ctx)
			defer engine.Close()
			ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{Search: engine, People: people.NewEngine(st, fm)}, &fakeEvents{})
			defer ix.encoder.release()
			defer ix.faces.release()
			openFaces := ix.faces.open
			if failFaces {
				ix.faces.open = func() (*ai.Faces, error) { return nil, errors.New("face model unavailable") }
			}
			passes := 0
			sample := func(ctx context.Context, _ string, _, side int, consume func(media.Frame) error) error {
				passes++
				if side != faceSide {
					t.Fatalf("sample side = %d", side)
				}
				for _, at := range []int64{0, 0, 5000} {
					if err := consume(media.Frame{At: at, Image: media.Image{Pix: make([]byte, 320*180*3), W: 320, H: 180}}); err != nil {
						return err
					}
				}
				return ctx.Err()
			}
			jobs, err = ix.fetchVideos(ctx, 4)
			if err != nil || len(jobs) != 1 {
				t.Fatalf("video jobs = %v, %v", jobs, err)
			}
			if photos, err := ix.fetch(ctx, core.StageEmbedding, 4); err != nil || len(photos) != 0 {
				t.Fatalf("video in photo queue = %v, %v", photos, err)
			}
			rs, err := ix.processVideos(ctx, jobs, sample)
			if (err != nil) != failFaces || passes != 1 || len(rs) != 1 || len(rs[0].embeddings) != 1 || len(rs[0].embeddings[0].Frames) != 2 {
				t.Fatalf("results = %+v, %v; passes = %d", rs, err, passes)
			}
			if err := ix.saveVideos(ctx, rs); err != nil {
				t.Fatal(err)
			}
			if n, err := st.CountPending(ctx, core.StageEmbedding); err != nil || n != 0 {
				t.Fatalf("embedding remains pending = %d, %v", n, err)
			}
			if failFaces {
				ix.faces.open = openFaces
				jobs, err = ix.fetchVideos(ctx, 4)
				if err != nil || len(jobs) != 1 {
					t.Fatalf("face retry = %v, %v", jobs, err)
				}
				rs, err = ix.processVideos(ctx, jobs, sample)
				if err != nil || len(rs) != 1 || len(rs[0].embeddings) != 0 || len(rs[0].faces) != 1 || passes != 2 {
					t.Fatalf("retry = %+v, %v; passes = %d", rs, err, passes)
				}
				if err := ix.saveVideos(ctx, rs); err != nil {
					t.Fatal(err)
				}
			}
			if n, err := st.CountPending(ctx, core.StageFaces); err != nil || n != 0 {
				t.Fatalf("faces remain pending = %d, %v", n, err)
			}
			if n, err := st.RequeueVersions(ctx, map[core.Stage]store.StageVersion{core.StageEmbedding: ix.version[core.StageEmbedding], core.StageFaces: ix.version[core.StageFaces]}); err != nil || n != 0 {
				t.Fatalf("saved versions are outdated = %d, %v", n, err)
			}
		})
	}
}

// Real photos of people through the whole pipeline: faces are found, and
// the people engine groups the ones seen again and again.
func TestIndexerFindsPeople(t *testing.T) {
	photos := testenv.FacePhotos(t, 40)
	rt := testenv.Runtime(t)
	if err := ai.Init(rt); err != nil {
		t.Fatal(err)
	}
	fm, err := ai.LoadFaceModel(filepath.Join(rt, "models", "faces"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, p := range photos {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, filepath.Base(p)), data, 0o644); err != nil {
			t.Fatal(err)
		}
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := st.SetBool(ctx, core.SettingAIOnBattery, true); err != nil { // must not depend on the power source
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, dir); err != nil {
		t.Fatal(err)
	}
	engine := people.NewEngine(st, fm)
	ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{People: engine}, &fakeEvents{})
	ix.Start(ctx)
	defer func() {
		cancel()
		ix.Stop()
	}()
	waitIdle(t, ix, len(photos))
	var found []core.Person
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); time.Sleep(time.Second) {
		if found, _ = st.People(ctx); len(found) > 0 {
			break
		}
	}
	if len(found) == 0 {
		t.Fatal("no people found")
	}
	for _, p := range found {
		if p.Cover.ID == 0 || p.Cover.W <= 0 || p.Cover.W > 1 {
			t.Errorf("person %d has a bad cover: %+v", p.ID, p.Cover)
		}
	}
	t.Logf("%d photos: %d people, largest in %d photos", len(photos), len(found), found[0].Count)
}
