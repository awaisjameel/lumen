//go:build windows

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/index"
	"lumen/internal/media"
	"lumen/internal/store"
)

// organiseLibrary is a library of real photos under a temporary folder, a
// service over it, and the progress the service sent.
type organiseLibrary struct {
	t      *testing.T
	st     *store.Store
	s      *LibraryService
	ix     *index.Indexer
	root   string
	src    core.Source
	mu     sync.Mutex
	events []core.OrganiseProgress
}

// jpegBytes is a small distinct photo: each shade makes different content.
func jpegBytes(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 16))
	for y := range 16 {
		for x := range 24 {
			img.Set(x, y, color.RGBA{shade, uint8(x * 8), uint8(y * 8), 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newOrganiseLibrary has a store, a source folder and a service with an
// indexer that is not started (so nothing scans unless a test starts it).
func newOrganiseLibrary(t *testing.T) *organiseLibrary {
	t.Helper()
	dir := t.TempDir()
	paths := core.PathsIn(filepath.Join(dir, "data"))
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	root := filepath.Join(dir, "Photos")
	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatal(err)
	}
	src, err := st.AddSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	// Indexing must not give way to whatever else this PC is running.
	for _, key := range []string{core.SettingPauseFullScreen, core.SettingPauseBusy} {
		if err := st.SetBool(t.Context(), key, false); err != nil {
			t.Fatal(err)
		}
	}
	ix := index.New(st, paths, media.NewCache(paths.ThumbDir, media.Decoders{}), index.Models{}, quietEvents{})
	l := &organiseLibrary{t: t, st: st, ix: ix, root: root, src: src}
	l.s = NewLibraryService(st, ix, nil, nil)
	l.s.organise.emit = func(p core.OrganiseProgress) {
		l.mu.Lock()
		l.events = append(l.events, p)
		l.mu.Unlock()
	}
	t.Cleanup(func() { check(t, l.s.ServiceShutdown()) })
	return l
}

func (l *organiseLibrary) path(rel string) string {
	return filepath.Join(l.root, filepath.FromSlash(rel))
}

// add writes a photo and has the library ingest it as taken at the given date.
func (l *organiseLibrary) add(rel string, shade uint8, taken time.Time) int64 {
	l.t.Helper()
	path := l.path(rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		l.t.Fatal(err)
	}
	data := jpegBytes(l.t, shade)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		l.t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		l.t.Fatal(err)
	}
	if _, err := l.st.Ingest(l.t.Context(), []store.IngestItem{{
		SourceID: l.src.ID, Path: path, Size: info.Size(), MTime: info.ModTime().UnixNano(), Hash: data,
		Kind: core.KindPhoto, Ext: filepath.Ext(path), TakenAt: core.WallClock(taken), DateFrom: core.DateFromFile,
	}}); err != nil {
		l.t.Fatal(err)
	}
	snap, err := l.st.OrganiseSnapshot(l.t.Context(), 0, core.EventGapDefault)
	if err != nil {
		l.t.Fatal(err)
	}
	for _, f := range snap.Files {
		if f.Path == path {
			return f.AssetID
		}
	}
	l.t.Fatalf("no file %s in the library", path)
	return 0
}

func (l *organiseLibrary) progress() []core.OrganiseProgress {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.events)
}

// waitState waits for the run to reach one of the states and returns it.
func (l *organiseLibrary) waitState(runID int64, states ...core.OrganiseRunState) core.OrganiseRun {
	l.t.Helper()
	var last core.OrganiseRun
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		runs, err := l.s.OrganiseRuns(l.t.Context())
		if err != nil {
			l.t.Fatal(err)
		}
		for _, r := range runs {
			if r.ID == runID {
				last = r
				if slices.Contains(states, r.State) {
					return r
				}
			}
		}
	}
	l.t.Fatalf("run %d did not reach %v; last seen %+v", runID, states, last)
	return last
}

func (l *organiseLibrary) wantFile(rel string, present bool) {
	l.t.Helper()
	if _, err := os.Lstat(l.path(rel)); (err == nil) != present {
		l.t.Errorf("%s present = %v, want %v", rel, err == nil, present)
	}
}

func failureCode(t *testing.T, err error) ErrorCode {
	t.Helper()
	var f Failure
	if err := json.Unmarshal(MarshalError(err), &f); err != nil {
		t.Fatalf("no failure code for the error: %v", err)
	}
	return f.Code
}

