package index

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
	"lumen/internal/xmp"
)

func dirtyPass(t *testing.T, f *xmpFixture) core.XMPExport {
	t.Helper()
	res, err := f.ix.exportXMP(t.Context(), xmpOptions{dirty: true, batch: 1})
	check(t, err)
	pending, err := f.st.XMPPending(t.Context())
	check(t, err)
	if pending {
		t.Fatal("a completed dirty pass left work unacknowledged")
	}
	return res
}

func TestDirtyExportVisitsOnlyThePhotoWhoseTagsChanged(t *testing.T) {
	f := newXMPFixture(t)
	dirtyPass(t, f)
	check(t, f.st.TagAsset(t.Context(), f.b, f.holiday))
	if got := dirtyPass(t, f); got != (core.XMPExport{Written: 1}) {
		t.Fatalf("tag change visited other photos: %+v", got)
	}
	if got := dirtyPass(t, f); got != (core.XMPExport{}) {
		t.Fatalf("completed work ran again: %+v", got)
	}
	if !strings.Contains(readFile(t, f.sidecarB), "Holiday") {
		t.Fatal("the changed photo did not gain its tag")
	}
}

func TestDirtyExportRetainsAChangeCommittedDuringTheWrite(t *testing.T) {
	f := newXMPFixture(t)
	dirtyPass(t, f)
	ctx := t.Context()
	check(t, f.st.SetFavourite(ctx, f.a, true))
	write := func(ctx context.Context, path string, data, wrote []byte) (fileops.Sidecar, error) {
		out, err := fileops.WriteSidecar(ctx, path, data, wrote)
		check(t, f.st.TagAsset(ctx, f.a, f.dogs))
		return out, err
	}
	_, err := f.ix.exportXMP(ctx, xmpOptions{dirty: true, write: write})
	check(t, err)
	batch, err := f.st.XMPDirtyAssets(ctx, 0, 100)
	check(t, err)
	if len(batch.Pending) != 1 || batch.Pending[0].AssetID != f.a {
		t.Fatalf("a newer change was cleared: %+v", batch.Pending)
	}
	if got := dirtyPass(t, f); got.Written != 1 || !strings.Contains(readFile(t, f.sidecarA), "Dogs") {
		t.Fatalf("the retained change was not exported: %+v", got)
	}
}

func TestDirtyExportRecoversACrashBetweenWritingAndAcknowledging(t *testing.T) {
	f := newXMPFixture(t)
	ctx := t.Context()
	// Losing the pass after the file write leaves both ownership and work uncommitted.
	crash := "export interrupted"
	write := func(ctx context.Context, path string, data, wrote []byte) (fileops.Sidecar, error) {
		_, err := fileops.WriteSidecar(ctx, path, data, wrote)
		check(t, err)
		panic(crash)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != crash {
				t.Fatalf("interruption = %v; want %q", recovered, crash)
			}
		}()
		_, _ = f.ix.exportXMP(ctx, xmpOptions{dirty: true, write: write})
	}()
	if !fileExists(f.sidecarA) || len(f.recordedHash(t, f.pathA)) != 0 {
		t.Fatal("the interruption did not fall between writing and recording")
	}
	pending, err := f.st.XMPPending(ctx)
	check(t, err)
	if !pending {
		t.Fatal("the interruption lost pending work")
	}
	f.ix = quiet(New(f.st, core.PathsIn(t.TempDir()), nil, Models{}, &fakeEvents{}))
	got := dirtyPass(t, f)
	if got != (core.XMPExport{Written: 1, Unchanged: 1}) {
		t.Fatalf("recovery = %+v; want the first file reclaimed and the second created", got)
	}
	if hash := f.recordedHash(t, f.pathA); !bytes.Equal(hash, fileops.SidecarHash([]byte(readFile(t, f.sidecarA)))) {
		t.Fatal("the recovered sidecar was not reclaimed")
	}
	pending, err = f.st.XMPPending(ctx)
	check(t, err)
	if pending {
		t.Fatal("recovery did not acknowledge its work")
	}
}

