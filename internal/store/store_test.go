package store

import (
	"context"
	"errors"
	"io/fs"
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
	names, _ := fs.Glob(migrationFS, "migrations/*.sql")
	latest := len(names) // migrations are numbered 1..n
	path := filepath.Join(t.TempDir(), "library.db")
	for range 2 { // the second open must find nothing to migrate
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var v int
		err = st.r.QueryRow("PRAGMA user_version").Scan(&v)
		st.Close()
		if err != nil || v != latest {
			t.Fatalf("user_version = %d (%v), want %d", v, err, latest)
		}
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

	if n, err := st.CountPending(ctx, core.StageEmbedding, core.StageFaces); err != nil || n != 1 {
		t.Fatalf("photos waiting for the AI = %d (%v), want 1", n, err)
	}
	// Thumbnails wait for metadata (it supplies the orientation).
	if jobs, _ := st.PendingJobs(ctx, core.StageThumbnail, nil, 10); len(jobs) != 0 {
		t.Fatalf("thumbnail jobs before metadata: %d", len(jobs))
	}
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("metadata jobs = %d (%v)", len(jobs), err)
	}
	id := jobs[0].AssetID
	if err := st.SaveMetadata(ctx, "m1", []MetadataResult{{AssetID: id, Width: 4000, Height: 3000, Orientation: 6}}); err != nil {
		t.Fatal(err)
	}
	jobs, _ = st.PendingJobs(ctx, core.StageThumbnail, nil, 10)
	if len(jobs) != 1 || jobs[0].Orientation != 6 {
		t.Fatalf("thumbnail jobs = %+v", jobs)
	}
	// The AI stages wait for the thumbnail (and so for the orientation).
	for _, stage := range []core.Stage{core.StageEmbedding, core.StageFaces} {
		if jobs, _ := st.PendingJobs(ctx, stage, nil, 10); len(jobs) != 0 {
			t.Fatalf("%s jobs before thumbnails: %d", stage, len(jobs))
		}
	}
	if err := st.SaveThumbnails(ctx, "t1", []ThumbResult{{AssetID: id, Skipped: true}}); err != nil {
		t.Fatal(err)
	}
	if _, _, skipped, _ := st.JobCounts(ctx); skipped != 1 {
		t.Fatalf("skipped = %d, want 1", skipped)
	}
	for _, stage := range []core.Stage{core.StageEmbedding, core.StageFaces} {
		if jobs, _ := st.PendingJobs(ctx, stage, nil, 10); len(jobs) != 1 || jobs[0].Orientation != 6 {
			t.Fatalf("%s jobs after thumbnails: %+v, want 1 with orientation 6", stage, jobs)
		}
	}

	// Jobs of an asset whose file is gone neither run nor count as waiting.
	st.RemoveFiles(ctx, []int64{1})
	if pending, _, skipped, _ := st.JobCounts(ctx); pending[core.StageEmbedding] != 0 || skipped != 0 {
		t.Fatalf("missing asset still counted: %v, %d skipped", pending, skipped)
	}
	if jobs, _ := st.PendingJobs(ctx, core.StageEmbedding, nil, 10); len(jobs) != 0 {
		t.Fatalf("missing asset's job offered: %+v", jobs)
	}

	// Current versions: nothing to redo. A new version of one stage: redo it alone.
	versions := map[core.Stage]string{core.StageMetadata: "m1", core.StageThumbnail: "t1"}
	if n, _ := st.RequeueOutdated(ctx, versions); n != 0 {
		t.Fatalf("requeued %d jobs, want 0", n)
	}
	versions[core.StageThumbnail] = "t2"
	if n, _ := st.RequeueOutdated(ctx, versions); n != 1 {
		t.Fatalf("requeued %d jobs, want 1", n)
	}
}

