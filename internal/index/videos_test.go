package index

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/media"
	"lumen/internal/people"
	"lumen/internal/store"
)

func TestVideoCancellationStopsSamplingAndLeavesJobsPending(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	src, err := st.AddSource(ctx, `C:\Videos`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{
		{SourceID: src.ID, Path: `C:\Videos\one.mp4`, Hash: []byte("one"), Kind: core.KindVideo, Ext: ".mp4", TakenAt: 100},
		{SourceID: src.ID, Path: `C:\Videos\two.mp4`, Hash: []byte("two"), Kind: core.KindVideo, Ext: ".mp4", TakenAt: 200},
	}); err != nil {
		t.Fatal(err)
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 2)
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs = %v, %v", jobs, err)
	}
	ix := New(st, paths, nil, Models{People: people.NewEngine(st, ai.FaceModel{})}, &fakeEvents{})
	ix.faces = &model[*ai.Faces]{
		open:   func() (*ai.Faces, error) { return &ai.Faces{}, nil },
		close:  func(*ai.Faces) {},
		report: func(*ai.Faces, bool, error) {},
	}
	defer ix.faces.release()
	passes := 0
	closed := false
	sample := func(ctx context.Context, _ string, _, _ int, consume func(media.Frame) error) error {
		passes++
		defer func() { closed = true }()
		cancel()
		if err := consume(media.Frame{}); !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled frame callback = %v", err)
		}
		return ctx.Err()
	}
	rs, err := ix.processVideos(ctx, jobs, sample)
	if !errors.Is(err, context.Canceled) || len(rs) != 0 || passes != 1 || !closed {
		t.Fatalf("cancelled batch = %+v, %v; passes = %d, closed = %v", rs, err, passes, closed)
	}
	if ix.videoFaceRetry.retry != 0 {
		t.Fatal("cancellation was counted as a model failure")
	}
	if n, err := st.CountPending(t.Context(), core.StageFaces); err != nil || n != 2 {
		t.Fatalf("pending face jobs = %d, %v; want 2", n, err)
	}
}
