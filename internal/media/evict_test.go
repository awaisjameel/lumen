package media

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
)

// pair is a thumbnail pair of known sizes: 100 bytes and 1,000.
func pair() Thumbs {
	return Thumbs{Small: bytes.Repeat([]byte{1}, 100), Large: bytes.Repeat([]byte{2}, 1000)}
}

func TestCacheCountsWhatItWritesAndWhatItReplaces(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	check(t, c.Write(1, pair()))
	if got := c.Bytes(); got != 1100 {
		t.Fatalf("after one pair: %d bytes, want 1100", got)
	}
	video := pair()
	video.Sprite = bytes.Repeat([]byte{3}, 500)
	check(t, c.Write(2, video))
	if got := c.Bytes(); got != 2700 {
		t.Fatalf("after a video with a sprite: %d bytes, want 2700", got)
	}
	// A rebuild replaces files: only the difference counts.
	bigger := Thumbs{Small: bytes.Repeat([]byte{4}, 150), Large: bytes.Repeat([]byte{5}, 900)}
	check(t, c.Write(1, bigger))
	if got := c.Bytes(); got != 2700-1100+1050 {
		t.Fatalf("after a rebuild: %d bytes, want %d", got, 2700-1100+1050)
	}
}

func TestCacheLeavesVideoFramesOutOfItsTotal(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	check(t, c.writeAtomic(c.FramePath(1, 0), bytes.Repeat([]byte{9}, 4000)))
	check(t, c.Measure(t.Context()))
	if got := c.Bytes(); got != 0 {
		t.Fatalf("a frame counted: %d bytes, want 0", got)
	}
}

func TestMeasureFindsWhatIsOnDiskAndCorrectsADriftedTotal(t *testing.T) {
	dir := t.TempDir()
	first := NewCache(dir, Decoders{})
	video := pair()
	video.Sprite = bytes.Repeat([]byte{3}, 500)
	check(t, first.Write(1, pair()))
	check(t, first.Write(300, video)) // another fan-out folder

	c := NewCache(dir, Decoders{}) // a start: it knows nothing yet
	if c.Bytes() != 0 {
		t.Fatalf("a new cache claims %d bytes", c.Bytes())
	}
	check(t, c.Measure(t.Context()))
	if got := c.Bytes(); got != 2700 {
		t.Fatalf("measured %d bytes, want 2700", got)
	}
	c.size.add(5000) // drift: the user removed files by hand, say
	check(t, c.Measure(t.Context()))
	if got := c.Bytes(); got != 2700 {
		t.Fatalf("measured again: %d bytes, want 2700", got)
	}
}

func TestMeasureStopsWhenCancelledAndKeepsTheTotal(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	check(t, c.Write(1, pair()))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := c.Measure(ctx); err == nil {
		t.Fatal("a cancelled walk reported success")
	}
	if got := c.Bytes(); got != 1100 {
		t.Fatalf("total after a cancelled walk = %d, want it kept at 1100", got)
	}
}

func TestMeasureKeepsChangesMadeWhileItWalks(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	c.size.beginWalk()
	c.size.add(700) // a Write during the walk, after the walker passed its folder
	c.size.set(300)
	if got := c.Bytes(); got != 1000 {
		t.Fatalf("walk of 300 plus 700 written meanwhile = %d, want 1000", got)
	}
}

func TestEvictRemovesTheThumbnailsAndSpriteAndSaysHowMuchItFreed(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	video := pair()
	video.Sprite = bytes.Repeat([]byte{3}, 500)
	check(t, c.Write(1, pair()))
	check(t, c.Write(2, video))
	check(t, c.Write(3, pair()))
	freed, err := c.Evict(t.Context(), []int64{2, 3, 99}) // 99 has nothing cached
	check(t, err)
	if freed != 1600+1100 {
		t.Fatalf("freed %d bytes, want 2700", freed)
	}
	if got := c.Bytes(); got != 1100 {
		t.Fatalf("total after eviction = %d, want 1100", got)
	}
	for _, p := range []string{c.Path(2, 256), c.Path(2, 1024), c.SpritePath(2), c.Path(3, 256), c.Path(3, 1024)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived eviction (%v)", p, err)
		}
	}
	if _, err := os.Stat(c.Path(1, 256)); err != nil {
		t.Errorf("an asset that was not evicted lost its thumbnail: %v", err)
	}
}

func TestEvictedThumbnailIsRebuiltOnItsNextRequest(t *testing.T) {
	d := &countingDecoder{}
	c := NewCache(t.TempDir(), d)
	check(t, c.Write(1, pair()))
	orig := Original{Path: "photo.jpg", Kind: core.KindPhoto}
	check(t, c.Ensure(t.Context(), 1, core.ThumbSmall, orig))
	if d.calls != 0 {
		t.Fatalf("a cached thumbnail was decoded again (%d calls)", d.calls)
	}
	_, err := c.Evict(t.Context(), []int64{1})
	check(t, err)
	check(t, c.Ensure(t.Context(), 1, core.ThumbSmall, orig))
	if d.calls != 1 {
		t.Fatalf("%d decodes after eviction, want 1", d.calls)
	}
	if got := c.Bytes(); got == 0 {
		t.Fatal("the rebuilt thumbnail was not counted")
	}
}

