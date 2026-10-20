package index

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/store"
)

type fakeEvents struct{ changed atomic.Int32 }

func (*fakeEvents) Progress(core.Progress)        {}
func (f *fakeEvents) LibraryChanged(core.Changes) { f.changed.Add(1) }

func TestReportLoopLogsVisibilityReadFailures(t *testing.T) {
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// A cancelled read is normal shutdown and must not become a background error.
	ix := quiet(New(st, paths, nil, Models{}, &fakeEvents{}))
	ix.reportLoop(ctx)
	if logs.Len() != 0 {
		t.Fatalf("shutdown was logged as a failure: %s", logs.String())
	}
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ix.reportLoop(ctx)
	if !strings.Contains(logs.String(), "read visibility revision") {
		t.Fatalf("visibility failure was not logged: %s", logs.String())
	}
}

func TestFilesystemDebounceDoesNotExtendPastItsMaximum(t *testing.T) {
	ix := quiet(New(nil, core.PathsIn(t.TempDir()), nil, Models{}, &fakeEvents{}))
	ix.rootCtx = t.Context()
	ix.pendingFS["older.jpg"] = true
	ix.fsSince = time.Now().Add(-debounceMax)
	ix.onFSChange("newer.jpg")
	if !ix.fsDue.Equal(ix.fsSince.Add(debounceMax)) {
		t.Fatalf("flush deadline = %v, want %v", ix.fsDue, ix.fsSince.Add(debounceMax))
	}
}

func TestStopWaitsForAnActiveFilesystemFlush(t *testing.T) {
	ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	ix.rootCtx, ix.cancel = context.WithCancel(t.Context())
	started, release := make(chan struct{}), make(chan struct{})
	ix.run(func() {
		ix.fsLoop(ix.rootCtx, func() { close(started); <-release })
	})
	ix.mu.Lock()
	ix.pendingFS["photo.jpg"] = true
	ix.fsDue = time.Now().Add(-time.Second)
	ix.mu.Unlock()
	nudge(ix.wakeFS)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(release)
		ix.Stop()
		t.Fatal("filesystem flush did not start")
	}
	done := make(chan struct{})
	go func() { ix.Stop(); close(done) }()
	<-ix.rootCtx.Done()
	select {
	case <-done:
		close(release)
		t.Fatal("Stop returned while the filesystem flush was active")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not finish after the filesystem flush returned")
	}
}

func TestStopCancelsAPendingFilesystemFlush(t *testing.T) {
	ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	ix.rootCtx, ix.cancel = context.WithCancel(t.Context())
	var flushes atomic.Int32
	ix.run(func() { ix.fsLoop(ix.rootCtx, func() { flushes.Add(1) }) })
	ix.mu.Lock()
	ix.pendingFS["photo.jpg"] = true
	ix.fsDue = time.Now().Add(time.Hour)
	ix.mu.Unlock()
	nudge(ix.wakeFS)
	ix.Stop()
	if flushes.Load() != 0 {
		t.Fatal("filesystem changes were flushed after cancellation")
	}
}

func TestDeletedDottedFolderTriggersReconciliation(t *testing.T) {
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	photos := t.TempDir()
	src, err := st.AddSource(t.Context(), photos)
	if err != nil {
		t.Fatal(err)
	}
	ix := quiet(New(st, paths, nil, Models{}, &fakeEvents{}))
	ix.rootCtx, ix.cancel = context.WithCancel(t.Context())
	defer ix.Stop()
	ix.onFSChange(filepath.Join(photos, "Trip.v1"))
	ix.flushFSChanges()
	if ix.pendingScans() != 1 {
		t.Fatal("deleted dotted folder was ignored")
	}
	req, ok := ix.nextScan(t.Context())
	if !ok || req.sourceID != src.ID || req.under != "" {
		t.Fatalf("wrong rescan: %+v", req)
	}
}

func TestScanQueueCoalescesWithoutDroppingSources(t *testing.T) {
	ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	for id := int64(1); id <= 300; id++ {
		ix.queueScan(scanRequest{sourceID: id, under: `C:\Photos\one`})
		ix.queueScan(scanRequest{sourceID: id, under: `C:\Photos\two`})
	}
	if n := ix.pendingScans(); n != 300 {
		t.Fatalf("pending sources = %d, want 300", n)
	}
	for id := int64(1); id <= 300; id++ {
		r, ok := ix.nextScan(t.Context())
		if !ok || r.sourceID != id || r.under != "" {
			t.Fatalf("source %d was not fully reconciled: %+v", id, r)
		}
	}
}

