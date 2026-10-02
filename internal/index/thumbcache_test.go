package index

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

// A thumbnail pair in the fixtures weighs 1,000 bytes.
const pairBytes = 1000

// thumbFixture is a library of n photos (ids 1..n, added in that order), each
// with a thumbnail pair in the cache and every stage finished except the
// embedding of the assets in wanted, which the image model has yet to read.
type thumbFixture struct {
	st    *store.Store
	cache *media.Cache
	ix    *Indexer
}

func newThumbFixture(tb testing.TB, n int, wanted ...int64) *thumbFixture {
	tb.Helper()
	ctx := context.Background()
	paths := core.PathsIn(tb.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		tb.Fatal(err)
	}
	items := make([]store.IngestItem, n)
	for i := range items {
		path := fmt.Sprintf(`C:\Photos\%d.jpg`, i+1)
		items[i] = store.IngestItem{SourceID: src.ID, Path: path, Size: 1, MTime: 1, Hash: []byte(path),
			Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}
	}
	if _, err := st.Ingest(ctx, items); err != nil {
		tb.Fatal(err)
	}
	cache := media.NewCache(paths.ThumbDir, thumbDecoder{})
	meta, thumbs, embeds, faces := []store.MetadataResult{}, []store.ThumbResult{}, []store.EmbedResult{}, []store.FaceResult{}
	for id := int64(1); id <= int64(n); id++ {
		meta = append(meta, store.MetadataResult{AssetID: id, Width: 4, Height: 3, Orientation: 1})
		thumbs = append(thumbs, store.ThumbResult{AssetID: id})
		if !slices.Contains(wanted, id) {
			embeds = append(embeds, store.EmbedResult{AssetID: id, Skipped: true})
		}
		faces = append(faces, store.FaceResult{AssetID: id, Skipped: true})
		small, large := bytes.Repeat([]byte{1}, 100), bytes.Repeat([]byte{2}, pairBytes-100)
		if err := cache.Write(id, media.Thumbs{Small: small, Large: large}); err != nil {
			tb.Fatal(err)
		}
	}
	for _, save := range []func() error{
		func() error { return st.SaveMetadata(ctx, "v", meta) },
		func() error { return st.SaveThumbnails(ctx, "v", thumbs) },
		func() error { return st.SaveEmbeddings(ctx, "m", "v", embeds) },
		func() error { return st.SaveFaces(ctx, "m", "v", faces) },
	} {
		if err := save(); err != nil {
			tb.Fatal(err)
		}
	}
	return &thumbFixture{st: st, cache: cache, ix: New(st, paths, cache, Models{}, &fakeEvents{})}
}

// thumbDecoder makes a thumbnail pair of fixed bytes, as a rebuild would.
type thumbDecoder struct{ media.Decoder }

func (thumbDecoder) Thumbnail(context.Context, string, core.Kind, int) (media.Thumbs, error) {
	return media.Thumbs{Small: bytes.Repeat([]byte{1}, 100), Large: bytes.Repeat([]byte{2}, pairBytes-100)}, nil
}

