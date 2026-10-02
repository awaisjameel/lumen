//go:build windows

package fileops

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"lumen/internal/core"
	"lumen/internal/scanner"
	"lumen/internal/store"
)

// eventLog collects a runner's progress.
type eventLog struct {
	mu     sync.Mutex
	events []core.OrganiseProgress
}

func (e *eventLog) add(p core.OrganiseProgress) {
	e.mu.Lock()
	e.events = append(e.events, p)
	e.mu.Unlock()
}

func (e *eventLog) all() []core.OrganiseProgress {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.events)
}

// harness is a saved plan and the runner that makes it.
type harness struct {
	library
	run    int64
	plan   Planned
	events *eventLog
	runner *Runner
}

// start previews the template for every source and saves it as a run.
func (l library) start(template string) *harness {
	l.t.Helper()
	run, plan := l.savePlan(template)
	h := &harness{library: l, run: run, plan: plan, events: &eventLog{}}
	h.runner = h.newRunner()
	return h
}

func (h *harness) newRunner() *Runner {
	return &Runner{Store: h.st, Progress: h.events.add}
}

// restart is the process dying and Lumen starting again: a fresh runner that
// settles whatever the journal says was in doubt.
func (h *harness) restart() {
	h.t.Helper()
	h.runner = h.newRunner()
	if err := h.runner.RecoverAll(h.ctx); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) mustRun() {
	h.t.Helper()
	if err := h.runner.Run(h.ctx, h.run); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) mustUndo() {
	h.t.Helper()
	if err := h.runner.Undo(h.ctx, h.run); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) runState() core.OrganiseRunState {
	h.t.Helper()
	run, err := h.st.OrganiseRun(h.ctx, h.run)
	if err != nil {
		h.t.Fatal(err)
	}
	return run.State
}

// moves are the run's moves as the journal holds them now.
func (h *harness) moves() []store.OrganiseMove {
	h.t.Helper()
	m, err := h.st.OrganiseMoves(h.ctx, h.run, false)
	if err != nil {
		h.t.Fatal(err)
	}
	return m
}

func (h *harness) states() []core.OrganiseMoveState {
	var out []core.OrganiseMoveState
	for _, m := range h.moves() {
		out = append(out, m.State)
	}
	return out
}

// libraryPaths are the paths the library has for the asset.
func (h *harness) libraryPaths(asset int64) []string {
	h.t.Helper()
	d, err := h.st.AssetDetail(h.ctx, asset, false)
	if err != nil {
		h.t.Fatal(err)
	}
	return d.Paths
}

// at reports whether a file is at the path, and what it holds.
func (h *harness) at(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (h *harness) wantFile(path, content string) {
	h.t.Helper()
	if got, ok := h.at(path); !ok || got != content {
		h.t.Errorf("%s holds %q (present %v), want %q", path, got, ok, content)
	}
}

func (h *harness) wantNoFile(path string) {
	h.t.Helper()
	if exist(path) {
		h.t.Errorf("%s exists, want nothing there", path)
	}
}

// crashAtTheSecondMove makes the runner stop where a dying process would, at
// the second move of the run (the middle one of three, whichever way it goes).
func (h *harness) crashAtTheSecondMove(point crashPoint) {
	h.runner.crash = func(p crashPoint, seq int) bool { return p == point && seq == 2 }
}

func threePhotos(t *testing.T) (l library, assets []int64) {
	t.Helper()
	l = newLibrary(t)
	return l, []int64{
		l.add("IMG_1.jpg", "one", day(2024, time.August, 5)),
		l.add("IMG_2.jpg", "two", day(2024, time.August, 6)),
		l.add("sub/IMG_3.jpg", "three", day(2025, time.January, 7)),
	}
}

func TestARunMovesEveryPlannedFileAndPointsTheLibraryAtThem(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Month}/{Name}")
	if len(h.plan.Moves) != 3 {
		t.Fatalf("plan = %+v", h.plan.Moves)
	}
	h.mustRun()

	h.wantFile(l.path("2024/08/IMG_1.jpg"), "one")
	h.wantFile(l.path("2024/08/IMG_2.jpg"), "two")
	h.wantFile(l.path("2025/01/IMG_3.jpg"), "three")
	for _, rel := range []string{"IMG_1.jpg", "IMG_2.jpg", "sub/IMG_3.jpg"} {
		h.wantNoFile(l.path(rel))
	}
	if got := h.libraryPaths(assets[0]); !equalStrings(got, []string{l.path("2024/08/IMG_1.jpg")}) {
		t.Errorf("the library's paths for the first photo = %q", got)
	}
	if h.runState() != core.OrganiseDone {
		t.Errorf("run state = %s, want done", h.runState())
	}
	for _, s := range h.states() {
		if s != core.MoveMoved {
			t.Errorf("move states = %v, want all moved", h.states())
			break
		}
	}
	dirs, err := l.st.OrganiseDirs(l.ctx, h.run)
	if err != nil || len(dirs) != 4 { // 2024, 2024\08, 2025, 2025\01
		t.Errorf("journaled folders = %q, %v; want the four Lumen made", dirs, err)
	}
	ev := h.events.all()
	if len(ev) < 2 || ev[0].State != core.OrganiseRunning || ev[0].Total != 3 {
		t.Fatalf("progress = %+v, want it to start running with 3 to do", ev)
	}
	if last := ev[len(ev)-1]; last.State != core.OrganiseDone || last.Done != 3 || last.Total != 3 || last.RunID != h.run {
		t.Errorf("last progress = %+v, want done 3 of 3", last)
	}
	// The scanner agrees with the library about every file: nothing to ingest.
	if again := l.plan("{Year}/{Month}/{Name}"); len(again.Moves) != 0 || again.InPlace != 3 {
		t.Errorf("planning again after the run: %d moves, %d in place; want 0 and 3", len(again.Moves), again.InPlace)
	}
}