func TestOrganisingPreviewsFirstMovesExactlyWhatWasPreviewedAndUndoesIt(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	l.add("IMG_2.jpg", 20, time.Date(2023, time.March, 9, 12, 0, 0, 0, time.UTC))

	plan, err := l.s.PlanOrganise(ctx, "{Year}/{MonthName}/{Name}", 0)
	check(t, err)
	if plan.RunID == 0 || len(plan.Moves) != 2 || plan.Renamed != 0 || plan.InPlace != 0 || plan.Template != "{Year}/{MonthName}/{Name}" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.Moves[0].To != l.path("2023/March/IMG_2.jpg") || plan.Moves[0].State != core.MovePlanned || plan.Moves[0].Seq != 1 {
		t.Fatalf("first move = %+v", plan.Moves[0])
	}
	// Planning is a dry run: nothing moved, and the preview is not a past run.
	l.wantFile("IMG_1.jpg", true)
	l.wantFile("2023", false)
	if runs, err := l.s.OrganiseRuns(ctx); err != nil || len(runs) != 0 {
		t.Fatalf("runs after a preview = %+v, %v", runs, err)
	}

	check(t, l.s.StartOrganise(ctx, plan.RunID))
	run := l.waitState(plan.RunID, core.OrganiseDone)
	if run.Moved != 2 || run.Total != 2 || run.Template != plan.Template {
		t.Fatalf("run = %+v", run)
	}
	l.wantFile("2024/August/IMG_1.jpg", true)
	l.wantFile("2023/March/IMG_2.jpg", true)
	l.wantFile("IMG_1.jpg", false)
	ev := l.progress()
	if len(ev) < 2 || ev[0].State != core.OrganiseRunning || ev[0].Total != 2 {
		t.Fatalf("progress = %+v, want a start of 2 to move", ev)
	}
	if last := ev[len(ev)-1]; last.State != core.OrganiseDone || last.Done != 2 || last.Total != 2 || last.RunID != plan.RunID {
		t.Fatalf("last progress = %+v", last)
	}

	// Done and undone runs are not started again; a done one can be undone.
	if err := l.s.StartOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseState) {
		t.Fatalf("starting a finished run = %v, want ErrOrganiseState", err)
	}
	check(t, l.s.UndoOrganise(ctx, plan.RunID))
	run = l.waitState(plan.RunID, core.OrganiseUndone)
	if run.Moved != 0 {
		t.Fatalf("run after undo = %+v", run)
	}
	l.wantFile("IMG_1.jpg", true)
	l.wantFile("IMG_2.jpg", true)
	l.wantFile("2024", false)
	l.wantFile("2023", false)
	if err := l.s.UndoOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseState) {
		t.Fatalf("undoing an undone run = %v, want ErrOrganiseState", err)
	}
	if err := l.s.StartOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseState) {
		t.Fatalf("starting an undone run = %v, want ErrOrganiseState", err)
	}
}

func TestAPreviewThatWasNeverStartedCannotBeUndoneAndAnUnknownRunIsNotFound(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)
	if err := l.s.UndoOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseState) {
		t.Fatalf("undoing a preview = %v, want ErrOrganiseState", err)
	}
	if err := l.s.StartOrganise(ctx, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("starting an unknown run = %v, want ErrNotFound", err)
	}
	if err := l.s.UndoOrganise(ctx, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("undoing an unknown run = %v, want ErrNotFound", err)
	}
	// A refused call must not leave the one place taken.
	check(t, l.s.StartOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseDone)
}

func TestPlanningRefusesABadSchemeAndAnUnknownSourceWithCodesTheUICanExplain(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	for _, bad := range []string{"", "../{Name}", "{Nope}", "/abs/{Name}", "{Year}/CON/{Name}"} {
		_, err := l.s.PlanOrganise(ctx, bad, 0)
		if !errors.Is(err, fileops.ErrInvalidTemplate) || failureCode(t, err) != CodeInvalidTemplate {
			t.Errorf("scheme %q: %v (code %q), want invalid_template", bad, err, failureCode(t, err))
		}
	}
	if _, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", l.src.ID+100); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an unknown source gave %v, want ErrNotFound", err)
	}
	if plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", l.src.ID); err != nil || len(plan.Moves) != 1 {
		t.Errorf("planning for one source = %+v, %v", plan, err)
	}
	if got := failureCode(t, ErrOrganiseBusy); got != CodeOrganiseBusy {
		t.Errorf("busy code = %q", got)
	}
	if got := failureCode(t, ErrOrganiseState); got != CodeOrganiseState {
		t.Errorf("state code = %q", got)
	}
}