func writeJPEG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{c.R, uint8(x), uint8(y), 255})
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	var err error
	if filepath.Ext(path) == ".png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	}
	if err == nil {
		err = os.WriteFile(path, buf.Bytes(), 0o600)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// writeFile writes data to path, creating its folder.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
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
	writeFile(t, filepath.Join(photos, "notes.txt"), []byte("not media"))
	writeFile(t, filepath.Join(photos, "photo.heic"), []byte("not really heic"))
	// An exact duplicate in another folder: must become the same asset.
	data, err := os.ReadFile(filepath.Join(photos, "trip", "IMG_20240101_090000.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(photos, "backup", "copy.jpg"), data)

	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if _, err := st.AddSource(ctx, photos); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, filepath.Join(photos, "trip")); !errors.Is(err, store.ErrAlreadyCovered) {
		t.Fatalf("nested source: got %v, want ErrAlreadyCovered", err)
	}

	ev := &fakeEvents{}
	cache := media.NewCache(paths.ThumbDir, media.Decoders{})
	ix := quiet(New(st, paths, cache, Models{}, ev))
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
		d, _ := st.AssetDetail(ctx, it.ID, false)
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
	settled := false
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		p = ix.Progress(context.Background())
		// Progress reads job counts before the total. Ingest can commit
		// between them, so confirm an idle result on the next snapshot.
		idle := !p.Scanning && p.Metadata == 0 && p.Thumbnail == 0 && p.Embedding == 0 && p.Total == total && ix.pendingScans() == 0
		if idle && settled {
			return
		}
		settled = idle
	}
	t.Fatalf("indexer did not settle: %+v (want total %d)", p, total)
}

// eventually polls cond until it holds or a few seconds pass.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestHoldScansWaitsForARunningScanAndKeepsNewOnesFromStartingUntilReleased(t *testing.T) {
	photos, paths := t.TempDir(), core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	src, err := st.AddSource(ctx, photos)
	if err != nil {
		t.Fatal(err)
	}
	writeJPEG(t, filepath.Join(photos, "first.jpg"), 40, 30, color.RGBA{R: 1})

	ix := quiet(New(st, paths, nil, Models{}, &fakeEvents{}))
	var scans atomic.Int32
	started, letGo := make(chan struct{}), make(chan struct{})
	ix.onScanTurn = func() {
		if scans.Add(1) == 1 {
			close(started)
			<-letGo // the first scan is "running" until the test says so
		}
	}
	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { ix.scanLoop(loopCtx); close(done) }()
	defer func() { stop(); <-done }()

	known := func() int {
		files, err := st.KnownFiles(ctx, src.ID, "")
		if err != nil {
			t.Error(err)
		}
		return len(files)
	}

	ix.queueScan(scanRequest{sourceID: src.ID})
	<-started

	held := make(chan func(), 1)
	go func() {
		release, err := ix.HoldScans(ctx)
		if err != nil {
			t.Error(err)
			return
		}
		held <- release
	}()
	select {
	case <-held:
		t.Fatal("a hold was given while a scan was running")
	case <-time.After(200 * time.Millisecond):
	}

	close(letGo)
	var release func()
	select {
	case release = <-held:
	case <-time.After(10 * time.Second):
		t.Fatal("the hold did not start after the scan finished")
	}
	if known() != 1 {
		t.Fatalf("the running scan did not finish before the hold: %d files known", known())
	}

	// A change found while held is queued, and is not scanned until release.
	writeJPEG(t, filepath.Join(photos, "second.jpg"), 40, 30, color.RGBA{R: 2})
	ix.queueScan(scanRequest{sourceID: src.ID})
	time.Sleep(300 * time.Millisecond)
	if scans.Load() != 1 || known() != 1 {
		t.Fatalf("a scan started during the hold: %d scans, %d files known", scans.Load(), known())
	}

	release()
	release() // letting go twice is harmless
	eventually(t, "the queued scan after release", func() bool { return known() == 2 })
}

func TestHoldScansGivesUpWhenItsContextEnds(t *testing.T) {
	ix := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	release, err := ix.HoldScans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := ix.HoldScans(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a hold that cannot start returned %v, want the deadline error", err)
	}
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	other := quiet(New(nil, core.Paths{}, nil, Models{}, &fakeEvents{}))
	if _, err := other.HoldScans(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("a hold with an ended context returned %v, want the cancellation", err)
	}
}

