package store

import (
	"context"
	"testing"

	"lumen/internal/core"
)

func TestVideoMetadataFramesAndSearchPersistence(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Videos`)
	if err != nil {
		t.Fatal(err)
	}
	in := item(src.ID, `C:\Videos\clip.avi`, "video-content")
	in.Kind = core.KindVideo
	if _, err := st.Ingest(ctx, []IngestItem{in}); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %+v, %v", jobs, err)
	}
	id := jobs[0].AssetID
	if err := st.SaveMetadata(ctx, "video", []MetadataResult{{AssetID: id, Width: 640, Height: 360,
		Duration: 12_000, VideoCodec: "mpeg4", AudioCodec: "mp3"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveThumbnails(ctx, "video", []ThumbResult{{AssetID: id}}); err != nil {
		t.Fatal(err)
	}
	detail, err := st.AssetDetail(ctx, id)
	if err != nil || detail.Video == nil || detail.Video.Duration != 12_000 || detail.Video.Play != core.PlayConvert {
		t.Fatalf("details: %+v, %v", detail, err)
	}
	grid, err := st.GridItems(ctx, []int64{id})
	if err != nil || len(grid) != 1 || !grid[0].Video || grid[0].Duration != 12_000 || grid[0].Thumb == 0 {
		t.Fatalf("grid: %+v, %v", grid, err)
	}
	frames := []FrameEmbedding{{At: 1000, Vec: []int8{1, 2}, Scale: 0.01}, {At: 5000, Vec: []int8{3, 4}, Scale: 0.02}}
	if err := st.SaveEmbeddings(ctx, "m", "m", []EmbedResult{{AssetID: id, Vec: []int8{2, 3}, Scale: 0.01, Frames: frames}}); err != nil {
		t.Fatal(err)
	}
	var got []int64
	if err := st.VideoEmbeddings(ctx, "m", func(asset, at int64, _ []int8, _ float32) {
		if asset == id {
			got = append(got, at)
		}
	}); err != nil || len(got) != 2 || got[0] != 1000 || got[1] != 5000 {
		t.Fatalf("frame times: %v, %v", got, err)
	}
	if err := st.SaveEmbeddings(ctx, "m", "m", []EmbedResult{{AssetID: id, Vec: []int8{2, 3}, Scale: 0.01,
		Frames: frames[:1]}}); err != nil {
		t.Fatal(err)
	}
	got = nil
	_ = st.VideoEmbeddings(ctx, "m", func(_ int64, at int64, _ []int8, _ float32) { got = append(got, at) })
	if len(got) != 1 || got[0] != 1000 {
		t.Fatalf("stale frame survived re-index: %v", got)
	}
	f1, f2 := face(0.2, 1), face(0.2, 1)
	f1.Frame, f2.Frame = 1000, 5000
	if err := st.SaveFaces(ctx, "faces", "faces", []FaceResult{{AssetID: id, Faces: []DetectedFace{f1, f2}}}); err != nil {
		t.Fatal(err)
	}
	before, err := st.AssetFaces(ctx, id)
	if err != nil || len(before) != 2 || !before[0].Video || before[0].Frame != 1000 {
		t.Fatalf("video faces: %+v, %v", before, err)
	}
	f1.Frame = 1100
	if err := st.SaveFaces(ctx, "faces", "faces", []FaceResult{{AssetID: id, Faces: []DetectedFace{f1, f2}}}); err != nil {
		t.Fatal(err)
	}
	after, err := st.AssetFaces(ctx, id)
	if err != nil || len(after) != 2 || after[0].ID != before[0].ID || after[1].ID != before[1].ID {
		t.Fatalf("faces lost identities on re-index: %+v, %v", after, err)
	}
}