func TestARunWaitsForScansToBeHeldAndASecondStartWhileItRunsIsRefused(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	l.add("IMG_2.jpg", 20, time.Date(2024, time.August, 6, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)

	// Someone else holds the scans (a running scan would): the run is
	// accepted and waits for its turn without touching a file.
	release, err := l.ix.HoldScans(ctx)
	check(t, err)
	check(t, l.s.StartOrganise(ctx, plan.RunID))
	if err := l.s.StartOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseBusy) {
		t.Fatalf("a second start = %v, want ErrOrganiseBusy", err)
	}
	if err := l.s.UndoOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseBusy) {
		t.Fatalf("an undo during a run = %v, want ErrOrganiseBusy", err)
	}
	time.Sleep(200 * time.Millisecond)
	l.wantFile("IMG_1.jpg", true)
	l.wantFile("2024", false)
	if runs, _ := l.s.OrganiseRuns(ctx); len(runs) != 1 || runs[0].State != core.OrganiseRunning {
		t.Fatalf("runs while waiting for the scans = %+v, want one running", runs)
	}

	release()
	l.waitState(plan.RunID, core.OrganiseDone)
	l.wantFile("2024/IMG_1.jpg", true)
	// And the place is free again afterwards.
	check(t, l.s.UndoOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseUndone)
}

func TestOrganiseCompletionIsOfferedOnlyAfterTheOwnerHasFinished(t *testing.T) {
	l := newOrganiseLibrary(t)
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(t.Context(), "{Year}/{Name}", 0)
	check(t, err)
	journalFinished, releaseWork := make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(releaseWork) })
	terminal := make(chan core.OrganiseProgress, 2)
	l.s.organise.emit = func(p core.OrganiseProgress) {
		if p.State == core.OrganiseDone || p.State == core.OrganiseUndone {
			terminal <- p
		}
	}
	check(t, l.s.begin(t.Context(), plan.RunID, organiseOp{
		started: core.OrganiseRunning,
		allowed: func(core.OrganiseRun, map[core.OrganiseMoveState]int) bool { return true },
		todo:    func(map[core.OrganiseMoveState]int) int { return 1 },
		work: func(ctx context.Context, runner *fileops.Runner, runID int64) error {
			err := runner.Run(ctx, runID)
			close(journalFinished)
			<-releaseWork // the journal ended, but the worker has not returned
			return err
		},
	}))
	select {
	case <-journalFinished:
	case <-time.After(20 * time.Second):
		t.Fatal("the journal did not finish")
	}
	stored, err := l.st.OrganiseRun(t.Context(), plan.RunID)
	check(t, err)
	if stored.State != core.OrganiseDone {
		t.Fatalf("the journal state = %s, want done", stored.State)
	}
	public, err := l.s.OrganiseRuns(t.Context())
	check(t, err)
	if len(public) != 1 || public[0].State != core.OrganiseRunning {
		t.Fatalf("the public state offered completion before cleanup: %+v", public)
	}
	select {
	case p := <-terminal:
		t.Fatalf("completion was emitted before cleanup: %+v", p)
	default:
	}
	if err := l.s.UndoOrganise(t.Context(), plan.RunID); !errors.Is(err, ErrOrganiseBusy) {
		t.Fatalf("undo while cleaning up = %v, want ErrOrganiseBusy", err)
	}
	release.Do(func() { close(releaseWork) })
	select {
	case <-terminal:
	case <-time.After(20 * time.Second):
		t.Fatal("completion was not emitted after cleanup")
	}
	// A client can act as soon as it receives completion, without retrying.
	check(t, l.s.UndoOrganise(t.Context(), plan.RunID))
	l.waitState(plan.RunID, core.OrganiseUndone)
}