func TestARunNeverOverwritesAFileThatAppearedAtTheDestinationAfterThePreview(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Month}/{Name}")
	l.write(l.path("2024/08/IMG_1.jpg"), "a stranger's file, made after the preview")
	h.mustRun()

	h.wantFile(l.path("2024/08/IMG_1.jpg"), "a stranger's file, made after the preview")
	h.wantFile(l.path("IMG_1.jpg"), "one")
	moves := h.moves()
	if moves[0].State != core.MoveSkipped || moves[0].Error != ReasonTaken {
		t.Errorf("first move = %+v, want skipped %q", moves[0], ReasonTaken)
	}
	if got := h.libraryPaths(assets[0]); !equalStrings(got, []string{l.path("IMG_1.jpg")}) {
		t.Errorf("the library's paths for the file that stayed = %q", got)
	}
	h.wantFile(l.path("2024/08/IMG_2.jpg"), "two")
	if h.runState() != core.OrganiseDone {
		t.Errorf("run state = %s, want done: a skipped file does not fail the run", h.runState())
	}
}

func TestARunSkipsAFileThatChangedSinceThePreview(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Name}")
	l.write(l.path("IMG_1.jpg"), "edited, and longer than before") // another size
	stamp := time.Now().Add(-48 * time.Hour)                       // the same size, another time
	if err := os.Chtimes(l.path("IMG_2.jpg"), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	h.mustRun()

	moves := h.moves()
	for i := range 2 {
		if moves[i].State != core.MoveSkipped || moves[i].Error != ReasonChanged {
			t.Errorf("move %d = %+v, want skipped %q", i+1, moves[i], ReasonChanged)
		}
	}
	h.wantFile(l.path("IMG_1.jpg"), "edited, and longer than before")
	h.wantFile(l.path("IMG_2.jpg"), "two")
	h.wantFile(l.path("2025/IMG_3.jpg"), "three")
}

func TestARunLeavesAlonePhotosBlockedOrTrashedAfterThePreviewAndFilesThatWentMissing(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Name}")
	if _, err := l.st.SetBlock(l.ctx, core.BlockAsset, assets[0], "", core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	if err := l.st.TrashAssets(l.ctx, []int64{assets[1]}); err != nil {
		t.Fatal(err)
	}
	h.mustRun()
	h.wantFile(l.path("IMG_1.jpg"), "one")
	h.wantFile(l.path("IMG_2.jpg"), "two")
	h.wantFile(l.path("2025/IMG_3.jpg"), "three")
	moves := h.moves()
	for i := range 2 {
		if moves[i].State != core.MoveSkipped || moves[i].Error != ReasonNotVisible {
			t.Errorf("move %d = %+v, want skipped %q", i+1, moves[i], ReasonNotVisible)
		}
	}
}

func TestARunSkipsAFileThatWentMissingAndOneWhoseDestinationFolderWasBlocked(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Name}")
	if err := os.Remove(l.path("IMG_1.jpg")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.st.SetBlock(l.ctx, core.BlockFolder, 0, l.path("2025"), core.BlockPhotos); err != nil {
		t.Fatal(err)
	}
	h.mustRun()
	moves := h.moves()
	if moves[0].State != core.MoveSkipped || moves[0].Error != ReasonMissing {
		t.Errorf("first move = %+v, want skipped %q", moves[0], ReasonMissing)
	}
	if moves[2].State != core.MoveSkipped || moves[2].Error != ReasonBlockedFolder {
		t.Errorf("third move = %+v, want skipped %q", moves[2], ReasonBlockedFolder)
	}
	h.wantFile(l.path("sub/IMG_3.jpg"), "three")
	h.wantNoFile(l.path("2025"))
	h.wantFile(l.path("2024/IMG_2.jpg"), "two")
}

func TestARunJournalsEachFolderBeforeItIsCreated(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Month}/{Name}")
	var made []string
	h.runner.mkdir = func(path string) error {
		dirs, err := l.st.OrganiseDirs(l.ctx, h.run)
		if err != nil {
			return err
		}
		if !slices.Contains(dirs, path) {
			t.Errorf("folder %s is being created and the journal does not list it yet: %q", path, dirs)
		}
		made = append(made, path)
		return os.Mkdir(path, 0o755)
	}
	h.mustRun()
	if len(made) != 4 {
		t.Fatalf("folders made = %q, want four", made)
	}
}

func TestACrashBetweenTheJournalAndTheMoveLeavesTheFileWhereItWasAndResumeFinishesTheRun(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.crashAtTheSecondMove(afterJournal)
	if err := h.runner.Run(l.ctx, h.run); !errors.Is(err, errCrashed) {
		t.Fatalf("run = %v, want the injected crash", err)
	}
	if got := h.states(); !slices.Equal(got, []core.OrganiseMoveState{core.MoveMoved, core.MoveMoving, core.MovePlanned}) {
		t.Fatalf("journal after the crash = %v", got)
	}
	h.wantFile(l.path("IMG_2.jpg"), "two")

	h.restart()
	if got := h.states(); !slices.Equal(got, []core.OrganiseMoveState{core.MoveMoved, core.MovePlanned, core.MovePlanned}) {
		t.Fatalf("journal after recovery = %v, want the move in doubt back to planned", got)
	}
	if h.runState() != core.OrganiseFailed {
		t.Fatalf("run state = %s, want failed (resumable)", h.runState())
	}
	h.mustRun() // resume
	h.wantFile(l.path("2024/IMG_2.jpg"), "two")
	h.wantFile(l.path("2025/IMG_3.jpg"), "three")
	if h.runState() != core.OrganiseDone {
		t.Fatalf("run state after resuming = %s", h.runState())
	}
	if got := h.libraryPaths(assets[1]); !equalStrings(got, []string{l.path("2024/IMG_2.jpg")}) {
		t.Errorf("library paths after resuming = %q", got)
	}
}

