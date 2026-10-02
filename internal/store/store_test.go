package store

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"

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

func TestAssetDetailReadsPathsFromItsMetadataSnapshot(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	before := item(src.ID, `C:\Photos\before.jpg`, "photo")
	check(t, errOf(st.Ingest(ctx, []IngestItem{before})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	if len(jobs) != 1 {
		t.Fatal("missing fixture job")
	}
	id := jobs[0].AssetID
	_, err = st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\privé`, core.BlockPhotos)
	check(t, err)
	// The Unicode block calls fold during the metadata read. Commit a rename
	// and date change there, so the later paths read must keep that snapshot.
	st.r.SetMaxOpenConns(1)
	conn, err := st.r.Conn(ctx)
	check(t, err)
	defer func() { _ = conn.Close() }() // also release the connection if fixture setup fails
	var changed sync.Once
	var changeErr error
	check(t, conn.Raw(func(raw any) error {
		sqlite, ok := raw.(*sqlite3.SQLiteConn)
		if !ok {
			return errors.New("unexpected SQLite connection")
		}
		return sqlite.RegisterFunc("fold", func(v any) (any, error) {
			changed.Do(func() {
				changeErr = st.tx(ctx, func(tx *sql.Tx) error {
					if _, err := tx.ExecContext(ctx, "UPDATE assets SET taken_at = ? WHERE id = ?", before.TakenAt+1, id); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, "UPDATE files SET path = ? WHERE asset_id = ?", `C:\Photos\after.jpg`, id)
					return err
				})
			})
			return foldSQL(v), changeErr
		}, false)
	}))
	check(t, conn.Close())
	detail, err := st.AssetDetail(ctx, id)
	check(t, err)
	check(t, changeErr)
	if detail.TakenAt != before.TakenAt || len(detail.Paths) != 1 || detail.Paths[0] != before.Path {
		t.Fatalf("mixed metadata and paths from different snapshots: %+v", detail)
	}
	var after string
	check(t, st.r.QueryRowContext(ctx, "SELECT path FROM files WHERE asset_id = ?", id).Scan(&after))
	if after != `C:\Photos\after.jpg` {
		t.Fatalf("concurrent rename did not commit: %q", after)
	}
}

func TestClusterDataReadsCorrectionsFromItsFacesSnapshot(t *testing.T) {
	st, ctx, asset, _, _ := peopleFixture(t)
	face := facesOf(t, st, asset)[0].ID
	person, err := st.NameFaces(ctx, []int64{face}, "Alice")
	check(t, err)
	st.r.SetMaxOpenConns(1)
	conn, err := st.r.Conn(ctx)
	check(t, err)
	defer func() { _ = conn.Close() }() // also release the connection if fixture setup fails
	var changed sync.Once
	var changeErr error
	check(t, conn.Raw(func(raw any) error {
		sqlite, ok := raw.(*sqlite3.SQLiteConn)
		if !ok {
			return errors.New("unexpected SQLite connection")
		}
		return sqlite.RegisterFunc("read_face", func(id int64) (int64, error) {
			changed.Do(func() { changeErr = st.RejectFaces(ctx, person, []int64{face}) })
			return id, changeErr
		}, false)
	}))
	// Commit a correction while faces are read. The later people and rejection
	// reads must describe that same snapshot, before the correction.
	check(t, errOf(conn.ExecContext(ctx, `PRAGMA query_only = OFF;
		CREATE TEMP VIEW faces AS SELECT read_face(id) AS id, asset_id, at_ms, person_id, assigned_by, sharpness, yaw, vec, model FROM main.faces;
		PRAGMA query_only = ON;`)))
	check(t, conn.Close())
	faces, people, rejections, err := st.ClusterData(ctx, "f1")
	check(t, err)
	check(t, changeErr)
	if len(people) != 1 || people[0].ID != person || len(rejections) != 0 {
		t.Fatalf("corrections did not describe the faces snapshot: people %+v, rejections %+v", people, rejections)
	}
	found := false
	for _, f := range faces {
		if f.ID == face {
			found = f.Person == person
		}
	}
	if !found {
		t.Fatalf("snapshot lost the pre-correction face assignment: %+v", faces)
	}
	var stored int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM face_rejections WHERE face_id = ? AND person_id = ?", face, person).Scan(&stored))
	if stored != 1 {
		t.Fatal("concurrent correction did not commit")
	}
}

// latestMigration is the schema version a fully migrated store has: the
// highest migration number (a number reserved for later work leaves a gap).
func latestMigration(t *testing.T) int {
	t.Helper()
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	latest := 0
	for _, name := range names {
		n, err := strconv.Atoi(strings.SplitN(filepath.Base(name), "_", 2)[0])
		if err != nil {
			t.Fatal(err)
		}
		latest = max(latest, n)
	}
	return latest
}

func TestReopenKeepsSchemaVersion(t *testing.T) {
	latest := latestMigration(t)
	path := filepath.Join(t.TempDir(), "library.db")
	for range 2 { // the second open must find nothing to migrate
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var v int
		err = st.r.QueryRowContext(context.Background(), "PRAGMA user_version").Scan(&v)
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

func TestMarkOrphansHandlesDuplicateCandidatesAndRemainingCopies(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	other, err := st.AddSource(ctx, `D:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\gone.jpg`, "gone"),
		item(src.ID, `C:\Photos\copy.jpg`, "gone"),
		item(src.ID, `C:\Photos\kept.jpg`, "kept"),
		item(other.ID, `D:\Photos\kept.jpg`, "kept"),
	})))
	var gone, kept int64
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte("gone")).Scan(&gone))
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte("kept")).Scan(&kept))
	check(t, st.RemoveSource(ctx, src.ID))
	var missing sql.NullInt64
	check(t, st.r.QueryRowContext(ctx, "SELECT missing_since FROM assets WHERE id = ?", gone).Scan(&missing))
	if !missing.Valid {
		t.Fatal("asset whose two files were removed is still present")
	}
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		return markOrphans(ctx, tx, []int64{gone, gone, kept, kept}, missing.Int64+1)
	}))
	var again sql.NullInt64
	check(t, st.r.QueryRowContext(ctx, "SELECT missing_since FROM assets WHERE id = ?", gone).Scan(&again))
	if again != missing {
		t.Fatalf("duplicate candidates changed an existing missing time: %v, want %v", again, missing)
	}
	check(t, st.r.QueryRowContext(ctx, "SELECT missing_since FROM assets WHERE id = ?", kept).Scan(&again))
	if again.Valid {
		t.Fatal("asset with a copy in another source was marked missing")
	}
}

func TestMetadataWithoutCaptureDateRestoresRejectedEXIFGuess(t *testing.T) {
	fileTime := time.Date(2024, time.April, 5, 12, 30, 0, 0, time.Local)
	fileAt := core.WallClock(fileTime)
	nameAt := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC).Unix()
	for _, tc := range []struct {
		name     string
		paths    []string
		oldFrom  core.DateSource
		oldAt    int64
		modified int64
		wantFrom core.DateSource
		wantAt   int64
	}{
		{"filename", []string{"IMG_20200102_030405.jpg"}, core.DateFromEXIF, 0, fileAt - 86400, core.DateFromFilename, nameAt},
		{"file time", []string{"photo.jpg"}, core.DateFromEXIF, 0, 0, core.DateFromFile, fileAt},
		{"QuickTime epoch", []string{"photo.jpg"}, core.DateFromEXIF, time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC).Unix(), 0, core.DateFromFile, fileAt},
		{"saved earlier", []string{"photo.jpg"}, core.DateFromEXIF, 0, fileAt - 86400, core.DateFromFile, fileAt - 86400},
		{"saved later", []string{"photo.jpg"}, core.DateFromEXIF, 0, fileAt + 86400, core.DateFromFile, fileAt},
		{"filename on another copy", []string{"photo.jpg", "IMG_20200102_030405.jpg"}, core.DateFromEXIF, 0, fileAt - 86400, core.DateFromFilename, nameAt},
		{"older file copy", []string{"photo.jpg", "copy.jpg"}, core.DateFromEXIF, 0, 0, core.DateFromFile, fileAt - 86400},
		{"keep filename", []string{"photo.jpg"}, core.DateFromFilename, nameAt, nameAt - 86400, core.DateFromFilename, nameAt},
		{"keep file guess", []string{"photo.jpg"}, core.DateFromFile, fileAt - 86400, 0, core.DateFromFile, fileAt - 86400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			src, err := st.AddSource(ctx, `C:\Photos`)
			check(t, err)
			for i, path := range tc.paths {
				in := item(src.ID, filepath.Join(src.Path, path), "same")
				in.MTime = fileTime.Add(-time.Duration(i) * 24 * time.Hour).UnixNano()
				check(t, errOf(st.Ingest(ctx, []IngestItem{in})))
			}
			jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
			check(t, err)
			id := jobs[0].AssetID
			check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET taken_at = ?, date_from = ? WHERE id = ?", tc.oldAt, tc.oldFrom, id)))
			check(t, st.SaveMetadata(ctx, "new", []MetadataResult{{AssetID: id, Modified: tc.modified}}))
			var gotAt int64
			var gotFrom core.DateSource
			check(t, st.r.QueryRowContext(ctx, "SELECT taken_at, date_from FROM assets WHERE id = ?", id).Scan(&gotAt, &gotFrom))
			if gotAt != tc.wantAt || gotFrom != tc.wantFrom {
				t.Fatalf("date = %d from %s, want %d from %s", gotAt, gotFrom, tc.wantAt, tc.wantFrom)
			}
			var wrong int
			check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM asset_jobs WHERE asset_id = ? AND taken_at != ?", id, tc.wantAt).Scan(&wrong))
			if wrong != 0 {
				t.Fatalf("%d jobs kept the rejected capture time", wrong)
			}
		})
	}
}

func TestUnchangedMetadataOnlyRecordsTheVersion(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	id := jobs[0].AssetID
	lat, lon := 48.8584, 2.2945
	check(t, st.SaveMetadata(ctx, "old", []MetadataResult{{AssetID: id, TakenAt: 1000, Width: 4000, Height: 3000,
		Orientation: 6, Camera: "Canon", Lat: &lat, Lon: &lon}}))
	_, err = st.RequeueOutdated(ctx, map[core.Stage]string{core.StageMetadata: "new"})
	check(t, err)
	jobs, err = st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	if len(jobs) != 1 || jobs[0].Prior != "old" || jobs[0].Width != 4000 || jobs[0].Height != 3000 {
		t.Fatalf("requeued job = %+v", jobs)
	}
	// An unchanged result carries nothing else: none of it may be written.
	check(t, st.SaveMetadata(ctx, "new", []MetadataResult{{AssetID: id, Unchanged: true}}))
	var takenAt int64
	var width, orientation int
	var camera string
	var placed bool
	check(t, st.r.QueryRowContext(ctx, "SELECT taken_at, width, orientation, camera, place_id IS NOT NULL FROM assets WHERE id = ?",
		id).Scan(&takenAt, &width, &orientation, &camera, &placed))
	if takenAt != 1000 || width != 4000 || orientation != 6 || camera != "Canon" || !placed {
		t.Fatalf("asset = taken %d, width %d, orientation %d, camera %q, placed %v", takenAt, width, orientation, camera, placed)
	}
	var version, status string
	check(t, st.r.QueryRowContext(ctx, "SELECT model_version, status FROM asset_jobs WHERE asset_id = ? AND stage = ?",
		id, core.StageMetadata).Scan(&version, &status))
	if version != "new" || status != string(core.JobDone) {
		t.Fatalf("metadata job = %s at %q", status, version)
	}
}

func TestJobsFlowAndRequeue(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, _ := st.AddSource(ctx, `C:\Photos`)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.heic`, "a")})))

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
	check(t, st.RemoveFiles(ctx, []int64{1}))
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
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
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

	err = st.SaveEmbeddings(ctx, "m", "m", []EmbedResult{
		{AssetID: a, Vec: []int8{127, -5}, Scale: 0.01, Tags: []TagScore{{beach, 0.4}, {dogs, 0.01}}},
		{AssetID: b, Skipped: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []int8
	check(t, st.Embeddings(ctx, "m", func(id int64, v []int8, scale float32) {
		if id == a && scale == 0.01 {
			got = append(got, v...)
		}
	}))
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
	check(t, st.SaveEmbeddings(ctx, "m", "m", []EmbedResult{{AssetID: a, Vec: []int8{1, 1}, Scale: 1, Tags: []TagScore{{beach, 0.9}}}}))
	check(t, st.SetTagScores(ctx, beach, []AssetScore{{a, 0.9}, {b, 0.5}}))
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
	check(t, st.TagAsset(ctx, b, id))
	if tags, _ = st.AssetTags(ctx, b, 0.99); len(tags) != 1 || tags[0].Source != core.TagByUser {
		t.Fatalf("user tag = %+v", tags)
	}
	check(t, st.UntagAsset(ctx, b, id))
	var rows int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM asset_tags WHERE tag_id = ?", id).Scan(&rows))
	if rows != 0 {
		t.Fatalf("manual tag removal left %d rows", rows)
	}
	if err := st.DeleteTag(ctx, beach); !errors.Is(err, ErrBuiltinTag) {
		t.Fatalf("deleting a built-in: %v", err)
	}
	if found, _ := st.SearchTags(ctx, "BIRTH", 5); len(found) != 1 || found[0].ID != id {
		t.Fatalf("search tags = %+v", found)
	}

	// Vocabulary changes: dropped built-ins go; one the user relied on stays as theirs.
	check(t, st.TagAsset(ctx, b, dogs))
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
	err = st.SaveEmbeddings(ctx, "m", "m", []EmbedResult{{AssetID: b, Vec: []int8{1, 1}, Scale: 1,
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
	check(t, st.SetFloat(ctx, core.SettingVocabulary, 3)) // how earlier versions stored it
	if v, err := st.Int(ctx, core.SettingVocabulary, 0); err != nil || v != 3 {
		t.Fatalf("int = %d (%v), want 3", v, err)
	}
}

func TestGridItemsKeepOrder(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, _ := st.AddSource(ctx, `C:\Photos`)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
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
	check(t, st.SetFloat(ctx, core.SettingTagConfidence, 0.125))
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

// check fails the test on a setup error.
func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// errOf is the error of a (value, error) call, for check.
func errOf[T any](_ T, err error) error { return err }

func item(sourceID int64, path, content string) IngestItem {
	return IngestItem{
		SourceID: sourceID, Path: path, Size: int64(len(content)), MTime: 1,
		Hash: []byte(content), Kind: core.KindPhoto, Ext: filepath.Ext(path),
		TakenAt: 1_700_000_000, DateFrom: core.DateFromFile,
	}
}
