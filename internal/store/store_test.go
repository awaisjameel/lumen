package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestIsWithin(t *testing.T) {
	cases := []struct {
		path, dir string
		want      bool
	}{
		{`C:\Photos`, `C:\Photos`, true},
		{`C:\Photos\2024\a.jpg`, `C:\Photos`, true},
		{`c:\photos\2024`, `C:\Photos\`, true},
		{`C:\Photos2`, `C:\Photos`, false},
		{`C:\Pho`, `C:\Photos`, false},
		{`D:\Photos`, `C:\Photos`, false},
	}
	for _, c := range cases {
		if got := IsWithin(c.path, c.dir); got != c.want {
			t.Errorf("IsWithin(%q, %q) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}

func TestReopenKeepsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	for range 2 { // the second open must find nothing to migrate
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var v int
		if err := st.r.QueryRow("PRAGMA user_version").Scan(&v); err != nil || v != 2 {
			t.Fatalf("user_version = %d (%v), want 2", v, err)
		}
		st.Close()
	}
}

func TestAddSourceNeverOverlaps(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	child, err := st.AddSource(ctx, `C:\Photos\2024`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []IngestItem{item(child.ID, `C:\Photos\2024\a.jpg`, "a")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSource(ctx, `c:\photos\2024\trip`); !errors.Is(err, ErrAlreadyCovered) {
		t.Fatalf("nested folder: err = %v, want ErrAlreadyCovered", err)
	}

	// Adding the parent absorbs the child, and its files move with it.
	parent, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	srcs, _ := st.ListSources(ctx)
	if len(srcs) != 1 || srcs[0].ID != parent.ID {
		t.Fatalf("sources = %+v, want only the parent", srcs)
	}
	if files, _ := st.KnownFiles(ctx, parent.ID, ""); len(files) != 1 {
		t.Fatalf("parent has %d files, want 1", len(files))
	}
}

func TestIngestJoinsDuplicatesAndKeepsMovedAssets(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, _ := st.AddSource(ctx, `C:\Photos`)

	n, err := st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\a.jpg`, "same"),
		item(src.ID, `C:\Photos\copy of a.jpg`, "same"),
		item(src.ID, `C:\Photos\b.jpg`, "other"),
	})
	if err != nil || n != 2 {
		t.Fatalf("created %d assets (%v), want 2", n, err)
	}
	if v, _ := st.CountVisible(ctx); v != 2 {
		t.Fatalf("visible = %d, want 2", v)
	}

	// b.jpg is deleted: its asset leaves the grid but is kept.
	files, _ := st.KnownFiles(ctx, src.ID, "")
	if err := st.RemoveFiles(ctx, []int64{files[`c:\photos\b.jpg`].ID}); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.CountVisible(ctx); v != 1 {
		t.Fatalf("visible after delete = %d, want 1", v)
	}
	// ...and comes back as the same asset when the content reappears elsewhere.
	if n, err := st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\moved\b.jpg`, "other")}); err != nil || n != 0 {
		t.Fatalf("re-ingest created %d assets (%v), want 0", n, err)
	}
	if v, _ := st.CountVisible(ctx); v != 2 {
		t.Fatalf("visible after move = %d, want 2", v)
	}

	pending, failed, skipped, err := st.JobCounts(ctx)
	if err != nil || pending[core.StageMetadata] != 2 || pending[core.StageThumbnail] != 2 || failed != 0 || skipped != 0 {
		t.Fatalf("job counts = %v, %d, %d (%v)", pending, failed, skipped, err)
	}
}

func TestJobsFlowAndRequeue(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, _ := st.AddSource(ctx, `C:\Photos`)
	st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.heic`, "a")})

	// Thumbnails wait for metadata (it supplies the orientation).
	if jobs, _ := st.PendingJobs(ctx, core.StageThumbnail, nil, 10); len(jobs) != 0 {
		t.Fatalf("thumbnail jobs before metadata: %d", len(jobs))
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("metadata jobs = %d (%v)", len(jobs), err)
	}
	id := jobs[0].AssetID
	if err := st.SaveMetadata(ctx, []MetadataResult{{AssetID: id, Width: 4000, Height: 3000, Orientation: 6}}); err != nil {
		t.Fatal(err)
	}
	jobs, _ = st.PendingJobs(ctx, core.StageThumbnail, nil, 10)
	if len(jobs) != 1 || jobs[0].Orientation != 6 {
		t.Fatalf("thumbnail jobs = %+v", jobs)
	}
	if err := st.SaveThumbnails(ctx, []ThumbResult{{AssetID: id, Skipped: true}}); err != nil {
		t.Fatal(err)
	}
	if _, _, skipped, _ := st.JobCounts(ctx); skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}

	// Current versions: nothing to redo. An older recorded version: redo.
	if n, _ := st.RequeueOutdated(ctx); n != 0 {
		t.Fatalf("requeued %d jobs, want 0", n)
	}
	st.w.Exec("UPDATE asset_jobs SET model_version = 'old' WHERE stage = ?", core.StageThumbnail)
	if n, _ := st.RequeueOutdated(ctx); n != 1 {
		t.Fatalf("requeued %d jobs, want 1", n)
	}
}

func TestBoolSetting(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	if v, err := st.Bool(ctx, core.SettingVideos, true); err != nil || !v {
		t.Fatalf("default = %v (%v), want true", v, err)
	}
	if err := st.SetBool(ctx, core.SettingVideos, false); err != nil {
		t.Fatal(err)
	}
	if v, _ := st.Bool(ctx, core.SettingVideos, true); v {
		t.Fatal("stored false, read true")
	}
}

func item(sourceID int64, path, content string) IngestItem {
	return IngestItem{
		SourceID: sourceID, Path: path, Size: int64(len(content)), MTime: 1,
		Hash: []byte(content), Kind: core.KindPhoto, Ext: filepath.Ext(path),
		TakenAt: 1_700_000_000, DateFrom: core.DateFromFile,
	}
}