func TestACrashBetweenTheJournalAndTheMoveCanBeUndoneInstead(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.crashAtTheSecondMove(afterJournal)
	_ = h.runner.Run(l.ctx, h.run)
	h.restart()
	h.mustUndo()
	for rel, content := range map[string]string{"IMG_1.jpg": "one", "IMG_2.jpg": "two", "sub/IMG_3.jpg": "three"} {
		h.wantFile(l.path(rel), content)
	}
	h.wantNoFile(l.path("2024"))
	h.wantNoFile(l.path("2025"))
	if h.runState() != core.OrganiseUndone {
		t.Fatalf("run state = %s, want undone", h.runState())
	}
}

func TestACrashBetweenTheMoveAndTheCommitIsFinishedFromTheDiskAndResumeCarriesOn(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.crashAtTheSecondMove(afterMove)
	if err := h.runner.Run(l.ctx, h.run); !errors.Is(err, errCrashed) {
		t.Fatalf("run = %v, want the injected crash", err)
	}
	// The file has moved and the library has not heard.
	h.wantFile(l.path("2024/IMG_2.jpg"), "two")
	if got := h.libraryPaths(assets[1]); !equalStrings(got, []string{l.path("IMG_2.jpg")}) {
		t.Fatalf("library paths before recovery = %q, want the old one", got)
	}

	h.restart()
	if got := h.states(); !slices.Equal(got, []core.OrganiseMoveState{core.MoveMoved, core.MoveMoved, core.MovePlanned}) {
		t.Fatalf("journal after recovery = %v, want the move finished", got)
	}
	if got := h.libraryPaths(assets[1]); !equalStrings(got, []string{l.path("2024/IMG_2.jpg")}) {
		t.Fatalf("library paths after recovery = %q, want the new one", got)
	}
	h.mustRun()
	h.wantFile(l.path("2025/IMG_3.jpg"), "three")
	h.mustUndo()
	h.wantFile(l.path("IMG_1.jpg"), "one")
	h.wantFile(l.path("IMG_2.jpg"), "two")
	h.wantFile(l.path("sub/IMG_3.jpg"), "three")
	if h.runState() != core.OrganiseUndone {
		t.Fatalf("run state = %s, want undone", h.runState())
	}
}

func TestRecoveryTouchesNothingWhenBothPlacesHoldAFileOrNeitherDoes(t *testing.T) {
	for _, c := range []struct {
		name    string
		disturb func(l library)
		want    string
	}{
		{"both", func(l library) { l.write(l.path("2024/IMG_2.jpg"), "a stranger's file") }, "at both"},
		{"neither", func(l library) {
			if err := os.Remove(l.path("IMG_2.jpg")); err != nil {
				l.t.Fatal(err)
			}
		}, "no file is at"},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, _ := threePhotos(t)
			h := l.start("{Year}/{Name}")
			h.crashAtTheSecondMove(afterJournal)
			_ = h.runner.Run(l.ctx, h.run)
			c.disturb(l)
			before := map[string]string{}
			for _, p := range []string{"IMG_2.jpg", "2024/IMG_2.jpg"} {
				if content, ok := h.at(l.path(p)); ok {
					before[p] = content
				}
			}
			h.restart()
			if got := h.moves()[1]; got.State != core.MoveFailed || !strings.Contains(got.Error, c.want) {
				t.Fatalf("move in doubt = %+v, want failed, saying \"%s\"", got, c.want)
			}
			for p, content := range before {
				h.wantFile(l.path(p), content)
			}
			for _, p := range []string{"IMG_2.jpg", "2024/IMG_2.jpg"} {
				if _, was := before[p]; !was {
					h.wantNoFile(l.path(p))
				}
			}
			// The run can still be resumed for the rest.
			h.mustRun()
			h.wantFile(l.path("2025/IMG_3.jpg"), "three")
			if h.runState() != core.OrganiseDone {
				t.Errorf("run state = %s after resuming", h.runState())
			}
		})
	}
}

func TestRecoveryDoesNotTrustAFileAtTheDestinationThatIsNotTheOneThatWasMoved(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.crashAtTheSecondMove(afterMove)
	_ = h.runner.Run(l.ctx, h.run)
	// After the move, but before the restart, something replaced the file.
	l.write(l.path("2024/IMG_2.jpg"), "replaced by something much longer than the original")
	h.restart()
	if got := h.moves()[1]; got.State != core.MoveFailed {
		t.Fatalf("move in doubt = %+v, want failed", got)
	}
	if got := h.libraryPaths(assets[1]); !equalStrings(got, []string{l.path("IMG_2.jpg")}) {
		t.Errorf("library paths = %q, the library must not be pointed at a file that is not its own", got)
	}
}

func TestUndoPutsEveryFileBackRemovesOnlyTheFoldersItMadeAndRestoresTheLibrary(t *testing.T) {
	l, assets := threePhotos(t)
	if err := os.MkdirAll(l.path("2024"), 0o750); err != nil { // a folder that was already there
		t.Fatal(err)
	}
	h := l.start("{Year}/{Month}/{Name}")
	h.mustRun()
	// A stranger's file in a folder Lumen made: that folder stays.
	l.write(l.path("2025/note.txt"), "keep me")
	h.mustUndo()

	h.wantFile(l.path("IMG_1.jpg"), "one")
	h.wantFile(l.path("IMG_2.jpg"), "two")
	h.wantFile(l.path("sub/IMG_3.jpg"), "three")
	h.wantFile(l.path("2025/note.txt"), "keep me")
	h.wantNoFile(l.path("2025/01"))
	h.wantNoFile(l.path("2024/08"))
	if !exist(l.path("2024")) {
		t.Error("a folder that was there before the run was removed")
	}
	if got := h.libraryPaths(assets[2]); !equalStrings(got, []string{l.path("sub/IMG_3.jpg")}) {
		t.Errorf("library paths after undo = %q", got)
	}
	if h.runState() != core.OrganiseUndone {
		t.Errorf("run state = %s, want undone", h.runState())
	}
	ev := h.events.all()
	if last := ev[len(ev)-1]; last.State != core.OrganiseUndone || last.Done != 3 || last.Total != 3 {
		t.Errorf("last progress = %+v, want undone 3 of 3", last)
	}
}