func TestEmbeddingsAndTags(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, _ := st.AddSource(ctx, `C:\Photos`)
	st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})
	jobs, _ := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	a, b := jobs[0].AssetID, jobs[1].AssetID

	if err := st.SyncVocabulary(ctx, []VocabLabel{{Name: "Beaches", Prompts: []string{"a beach"}}, {Name: "Dogs", Prompts: []string{"a dog"}}}); err != nil {
		t.Fatal(err)
	}
	auto, err := st.AutoTags(ctx)
	if err != nil || len(auto) != 2 || auto[0].Model != "" {
		t.Fatalf("auto tags = %+v (%v)", auto, err)
	}
	beach, dogs := auto[0].ID, auto[1].ID
	if err := st.SetTagVectors(ctx, beach, "m", []float32{1, 0}); err != nil {
		t.Fatal(err)
	}
	if auto, _ = st.AutoTags(ctx); auto[0].Model != "m" || len(auto[0].Vecs) != 2 || auto[0].Vecs[0] != 1 {
		t.Fatalf("vectors not stored: %+v", auto[0])
	}

	err = st.SaveEmbeddings(ctx, "m", []EmbedResult{
		{AssetID: a, Vec: []int8{127, -5}, Scale: 0.01, Tags: []TagScore{{beach, 0.4}, {dogs, 0.01}}},
		{AssetID: b, Skipped: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []int8
	st.Embeddings(ctx, "m", func(id int64, v []int8, scale float32) {
		if id == a && scale == 0.01 {
			got = append(got, v...)
		}
	})
	if len(got) != 2 || got[0] != 127 || got[1] != -5 {
		t.Fatalf("embedding round trip: %v", got)
	}

	// The confidence threshold decides what counts, without re-scoring.
	tags, _ := st.AssetTags(ctx, a, 0.02)
	if len(tags) != 1 || tags[0].ID != beach {
		t.Fatalf("tags at 0.02 = %+v", tags)
	}
	if tags, _ = st.AssetTags(ctx, a, 0.005); len(tags) != 2 {
		t.Fatalf("tags at 0.005 = %+v", tags)
	}
	buckets, _ := st.DayBuckets(ctx, core.View{Tag: beach}, 0.02)
	if len(buckets) != 1 || buckets[0].Count != 1 {
		t.Fatalf("beach view = %+v", buckets)
	}
	things, _ := st.Things(ctx, 0.02)
	if len(things) != 2 || things[0].ID != beach || things[0].Count != 1 || things[0].Cover != 0 {
		t.Fatalf("things = %+v (no thumbnail yet, so no cover)", things)
	}

	// The user's word is final: a removed AI tag stays removed when the AI re-runs.
	if err := st.UntagAsset(ctx, a, beach); err != nil {
		t.Fatal(err)
	}
	st.SaveEmbeddings(ctx, "m", []EmbedResult{{AssetID: a, Vec: []int8{1, 1}, Scale: 1, Tags: []TagScore{{beach, 0.9}}}})
	st.SetTagScores(ctx, beach, []AssetScore{{a, 0.9}, {b, 0.5}})
	if tags, _ = st.AssetTags(ctx, a, 0.002); len(tags) != 0 {
		t.Fatalf("rejected tag came back: %+v", tags)
	}
	if tags, _ = st.AssetTags(ctx, b, 0.002); len(tags) != 1 {
		t.Fatalf("re-scored tag missing on b: %+v", tags)
	}

	// Manual tags: created by name, applied by the user, removed outright.
	id, err := st.CreateTag(ctx, "Ali's birthday", core.TagManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateTag(ctx, "ali's BIRTHDAY", core.TagManual, nil); !errors.Is(err, ErrTagExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	st.TagAsset(ctx, b, id)
	if tags, _ = st.AssetTags(ctx, b, 0.99); len(tags) != 1 || tags[0].Source != core.TagByUser {
		t.Fatalf("user tag = %+v", tags)
	}
	st.UntagAsset(ctx, b, id)
	var rows int
	st.r.QueryRow("SELECT count(*) FROM asset_tags WHERE tag_id = ?", id).Scan(&rows)
	if rows != 0 {
		t.Fatalf("manual tag removal left %d rows", rows)
	}
	if err := st.DeleteTag(ctx, beach); err != ErrBuiltinTag {
		t.Fatalf("deleting a built-in: %v", err)
	}
	if found, _ := st.SearchTags(ctx, "BIRTH", 5); len(found) != 1 || found[0].ID != id {
		t.Fatalf("search tags = %+v", found)
	}

	// Vocabulary changes: dropped built-ins go; one the user relied on stays as theirs.
	st.TagAsset(ctx, b, dogs)
	if err := st.SyncVocabulary(ctx, []VocabLabel{{Name: "Beaches", Prompts: []string{"a beach", "the seaside"}}}); err != nil {
		t.Fatal(err)
	}
	auto, _ = st.AutoTags(ctx)
	if len(auto) != 1 || auto[0].Model != "" {
		t.Fatalf("after sync: %+v (changed prompts must drop old vectors)", auto)
	}
	if _, kind, err := st.TagByName(ctx, "dogs"); err != nil || kind != core.TagManual {
		t.Fatalf("dogs = %v %v, want kept as manual", kind, err)
	}

	// A tag deleted after the AI scored it is skipped, not a failed batch.
	label, _ := st.CreateTag(ctx, "Kites", core.TagLabel, []string{"a kite"})
	if err := st.DeleteTag(ctx, label); err != nil {
		t.Fatal(err)
	}
	err = st.SaveEmbeddings(ctx, "m", []EmbedResult{{AssetID: b, Vec: []int8{1, 1}, Scale: 1,
		Tags: []TagScore{{label, 0.9}, {auto[0].ID, 0.9}}}})
	if err != nil {
		t.Fatalf("save with a deleted tag: %v", err)
	}
	// b keeps the user's Dogs and gains Beaches; the deleted Kites is gone.
	if tags, _ = st.AssetTags(ctx, b, 0.5); len(tags) != 2 || tags[1].ID != auto[0].ID {
		t.Fatalf("tags after save = %+v", tags)
	}
}

func TestEmptyListsAreNotNil(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	srcs, err := st.ListSources(ctx)
	if err != nil || srcs == nil {
		t.Fatalf("sources = %#v (%v), want an empty list", srcs, err)
	}
	if tags, _ := st.SearchTags(ctx, " ", 5); tags == nil {
		t.Fatal("tag search = nil, want an empty list")
	}
}

func TestIntSettingReadsOlderFloat(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	st.SetFloat(ctx, core.SettingVocabulary, 3) // how earlier versions stored it
	if v, err := st.Int(ctx, core.SettingVocabulary, 0); err != nil || v != 3 {
		t.Fatalf("int = %d (%v), want 3", v, err)
	}
}

func TestGridItemsKeepOrder(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, _ := st.AddSource(ctx, `C:\Photos`)
	st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})
	jobs, _ := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	a, b := jobs[0].AssetID, jobs[1].AssetID
	items, err := st.GridItems(ctx, []int64{b, 999, a})
	if err != nil || len(items) != 2 || items[0].ID != b || items[1].ID != a {
		t.Fatalf("items = %+v (%v), want b then a", items, err)
	}
}

func TestFloatSetting(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	if v, _ := st.Float(ctx, core.SettingTagConfidence, 0.02); v != 0.02 {
		t.Fatalf("default = %v", v)
	}
	st.SetFloat(ctx, core.SettingTagConfidence, 0.125)
	if v, _ := st.Float(ctx, core.SettingTagConfidence, 0.02); v != 0.125 {
		t.Fatalf("stored = %v", v)
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
