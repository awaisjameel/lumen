package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"lumen/internal/core"
)

// copyOf is a plain JPEG of 4000 × 3000 that the rule finds nothing special
// about: each test changes the one thing it is about.
func copyOf(id int64, change func(*core.DuplicateItem)) core.DuplicateItem {
	it := core.DuplicateItem{
		ID: id, Paths: []string{`C:\Photos\IMG_0001.jpg`}, Size: 4_000_000, Width: 4000, Height: 3000,
		Ext: ".jpg", AddedAt: 1_700_000_000, Mtimes: []int64{1_600_000_000},
	}
	if change != nil {
		change(&it)
	}
	return it
}

// decides checks that the rule keeps better over worse whichever way round
// the pair comes, and that the two compare as opposites. The better item gets
// the higher id, so the final tie-break (the lower id) never passes a case the
// step under test failed to decide.
func decides(t *testing.T, name string, better, worse core.DuplicateItem) {
	t.Helper()
	better.ID, worse.ID = 2, 1
	if betterCopy(better, worse) >= 0 || betterCopy(worse, better) <= 0 {
		t.Errorf("%s: betterCopy(better, worse) = %d, betterCopy(worse, better) = %d; want negative and positive",
			name, betterCopy(better, worse), betterCopy(worse, better))
	}
	items := []core.DuplicateItem{worse, better}
	if got := bestOf(items); items[got].ID != better.ID {
		t.Errorf("%s: kept item %d, want %d", name, items[got].ID, better.ID)
	}
	slices.Reverse(items)
	if got := bestOf(items); items[got].ID != better.ID {
		t.Errorf("%s: reversed, kept item %d, want %d", name, items[got].ID, better.ID)
	}
}

func TestBestCopyStepOneKeepsTheHigherResolution(t *testing.T) {
	big := copyOf(1, nil)
	small := copyOf(2, func(it *core.DuplicateItem) { it.Width, it.Height = 2000, 1500 })
	decides(t, "resolution alone", big, small)
	// The pixel count counts, not the longer side.
	wide := copyOf(3, func(it *core.DuplicateItem) { it.Width, it.Height = 6000, 1000 })
	decides(t, "width times height", big, wide)
	// Resolution outranks every later step.
	richer := copyOf(4, func(it *core.DuplicateItem) {
		it.Width, it.Height = 2000, 1500
		it.Ext, it.Camera, it.ExifDate, it.GPS = ".png", true, true, true
		it.Size = 90_000_000
		it.Mtimes = []int64{1}
	})
	decides(t, "resolution over everything", big, richer)
}

func TestBestCopyStepTwoKeepsTheOriginalCameraFile(t *testing.T) {
	camera := copyOf(1, func(it *core.DuplicateItem) { it.Camera, it.ExifDate = true, true })
	decides(t, "camera and date", camera, copyOf(2, nil))
	// Both signals are needed: a camera without a date, or a date without a camera, is not an original.
	decides(t, "camera alone is not enough", camera, copyOf(3, func(it *core.DuplicateItem) { it.Camera = true }))
	decides(t, "date alone is not enough", camera, copyOf(4, func(it *core.DuplicateItem) { it.ExifDate = true }))
	// Neither signal alone counts as one: a larger file with no camera outranks each.
	larger := copyOf(6, func(it *core.DuplicateItem) { it.Size = 5_000_000 })
	decides(t, "camera alone is not an original", larger, copyOf(7, func(it *core.DuplicateItem) { it.Camera = true }))
	decides(t, "date alone is not an original", larger, copyOf(8, func(it *core.DuplicateItem) { it.ExifDate = true }))
	// The camera file beats a better format, a bigger file and a place.
	png := copyOf(5, func(it *core.DuplicateItem) {
		it.Ext, it.Size, it.GPS, it.Mtimes = ".png", 20_000_000, true, []int64{1}
	})
	decides(t, "camera file over format, size, place and age", camera, png)
}

