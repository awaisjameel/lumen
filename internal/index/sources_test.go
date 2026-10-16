package index

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/scanner"
)

type sourceEvents struct{ changes chan core.Changes }

func (*sourceEvents) Progress(core.Progress)          {}
func (e *sourceEvents) LibraryChanged(c core.Changes) { e.changes <- c }

func TestSourceChecksReportDisconnectionAndRescanRecoveredFolders(t *testing.T) {
	st := openTestStore(t)
	root := filepath.Join(t.TempDir(), "photos")
	writeFile(t, filepath.Join(root, "first.jpg"), []byte("first"))
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	ev := &sourceEvents{changes: make(chan core.Changes, 20)}
	ix := quiet(New(st, core.PathsIn(t.TempDir()), nil, Models{}, ev))
	ix.rootCtx, ix.cancel = context.WithCancel(t.Context())
	defer ix.Stop()
	ix.run(func() { ix.sourceLoop(ix.rootCtx) })
	ix.run(func() { ix.scanLoop(ix.rootCtx) })
	ix.run(func() { ix.reportLoop(ix.rootCtx) })
	state := func() core.SourceAvailability {
		srcs := []core.Source{src}
		ix.SourceAvailability(srcs)
		return srcs[0].Availability
	}
	if got := state(); got != core.SourceChecking {
		t.Fatalf("initial state = %s", got)
	}
	ix.queueScan(scanRequest{sourceID: src.ID})
	eventually(t, "the first scan", func() bool {
		files, err := st.KnownFiles(t.Context(), src.ID, "")
		return err == nil && len(files) == 1 && state() == core.SourceAvailable
	})
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	ix.RecheckSources()
	eventually(t, "unavailable source", func() bool { return state() == core.SourceUnavailable })
	// Scans triggered by a failed watcher must leave the known files untouched.
	ix.queueScan(scanRequest{sourceID: src.ID})
	eventually(t, "the skipped scan", func() bool { return ix.pendingScans() == 0 })
	release, err := ix.HoldScans(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
	files, err := st.KnownFiles(t.Context(), src.ID, "")
	if err != nil || len(files) != 1 {
		t.Fatalf("disconnection lost files: %v, %v", files, err)
	}
	select {
	case c := <-ev.changes:
		if !c.Sources {
			t.Fatalf("source event lacked sources: %+v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no source event")
	}
	writeFile(t, filepath.Join(root, "second.jpg"), []byte("second"))
	// Try again queues the full scan that catches changes missed offline.
	ix.RecheckSources()
	eventually(t, "recovered source and automatic scan", func() bool {
		files, err := st.KnownFiles(t.Context(), src.ID, "")
		_, second := files[core.Fold(filepath.Join(root, "second.jpg"))]
		return err == nil && len(files) == 1 && second && state() == core.SourceAvailable
	})
}

func TestSlowSourceChecksStayBoundedAndDoNotBlockCachedReadsOrOtherSources(t *testing.T) {
	st := openTestStore(t)
	slow, err := st.AddSource(t.Context(), filepath.Join(t.TempDir(), "slow"))
	if err != nil {
		t.Fatal(err)
	}
	fast, err := st.AddSource(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ix := quiet(New(st, core.Paths{}, nil, Models{}, &fakeEvents{}))
	ix.rootCtx, ix.cancel = context.WithCancel(t.Context())
	defer ix.Stop()
	var calls atomic.Int32
	started := make(chan struct{})
	ix.checkSource = func(ctx context.Context, path string) error {
		if path == slow.Path {
			if calls.Add(1) == 1 {
				close(started)
			}
			<-ctx.Done()
			return ctx.Err()
		}
		return scanner.CheckRoot(ctx, path)
	}
	ix.run(func() { ix.sourceLoop(ix.rootCtx) })
	ix.RecheckSources()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not start")
	}
	eventually(t, "the independent healthy source", func() bool {
		sources := []core.Source{slow, fast}
		ix.SourceAvailability(sources)
		return sources[0].Availability == core.SourceChecking && sources[1].Availability == core.SourceAvailable
	})
	for range 20 {
		ix.checkSources(ix.rootCtx)
		ix.RecheckSources()
	}
	if calls.Load() != 1 {
		t.Fatalf("slow probes accumulated: %d", calls.Load())
	}
}

func TestUnchangedSourceAvailabilityDoesNotPublishOrRescan(t *testing.T) {
	ix := New(nil, core.Paths{}, nil, Models{}, &fakeEvents{})
	ix.setSourceAvailability(core.Source{ID: 1}, core.SourceUnavailable)
	if ix.changes.Swap(0) != changedSources {
		t.Fatal("availability did not mark sources changed")
	}
	ix.setSourceAvailability(core.Source{ID: 1}, core.SourceUnavailable)
	if ix.changes.Load() != 0 || ix.pendingScans() != 0 {
		t.Fatal("unchanged availability caused work")
	}
	ix.setSourceAvailability(core.Source{ID: 1}, core.SourceAvailable)
	if ix.pendingScans() != 1 {
		t.Fatal("recovery did not queue a scan")
	}
}

func TestCancelledRootProbeDoesNotAccessTheFilesystem(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := scanner.CheckRoot(ctx, "missing"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRemovedSourceStatusCannotApplyToAReusedID(t *testing.T) {
	ix := New(nil, core.Paths{}, nil, Models{}, &fakeEvents{})
	old := core.Source{ID: 1, Path: `D:\Photos`}
	next := core.Source{ID: 1, Path: `E:\Photos`}
	ix.setSourceAvailability(old, core.SourceUnavailable)
	sources := []core.Source{next}
	ix.SourceAvailability(sources)
	if sources[0].Availability != core.SourceChecking {
		t.Fatal("a removed folder's status applied to its replacement")
	}
	ix.setSourceAvailability(next, core.SourceAvailable)
	ix.setSourceAvailability(old, core.SourceUnavailable)
	ix.SourceAvailability(sources)
	if sources[0].Availability != core.SourceAvailable {
		t.Fatal("a stale probe overwrote the replacement folder's status")
	}
}

func TestPeriodicSourceChecksNoticeRootRenamesAndRecovery(t *testing.T) {
	st := openTestStore(t)
	root := filepath.Join(t.TempDir(), "photos")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	ix := New(st, core.Paths{}, nil, Models{}, &fakeEvents{})
	ix.rootCtx, ix.cancel = context.WithCancel(t.Context())
	defer ix.Stop()
	ticks := make(chan time.Time)
	ix.run(func() { ix.sourceChecks(ix.rootCtx, ticks) })
	state := func() core.SourceAvailability {
		sources := []core.Source{src}
		ix.SourceAvailability(sources)
		return sources[0].Availability
	}
	ix.setSourceAvailability(src, core.SourceAvailable)
	renamed := root + "-renamed"
	if err := os.Rename(root, renamed); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now()
	eventually(t, "renamed root to be unavailable", func() bool { return state() == core.SourceUnavailable })
	if err := os.Rename(renamed, root); err != nil {
		t.Fatal(err)
	}
	ticks <- time.Now()
	eventually(t, "periodic recovery scan", func() bool { return state() == core.SourceAvailable && ix.pendingScans() == 1 })
}

func TestWatcherErrorsCheckAvailabilityAndRecoveryQueuesAScan(t *testing.T) {
	st := openTestStore(t)
	root := filepath.Join(t.TempDir(), "offline")
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	ix := New(st, core.PathsIn(t.TempDir()), nil, Models{}, &fakeEvents{})
	ix.rootCtx, ix.cancel = context.WithCancel(t.Context())
	defer ix.Stop()
	state := func() core.SourceAvailability {
		sources := []core.Source{src}
		ix.SourceAvailability(sources)
		return sources[0].Availability
	}
	ix.setSourceAvailability(src, core.SourceAvailable)
	ix.run(func() { ix.sourceLoop(ix.rootCtx) })
	ix.run(func() { ix.watchLoop(ix.rootCtx, src) })
	eventually(t, "failed watch availability", func() bool { return state() == core.SourceUnavailable })
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	eventually(t, "watch recovery scan", func() bool { return state() == core.SourceAvailable && ix.pendingScans() == 1 })
}