func TestARunThatStopsBeforeRecordingAnEndIsLeftFailedAndCanBeResumed(t *testing.T) {
	l := newOrganiseLibrary(t)
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(t.Context(), "{Year}/{Name}", 0)
	check(t, err)
	check(t, l.s.begin(t.Context(), plan.RunID, organiseOp{
		started: core.OrganiseRunning,
		allowed: func(core.OrganiseRun, map[core.OrganiseMoveState]int) bool { return true },
		todo:    func(map[core.OrganiseMoveState]int) int { return 1 },
		work: func(context.Context, *fileops.Runner, int64) error {
			return errors.New("the database could not be read")
		},
	}))
	l.waitState(plan.RunID, core.OrganiseFailed)
	ev := l.progress()
	if last := ev[len(ev)-1]; last.State != core.OrganiseFailed || last.RunID != plan.RunID {
		t.Fatalf("last progress = %+v, want failed", last)
	}
	l.wantFile("IMG_1.jpg", true)
	check(t, l.s.StartOrganise(t.Context(), plan.RunID))
	l.waitState(plan.RunID, core.OrganiseDone)
	l.wantFile("2024/IMG_1.jpg", true)
}

func TestScansThatFollowAnOrganiseRunFindTheLibraryAlreadyMatchingTheDisk(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	var assets []int64
	for i, rel := range []string{"a/IMG_1.jpg", "a/IMG_2.jpg", "b/IMG_3.jpg", "IMG_4.jpg"} {
		assets = append(assets, l.add(rel, uint8(10*(i+1)), time.Date(2024, time.Month(i+1), 5, 12, 0, 0, 0, time.UTC)))
	}
	// The real thing: scanning and watching are running.
	l.ix.Start(ctx)
	t.Cleanup(l.ix.Stop)
	settle := func(want int) {
		t.Helper()
		var p core.Progress
		for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
			p = l.ix.Progress(ctx)
			if !p.Scanning && p.Metadata == 0 && p.Thumbnail == 0 && p.Total == want {
				return
			}
		}
		t.Fatalf("the indexer did not settle: %+v", p)
	}
	settle(4)
	time.Sleep(time.Second) // the first watch is opened by now
	before := fileRows(t, l.st)
	scans := &scanLog{}
	previous := slog.Default()
	slog.SetDefault(slog.New(scans))
	t.Cleanup(func() { slog.SetDefault(previous) })

	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Month}/{Name}", 0)
	check(t, err)
	if len(plan.Moves) != 4 {
		t.Fatalf("plan = %+v", plan.Moves)
	}
	check(t, l.s.StartOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseDone)
	// The watcher saw every move; its rescans are queued for after the run and
	// are debounced. Give them time to run, then to finish.
	time.Sleep(debounceWait)
	settle(4)
	if done := scans.done(); len(done) == 0 {
		t.Fatal("no scan followed the run, so the test proved nothing")
	} else {
		for _, d := range done {
			if d != [3]int64{} {
				t.Errorf("a scan after the run changed %d, added %d and removed %d: the library did not match the disk", d[0], d[1], d[2])
			}
		}
	}

	after := fileRows(t, l.st)
	if len(after) != len(before) {
		t.Fatalf("file rows: %d before, %d after", len(before), len(after))
	}
	for id, was := range before {
		now, ok := after[id]
		if !ok {
			t.Fatalf("file %d (asset %d) lost its row", id, was.asset)
		}
		if now.asset != was.asset || now.size != was.size || now.mtime != was.mtime {
			t.Errorf("file %d changed from %+v to %+v: a scan reworked it", id, was, now)
		}
		if now.path == was.path {
			t.Errorf("file %d was never moved: %s", id, now.path)
		}
		if _, err := os.Stat(now.path); err != nil {
			t.Errorf("the library points at %s, which is not there: %v", now.path, err)
		}
	}
	if n, err := l.st.CountVisible(ctx); err != nil || n != 4 {
		t.Errorf("visible assets = %d, %v; want 4: none lost, none gained", n, err)
	}
	for _, id := range assets {
		d, err := l.st.AssetDetail(ctx, id, false)
		if err != nil || len(d.Paths) != 1 {
			t.Errorf("asset %d paths = %v, %v; want one", id, d.Paths, err)
		}
	}
}

// scanLog records what the indexer says about each scan it finishes.
type scanLog struct {
	mu    sync.Mutex
	scans [][3]int64 // changed, new and removed files of each scan
}