func TestUndoNeverOverwritesAndReportsAFileWhoseOldPlaceIsTakenThenSucceedsWhenItIsFree(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.mustRun()
	l.write(l.path("IMG_1.jpg"), "someone put this here")
	h.mustUndo()

	h.wantFile(l.path("IMG_1.jpg"), "someone put this here")
	h.wantFile(l.path("2024/IMG_1.jpg"), "one")
	h.wantFile(l.path("IMG_2.jpg"), "two")
	moves := h.moves()
	if moves[0].State != core.MoveMoved || moves[0].Error != ReasonTaken {
		t.Errorf("the blocked file's move = %+v, want moved with reason %q", moves[0], ReasonTaken)
	}
	if h.runState() != core.OrganiseDone {
		t.Errorf("run state = %s, want done (not undone while a file is still moved)", h.runState())
	}
	if got := h.libraryPaths(assets[0]); !equalStrings(got, []string{l.path("2024/IMG_1.jpg")}) {
		t.Errorf("library paths = %q, want the file's real place", got)
	}
	ev := h.events.all()
	if last := ev[len(ev)-1]; last.State != core.OrganiseDone || last.Done != 3 || last.Total != 3 {
		t.Errorf("last progress = %+v", last)
	}

	if err := os.Remove(l.path("IMG_1.jpg")); err != nil { // the test's own scratch file
		t.Fatal(err)
	}
	h.mustUndo()
	h.wantFile(l.path("IMG_1.jpg"), "one")
	if h.runState() != core.OrganiseUndone {
		t.Errorf("run state = %s after the second undo", h.runState())
	}
}

func TestUndoLeavesAFileTheUserMovedThemselvesWhereItIs(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.mustRun()
	if err := os.Rename(l.path("2024/IMG_1.jpg"), l.path("2024/renamed.jpg")); err != nil {
		t.Fatal(err)
	}
	h.mustUndo()
	h.wantFile(l.path("2024/renamed.jpg"), "one")
	if m := h.moves()[0]; m.State != core.MoveMoved || m.Error != ReasonMissing {
		t.Errorf("move = %+v, want it still moved, with reason %q", m, ReasonMissing)
	}
	h.wantFile(l.path("IMG_2.jpg"), "two")
}

func TestACrashDuringUndoIsSettledFromTheDiskAndUndoCanBeRunAgain(t *testing.T) {
	for _, point := range []crashPoint{afterJournal, afterMove} {
		l, assets := threePhotos(t)
		h := l.start("{Year}/{Name}")
		h.mustRun()
		// Undo goes last move first, so the second file is the middle of three.
		h.crashAtTheSecondMove(point)
		if err := h.runner.Undo(l.ctx, h.run); !errors.Is(err, errCrashed) {
			t.Fatalf("undo = %v, want the injected crash", err)
		}
		h.restart()
		if h.runState() != core.OrganiseDone {
			t.Fatalf("crash %d: run state = %s, want done with files still moved", point, h.runState())
		}
		for _, m := range h.moves() {
			if m.State != core.MoveMoved && m.State != core.MoveUndone {
				t.Fatalf("crash %d: a move is left %s", point, m.State)
			}
		}
		h.mustUndo()
		h.wantFile(l.path("IMG_2.jpg"), "two")
		h.wantFile(l.path("IMG_1.jpg"), "one")
		h.wantFile(l.path("sub/IMG_3.jpg"), "three")
		if got := h.libraryPaths(assets[1]); !equalStrings(got, []string{l.path("IMG_2.jpg")}) {
			t.Errorf("crash %d: library paths = %q", point, got)
		}
		if h.runState() != core.OrganiseUndone {
			t.Errorf("crash %d: run state = %s, want undone", point, h.runState())
		}
	}
}

func TestARunThatIsStoppedPartWayEndsFailedAndResumes(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Name}")
	ctx, cancel := context.WithCancel(l.ctx)
	h.runner.crash = func(p crashPoint, seq int) bool {
		if p == afterMove && seq == 1 {
			cancel() // the app is closing, just after the first file moved
		}
		return false
	}
	err := h.runner.Run(ctx, h.run)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("run = %v, want the cancellation", err)
	}
	if h.runState() != core.OrganiseFailed {
		t.Fatalf("run state = %s, want failed", h.runState())
	}
	if got := h.states(); got[0] != core.MoveMoved || got[2] != core.MovePlanned {
		t.Fatalf("journal = %v", got)
	}
	h.runner.crash = nil
	h.mustRun()
	h.wantFile(l.path("2025/IMG_3.jpg"), "three")
	if h.runState() != core.OrganiseDone {
		t.Fatalf("run state after resuming = %s", h.runState())
	}
}

