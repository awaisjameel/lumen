package index

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
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
	ix := New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), engine, nil, &fakeEvents{})
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
		hits, _, err := engine.Search(ctx, "a plain "+name+" image")
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 || hits[0].ID != byName[name] {
			t.Errorf("%s: hits %+v, want asset %d first", name, hits, byName[name])
		}
	}
	if hits, _, _ := engine.Search(ctx, "a crowded beach at sunset"); len(hits) != 0 {
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
	if !ix.held(core.StageEmbedding) || ix.held(core.StageThumbnail) {
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