func TestEvictTakesTheAssetsWriteLockSoItNeverRacesARebuild(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	check(t, c.Write(1, pair()))
	release := make(chan struct{})
	holding := make(chan struct{})
	go func() {
		_ = c.lockedWrite(1, func() error { // a rebuild in the middle of its write
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	done := make(chan struct{})
	go func() {
		_, _ = c.Evict(t.Context(), []int64{1})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("eviction went ahead while the asset was being written")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("eviction never finished once the write ended")
	}
}

func TestEvictLeavesAssetsShownSinceTheDatabaseLastHeard(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	check(t, c.Write(1, pair()))
	check(t, c.Write(2, pair()))
	c.Viewed(1)
	freed, err := c.Evict(t.Context(), []int64{1, 2})
	check(t, err)
	if freed != 1100 {
		t.Fatalf("freed %d bytes, want only asset 2's 1100", freed)
	}
	if _, err := os.Stat(c.Path(1, 256)); err != nil {
		t.Fatalf("a thumbnail shown a moment ago was evicted: %v", err)
	}
	// Once the database has the view, it is the database's order that counts.
	c.ViewsSaved(c.PendingViews())
	if len(c.PendingViews()) != 0 {
		t.Fatal("saved views were kept")
	}
}

func TestViewsSavedKeepsAViewMadeAfterTheCopyWasTaken(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	c.Viewed(1)
	taken := c.PendingViews()
	c.views.mu.Lock()
	c.views.seen[1] = taken[1] + 5 // shown again after the copy
	c.views.mu.Unlock()
	c.ViewsSaved(taken)
	if _, ok := c.PendingViews()[1]; !ok {
		t.Fatal("a view newer than the saved one was forgotten")
	}
}

func TestCacheWakesWhoeverWaitsWhenItPassesItsCapOrTheCapChanges(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	woken := func() bool {
		select {
		case <-c.Wake():
			return true
		default:
			return false
		}
	}
	check(t, c.Write(1, pair()))
	if woken() {
		t.Fatal("woken with no cap set")
	}
	c.SetCap(5000)
	if !woken() {
		t.Fatal("a changed cap did not wake the loop")
	}
	check(t, c.Write(2, pair()))
	if woken() {
		t.Fatal("woken while under the cap")
	}
	c.SetCap(1500)
	woken()
	check(t, c.Write(3, pair()))
	if !woken() {
		t.Fatal("passing the cap did not wake the loop")
	}
	if c.Cap() != 1500 {
		t.Fatalf("cap = %d, want 1500", c.Cap())
	}
}

func BenchmarkEvictFiveThousandFiles(b *testing.B) {
	c := NewCache(b.TempDir(), Decoders{})
	small, large := bytes.Repeat([]byte{1}, 12<<10), bytes.Repeat([]byte{2}, 90<<10)
	const assets = 2500 // a thumbnail pair each: 5,000 files
	ids := make([]int64, assets)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	for b.Loop() {
		b.StopTimer()
		for _, id := range ids {
			for path, data := range map[string][]byte{c.Path(id, core.ThumbSmall): small, c.Path(id, core.ThumbLarge): large} {
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0o600); err != nil {
					b.Fatal(err)
				}
			}
		}
		b.StartTimer()
		if _, err := c.Evict(b.Context(), ids); err != nil {
			b.Fatal(err)
		}
	}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestRebuildThroughEnsureRecordsAViewSoTheDatabaseClearsTheEvictedMarker(t *testing.T) {
	d := &countingDecoder{}
	c := NewCache(t.TempDir(), d)
	orig := Original{Path: "photo.jpg", Kind: core.KindPhoto}
	check(t, c.Ensure(t.Context(), 1, core.ThumbSmall, orig)) // made now
	if _, ok := c.PendingViews()[1]; !ok {
		t.Fatal("a rebuild did not record a view")
	}
	c.ViewsSaved(c.PendingViews())
	check(t, c.Ensure(t.Context(), 1, core.ThumbSmall, orig)) // already there: nothing happens
	if len(c.PendingViews()) != 0 {
		t.Fatal("finding the thumbnail in the cache recorded a view")
	}
}

func TestEvictOneReportsAFileItCouldNotRemoveAndLeavesTheAssetUnevicted(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	check(t, c.Write(1, pair()))
	// A non-empty folder where the small thumbnail was: removing it fails.
	small := c.Path(1, core.ThumbSmall)
	check(t, os.Remove(small))
	check(t, os.MkdirAll(small, 0o750))
	check(t, os.WriteFile(filepath.Join(small, "x"), []byte("x"), 0o600))
	freed, evicted, err := c.EvictOne(t.Context(), 1)
	if err == nil || evicted {
		t.Fatalf("EvictOne = evicted %v, %v; want it to report the failure", evicted, err)
	}
	if freed != 1000 {
		t.Fatalf("freed %d, want the large thumbnail's 1000", freed)
	}
	if _, _, err := c.EvictOne(t.Context(), 2); err != nil { // nothing cached is evicted without error
		t.Fatal(err)
	}
}

func TestEvictOneRechecksForAViewAfterTakingTheLock(t *testing.T) {
	c := NewCache(t.TempDir(), Decoders{})
	check(t, c.Write(1, pair()))
	release, holding := make(chan struct{}), make(chan struct{})
	go func() {
		_ = c.lockedWrite(1, func() error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding
	type result struct {
		evicted bool
		err     error
	}
	got := make(chan result, 1)
	go func() {
		_, evicted, err := c.EvictOne(t.Context(), 1)
		got <- result{evicted, err}
	}()
	time.Sleep(50 * time.Millisecond) // EvictOne is now waiting for the lock
	c.Viewed(1)                       // a request shows it while it waits
	close(release)
	r := <-got
	if r.err != nil || r.evicted {
		t.Fatalf("EvictOne = evicted %v, %v; want it left alone", r.evicted, r.err)
	}
	if _, err := os.Stat(c.Path(1, core.ThumbSmall)); err != nil {
		t.Fatalf("a thumbnail shown while eviction waited was removed: %v", err)
	}
}
