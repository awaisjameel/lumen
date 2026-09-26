package index

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

type fakeEvents struct{ changed atomic.Int32 }

func (*fakeEvents) Progress(core.Progress) {}
func (f *fakeEvents) LibraryChanged(int)   { f.changed.Add(1) }

func writeJPEG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{c.R, uint8(x), uint8(y), 255})
		}
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if filepath.Ext(path) == ".png" {
		png.Encode(f, img)
	} else {
		jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
	}
}

func TestIndexerEndToEnd(t *testing.T) {
	photos := t.TempDir()
	paths := core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}

	writeJPEG(t, filepath.Join(photos, "IMG_20230514_184502.jpg"), 3000, 2000, color.RGBA{R: 10})
	writeJPEG(t, filepath.Join(photos, "trip", "IMG_20240101_090000.jpg"), 400, 300, color.RGBA{R: 20})
	writeJPEG(t, filepath.Join(photos, "Screenshot_2024-01-01-10-00-00.png"), 300, 600, color.RGBA{R: 30})
	writeJPEG(t, filepath.Join(photos, ".hidden", "IMG_20200101_000000.jpg"), 50, 50, color.RGBA{R: 40})
	os.WriteFile(filepath.Join(photos, "notes.txt"), []byte("not media"), 0o644)
	os.WriteFile(filepath.Join(photos, "photo.heic"), []byte("not really heic"), 0o644)
	// An exact duplicate in another folder: must become the same asset.
	data, _ := os.ReadFile(filepath.Join(photos, "trip", "IMG_20240101_090000.jpg"))
	os.MkdirAll(filepath.Join(photos, "backup"), 0o755)
	os.WriteFile(filepath.Join(photos, "backup", "copy.jpg"), data, 0o644)

	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.AddSource(ctx, photos); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, filepath.Join(photos, "trip")); err != store.ErrAlreadyCovered {
		t.Fatalf("nested source: got %v, want ErrAlreadyCovered", err)
	}

	ev := &fakeEvents{}
	cache := media.NewCache(paths.ThumbDir, media.Decoders{})
	ix := New(st, paths, cache, Models{}, ev)
	ix.Start(ctx)
	defer ix.Stop()

	// 3 photos + 1 heic (thumbnail skipped). Hidden folder and .txt ignored.
	waitIdle(t, ix, 4)

	buckets, err := st.DayBuckets(ctx, core.View{}, core.TagConfidenceDefault)
	if err != nil {
		t.Fatal(err)
	}
	// The fake .heic has no date in its name, so it falls back to today (its mtime).
	today := time.Now().Format(time.DateOnly)
	want := []core.DayBucket{{Day: today, Count: 1}, {Day: "2024-01-01", Count: 2}, {Day: "2023-05-14", Count: 1}}
	if len(buckets) != len(want) {
		t.Fatalf("buckets = %v, want %v", buckets, want)
	}
	for i := range want {
		if buckets[i] != want[i] {
			t.Fatalf("buckets = %v, want %v", buckets, want)
		}
	}

	days, err := st.DayItems(ctx, "2023-05-14", "2023-05-14", core.View{}, core.TagConfidenceDefault)
	if err != nil || len(days) != 1 || len(days[0].Items) != 1 {
		t.Fatalf("day items = %v, %v", days, err)
	}
	big := days[0].Items[0]
	if big.W != 3000 || big.H != 2000 || big.Thumb == 0 {
		t.Errorf("big photo item = %+v", big)
	}
	for _, size := range []int{core.ThumbSmall, core.ThumbLarge} {
		f, err := os.Open(cache.Path(big.ID, size))
		if err != nil {
			t.Fatalf("thumbnail %d missing: %v", size, err)
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || max(cfg.Width, cfg.Height) != size {
			t.Errorf("thumbnail %d is %dx%d (%v)", size, cfg.Width, cfg.Height, err)
		}
	}

	// The duplicate shares one asset with two paths.
	trip, _ := st.DayItems(ctx, "2024-01-01", "2024-01-01", core.View{}, core.TagConfidenceDefault)
	var dupPaths int
	for _, it := range trip[0].Items {
		d, _ := st.AssetDetail(ctx, it.ID)
		dupPaths = max(dupPaths, len(d.Paths))
	}
	if dupPaths != 2 {
		t.Errorf("duplicate should have 2 paths, max was %d", dupPaths)
	}

	if p := ix.Progress(ctx); p.Skipped != 1 || p.Errors != 0 {
		t.Errorf("progress = %+v, want 1 skipped (heic), 0 errors", p)
	}

	// Deleting a file on disk removes it from the grid after a rescan.
	os.Remove(filepath.Join(photos, "IMG_20230514_184502.jpg"))
	ix.RescanAll()
	waitIdle(t, ix, 3)
	// Events are throttled to one per second.
	for i := 0; i < 30 && ev.changed.Load() == 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if ev.changed.Load() == 0 {
		t.Error("no library:changed events were emitted")
	}
}

// waitIdle waits until scanning and all stages are done and total assets match.
func waitIdle(t *testing.T, ix *Indexer, total int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var p core.Progress
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		p = ix.Progress(context.Background())
		if !p.Scanning && p.Metadata == 0 && p.Thumbnail == 0 && p.Embedding == 0 && p.Total == total && len(ix.scanReqs) == 0 {
			return
		}
	}
	t.Fatalf("indexer did not settle: %+v (want total %d)", p, total)
}