func TestDirtySidecarsEqualAFullPassAfterRandomChanges(t *testing.T) {
	for _, seed := range []uint64{7, 19, 83} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			f := newXMPFixture(t)
			ctx := t.Context()
			rng := rand.New(rand.NewPCG(seed, seed+1))
			dirtyPass(t, f)
			ids := []int64{f.a, f.b, f.c}
			tags := []int64{f.beaches, f.dogs, f.holiday}
			paths := []string{f.sidecarA, f.sidecarB, f.sidecarC}
			var blocks []int64
			for step := range 120 {
				id, tag := ids[rng.IntN(len(ids))], tags[rng.IntN(len(tags))]
				switch rng.IntN(14) {
				case 0:
					check(t, f.st.TagAsset(ctx, id, tag))
				case 1:
					check(t, f.st.UntagAsset(ctx, id, tag))
				case 2:
					check(t, f.st.SetFavourite(ctx, id, rng.IntN(2) == 1))
				case 3:
					check(t, f.st.TrashAssets(ctx, []int64{id}))
				case 4:
					check(t, f.st.RestoreAssets(ctx, []int64{id}))
				case 5:
					check(t, f.st.NamePerson(ctx, f.ali, fmt.Sprintf("Person %d", step)))
				case 6:
					check(t, f.st.SetFloat(ctx, core.SettingTagConfidence, []float64{0.02, 0.3, 0.5}[rng.IntN(3)]))
				case 7:
					block, err := f.st.SetBlock(ctx, core.BlockAsset, id, "", core.BlockPhotos)
					check(t, err)
					if !slices.Contains(blocks, block) {
						blocks = append(blocks, block)
					}
				case 8:
					if len(blocks) > 0 {
						check(t, f.st.RemoveBlock(ctx, blocks[0]))
						blocks = blocks[1:]
					}
				case 9:
					check(t, f.st.RenameTag(ctx, f.holiday, fmt.Sprintf("Trip %d", step)))
				case 10:
					check(t, f.st.SaveMetadata(ctx, "v", []store.MetadataResult{{AssetID: id, Width: 100 + step, Height: 100, Orientation: 1}}))
				case 11:
					check(t, f.st.SetXMP(ctx, false))
					check(t, f.st.SetFavourite(ctx, id, true))
					check(t, f.st.SetXMP(ctx, true))
				case 12:
					check(t, f.st.SaveFaces(ctx, "f1", "f1", []store.FaceResult{{AssetID: f.b, Faces: []store.DetectedFace{{X: 0.1, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Vec: []float32{1, 0, 0, 0}}}}}))
					faces, err := f.st.AssetFaces(ctx, f.b)
					check(t, err)
					if len(faces) > 0 {
						other, err := f.st.NameFaces(ctx, []int64{faces[0].ID}, fmt.Sprintf("Guest %d", step))
						check(t, err)
						check(t, f.st.MergePeople(ctx, other, f.ali))
					}
				case 13:
					files, err := f.st.KnownFiles(ctx, 1, "")
					check(t, err)
					body := f.original[f.pathA]
					newPath := fmt.Sprintf("%s.%d.jpg", f.pathA, step)
					check(t, os.Rename(f.pathA, newPath))
					_, err = f.st.Ingest(ctx, []store.IngestItem{{SourceID: 1, Path: newPath, Size: int64(len(body)), MTime: originalMTime.UnixNano(), Hash: body, Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}})
					check(t, err)
					check(t, f.st.RemoveFiles(ctx, []int64{files[core.Fold(f.pathA)].ID}))
					f.original[newPath] = body
					f.pathA, f.sidecarA = newPath, newPath+".xmp"
					paths = append(paths, f.sidecarA)
				}
				if step%4 != 3 {
					continue
				}
				dirtyPass(t, f)
				before := make([]string, len(paths))
				for i, path := range paths {
					before[i] = tryRead(path)
				}
				if got := f.export(t); got.Written != 0 || got.Skipped != 0 {
					t.Fatalf("step %d: full pass corrected dirty output: %+v", step, got)
				}
				for i, path := range paths {
					if got := tryRead(path); got != before[i] {
						t.Fatalf("step %d: %s differs from the full pass", step, path)
					}
				}
			}
		})
	}
}

func TestDirtyExportClearsOnlyTheChangedAbandonedSidecar(t *testing.T) {
	f := newXMPFixture(t)
	dirtyPass(t, f)
	ctx := t.Context()
	unchanged := readFile(t, f.sidecarB)
	files, err := f.st.KnownFiles(ctx, 1, "")
	check(t, err)
	check(t, os.Rename(f.pathA, f.pathA+".moved"))
	check(t, f.st.RemoveFiles(ctx, []int64{files[core.Fold(f.pathA)].ID}))
	if got := dirtyPass(t, f); got != (core.XMPExport{Written: 1}) {
		t.Fatalf("abandoned sidecar pass visited other assets: %+v", got)
	}
	if got := readFile(t, f.sidecarA); got != string(xmp.Marshal(xmp.Doc{})) {
		t.Fatal("the abandoned sidecar kept content")
	}
	if got := readFile(t, f.sidecarB); got != unchanged {
		t.Fatal("an unrelated sidecar changed")
	}
}