func TestAlbumsTagsPeopleAndBlocksStayAttachedToTheMovedFiles(t *testing.T) {
	l, assets := threePhotos(t)
	a := assets[0]
	album, err := l.st.CreateAlbum(l.ctx, "Holiday", core.AlbumManual, core.AlbumRule{}, []int64{a})
	if err != nil {
		t.Fatal(err)
	}
	tag, err := l.st.CreateTag(l.ctx, "Sea", core.TagManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.st.TagAsset(l.ctx, a, tag); err != nil {
		t.Fatal(err)
	}
	face := func(x float32, k int) store.DetectedFace {
		vec := make([]float32, 4)
		vec[k] = 1
		return store.DetectedFace{X: x, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: vec}
	}
	if err := l.st.SaveFaces(l.ctx, "f1", "f1", []store.FaceResult{
		{AssetID: a, Faces: []store.DetectedFace{face(0.1, 0)}}, {AssetID: assets[2], Faces: []store.DetectedFace{face(0.3, 1)}},
	}); err != nil {
		t.Fatal(err)
	}
	faceOf := func(asset int64) core.Face {
		faces, err := l.st.AssetFaces(l.ctx, asset)
		if err != nil || len(faces) != 1 {
			t.Fatalf("faces of %d = %+v, %v", asset, faces, err)
		}
		return faces[0]
	}
	bob, err := l.st.NameFaces(l.ctx, []int64{faceOf(assets[2]).ID}, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	person, err := l.st.NameFaces(l.ctx, []int64{faceOf(a).ID}, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	// A label block on Alice: the photos stay, her name is hidden. A block
	// that hides photos would keep them out of the run altogether.
	block, err := l.st.SetBlock(l.ctx, core.BlockPerson, person, "", core.BlockLabel)
	if err != nil {
		t.Fatal(err)
	}
	faces, err := l.st.AssetFaces(l.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	aliceShown := len(faces)
	private, err := l.st.SetBlock(l.ctx, core.BlockAsset, assets[1], "", core.BlockPhotos)
	if err != nil {
		t.Fatal(err)
	}

	h := l.start("{Year}/{Name}")
	if len(h.plan.Moves) != 2 { // the blocked photo is not planned
		t.Fatalf("the blocked photo was planned: %+v", h.plan.Moves)
	}
	h.mustRun()
	h.wantFile(l.path("IMG_2.jpg"), "two") // the blocked photo never moved

	if albums, err := l.st.AssetAlbums(l.ctx, a); err != nil || !equalInts(albums, []int64{album}) {
		t.Errorf("albums = %v, %v", albums, err)
	}
	if tags, err := l.st.AssetTags(l.ctx, a, core.TagConfidenceDefault); err != nil || len(tags) != 1 || tags[0].ID != tag {
		t.Errorf("tags = %+v, %v", tags, err)
	}
	if got := faceOf(assets[2]); got.Person != bob {
		t.Errorf("Bob's face after the move belongs to person %d, want %d", got.Person, bob)
	}
	if faces, err := l.st.AssetFaces(l.ctx, a); err != nil || len(faces) != aliceShown {
		t.Errorf("Alice's face is shown %d times after the move, was %d (her name is blocked): %v", len(faces), aliceShown, err)
	}
	blocks, err := l.st.Blocks(l.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, b := range blocks {
		ids = append(ids, b.ID)
	}
	slices.Sort(ids)
	if want := []int64{block, private}; !equalInts(ids, want) {
		t.Errorf("blocks = %v, want %v", ids, want)
	}
	if paths := h.libraryPaths(a); !equalStrings(paths, []string{l.path("2024/IMG_1.jpg")}) {
		t.Errorf("paths = %q", paths)
	}
}

func equalInts(a, b []int64) bool { return slices.Equal(a, b) }

func TestMoveNeverReplacesAFileAtTheDestination(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "a.jpg"), filepath.Join(dir, "b.jpg")
	for path, content := range map[string]string{from: "mine", to: "theirs"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := Move(from, to); !errors.Is(err, ErrExists) {
		t.Fatalf("Move over an existing file = %v, want ErrExists", err)
	}
	for path, want := range map[string]string{from: "mine", to: "theirs"} {
		if got, _ := os.ReadFile(path); string(got) != want {
			t.Errorf("%s holds %q after the refused move, want %q", path, got, want)
		}
	}
	// Not over a folder either, nor a folder itself.
	if err := Move(from, dir); err == nil {
		t.Error("Move onto a folder succeeded")
	}
	if err := Move(dir, filepath.Join(t.TempDir(), "elsewhere")); err == nil {
		t.Error("Move of a folder succeeded")
	}
	if err := Move("relative.jpg", to); err == nil {
		t.Error("Move of a relative path succeeded")
	}
	// And a free destination works, keeping the file's time.
	free := filepath.Join(dir, "c.jpg")
	before, _ := os.Stat(from)
	if err := Move(from, free); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(free)
	if err != nil || !after.ModTime().Equal(before.ModTime()) || exist(from) {
		t.Fatalf("after a move: %v, %v; want the same time and no file at the old path", after, err)
	}
}

func TestRemoveDirRemovesAnEmptyFolderAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	empty, full, file := filepath.Join(dir, "empty"), filepath.Join(dir, "full"), filepath.Join(dir, "file.jpg")
	for _, d := range []string{empty, full} {
		if err := os.Mkdir(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{filepath.Join(full, "x.jpg"), file} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveDir(empty); err != nil || exist(empty) {
		t.Errorf("empty folder: %v, still there %v", err, exist(empty))
	}
	if err := RemoveDir(full); err == nil || !exist(filepath.Join(full, "x.jpg")) {
		t.Errorf("a folder with a file in it: %v; it must stay", err)
	}
	if err := RemoveDir(file); err == nil || !exist(file) {
		t.Errorf("a file: %v; it must stay", err)
	}
}

// twoWithSidecars is threePhotos with sidecars for the first and second.
func twoWithSidecars(t *testing.T) (l library, assets []int64) {
	t.Helper()
	l, assets = threePhotos(t)
	l.write(l.path("IMG_1.jpg.xmp"), "xmp one")
	l.write(l.path("IMG_2.jpg.XMP"), "xmp two") // the file system folds case
	return l, assets
}

func TestASidecarMovesWithItsPhotoUnderThePhotosNewName(t *testing.T) {
	l, _ := twoWithSidecars(t)
	l.add("other/IMG_1.jpg", "another photo with the same name", day(2024, time.August, 9))
	l.write(l.path("other/IMG_1.jpg.xmp"), "xmp of the other")
	l.write(l.path("IMG_3.xmp"), "not a sidecar: it lacks the photo's extension")
	h := l.start("{Year}/{Name}")
	h.mustRun()

	if got := names(t, l.path("2024")); !equalStrings(got, []string{"IMG_1 (2).jpg", "IMG_1 (2).jpg.xmp", "IMG_1.jpg", "IMG_1.jpg.xmp", "IMG_2.jpg", "IMG_2.jpg.xmp"}) {
		t.Fatalf("the new folder holds %q", got)
	}
	h.wantFile(l.path("2024/IMG_1.jpg.xmp"), "xmp one")
	h.wantFile(l.path("2024/IMG_2.jpg.xmp"), "xmp two")
	h.wantFile(l.path("2024/IMG_1 (2).jpg.xmp"), "xmp of the other")
	h.wantNoFile(l.path("IMG_1.jpg.xmp"))
	h.wantNoFile(l.path("IMG_2.jpg.xmp"))
	h.wantFile(l.path("IMG_3.xmp"), "not a sidecar: it lacks the photo's extension")
	for _, m := range h.moves() {
		if m.State == core.MoveMoved && filepath.Base(m.From) != "IMG_3.jpg" && m.Sidecar != store.SidecarMoved {
			t.Errorf("move %d sidecar = %q, want moved", m.Seq, m.Sidecar)
		}
	}
}

func TestASidecarWhoseNewNameIsTakenStaysAndThePhotoStillMoves(t *testing.T) {
	l, _ := twoWithSidecars(t)
	h := l.start("{Year}/{Name}")
	l.write(l.path("2024/IMG_1.jpg.xmp"), "a stranger's sidecar, made after the preview")
	h.mustRun()

	h.wantFile(l.path("2024/IMG_1.jpg"), "one")
	h.wantFile(l.path("IMG_1.jpg.xmp"), "xmp one")
	h.wantFile(l.path("2024/IMG_1.jpg.xmp"), "a stranger's sidecar, made after the preview")
	m := h.moves()[0]
	if m.State != core.MoveMoved || m.Sidecar != store.SidecarKept || m.Error != ReasonSidecarTaken {
		t.Fatalf("move = %+v, want moved with the sidecar kept and the reason recorded", m)
	}
	h.wantFile(l.path("2024/IMG_2.jpg.xmp"), "xmp two")
}

func TestAPhotoThatIsSkippedLeavesItsSidecarWhereItIs(t *testing.T) {
	l, _ := twoWithSidecars(t)
	h := l.start("{Year}/{Name}")
	l.write(l.path("IMG_1.jpg"), "edited, and longer than before")
	h.mustRun()
	h.wantFile(l.path("IMG_1.jpg.xmp"), "xmp one")
	h.wantNoFile(l.path("2024/IMG_1.jpg.xmp"))
}

func TestUndoPutsTheSidecarBackWithItsPhotoAndNeverReplacesOneThatAppearedAtItsOldPlace(t *testing.T) {
	l, _ := twoWithSidecars(t)
	h := l.start("{Year}/{Name}")
	h.mustRun()
	l.write(l.path("IMG_1.jpg.xmp"), "a stranger's sidecar, made after the run")
	h.mustUndo()

	h.wantFile(l.path("IMG_1.jpg"), "one")
	h.wantFile(l.path("IMG_1.jpg.xmp"), "a stranger's sidecar, made after the run")
	h.wantFile(l.path("2024/IMG_1.jpg.xmp"), "xmp one")
	if m := h.moves()[0]; m.State != core.MoveUndone || m.Sidecar != store.SidecarKept || m.Error != ReasonSidecarTaken {
		t.Errorf("first move = %+v, want undone with its sidecar kept", m)
	}
	h.wantFile(l.path("IMG_2.jpg"), "two")
	h.wantFile(l.path("IMG_2.jpg.xmp"), "xmp two")
	h.wantNoFile(l.path("2024/IMG_2.jpg.xmp"))
	if m := h.moves()[1]; m.Sidecar != store.SidecarUndone {
		t.Errorf("second move's sidecar = %q, want undone", m.Sidecar)
	}
	if h.runState() != core.OrganiseUndone {
		t.Errorf("run state = %s", h.runState())
	}
}

func TestACrashAroundAMoveWithASidecarIsSettledSoThePairEndsTogether(t *testing.T) {
	for _, c := range []struct {
		name  string
		point crashPoint
	}{
		{"after the journal", afterJournal},
		{"after the photo moved", afterMove},
		{"after the sidecar moved", afterSidecar},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, assets := twoWithSidecars(t)
			h := l.start("{Year}/{Name}")
			h.crashAtTheSecondMove(c.point)
			if err := h.runner.Run(l.ctx, h.run); !errors.Is(err, errCrashed) {
				t.Fatalf("run = %v, want the injected crash", err)
			}
			h.restart()
			h.mustRun()
			h.wantFile(l.path("2024/IMG_2.jpg"), "two")
			h.wantFile(l.path("2024/IMG_2.jpg.xmp"), "xmp two")
			h.wantNoFile(l.path("IMG_2.jpg.xmp"))
			if m := h.moves()[1]; m.State != core.MoveMoved || m.Sidecar != store.SidecarMoved {
				t.Fatalf("move = %+v, want moved with its sidecar moved", m)
			}
			if got := h.libraryPaths(assets[1]); !equalStrings(got, []string{l.path("2024/IMG_2.jpg")}) {
				t.Errorf("library paths = %q", got)
			}
			h.mustUndo()
			h.wantFile(l.path("IMG_2.jpg"), "two")
			h.wantFile(l.path("IMG_2.jpg.xmp"), "xmp two")
			h.wantNoFile(l.path("2024/IMG_2.jpg.xmp"))
		})
	}
}

func TestACrashDuringUndoOfAMoveWithASidecarIsSettledSoThePairEndsTogether(t *testing.T) {
	for _, c := range []struct {
		name  string
		point crashPoint
	}{
		{"after the journal", afterJournal},
		{"after the photo is back", afterMove},
		{"after the sidecar is back", afterSidecar},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, _ := twoWithSidecars(t)
			h := l.start("{Year}/{Name}")
			h.mustRun()
			h.crashAtTheSecondMove(c.point)
			if err := h.runner.Undo(l.ctx, h.run); !errors.Is(err, errCrashed) {
				t.Fatalf("undo = %v, want the injected crash", err)
			}
			h.restart()
			h.mustUndo()
			for _, rel := range []string{"IMG_1.jpg", "IMG_1.jpg.xmp", "IMG_2.jpg", "IMG_2.jpg.xmp"} {
				h.wantNoFile(l.path("2024/" + rel))
				if !exist(l.path(rel)) {
					t.Errorf("%s did not come back", rel)
				}
			}
			h.wantFile(l.path("IMG_2.jpg.xmp"), "xmp two")
			if h.runState() != core.OrganiseUndone {
				t.Errorf("run state = %s, want undone", h.runState())
			}
		})
	}
}

func TestRecoveryNeverReplacesASidecarAtBothPlaces(t *testing.T) {
	l, _ := twoWithSidecars(t)
	h := l.start("{Year}/{Name}")
	h.crashAtTheSecondMove(afterMove)
	_ = h.runner.Run(l.ctx, h.run)
	// The photo moved; a sidecar of someone else's is at the new name too.
	l.write(l.path("2024/IMG_2.jpg.xmp"), "someone else's")
	h.restart()
	h.wantFile(l.path("IMG_2.jpg.XMP"), "xmp two")
	h.wantFile(l.path("2024/IMG_2.jpg.xmp"), "someone else's")
	if m := h.moves()[1]; m.State != core.MoveMoved || m.Sidecar != store.SidecarKept || m.Error != ReasonSidecarTaken {
		t.Fatalf("move = %+v, want moved with the sidecar kept", m)
	}
}

// scanAgain runs a full scan of the library's source, as the indexer does
// after a run, and says what it made of the library.
func scanAgain(t *testing.T, l library) scanner.Result {
	t.Helper()
	sc := &scanner.Scanner{Store: l.st, HashWorkers: 2}
	res, err := sc.Scan(l.ctx, l.src, "", scanner.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAFullScanAfterARunLosesNothingEvenWhenValuesLookLikeNamesTheScannerSkips(t *testing.T) {
	l := newLibrary(t)
	a := l.add("a.jpg", "a", day(2024, time.May, 1))
	l.setEventName(a, "$Recycle.Bin")
	l.setCamera(a, "System Volume Information")
	b := l.add("b.jpg", "b", day(2025, time.May, 1))
	l.setEventName(b, ".hidden trip")
	h := l.start("{Event}/{Camera}/{Name}")
	h.mustRun()
	if res := scanAgain(t, l); res.Seen != 2 || res.Removed != 0 || res.Changed != 0 {
		t.Fatalf("scan after the run: %+v, want both files seen and nothing removed", res)
	}
	if n, err := l.st.CountVisible(l.ctx); err != nil || n != 2 {
		t.Fatalf("visible photos after the scan = %d, %v; want 2", n, err)
	}
}

func TestAnExistingHiddenFolderIsNotMovedInto(t *testing.T) {
	l := newLibrary(t)
	l.add("a.jpg", "a", day(2024, time.May, 1))
	l.add("b.jpg", "b", day(2025, time.May, 1))
	if err := os.Mkdir(l.path("2024"), 0o750); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(l.path("2024"))
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(name, windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
	h := l.start("{Year}/{Name}")
	moves := h.plan.Moves
	if len(moves) != 2 || moves[0].State != core.MoveSkipped || moves[0].Error != ReasonNotAFolder || moves[1].State != core.MovePlanned {
		t.Fatalf("plan = %+v, want the move into the hidden folder skipped and the other planned", moves)
	}
	h.mustRun()
	h.wantFile(l.path("a.jpg"), "a")
	h.wantFile(l.path("2025/b.jpg"), "b")
	if res := scanAgain(t, l); res.Removed != 0 {
		t.Fatalf("a scan after the run removed %d files", res.Removed)
	}
}

// junction makes a directory junction at link to target, or skips the test
// where Windows will not allow it.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("cannot make a junction here: %v: %s", err, out)
	}
}

func TestASourceThatIsAJunctionIsOrganisedAndUndoneInsideItself(t *testing.T) {
	l := newLibrary(t)
	target := filepath.Join(t.TempDir(), "RealPhotos")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "Pictures")
	junction(t, link, target)
	src := l.addSource(link)
	l.addTo(src, link, "IMG_1.jpg", "one", day(2024, time.August, 5))
	l.addTo(src, link, "sub/IMG_2.jpg", "two", day(2025, time.August, 5))

	h := l.start("{Year}/{Name}")
	if len(h.plan.Moves) != 2 || h.plan.Moves[0].State != core.MovePlanned {
		t.Fatalf("plan = %+v", h.plan.Moves)
	}
	h.mustRun()
	h.wantFile(filepath.Join(target, "2024", "IMG_1.jpg"), "one")
	h.wantFile(filepath.Join(target, "2025", "IMG_2.jpg"), "two")
	h.wantFile(filepath.Join(link, "2024", "IMG_1.jpg"), "one")
	if h.runState() != core.OrganiseDone {
		t.Fatalf("run state = %s, want done", h.runState())
	}
	for _, m := range h.moves() {
		if m.State != core.MoveMoved {
			t.Fatalf("a move did not happen: %+v", m)
		}
	}
	h.mustUndo()
	h.wantFile(filepath.Join(target, "IMG_1.jpg"), "one")
	h.wantFile(filepath.Join(target, "sub", "IMG_2.jpg"), "two")
	h.wantNoFile(filepath.Join(target, "2024"))
	if !exist(link) {
		t.Fatal("the source folder itself is gone")
	}
	if h.runState() != core.OrganiseUndone {
		t.Fatalf("run state after undo = %s", h.runState())
	}
}

func TestUndoLeavesAlonePlacesWhereAnotherRunOrAStrangerPutAFile(t *testing.T) {
	l := newLibrary(t)
	a := l.add("A.jpg", "alpha", day(2024, time.May, 1))
	first := l.start("{Year}/{Name}") // A.jpg -> 2024\A.jpg
	first.mustRun()
	second := l.start("{Month}/{Name}") // 2024\A.jpg -> 05\A.jpg
	second.mustRun()
	l.write(l.path("2024/A.jpg"), "a stranger's file at the first run's destination")

	first.mustUndo()
	first.wantFile(l.path("2024/A.jpg"), "a stranger's file at the first run's destination")
	first.wantFile(l.path("05/A.jpg"), "alpha")
	first.wantNoFile(l.path("A.jpg"))
	if m := first.moves()[0]; m.State != core.MoveMoved || m.Error != ReasonChanged {
		t.Fatalf("first run's move = %+v, want it left moved with reason %q", m, ReasonChanged)
	}
	if first.runState() != core.OrganiseDone {
		t.Fatalf("first run state = %s, want done (still undoable)", first.runState())
	}
	if got := first.libraryPaths(a); !equalStrings(got, []string{l.path("05/A.jpg")}) {
		t.Fatalf("library paths = %q, the file is where the second run put it", got)
	}

	// With the stranger gone and the second run undone, the first can be undone.
	if err := os.Remove(l.path("2024/A.jpg")); err != nil { // the test's own scratch file
		t.Fatal(err)
	}
	second.mustUndo()
	first.mustUndo()
	first.wantFile(l.path("A.jpg"), "alpha")
	if first.runState() != core.OrganiseUndone {
		t.Fatalf("first run state = %s, want undone", first.runState())
	}
}

func TestUndoLeavesAFileThatTheLibraryNoLongerHoldsAtItsPlace(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.mustRun()
	// The library no longer has the first photo's file where the run put it,
	// so it is not the run's to move.
	var gone int64
	for _, f := range l.snapshot(0).Files {
		if f.AssetID == assets[0] {
			gone = f.FileID
		}
	}
	if err := l.st.RemoveFiles(l.ctx, []int64{gone}); err != nil {
		t.Fatal(err)
	}
	h.mustUndo()
	h.wantFile(l.path("2024/IMG_1.jpg"), "one")
	if m := h.moves()[0]; m.State != core.MoveMoved || m.Error != ReasonMissing {
		t.Fatalf("move = %+v, want it left moved with reason %q", m, ReasonMissing)
	}
	h.wantFile(l.path("IMG_2.jpg"), "two")
}

func TestACrashDuringUndoFollowedByAFileAtTheOldPlaceLeavesTheRunUndoable(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Name}")
	h.mustRun()
	h.crashAtTheSecondMove(afterJournal)
	if err := h.runner.Undo(l.ctx, h.run); !errors.Is(err, errCrashed) {
		t.Fatalf("undo = %v, want the injected crash", err)
	}
	l.write(l.path("IMG_2.jpg"), "someone put this at the old place")
	h.restart()
	if m := h.moves()[1]; m.State != core.MoveMoved || m.Error != ReasonTaken {
		t.Fatalf("move in doubt = %+v, want it moved with reason %q", m, ReasonTaken)
	}
	if h.runState() != core.OrganiseDone {
		t.Fatalf("run state = %s, want done (not undone while a file is still moved)", h.runState())
	}
	h.mustUndo()
	h.wantFile(l.path("IMG_2.jpg"), "someone put this at the old place")
	h.wantFile(l.path("2024/IMG_2.jpg"), "two")
	h.wantFile(l.path("IMG_1.jpg"), "one")
	if h.runState() != core.OrganiseDone {
		t.Fatalf("run state = %s, want done while the way is blocked", h.runState())
	}
	if err := os.Remove(l.path("IMG_2.jpg")); err != nil { // the test's own scratch file
		t.Fatal(err)
	}
	h.mustUndo()
	h.wantFile(l.path("IMG_2.jpg"), "two")
	if h.runState() != core.OrganiseUndone {
		t.Fatalf("run state = %s, want undone", h.runState())
	}
}

func TestAStaleLibraryRowAtTheDestinationDoesNotWedgeTheRun(t *testing.T) {
	l, assets := threePhotos(t)
	h := l.start("{Year}/{Name}")
	// After the preview a row appears for a path the run is about to use, for
	// a file that is not on disk.
	stale := store.IngestItem{SourceID: l.src.ID, Path: l.path("2024/IMG_1.jpg"), Size: 5, MTime: 1,
		Hash: []byte("a ghost"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}
	if _, err := l.st.Ingest(l.ctx, []store.IngestItem{stale}); err != nil {
		t.Fatal(err)
	}
	h.mustRun()
	if h.runState() != core.OrganiseDone {
		t.Fatalf("run state = %s, want done", h.runState())
	}
	h.wantFile(l.path("2024/IMG_1.jpg"), "one")
	if got := h.libraryPaths(assets[0]); !equalStrings(got, []string{l.path("2024/IMG_1.jpg")}) {
		t.Fatalf("library paths = %q", got)
	}
	if m := h.moves()[0]; m.State != core.MoveMoved {
		t.Fatalf("move = %+v", m)
	}
}

// textLog is a slog handler that keeps what was logged, to look for paths.
type textLog struct {
	mu   sync.Mutex
	text strings.Builder
}

func (*textLog) Enabled(context.Context, slog.Level) bool { return true }
func (h *textLog) WithAttrs([]slog.Attr) slog.Handler     { return h }
func (h *textLog) WithGroup(string) slog.Handler          { return h }
func (h *textLog) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.text.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		h.text.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	h.text.WriteString("\n")
	return nil
}

func TestAFailedMoveIsLoggedWithoutAnyPathAndRecordedWithoutThem(t *testing.T) {
	l, _ := threePhotos(t)
	h := l.start("{Year}/{Name}")
	// The folder is not made, so the move fails: Windows names both paths in its error.
	h.runner.mkdir = func(string) error { return nil }
	logged := &textLog{}
	previous := slog.Default()
	slog.SetDefault(slog.New(logged))
	t.Cleanup(func() { slog.SetDefault(previous) })
	h.mustRun()

	logged.mu.Lock()
	text := logged.text.String()
	logged.mu.Unlock()
	if !strings.Contains(text, "organise: move a file") {
		t.Fatalf("the failure was not logged: %q", text)
	}
	if strings.Contains(text, l.root) || strings.Contains(text, "IMG_1") || strings.Contains(text, filepath.Base(l.root)) {
		t.Fatalf("a path reached the log:\n%s", text)
	}
	m := h.moves()[0]
	if m.State != core.MoveFailed || m.Error == "" || strings.Contains(m.Error, l.root) {
		t.Fatalf("move = %+v, want failed with Windows's words and no path", m)
	}
}