func TestBestCopyStepTwoRanksFormats(t *testing.T) {
	ranked := [][]string{{".dng", ".cr2", ".nef", ".arw"}, {".png", ".tif", ".tiff", ".heic", ".heif", ".avif"}, {".jpg", ".jpeg", ".jpe"}, {".webp", ".gif", ".bmp"}}
	for hi := range ranked {
		for _, better := range ranked[hi] {
			for _, worse := range slices.Concat(ranked[hi+1:]...) {
				decides(t, better+" over "+worse,
					copyOf(1, func(it *core.DuplicateItem) { it.Ext = better }),
					copyOf(2, func(it *core.DuplicateItem) { it.Ext = worse }))
			}
		}
	}
	// Upper-case extensions rank the same, and two formats of one rank do not decide.
	if got := formatRank(".PNG"); got != formatRank(".png") {
		t.Errorf("formatRank(.PNG) = %d, want %d", got, formatRank(".png"))
	}
	png := copyOf(1, func(it *core.DuplicateItem) { it.Ext = ".png" })
	heic := copyOf(2, func(it *core.DuplicateItem) { it.Ext = ".heic" })
	if betterCopy(png, heic) >= 0 || betterCopy(heic, png) <= 0 {
		t.Error("two formats of one rank must tie and fall through to the later steps (here: the lower id)")
	}
	// A better format beats a bigger file.
	decides(t, "format over size",
		copyOf(3, func(it *core.DuplicateItem) { it.Ext, it.Size = ".png", 1 }),
		copyOf(4, func(it *core.DuplicateItem) { it.Size = 9_000_000 }))
}

func TestBestCopyStepTwoKeepsTheCopyWithoutAMessengerName(t *testing.T) {
	for _, name := range []string{
		`C:\In\IMG-20240101-WA0001.jpg`, `C:\In\img-20240101-wa0012.jpg`, `C:\In\IMG_0001 (1).jpg`,
		`C:\In\IMG_0001-edited.jpg`, `C:\In\IMG_0001-EDITED.jpg`, `C:/In/sunset (12).jpg`,
	} {
		decides(t, name, copyOf(1, nil), copyOf(2, func(it *core.DuplicateItem) { it.Paths = []string{name} }))
	}
	for _, name := range []string{
		`C:\In\IMG_0001.jpg`, `C:\In\IMG-20240101.jpg`, `C:\In\edited holiday.jpg`, `C:\In\WA\IMG_0001.jpg`, `C:\In\Trip (2024)\a.jpg`,
	} {
		if resaved(copyOf(1, func(it *core.DuplicateItem) { it.Paths = []string{name} })) {
			t.Errorf("%s was taken for a re-save", name)
		}
	}
	// A copy that also has a file under a camera name can keep that file.
	mixed := copyOf(3, func(it *core.DuplicateItem) {
		it.Paths = []string{`C:\In\IMG-20240101-WA0001.jpg`, `C:\In\IMG_0001.jpg`}
	})
	if resaved(mixed) {
		t.Error("an item with one ordinary name was taken for a re-save")
	}
	// It outranks size, but not the format before it.
	decides(t, "re-save name over size",
		copyOf(4, func(it *core.DuplicateItem) { it.Size = 100 }),
		copyOf(5, func(it *core.DuplicateItem) { it.Paths = []string{`C:\In\IMG-20240101-WA0001.jpg`} }))
	decides(t, "format over re-save name",
		copyOf(6, func(it *core.DuplicateItem) { it.Ext, it.Paths = ".png", []string{`C:\In\IMG-20240101-WA0001.png`} }),
		copyOf(7, nil))
}

func TestBestCopyStepThreeKeepsTheLargerFile(t *testing.T) {
	decides(t, "size alone", copyOf(1, nil), copyOf(2, func(it *core.DuplicateItem) { it.Size = 1_000_000 }))
	// Size outranks metadata and age.
	decides(t, "size over metadata and age",
		copyOf(3, nil),
		copyOf(4, func(it *core.DuplicateItem) {
			it.Size, it.GPS, it.ExifDate, it.Mtimes = 1_000_000, true, true, []int64{1}
		}))
}

func TestBestCopyStepFourKeepsTheMoreCompleteMetadata(t *testing.T) {
	for name, change := range map[string]func(*core.DuplicateItem){
		"date":   func(it *core.DuplicateItem) { it.ExifDate = true },
		"gps":    func(it *core.DuplicateItem) { it.GPS = true },
		"camera": func(it *core.DuplicateItem) { it.Camera = true },
	} {
		decides(t, name+" alone", copyOf(1, change), copyOf(2, nil))
	}
	two := copyOf(3, func(it *core.DuplicateItem) { it.GPS, it.Camera = true, true })
	one := copyOf(4, func(it *core.DuplicateItem) { it.ExifDate = true })
	decides(t, "two points over one", two, one)
	// Metadata outranks age.
	decides(t, "metadata over age", copyOf(5, func(it *core.DuplicateItem) { it.GPS = true }),
		copyOf(6, func(it *core.DuplicateItem) { it.Mtimes = []int64{1} }))
	if completeness(copyOf(7, func(it *core.DuplicateItem) { it.GPS, it.Camera, it.ExifDate = true, true, true })) != 3 {
		t.Error("a photo with all three should score 3")
	}
}