// cached lists which assets still have a thumbnail on disk.
func (f *thumbFixture) cached(n int) []int64 {
	var ids []int64
	for id := int64(1); id <= int64(n); id++ {
		if _, err := os.Stat(f.cache.Path(id, core.ThumbLarge)); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func TestCacheOverItsCapEvictsTheLeastRecentlyViewedDownToNinetyPercent(t *testing.T) {
	// 10 photos of 1,000 bytes: 10,000 in all, against a cap of 8,000 (target 7,200).
	f := newThumbFixture(t, 10, 6) // photo 6 still waits for the image model
	ctx := t.Context()
	check(t, f.st.TouchThumbs(ctx, map[int64]int64{1: 500, 2: 100, 3: 300, 4: 200, 5: 400}))
	f.cache.SetCap(8000)
	check(t, f.ix.evictThumbs(ctx, 4))

	// The never-viewed go first, oldest first: 7, 8 and 9, then it stops at 7,000.
	if got, want := f.cached(10), []int64{1, 2, 3, 4, 5, 6, 10}; !slices.Equal(got, want) {
		t.Fatalf("thumbnails left = %v, want %v", got, want)
	}
	if got := f.cache.Bytes(); got != 7000 {
		t.Fatalf("cache = %d bytes, want 7000", got)
	}

	// Cap lowered to 5,000 (target 4,500): photo 10, never viewed, then the
	// oldest views, 2 and 4. Photo 6 stays, whatever its age: the model needs it.
	f.cache.SetCap(5000)
	check(t, f.ix.evictThumbs(ctx, 4))
	if got, want := f.cached(10), []int64{1, 3, 5, 6}; !slices.Equal(got, want) {
		t.Fatalf("thumbnails left = %v, want %v", got, want)
	}
	if got := f.cache.Bytes(); got != 4000 {
		t.Fatalf("cache = %d bytes, want 4000", got)
	}
}

func TestEvictionNeverTakesWhatAnUnfinishedAIJobStillReads(t *testing.T) {
	f := newThumbFixture(t, 3, 1, 2, 3) // every embedding is still to do
	f.cache.SetCap(1000)
	check(t, f.ix.evictThumbs(t.Context(), 10))
	if got := f.cached(3); len(got) != 3 {
		t.Fatalf("thumbnails left = %v, want all three", got)
	}
	// The job finishing releases it.
	check(t, f.st.SaveEmbeddings(t.Context(), "m", "v", []store.EmbedResult{{AssetID: 1, Skipped: true}, {AssetID: 2, Skipped: true}}))
	check(t, f.ix.evictThumbs(t.Context(), 10))
	if got := f.cached(3); !slices.Equal(got, []int64{3}) {
		t.Fatalf("thumbnails left = %v, want [3]", got)
	}
}

func TestEvictionSeesViewsTheDatabaseHasNotHeardOfYet(t *testing.T) {
	f := newThumbFixture(t, 4)
	check(t, f.st.TouchThumbs(t.Context(), map[int64]int64{1: 100, 2: 200, 3: 300, 4: 400}))
	f.cache.Viewed(1) // shown just now: not in the database, but the newest
	f.cache.SetCap(3000)
	check(t, f.ix.evictThumbs(t.Context(), 10))
	if got, want := f.cached(4), []int64{1, 4}; !slices.Equal(got, want) {
		t.Fatalf("thumbnails left = %v, want %v", got, want)
	}
	// The pass saved the view first, so the database now ranks photo 1 newest
	// rather than leaving it at its old time (or marked as gone).
	if got := len(f.cache.PendingViews()); got != 0 {
		t.Fatalf("%d views still waiting after the pass", got)
	}
	ids, err := f.st.ThumbEvictionCandidates(t.Context(), 10)
	check(t, err)
	if want := []int64{4, 1}; !slices.Equal(ids, want) {
		t.Fatalf("eviction order now = %v, want %v", ids, want)
	}
}

func TestEvictedThumbnailsAreNotPickedAgainOnTheNextPass(t *testing.T) {
	f := newThumbFixture(t, 6)
	f.cache.SetCap(4000)
	check(t, f.ix.evictThumbs(t.Context(), 10)) // to 3,600: three left
	first := f.cached(6)
	if len(first) != 3 {
		t.Fatalf("thumbnails left = %v, want three", first)
	}
	// Another pass, with the cap lowered: it must take from what is left, not
	// walk the three it already removed (which free nothing).
	f.cache.SetCap(2000)
	check(t, f.ix.evictThumbs(t.Context(), 2))
	if got, want := f.cached(6), first[len(first)-1:]; !slices.Equal(got, want) {
		t.Fatalf("thumbnails left = %v, want %v", got, want)
	}
}

func TestEvictionWaitsWhileIndexingIsPaused(t *testing.T) {
	f := newThumbFixture(t, 4)
	f.ix.SetPaused(true)
	f.cache.SetCap(2000)
	done := make(chan error, 1)
	go func() { done <- f.ix.evictThumbs(t.Context(), 10) }()
	select {
	case <-done:
		t.Fatal("eviction ran while paused")
	case <-time.After(150 * time.Millisecond):
	}
	if got := f.cache.Bytes(); got != 4*pairBytes {
		t.Fatalf("cache shrank to %d while paused", got)
	}
	f.ix.SetPaused(false)
	select {
	case err := <-done:
		check(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("eviction did not resume")
	}
	if got := f.cache.Bytes(); got > 1800 {
		t.Fatalf("cache = %d bytes after resuming, want at most 1800", got)
	}
}

func TestEvictionIgnoresPowerSource(t *testing.T) {
	f := newThumbFixture(t, 2)
	f.ix.setHold(func() { f.ix.onBattery, f.ix.aiOnBattery = true, false }) // AI is held; this is not AI
	f.cache.SetCap(1000)
	check(t, f.ix.evictThumbs(t.Context(), 10))
	if got := f.cache.Bytes(); got > 900 {
		t.Fatalf("cache = %d bytes on battery, want it trimmed", got)
	}
}

func TestThumbCacheLoopMeasuresThenTrimsWhenTheCapChanges(t *testing.T) {
	f := newThumbFixture(t, 6)
	// A restart: a new cache over the same folder knows nothing of its size.
	restarted := media.NewCache(f.cache.Dir, media.Decoders{})
	ix := New(f.st, core.PathsIn(t.TempDir()), restarted, Models{}, &fakeEvents{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		ix.runThumbCache(ctx, thumbCacheTiming{cooldown: time.Millisecond, poll: time.Hour, retry: time.Millisecond, batch: 10})
		close(done)
	}()
	defer func() { cancel(); <-done }()

	waitFor(t, "the walk at start has counted the cache", func() bool { return restarted.Bytes() == 6*pairBytes })
	if mb, _ := f.st.ThumbCacheMB(ctx); restarted.Cap() != int64(mb)<<20 {
		t.Fatalf("cap = %d, want the stored %d MB", restarted.Cap(), mb)
	}
	if got := len(f.cached(6)); got != 6 {
		t.Fatalf("%d thumbnails left under a 2 GB cap, want all 6", got)
	}
	ix.SetThumbCacheMB(256)
	if restarted.Cap() != 256<<20 {
		t.Fatalf("cap = %d, want 256 MB", restarted.Cap())
	}
	restarted.SetCap(3000) // far below the 256 MB floor the setting allows, to keep the fixture small
	waitFor(t, "the cache is trimmed to the new cap", func() bool { return restarted.Bytes() <= 2700 })
	if got := len(f.cached(6)); got != 2 {
		t.Fatalf("%d thumbnails left, want 2", got)
	}
}

func TestThumbCacheLoopLogsADatabaseFailureAndKeepsGoing(t *testing.T) {
	f := newThumbFixture(t, 2)
	check(t, f.st.Close())
	var logs lockedBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		f.ix.runThumbCache(ctx, thumbCacheTiming{cooldown: time.Millisecond, poll: time.Hour, retry: time.Millisecond, batch: 10})
		close(done)
	}()
	// The loop sets the cap it reads at start (the default: the read failed); then a smaller one arrives.
	waitFor(t, "the loop has set its cap", func() bool { return f.cache.Cap() == int64(core.ThumbCacheDefaultMB)<<20 })
	f.cache.SetCap(1000)
	waitFor(t, "the failure is logged", func() bool { return strings.Contains(logs.String(), "trim thumbnail cache") })
	cancel()
	<-done
}

func TestSettingTheCapOnAnIndexerWithoutACacheIsHarmless(*testing.T) {
	var none *Indexer
	none.SetThumbCacheMB(512)
	New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}).SetThumbCacheMB(512)
}

func BenchmarkThumbCachePassOverFiveThousandFiles(b *testing.B) {
	const assets = 2500 // a thumbnail pair each: 5,000 files
	f := newThumbFixture(b, assets)
	thumbs, embeds := make([]store.ThumbResult, assets), make([]store.EmbedResult, assets)
	for i := range thumbs {
		thumbs[i] = store.ThumbResult{AssetID: int64(i + 1)}
		embeds[i] = store.EmbedResult{AssetID: int64(i + 1), Skipped: true}
	}
	small, large := bytes.Repeat([]byte{1}, 12<<10), bytes.Repeat([]byte{2}, 90<<10)
	for b.Loop() {
		b.StopTimer()
		for id := int64(1); id <= assets; id++ {
			if err := f.cache.Write(id, media.Thumbs{Small: small, Large: large}); err != nil {
				b.Fatal(err)
			}
		}
		// A rebuild bumps the version, which makes the assets candidates again.
		// It also asks the image model to read the new thumbnails again: say it did.
		if err := f.st.SaveThumbnails(b.Context(), "v", thumbs); err != nil {
			b.Fatal(err)
		}
		if err := f.st.SaveEmbeddings(b.Context(), "m", "v", embeds); err != nil {
			b.Fatal(err)
		}
		f.cache.SetCap(f.cache.Bytes() / 2)
		b.StartTimer()
		if err := f.ix.evictThumbs(b.Context(), 200); err != nil {
			b.Fatal(err)
		}
		if f.cache.Bytes() > f.cache.Cap() {
			b.Fatalf("cache = %d bytes, over its cap of %d", f.cache.Bytes(), f.cache.Cap())
		}
	}
}

func check(tb testing.TB, err error) {
	tb.Helper()
	if err != nil {
		tb.Fatal(err)
	}
}

// lockedBuffer is a log sink the loop and the test can share.
type lockedBuffer struct {
	mu  sync.Mutex // guards buf
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAThumbnailRebuiltAfterEvictionBecomesEvictableAgain(t *testing.T) {
	f := newThumbFixture(t, 3)
	f.cache.SetCap(1000)
	check(t, f.ix.evictThumbs(t.Context(), 10))
	if got := f.cached(3); len(got) != 0 {
		t.Fatalf("thumbnails left = %v, want none", got)
	}
	// A stage (not a request) makes photo 2's thumbnail again.
	orig := media.Original{Path: "2.jpg", Kind: core.KindPhoto}
	check(t, f.cache.Ensure(t.Context(), 2, core.ThumbLarge, orig))
	check(t, f.cache.FlushViews(t.Context(), f.st))
	ids, err := f.st.ThumbEvictionCandidates(t.Context(), 10)
	check(t, err)
	if want := []int64{2}; !slices.Equal(ids, want) {
		t.Fatalf("candidates = %v, want %v: the rebuilt files count toward the cap, so they must be evictable", ids, want)
	}
}

func TestAnAssetWhoseFilesCouldNotBeRemovedStaysACandidate(t *testing.T) {
	f := newThumbFixture(t, 3)
	small := f.cache.Path(1, core.ThumbSmall)
	check(t, os.Remove(small))
	check(t, os.MkdirAll(small, 0o750)) // a folder that can't be removed
	check(t, os.WriteFile(filepath.Join(small, "x"), []byte("x"), 0o600))
	f.cache.SetCap(1000)
	check(t, f.ix.evictThumbs(t.Context(), 10))
	ids, err := f.st.ThumbEvictionCandidates(t.Context(), 10)
	check(t, err)
	if want := []int64{1}; !slices.Equal(ids, want) {
		t.Fatalf("candidates = %v, want %v: photo 1 still has files, so it must be tried again", ids, want)
	}
}

func TestModelInputMakesTheThumbnailAgainIfEvictionTakesItBeforeItIsRead(t *testing.T) {
	f := newThumbFixture(t, 1)
	orig := media.Original{Path: "1.jpg", Kind: core.KindPhoto}
	reads := 0
	read := func(path string, _ int) ([]byte, error) {
		reads++
		if reads == 1 { // the cap evicts it between Ensure and the read
			if _, evicted, err := f.cache.EvictOne(t.Context(), 1); err != nil || !evicted {
				t.Fatalf("setup eviction: %v, %v", evicted, err)
			}
		}
		return os.ReadFile(path)
	}
	px, err := modelInput(t.Context(), f.cache, 1, orig, 224, read)
	if err != nil || len(px) != pairBytes-100 {
		t.Fatalf("modelInput = %d bytes, %v; want the rebuilt 1024 px file", len(px), err)
	}
	if reads != 2 {
		t.Fatalf("%d reads, want 2", reads)
	}
}

func TestModelInputGivesUpAfterOneRetryAndReportsOtherFailuresAtOnce(t *testing.T) {
	f := newThumbFixture(t, 1)
	orig := media.Original{Path: "1.jpg", Kind: core.KindPhoto}
	reads := 0
	broken := func(string, int) ([]byte, error) { reads++; return nil, errors.New("not a picture") }
	if _, err := modelInput(t.Context(), f.cache, 1, orig, 224, broken); err == nil || reads != 1 {
		t.Fatalf("a corrupt file: %d reads, %v; want one read and the error", reads, err)
	}
}