func (*scanLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *scanLog) WithAttrs([]slog.Attr) slog.Handler     { return h }
func (h *scanLog) WithGroup(string) slog.Handler          { return h }

func (h *scanLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message != "scan done" {
		return nil
	}
	var counts [3]int64
	r.Attrs(func(a slog.Attr) bool {
		switch a.Key {
		case "changed":
			counts[0] = a.Value.Int64()
		case "new":
			counts[1] = a.Value.Int64()
		case "removed":
			counts[2] = a.Value.Int64()
		}
		return true
	})
	h.mu.Lock()
	h.scans = append(h.scans, counts)
	h.mu.Unlock()
	return nil
}

func (h *scanLog) done() [][3]int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.scans)
}

// debounceWait is longer than the watcher's quiet period before it rescans.
const debounceWait = 4 * time.Second

type fileRow struct {
	asset, size, mtime int64
	path               string
}

// fileRows are the library's visible files by id.
func fileRows(t *testing.T, st *store.Store) map[int64]fileRow {
	t.Helper()
	snap, err := st.OrganiseSnapshot(t.Context(), 0, core.EventGapDefault)
	check(t, err)
	out := map[int64]fileRow{}
	for _, f := range snap.Files {
		out[f.FileID] = fileRow{asset: f.AssetID, size: f.Size, mtime: f.MTime, path: f.Path}
	}
	return out
}

func TestARunInterruptedByAShutdownIsSettledAtTheNextStartAndCanBeResumed(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	l.add("IMG_2.jpg", 20, time.Date(2024, time.August, 6, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)

	// The app closes while the run waits for its turn.
	release, err := l.ix.HoldScans(ctx)
	check(t, err)
	check(t, l.s.StartOrganise(ctx, plan.RunID))
	check(t, l.s.ServiceShutdown())
	release()
	l.wantFile("IMG_1.jpg", true)

	// The next start finds the run still marked running and makes it resumable.
	again := NewLibraryService(l.st, l.ix, nil, nil)
	again.organise.emit = l.s.organise.emit
	check(t, again.ServiceStartup(ctx, application.ServiceOptions{}))
	t.Cleanup(func() { check(t, again.ServiceShutdown()) })
	var run core.OrganiseRun
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		runs, err := again.OrganiseRuns(ctx)
		check(t, err)
		if run = runs[0]; run.State == core.OrganiseFailed {
			break
		}
	}
	if run.State != core.OrganiseFailed {
		t.Fatalf("run after restart = %+v, want failed (resumable)", run)
	}
	check(t, again.StartOrganise(ctx, plan.RunID))
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if runs, _ := again.OrganiseRuns(ctx); runs[0].State == core.OrganiseDone {
			break
		}
	}
	l.wantFile("2024/IMG_1.jpg", true)
	l.wantFile("2024/IMG_2.jpg", true)
}

func TestStartupResolvesAMoveThatWasInDoubtFromTheDisk(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	assetID := l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)
	// The process died with the move journaled and the file moved.
	check(t, l.st.SetOrganiseRunState(ctx, plan.RunID, core.OrganiseRunning))
	check(t, l.st.JournalOrganiseMove(ctx, plan.RunID, 1, core.MovePlanned, core.MoveMoving, ""))
	check(t, os.MkdirAll(l.path("2024"), 0o750))
	check(t, os.Rename(l.path("IMG_1.jpg"), l.path("2024/IMG_1.jpg")))

	check(t, l.s.ServiceStartup(ctx, application.ServiceOptions{}))
	run := l.waitState(plan.RunID, core.OrganiseDone)
	if run.Moved != 1 {
		t.Fatalf("run = %+v, want the move finished", run)
	}
	d, err := l.st.AssetDetail(ctx, assetID, false)
	check(t, err)
	if !slices.Equal(d.Paths, []string{l.path("2024/IMG_1.jpg")}) {
		t.Fatalf("paths = %v, want the library to follow the file", d.Paths)
	}
}

