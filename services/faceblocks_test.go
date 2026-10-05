package services

import (
	"errors"
	"math"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/core"
	"lumen/internal/store"
)

// faceLibrary is two photos of one person (the second face is 10 degrees
// from the first) and a third of someone else, with the service over them.
func faceLibrary(t *testing.T) (s *LibraryService, st *store.Store, assets, faces []int64) {
	t.Helper()
	ctx := t.Context()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	var items []store.IngestItem
	for _, name := range []string{"a", "b", "c"} {
		items = append(items, store.IngestItem{SourceID: src.ID, Path: `C:\Photos\` + name + `.jpg`, Size: 1,
			Hash: []byte(name), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1})
	}
	if _, err := st.Ingest(ctx, items); err != nil {
		t.Fatal(err)
	}
	assets = []int64{1, 2, 3}
	vec := func(deg float64) []float32 {
		rad := deg * math.Pi / 180
		return []float32{float32(math.Cos(rad)), float32(math.Sin(rad))}
	}
	var results []store.FaceResult
	for i, deg := range []float64{0, 10, 150} {
		results = append(results, store.FaceResult{AssetID: assets[i], Faces: []store.DetectedFace{
			{X: 0.1, Y: 0.1, W: 0.3, H: 0.3, Score: 0.9, Vec: vec(deg)}}})
	}
	if err := st.SaveFaces(ctx, "m1", "m1", results); err != nil {
		t.Fatal(err)
	}
	for _, a := range assets {
		fs, err := st.AssetFaces(ctx, a)
		if err != nil || len(fs) != 1 {
			t.Fatalf("faces of %d = %v, %v", a, fs, err)
		}
		faces = append(faces, fs[0].ID)
	}
	return NewLibraryService(st, nil, nil, nil), st, assets, faces
}

func shownAssets(t *testing.T, st *store.Store, assets []int64) []bool {
	t.Helper()
	out := make([]bool, len(assets))
	for i, a := range assets {
		ok, err := st.AssetVisible(t.Context(), a, false)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = ok
	}
	return out
}

func TestBlockFaceFollowsTheRulesOfSetBlock(t *testing.T) {
	ctx := t.Context()
	s, st, assets, faces := faceLibrary(t)
	if _, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, true); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("block before a PIN exists: %v", err)
	}
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BlockFace(ctx, 999, core.BlockPhotos, true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("block of a face that is gone: %v", err)
	}

	change, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, true)
	if err != nil || !change.Created || change.ID == 0 {
		t.Fatalf("new block = %+v, %v", change, err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{false, false, true}) {
		t.Fatalf("shown = %v, want the face and its look-alike hidden", got)
	}
	// Asked again, the same block comes back and is not new: no Undo for it.
	again, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, true)
	if err != nil || again.Created || again.ID != change.ID {
		t.Fatalf("second block = %+v, %v", again, err)
	}
	if err := s.UndoBlock(ctx, change.ID); !errors.Is(err, ErrBlockUndoExpired) {
		t.Fatalf("undo of a block asked for twice: %v", err)
	}
	// Locked, the mode of an existing block is not the app's to change.
	if _, err := s.BlockFace(ctx, faces[0], core.BlockLabel, true); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("mode change while locked: %v", err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{false, false, true}) {
		t.Fatalf("a refused change moved the block: %v", got)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if c, err := s.BlockFace(ctx, faces[0], core.BlockLabel, true); err != nil || c.Created || c.ID != change.ID {
		t.Fatalf("mode change unlocked = %+v, %v", c, err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{true, true, true}) {
		t.Fatalf("label mode left photos hidden: %v", got)
	}
}

func TestBlockFaceOffersUndoOnlyForANewBlock(t *testing.T) {
	ctx := t.Context()
	s, st, assets, faces := faceLibrary(t)
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	change, err := s.BlockFace(ctx, faces[2], core.BlockPhotos, false)
	if err != nil || !change.Created {
		t.Fatalf("new block = %+v, %v", change, err)
	}
	if err := s.UndoBlock(ctx, change.ID); err != nil {
		t.Fatalf("undo of a new block: %v", err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{true, true, true}) {
		t.Fatalf("shown after undo = %v", got)
	}
}

func TestBlockFaceOfOneFaceLeavesItsLookalikesAndWideningNeedsNoPIN(t *testing.T) {
	ctx := t.Context()
	s, st, assets, faces := faceLibrary(t)
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, false); err != nil {
		t.Fatal(err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{false, true, true}) {
		t.Fatalf("shown = %v, want only the face's own photo hidden", got)
	}
	if _, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, true); err != nil {
		t.Fatalf("widening while locked: %v", err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{false, false, true}) {
		t.Fatalf("shown = %v, want the look-alike hidden too", got)
	}
}

func TestBlockFaceNarrowsABlockToTheFaceAloneOnlyWhileUnlocked(t *testing.T) {
	ctx := t.Context()
	s, st, assets, faces := faceLibrary(t)
	if err := s.CreateBlockPIN(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, false); !errors.Is(err, ErrBlockLocked) {
		t.Fatalf("narrowing while locked: %v", err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{false, false, true}) {
		t.Fatalf("a refused narrowing changed what is shown: %v", got)
	}
	if err := s.UnlockBlocks(ctx, "123456"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BlockFace(ctx, faces[0], core.BlockPhotos, false); err != nil {
		t.Fatalf("narrowing unlocked: %v", err)
	}
	if got := shownAssets(t, st, assets); !slices.Equal(got, []bool{false, true, true}) {
		t.Fatalf("shown after narrowing = %v", got)
	}
}
