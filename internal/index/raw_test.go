package index

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
	"lumen/internal/testenv"
)

func TestIndexerTakesCameraRawFilesIntoTheLibrary(t *testing.T) {
	raw := testenv.File(t, "lumen-raw.exe")
	photos := t.TempDir()
	paths := core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	// One RAW held a quarter turn, one whose sensor data was cut short (its
	// embedded preview stands in), and one that is not a camera file at all.
	writeFile(t, filepath.Join(photos, "IMG_0001.DNG"), testenv.DNG{Orientation: 6}.Bytes(t))
	writeFile(t, filepath.Join(photos, "IMG_0002.dng"), testenv.DNG{Cut: true}.Bytes(t))
	writeFile(t, filepath.Join(photos, "IMG_0003.CR2"), []byte(string(make([]byte, 8192))+"not a camera file"))

	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.AddSource(ctx, photos); err != nil {
		t.Fatal(err)
	}
	cache := media.NewCache(paths.ThumbDir, media.Decoders{Raw: raw})
	ix := quiet(New(st, paths, cache, Models{}, &fakeEvents{}))
	ix.Start(ctx)
	defer ix.Stop()
	waitIdle(t, ix, 3)

	// Both readable files carry the camera's date, so they share its day.
	days, err := st.DayItems(ctx, "2024-03-05", "2024-03-05", core.View{}, core.TagConfidenceDefault)
	if err != nil || len(days) != 1 || len(days[0].Items) != 2 {
		t.Fatalf("day items = %+v, %v", days, err)
	}
	shapes := map[string][2]int{}
	for _, it := range days[0].Items {
		d, err := st.AssetDetail(ctx, it.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if d.Camera != "Lumen Test Camera" || it.Thumb == 0 || d.Caps.Original {
			t.Errorf("%s: camera %q thumb %d caps %+v", d.Paths[0], d.Camera, it.Thumb, d.Caps)
		}
		f, err := os.Open(cache.Path(it.ID, core.ThumbSmall))
		if err != nil {
			t.Fatalf("%s: thumbnail missing: %v", d.Paths[0], err)
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		shapes[filepath.Base(d.Paths[0])] = [2]int{cfg.Width, cfg.Height}
		if (it.W > it.H) != (cfg.Width > cfg.Height) {
			t.Errorf("%s: recorded %dx%d but its thumbnail is %dx%d", d.Paths[0], it.W, it.H, cfg.Width, cfg.Height)
		}
	}
	if s := shapes["IMG_0001.DNG"]; s[0] >= s[1] {
		t.Errorf("the turned RAW's thumbnail is %dx%d, want upright", s[0], s[1])
	}
	// The file LibRaw cannot read is skipped with a reason, not failed and retried.
	if p := ix.Progress(ctx); p.Skipped == 0 || p.Errors != 0 {
		t.Errorf("progress = %+v, want the unreadable file skipped and no errors", p)
	}
}
