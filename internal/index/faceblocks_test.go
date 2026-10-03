package index

import (
	"slices"
	"testing"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/people"
	"lumen/internal/store"
)

// A face model upgrade re-runs the faces stage over the library, newest
// photos first; the photos holding a blocked face's anchor go before them.
func TestFaceModelUpgradeTakesFaceBlockAnchorPhotosFirst(t *testing.T) {
	ctx := t.Context()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	m := ai.FaceModel{Version: "sface-test", DetectSize: 640}
	ix := quiet(New(st, paths, nil, Models{People: people.NewEngine(st, m)}, &fakeEvents{}))

	// Five photos, the oldest holding the face that gets blocked.
	var ids []int64
	for i := range 5 {
		path := `C:\Photos\` + string(rune('a'+i)) + `.jpg`
		if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: path, Hash: []byte(path), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: int64(100 + i)}}); err != nil {
			t.Fatal(err)
		}
		jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
		if err != nil || len(jobs) != 1 {
			t.Fatalf("metadata = %v, %v", jobs, err)
		}
		id := jobs[0].AssetID
		ids = append(ids, id)
		if err := st.SaveMetadata(ctx, "m", []store.MetadataResult{{AssetID: id, Width: 1000, Height: 800, Orientation: 1, TakenAt: int64(100 + i)}}); err != nil {
			t.Fatal(err)
		}
		if err := st.SaveThumbnails(ctx, "t", []store.ThumbResult{{AssetID: id}}); err != nil {
			t.Fatal(err)
		}
		face := store.DetectedFace{X: 0.1, Y: 0.1, W: 0.3, H: 0.3, Score: 0.9, Vec: []float32{1, 0}}
		if err := st.SaveFaces(ctx, m.Version, "old", []store.FaceResult{{AssetID: id, Faces: []store.DetectedFace{face}}}); err != nil {
			t.Fatal(err)
		}
	}
	var face int64
	faces, err := st.AssetFaces(ctx, ids[0])
	if err != nil || len(faces) != 1 {
		t.Fatalf("faces = %v, %v", faces, err)
	}
	face = faces[0].ID
	if _, _, err := st.SetFaceBlock(ctx, face, core.BlockLabel, false, true); err != nil {
		t.Fatal(err)
	}

	order := func() []int64 {
		t.Helper()
		items, err := ix.fetch(ctx, core.StageFaces, 3)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]int64, len(items))
		for i, j := range items {
			got[i] = j.AssetID
		}
		return got
	}
	if got := order(); len(got) != 0 {
		t.Fatalf("photos fetched before the upgrade: %v", got)
	}
	if _, err := st.RequeueVersions(ctx, map[core.Stage]store.StageVersion{core.StageFaces: ix.version[core.StageFaces]}); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []int64{ids[0]}) {
		t.Fatalf("first fetch = %v, want only the anchor's photo %d", got, ids[0])
	}
	// Once it is done the rest follow, newest first.
	if err := st.SaveFaces(ctx, m.Version, ix.version[core.StageFaces].Photo, []store.FaceResult{{AssetID: ids[0], Unchanged: true}}); err != nil {
		t.Fatal(err)
	}
	if got := order(); !slices.Equal(got, []int64{ids[4], ids[3], ids[2]}) {
		t.Fatalf("second fetch = %v, want the newest photos", got)
	}
}