func TestBestCopyStepFiveKeepsTheOldestFile(t *testing.T) {
	older := copyOf(1, func(it *core.DuplicateItem) { it.Mtimes = []int64{1_500_000_000} })
	decides(t, "mtime alone", older, copyOf(2, nil))
	// An item's oldest file counts.
	decides(t, "oldest of several files",
		copyOf(3, func(it *core.DuplicateItem) {
			it.Paths, it.Mtimes = []string{`C:\a.jpg`, `C:\b.jpg`}, []int64{1_650_000_000, 1_400_000_000}
		}),
		copyOf(4, nil))
	// Without a recorded time, when Lumen first saw it decides; a missing time never wins.
	decides(t, "added at when there is no mtime",
		copyOf(5, func(it *core.DuplicateItem) { it.Mtimes, it.AddedAt = nil, 100 }),
		copyOf(6, func(it *core.DuplicateItem) { it.Mtimes, it.AddedAt = nil, 200 }))
	decides(t, "a zero mtime is not the oldest", copyOf(7, nil),
		copyOf(8, func(it *core.DuplicateItem) { it.Mtimes = []int64{0} }))
	// Everything equal: the lower id, so the order of the rows never matters.
	first, second := copyOf(9, nil), copyOf(10, nil)
	if betterCopy(first, second) >= 0 || betterCopy(second, first) <= 0 {
		t.Error("two identical items must fall back to the lower id")
	}
}

func TestBestCopyOfMixedGroupsFollowsTheDocumentedOrder(t *testing.T) {
	// The group's best is the original camera JPEG of the highest resolution,
	// not the larger PNG screenshot, the messenger copy or the small WebP.
	original := copyOf(1, func(it *core.DuplicateItem) {
		it.Camera, it.ExifDate, it.GPS = true, true, true
		it.Size = 5_000_000
	})
	screenshot := copyOf(2, func(it *core.DuplicateItem) { it.Ext, it.Size = ".png", 30_000_000 })
	whatsapp := copyOf(3, func(it *core.DuplicateItem) {
		it.Paths, it.Size, it.Width, it.Height = []string{`C:\In\IMG-20240101-WA0001.jpg`}, 200_000, 1600, 1200
	})
	thumbnail := copyOf(4, func(it *core.DuplicateItem) { it.Width, it.Height, it.Ext = 640, 480, ".webp" })
	group := []core.DuplicateItem{whatsapp, thumbnail, screenshot, original}
	for range group {
		if got := group[bestOf(group)].ID; got != original.ID {
			t.Fatalf("kept %d, want the original camera file", got)
		}
		group = append(group[1:], group[0])
	}

	// Without the camera file, and at one resolution (the default), the PNG wins on format,
	// then the JPEG, then the messenger copy.
	rest := []core.DuplicateItem{
		copyOf(3, func(it *core.DuplicateItem) {
			it.Paths = []string{`C:\In\IMG-20240101-WA0001.jpg`}
			it.Size = 9_000_000
		}),
		copyOf(5, func(it *core.DuplicateItem) { it.Size = 2_000_000 }),
		screenshot,
	}
	var order []int64
	for len(rest) > 0 {
		best := bestOf(rest)
		order = append(order, rest[best].ID)
		rest = slices.Delete(rest, best, best+1)
	}
	if !slices.Equal(order, []int64{2, 5, 3}) {
		t.Fatalf("order = %v, want the PNG, the JPEG, then the messenger copy", order)
	}

	// The WebP is the smallest picture, so it loses to the messenger JPEG
	// although the JPEG ranks last on every later step.
	if got := bestOf([]core.DuplicateItem{thumbnail, whatsapp}); got != 1 {
		t.Fatal("kept the low resolution WebP over the messenger JPEG")
	}
	if bestOf(nil) != -1 {
		t.Fatal("no items have no best")
	}
}