func TestThePreviewCountsSidecarsAndARunMovesThemWithTheirPhotos(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	l.add("IMG_2.jpg", 20, time.Date(2024, time.August, 6, 12, 0, 0, 0, time.UTC))
	check(t, os.WriteFile(l.path("IMG_1.jpg.xmp"), []byte("xmp"), 0o600))

	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)
	if plan.Sidecars != 1 || len(plan.Moves) != 2 {
		t.Fatalf("plan = %+v, want 2 moves taking 1 sidecar", plan)
	}
	check(t, l.s.StartOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseDone)
	l.wantFile("2024/IMG_1.jpg.xmp", true)
	l.wantFile("IMG_1.jpg.xmp", false)
	check(t, l.s.UndoOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseUndone)
	l.wantFile("IMG_1.jpg.xmp", true)
	l.wantFile("2024", false)
}

// failAllJobs makes every metadata job fail as many times as the queue allows,
// as a stage does that reads a file at the moment it moves.
func (l *organiseLibrary) failAllJobs() {
	l.t.Helper()
	for range store.MaxAttempts {
		jobs, err := l.st.PendingJobs(l.t.Context(), core.StageMetadata, nil, 100)
		check(l.t, err)
		var results []store.MetadataResult
		for _, j := range jobs {
			results = append(results, store.MetadataResult{AssetID: j.AssetID, Err: "the system cannot find the file"})
		}
		check(l.t, l.st.SaveMetadata(l.t.Context(), "v1", results))
	}
}

func (l *organiseLibrary) pendingMetadata() int {
	l.t.Helper()
	jobs, err := l.st.PendingJobs(l.t.Context(), core.StageMetadata, nil, 100)
	check(l.t, err)
	return len(jobs)
}

func TestARunAndItsUndoQueueTheFailedJobsOfTheFilesTheyMovedAgain(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	l.add("IMG_2.jpg", 20, time.Date(2024, time.August, 6, 12, 0, 0, 0, time.UTC))
	l.failAllJobs()
	if n := l.pendingMetadata(); n != 0 {
		t.Fatalf("%d jobs still pending after failing them", n)
	}
	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)
	check(t, l.s.StartOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseDone)
	eventually(t, "the failed jobs to be queued again after the run", func() bool { return l.pendingMetadata() == 2 })

	l.failAllJobs()
	check(t, l.s.UndoOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseUndone)
	eventually(t, "the failed jobs to be queued again after the undo", func() bool { return l.pendingMetadata() == 2 })
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

func TestNoRunIsTakenOnceShutdownHasBegun(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)
	check(t, l.s.ServiceShutdown())
	if err := l.s.StartOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseStopping) {
		t.Fatalf("a start after shutdown = %v, want ErrOrganiseStopping", err)
	}
	if err := l.s.UndoOrganise(ctx, plan.RunID); !errors.Is(err, ErrOrganiseStopping) {
		t.Fatalf("an undo after shutdown = %v, want ErrOrganiseStopping", err)
	}
	l.wantFile("IMG_1.jpg", true)
	// The refusal leaves nothing claimed or registered: a second shutdown returns at once.
	check(t, l.s.ServiceShutdown())
}

func TestARunInDoubtIsSettledByTheIndexerBeforeItsFirstScanWithoutTheServiceStarting(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	assetID := l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanOrganise(ctx, "{Year}/{Name}", 0)
	check(t, err)
	// The process died with the move journaled and the file moved.
	check(t, l.st.SetOrganiseRunState(ctx, plan.RunID, core.OrganiseRunning))
	check(t, l.st.JournalOrganiseMove(ctx, plan.RunID, 1, core.MovePlanned, core.MoveMoving, ""))
	check(t, os.MkdirAll(l.path("2024"), 0o750))
	check(t, os.Rename(l.path("IMG_1.jpg"), l.path("2024/IMG_1.jpg")))

	// The next start: a service over the indexer (as main builds them), and the
	// indexer starting. Nobody calls ServiceStartup.
	fresh := NewLibraryService(l.st, l.ix, nil, nil)
	t.Cleanup(func() { check(t, fresh.ServiceShutdown()) })
	l.ix.Start(ctx)
	t.Cleanup(l.ix.Stop)
	eventually(t, "the run to be settled", func() bool {
		runs, err := fresh.OrganiseRuns(ctx)
		check(t, err)
		return len(runs) == 1 && runs[0].State == core.OrganiseDone && runs[0].Moved == 1
	})
	d, err := l.st.AssetDetail(ctx, assetID, false)
	check(t, err)
	if !slices.Equal(d.Paths, []string{l.path("2024/IMG_1.jpg")}) {
		t.Fatalf("paths = %v, want the library to follow the file", d.Paths)
	}
}
