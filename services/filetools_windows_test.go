package services

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
)

func TestARenameIsPreviewedThenMadeThroughTheJournal(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	id := l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))

	plan, err := l.s.PlanFileMoves(ctx, []int64{id}, "", "Holiday.jpg")
	check(t, err)
	if plan.RunID == 0 || plan.Template != "" || len(plan.Moves) != 1 || plan.Moves[0].To != l.path("Holiday.jpg") || plan.Moves[0].State != core.MovePlanned {
		t.Fatalf("plan = %+v", plan)
	}
	l.wantFile("IMG_1.jpg", true) // a preview moves nothing
	l.wantFile("Holiday.jpg", false)

	check(t, l.s.StartOrganise(ctx, plan.RunID))
	l.waitState(plan.RunID, core.OrganiseDone)
	l.wantFile("IMG_1.jpg", false)
	l.wantFile("Holiday.jpg", true)
	moves, err := l.s.FileMoveOutcomes(ctx, plan.RunID)
	check(t, err)
	if len(moves) != 1 || moves[0].State != core.MoveMoved || moves[0].AssetID != id {
		t.Fatalf("outcomes = %+v", moves)
	}

	// The same name again is where the file already is: nothing to move.
	plan, err = l.s.PlanFileMoves(ctx, []int64{id}, "", "Holiday.jpg")
	check(t, err)
	if len(plan.Moves) != 0 || plan.InPlace != 1 {
		t.Fatalf("a rename to its own name: %+v", plan)
	}
}

func TestAMoveIntoAFolderOfItsSourceKeepsTheName(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	a := l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	b := l.add("IMG_2.jpg", 20, time.Date(2024, time.August, 6, 12, 0, 0, 0, time.UTC))
	plan, err := l.s.PlanFileMoves(ctx, []int64{b, a, a}, l.path("Trips"), "")
	check(t, err)
	if len(plan.Moves) != 2 || plan.Moves[0].To != l.path("Trips/IMG_1.jpg") || plan.Moves[1].To != l.path("Trips/IMG_2.jpg") {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestAMoveThatCannotBeMadeIsRefusedWithACodeTheUICanExplain(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	a := l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	b := l.add("IMG_2.jpg", 20, time.Date(2024, time.August, 6, 12, 0, 0, 0, time.UTC))
	for _, c := range []struct {
		what      string
		ids       []int64
		directory string
		name      string
		want      ErrorCode
	}{
		{"nothing selected", nil, "", "a.jpg", CodeNotFound},
		{"one name for two files", []int64{a, b}, "", "a.jpg", CodeNotFound},
		{"an asset that is not there", []int64{a, 999}, l.path("Trips"), "", CodeNotFound},
		{"a path as a name", []int64{a}, "", `sub\a.jpg`, CodeInvalidFileName},
		{"a character Windows forbids", []int64{a}, "", "what?.jpg", CodeInvalidFileName},
		{"a device name", []int64{a}, "", "CON.jpg", CodeInvalidFileName},
		{"another format", []int64{a}, "", "IMG_1.png", CodeDestinationOutside},
		{"a folder outside the source", []int64{a}, t.TempDir(), "", CodeDestinationOutside},
		{"a name another file has", []int64{a}, "", "IMG_2.jpg", CodeDestinationTaken},
	} {
		if _, err := l.s.PlanFileMoves(ctx, c.ids, c.directory, c.name); failureCode(t, err) != c.want {
			t.Errorf("%s: %v, want code %s", c.what, err, c.want)
		}
	}
	// Changed on disk since it was indexed: the preview is refused, not guessed.
	check(t, os.WriteFile(l.path("IMG_1.jpg"), jpegBytes(t, 99), 0o600))
	check(t, os.Chtimes(l.path("IMG_1.jpg"), time.Now().Add(time.Hour), time.Now().Add(time.Hour)))
	if _, err := l.s.PlanFileMoves(ctx, []int64{a}, "", "Later.jpg"); failureCode(t, err) != CodeSourceChanged {
		t.Errorf("a changed file: %v", err)
	}
	l.wantFile("IMG_1.jpg", true)
	l.wantFile("IMG_2.jpg", true)
}

func TestOnlyAFileOfTheAssetIsHandedToTheShell(t *testing.T) {
	l := newOrganiseLibrary(t)
	ctx := t.Context()
	id := l.add("IMG_1.jpg", 10, time.Date(2024, time.August, 5, 12, 0, 0, 0, time.UTC))
	type call struct {
		path   string
		choose bool
	}
	var calls []call
	l.s.open = func(path string, choose bool) error {
		calls = append(calls, call{path, choose})
		return nil
	}
	check(t, l.s.OpenMedia(ctx, id, "", false))
	check(t, l.s.OpenMedia(ctx, id, l.path("IMG_1.jpg"), true))
	if len(calls) != 2 || calls[0] != (call{l.path("IMG_1.jpg"), false}) || calls[1] != (call{l.path("IMG_1.jpg"), true}) {
		t.Fatalf("handed to the shell: %+v", calls)
	}

	other := filepath.Join(t.TempDir(), "other.jpg")
	check(t, os.WriteFile(other, jpegBytes(t, 1), 0o600))
	for what, err := range map[string]error{
		"a file that is not the asset's": l.s.OpenMedia(ctx, id, other, false),
		"an asset that is not there":     l.s.OpenMedia(ctx, 999, "", false),
	} {
		if failureCode(t, err) != CodeNotFound {
			t.Errorf("%s: %v", what, err)
		}
	}
	check(t, os.Remove(l.path("IMG_1.jpg")))
	if err := l.s.OpenMedia(ctx, id, "", false); failureCode(t, err) != CodeNotFound {
		t.Errorf("a file that is gone: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("a refused file reached the shell: %+v", calls)
	}
}