func TestAScanWaitingForAHoldReadsWhatToScanOnlyAfterTheHoldEnds(t *testing.T) {
	photos, paths := t.TempDir(), core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := t.Context()
	src, err := st.AddSource(ctx, photos)
	if err != nil {
		t.Fatal(err)
	}
	writeJPEG(t, filepath.Join(photos, "first.jpg"), 40, 30, color.RGBA{R: 1})

	ix := quiet(New(st, paths, nil, Models{}, &fakeEvents{}))
	var scans atomic.Int32
	ix.onScanTurn = func() { scans.Add(1) }
	loopCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { ix.scanLoop(loopCtx); close(done) }()
	defer func() { stop(); <-done }()

	release, err := ix.HoldScans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ix.queueScan(scanRequest{sourceID: src.ID})
	eventually(t, "the loop to take the request and wait for the hold", func() bool { return ix.pendingScans() == 0 })
	time.Sleep(200 * time.Millisecond) // long enough for it to have read anything it was going to read
	// While the hold lasts the source is removed: it must not be scanned as
	// it was when the request was taken.
	if err := st.RemoveSource(ctx, src.ID); err != nil {
		t.Fatal(err)
	}
	release()
	time.Sleep(300 * time.Millisecond)
	if n := scans.Load(); n != 0 {
		t.Fatalf("a removed source was scanned (%d scans)", n)
	}
	if files, err := st.OrganiseSnapshot(ctx, 0, core.EventGapDefault); err != nil || len(files.Files) != 0 {
		t.Fatalf("library files after the removed source: %+v, %v", files.Files, err)
	}

	// And the loop is still serving: a source added now is scanned.
	again, err := st.AddSource(ctx, photos)
	if err != nil {
		t.Fatal(err)
	}
	ix.queueScan(scanRequest{sourceID: again.ID})
	eventually(t, "a scan of the new source", func() bool { return scans.Load() == 1 })
}

func TestWorkRegisteredBeforeTheFirstScanRunsBeforeAnyScanStarts(t *testing.T) {
	photos, paths := t.TempDir(), core.PathsIn(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddSource(t.Context(), photos); err != nil {
		t.Fatal(err)
	}
	writeJPEG(t, filepath.Join(photos, "first.jpg"), 40, 30, color.RGBA{R: 1})

	ix := quiet(New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), Models{}, &fakeEvents{}))
	var settled, scannedTooSoon atomic.Bool
	ix.BeforeFirstScan(func(context.Context) {
		time.Sleep(300 * time.Millisecond) // long enough for a scan to start if it could
		settled.Store(true)
	})
	ix.onScanTurn = func() {
		if !settled.Load() {
			scannedTooSoon.Store(true)
		}
	}
	ix.Start(t.Context())
	defer ix.Stop()
	eventually(t, "the first scan", func() bool { return ix.Progress(t.Context()).Total == 1 })
	if scannedTooSoon.Load() {
		t.Fatal("a scan started before the registered work finished")
	}
}

type checkpointEvents struct {
	progress chan core.Progress
	changed  chan struct{}
}

func (ev *checkpointEvents) Progress(p core.Progress)    { ev.progress <- p }
func (ev *checkpointEvents) LibraryChanged(core.Changes) { ev.changed <- struct{}{} }

func TestReportLoopCheckpointsOncePerIdleTransition(t *testing.T) {
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ev := &checkpointEvents{progress: make(chan core.Progress, 16), changed: make(chan struct{}, 16)}
	ix := quiet(New(st, paths, nil, Models{}, ev))
	ix.changes.Store(changedAssets)
	done := make(chan struct{})
	go func() { ix.reportLoop(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	walSize := func() int64 {
		info, err := os.Stat(paths.DBPath + "-wal")
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	waitReport := func() {
		select {
		case <-ev.changed:
		case <-time.After(10 * time.Second):
			t.Fatal("library report did not arrive")
		}
	}
	if err := st.SetBool(ctx, "idle-checkpoint-first", true); err != nil {
		t.Fatal(err)
	}
	waitReport()
	if walSize() != 0 {
		t.Fatal("the initial idle state did not checkpoint")
	}
	if err := st.SetBool(ctx, "idle-checkpoint-second", true); err != nil {
		t.Fatal(err)
	}
	ix.markChanged(core.Changes{Assets: true})
	waitReport()
	if walSize() == 0 {
		t.Fatal("unchanged idle state repeated the checkpoint")
	}
	ix.setWorking(workMetadata, true)
	working := false
	for !working {
		select {
		case p := <-ev.progress:
			working = p.Working.Metadata
		case <-time.After(10 * time.Second):
			t.Fatal("active stage was not reported")
		}
	}
	if walSize() == 0 {
		t.Fatal("active work was checkpointed")
	}
	ix.setWorking(workMetadata, false)
	eventually(t, "the next idle transition", func() bool { return walSize() == 0 })
}

func TestReportLoopLogsCheckpointFailureAtDebugAndKeepsReporting(t *testing.T) {
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(paths.DBPath); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	ev := &fakeEvents{}
	ix := quiet(New(st, paths, nil, Models{}, ev))
	ix.stale.Store(true)
	ix.changes.Store(changedAssets)
	ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
	defer cancel()
	ix.reportLoop(ctx)
	if !strings.Contains(logs.String(), `level=DEBUG msg="checkpoint idle library"`) ||
		!strings.Contains(logs.String(), "checkpoint WAL") {
		t.Fatalf("checkpoint failure was not logged at debug: %s", logs.String())
	}
	if ev.changed.Load() != 1 {
		t.Fatal("checkpoint failure stopped library reporting")
	}
}