func TestDirtyExportNoticesAnExternallyChangedSkippedSidecar(t *testing.T) {
	f := newXMPFixture(t)
	check(t, os.WriteFile(f.sidecarA, []byte("foreign"), 0o600))
	if got := dirtyPass(t, f); got != (core.XMPExport{Written: 1, Skipped: 1}) {
		t.Fatalf("initial pass = %+v", got)
	}
	if got := dirtyPass(t, f); got != (core.XMPExport{}) {
		t.Fatalf("unchanged skip was visited: %+v", got)
	}
	check(t, os.Remove(f.sidecarA))
	if got := dirtyPass(t, f); got != (core.XMPExport{Written: 1}) {
		t.Fatalf("deleted skipped file was not retried alone: %+v", got)
	}
}

func TestDirtyExportMatchesTheFullPassForContentAndFileInputs(t *testing.T) {
	for _, name := range []string{"tag", "confidence", "name", "merge", "face", "favourite", "trash", "label block", "photo block", "face block", "enable", "file rename"} {
		t.Run(name, func(t *testing.T) {
			f := newXMPFixture(t)
			ctx := t.Context()
			dirtyPass(t, f)
			paths := []string{f.sidecarA, f.sidecarB, f.sidecarC}
			switch name {
			case "tag":
				check(t, f.st.TagAsset(ctx, f.a, f.dogs))
			case "confidence":
				check(t, f.st.SetFloat(ctx, core.SettingTagConfidence, 0.5))
			case "name":
				check(t, f.st.NamePerson(ctx, f.ali, "Alison"))
			case "merge":
				other, err := f.st.NameFaces(ctx, []int64{f.aliFace}, "Sara")
				check(t, err)
				check(t, f.st.SaveFaces(ctx, "f1", "f1", []store.FaceResult{{AssetID: f.b, Faces: []store.DetectedFace{{X: 0.1, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Vec: []float32{1, 0, 0, 0}}}}}))
				faces, err := f.st.AssetFaces(ctx, f.b)
				check(t, err)
				into, err := f.st.NameFaces(ctx, []int64{faces[0].ID}, "Ali")
				check(t, err)
				check(t, f.st.MergePeople(ctx, other, into))
			case "face":
				check(t, f.st.RejectFaces(ctx, f.ali, []int64{f.aliFace}))
			case "favourite":
				check(t, f.st.SetFavourite(ctx, f.a, true))
			case "trash":
				check(t, f.st.TrashAssets(ctx, []int64{f.a}))
			case "label block":
				_, err := f.st.SetBlock(ctx, core.BlockTag, f.holiday, "", core.BlockLabel)
				check(t, err)
			case "photo block":
				_, err := f.st.SetBlock(ctx, core.BlockAsset, f.a, "", core.BlockPhotos)
				check(t, err)
			case "face block":
				_, _, err := f.st.SetFaceBlock(ctx, f.aliFace, core.BlockLabel, false, true)
				check(t, err)
			case "enable":
				check(t, f.st.SetXMP(ctx, false))
				check(t, f.st.SetFavourite(ctx, f.c, true))
				check(t, f.st.SetXMP(ctx, true))
			case "file rename":
				files, err := f.st.KnownFiles(ctx, 1, "")
				check(t, err)
				newPath := f.pathA + ".renamed.jpg"
				check(t, os.Rename(f.pathA, newPath))
				_, err = f.st.Ingest(ctx, []store.IngestItem{{SourceID: 1, Path: newPath, Size: int64(len(f.original[f.pathA])), MTime: originalMTime.UnixNano(), Hash: f.original[f.pathA], Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1_700_000_000, DateFrom: core.DateFromFile}})
				check(t, err)
				check(t, f.st.RemoveFiles(ctx, []int64{files[core.Fold(f.pathA)].ID}))
				paths = append(paths, newPath+".xmp")
			}
			dirtyPass(t, f)
			before := make([]string, len(paths))
			for i, path := range paths {
				before[i] = tryRead(path)
			}
			if got := f.export(t); got.Written != 0 || got.Skipped != 0 {
				t.Fatalf("full pass corrected dirty output: %+v", got)
			}
			for i, path := range paths {
				if got := tryRead(path); got != before[i] {
					t.Fatalf("%s differs from a full pass", path)
				}
			}
		})
	}
}