func TestExactCopiesKeepTheOldestFileThenTheShortestPath(t *testing.T) {
	for _, row := range []struct {
		name   string
		paths  []string
		mtimes []int64
		want   string
	}{
		{"the oldest file", []string{`C:\a\copy.jpg`, `C:\b\IMG_1.jpg`, `C:\c\x.jpg`}, []int64{300, 100, 200}, `C:\b\IMG_1.jpg`},
		{"the shortest path of the oldest", []string{`C:\long\folder\IMG_1.jpg`, `C:\x\IMG_1.jpg`, `C:\y\newer.jpg`}, []int64{100, 100, 150}, `C:\x\IMG_1.jpg`},
		{"the first in path order when all tie", []string{`C:\b\a.jpg`, `C:\a\b.jpg`}, []int64{5, 5}, `C:\a\b.jpg`},
		{"a file with a recorded time over one without", []string{`C:\a.jpg`, `C:\long\b.jpg`}, []int64{0, 7}, `C:\long\b.jpg`},
		{"the shortest path without any times", []string{`C:\long\folder\a.jpg`, `C:\a.jpg`}, nil, `C:\a.jpg`},
		{"the only file", []string{`C:\a.jpg`}, []int64{1}, `C:\a.jpg`},
	} {
		it := core.DuplicateItem{ID: 1, Paths: row.paths, Mtimes: row.mtimes}
		if got := keptPath(it); got != row.want {
			t.Errorf("%s: kept %q, want %q", row.name, got, row.want)
		}
	}
	if keptPath(core.DuplicateItem{}) != "" {
		t.Error("an item with no files keeps nothing")
	}
}

func TestDuplicateGroupsMarkTheBestCopyAndTheFileToKeep(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	// An exact pair of files (one asset), plus a similar pair of photos.
	exactA := item(src.ID, `C:\Photos\2024\IMG_0001.jpg`, "a")
	exactB := item(src.ID, `C:\Photos\IMG_0001 (1).jpg`, "a")
	exactA.MTime, exactB.MTime = 3_000_000_000_000, 2_000_000_000_000 // nanoseconds: the second file is the older
	small := item(src.ID, `C:\Photos\small.jpg`, "small")
	big := item(src.ID, `C:\Photos\big.png`, "bigger-content")
	check(t, errOf(st.Ingest(ctx, []IngestItem{exactA, exactB, small, big})))
	ids := map[string]int64{}
	for name, it := range map[string]IngestItem{"exact": exactA, "small": small, "big": big} {
		var id int64
		check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets WHERE content_hash = ?`, it.Hash).Scan(&id))
		ids[name] = id
	}
	for name, dims := range map[string][2]int{"exact": {100, 100}, "small": {1000, 800}, "big": {2000, 1600}} {
		_, err := st.w.ExecContext(ctx, `UPDATE assets SET width = ?, height = ? WHERE id = ?`, dims[0], dims[1], ids[name])
		check(t, err)
	}
	for _, id := range []int64{ids["small"], ids["big"]} {
		_, err := st.w.ExecContext(ctx, `UPDATE assets SET phash = 0, thumb_version = 1 WHERE id = ?`, id)
		check(t, err)
	}
	// Only the big one has the camera, date and place the rule reads.
	_, err = st.w.ExecContext(ctx, `UPDATE assets SET camera = 'Canon', date_from = 'exif', lat = 1.5, lon = 2.5, added_at = 4242 WHERE id = ?`, ids["big"])
	check(t, err)

	groups, err := st.DuplicateGroups(ctx)
	check(t, err)
	if len(groups) != 2 || groups[0].Kind != core.DuplicateExact || groups[1].Kind != core.DuplicateSimilar {
		t.Fatalf("groups = %+v", groups)
	}
	exact := groups[0].Items[0]
	if !exact.Best || exact.Keep != `C:\Photos\IMG_0001 (1).jpg` {
		t.Fatalf("exact item best = %v, keep = %q; want the older file kept", exact.Best, exact.Keep)
	}
	// Paths come in path order; each time is the one of the same index, in seconds.
	if !slices.Equal(exact.Paths, []string{`C:\Photos\2024\IMG_0001.jpg`, `C:\Photos\IMG_0001 (1).jpg`}) ||
		!slices.Equal(exact.Mtimes, []int64{3000, 2000}) || exact.Ext != ".jpg" {
		t.Fatalf("exact item = %+v", exact)
	}
	similar := groups[1].Items
	if len(similar) != 2 {
		t.Fatalf("similar group = %+v", similar)
	}
	best := 0
	for _, it := range similar {
		if it.Best {
			best++
			if it.ID != ids["big"] || it.Keep != "" {
				t.Fatalf("best item = %+v, want the bigger photo and no kept path", it)
			}
		}
		if it.ID == ids["big"] && (it.Ext != ".png" || !it.Camera || !it.ExifDate || !it.GPS || it.AddedAt != 4242 || len(it.Mtimes) != 1) {
			t.Fatalf("big item = %+v, want its metadata filled", it)
		}
		if it.ID == ids["small"] && (it.Camera || it.ExifDate || it.GPS) {
			t.Fatalf("small item = %+v, want no metadata", it)
		}
	}
	if best != 1 {
		t.Fatalf("%d best items in the similar group, want one", best)
	}
}

func TestCopiesToRecycleNamesOnlyTheFilesAskedFor(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	a := item(src.ID, `C:\Photos\a.jpg`, "a")
	b := item(src.ID, `C:\Photos\Copy\b.jpg`, "a")
	c := item(src.ID, `C:\Photos\c.jpg`, "a")
	other := item(src.ID, `C:\Photos\other.jpg`, "other")
	a.Size, a.MTime = 11, 111
	check(t, errOf(st.Ingest(ctx, []IngestItem{a, b, c, other})))
	var id, otherID int64
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets WHERE content_hash = ?`, a.Hash).Scan(&id))
	check(t, st.r.QueryRowContext(ctx, `SELECT id FROM assets WHERE content_hash = ?`, other.Hash).Scan(&otherID))

	// Paths compare without case; c is in neither list, so it is not named.
	kept, extra, err := st.CopiesToRecycle(ctx, id, `c:\photos\A.JPG`, []string{`C:\Photos\Copy\b.jpg`, `C:\PHOTOS\COPY\B.JPG`})
	check(t, err)
	if kept.Path != a.Path || kept.AssetID != id || kept.Size != 11 || kept.MTime != 111 {
		t.Fatalf("kept = %+v, want a.jpg as recorded", kept)
	}
	if len(extra) != 1 || extra[0].Path != b.Path || extra[0].AssetID != id {
		t.Fatalf("extra = %+v, want b.jpg once", extra)
	}

	// Every request that names something else is refused, and names nothing.
	for name, ask := range map[string]struct {
		id      int64
		keep    string
		recycle []string
	}{
		"nothing to recycle":     {id, a.Path, nil},
		"the kept file listed":   {id, a.Path, []string{b.Path, a.Path}},
		"another photo's file":   {id, a.Path, []string{b.Path, other.Path}},
		"an unknown file":        {id, a.Path, []string{`C:\Photos\missing.jpg`}},
		"another photo's keeper": {id, other.Path, []string{b.Path}},
		"no keeper":              {id, "", []string{b.Path}},
		"an unknown photo":       {9999, a.Path, []string{b.Path}},
	} {
		if k, got, err := st.CopiesToRecycle(ctx, ask.id, ask.keep, ask.recycle); !errors.Is(err, ErrNotFound) || len(got) != 0 || k.Path != "" {
			t.Errorf("%s: got %+v, %v, %v; want ErrNotFound and nothing named", name, k, got, err)
		}
	}

	// A hidden, trashed or missing photo is not one the Duplicates screen lists.
	for _, set := range []string{"hidden = 1", "trashed_at = 5", "missing_since = 5"} {
		_, err := st.w.ExecContext(ctx, `UPDATE assets SET `+set+` WHERE id = ?`, id)
		check(t, err)
		if _, _, err := st.CopiesToRecycle(ctx, id, a.Path, []string{b.Path}); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", set, err)
		}
		_, err = st.w.ExecContext(ctx, `UPDATE assets SET hidden = 0, trashed_at = NULL, missing_since = NULL WHERE id = ?`, id)
		check(t, err)
	}
	if _, _, err := st.CopiesToRecycle(ctx, otherID, other.Path, []string{other.Path}); !errors.Is(err, ErrNotFound) {
		t.Errorf("a single file has nothing to recycle: err = %v", err)
	}
}
