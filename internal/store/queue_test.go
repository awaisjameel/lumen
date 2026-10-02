package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// jobStatus reads one job's status and attempts.
func readJob(t *testing.T, st *Store, asset int64, stage core.Stage) (status core.JobStatus, attempts int) {
	t.Helper()
	check(t, st.r.QueryRowContext(context.Background(), "SELECT status, attempts FROM asset_jobs WHERE asset_id = ? AND stage = ?",
		asset, stage).Scan(&status, &attempts))
	return status, attempts
}

func TestPendingJobsReadsTheQueueInIndexOrder(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	for _, within := range []*TimeRange{nil, {From: 0, To: 1}} {
		q, args := pendingJobsQuery(core.StageFaces, within, 1)
		plan, err := queryAll(ctx, st.r, func(r *sql.Rows) (d string, err error) {
			var id, parent, unused int
			err = r.Scan(&id, &parent, &unused, &d)
			return d, err
		}, "EXPLAIN QUERY PLAN "+q, args...)
		check(t, err)
		all := strings.Join(plan, "\n")
		if !strings.Contains(all, "asset_jobs_pending") || strings.Contains(all, "TEMP B-TREE") {
			t.Errorf("PendingJobs plan does not walk asset_jobs_pending in order:\n%s", all)
		}
	}
}

func TestPendingJobsKindSkipsTheOtherKindsBacklog(t *testing.T) {
	for _, kind := range []core.Kind{core.KindPhoto, core.KindVideo} {
		t.Run(string(kind), func(t *testing.T) {
			st, ctx := openTest(t), t.Context()
			src, err := st.AddSource(ctx, `C:\Media`)
			check(t, err)
			other := core.KindPhoto
			if kind == core.KindPhoto {
				other = core.KindVideo
			}
			items := make([]IngestItem, 10_000)
			for i := range items {
				items[i] = item(src.ID, fmt.Sprintf(`C:\Media\%d.jpg`, i), fmt.Sprint(i))
				items[i].Kind = other
				items[i].TakenAt += int64(i)
			}
			check(t, errOf(st.Ingest(ctx, items)))
			check(t, errOf(st.w.ExecContext(ctx, "UPDATE asset_jobs SET status = 'done' WHERE stage IN ('metadata', 'thumbnail')")))
			check(t, errOf(st.w.ExecContext(ctx, "UPDATE asset_jobs SET status = 'pending' WHERE stage = 'embedding'")))
			jobs, err := st.PendingJobsKind(ctx, core.StageEmbedding, kind, nil, 10)
			check(t, err)
			if len(jobs) != 0 {
				t.Fatalf("empty kind returned jobs: %+v", jobs)
			}
			for _, within := range []*TimeRange{nil, {From: 1_699_999_999, To: 1_700_000_001}} {
				q, args := pendingJobsKindQuery(core.StageEmbedding, kind, within, 10)
				plan, err := queryAll(ctx, st.r, func(r *sql.Rows) (d string, err error) {
					var id, parent, unused int
					err = r.Scan(&id, &parent, &unused, &d)
					return d, err
				}, "EXPLAIN QUERY PLAN "+q, args...)
				check(t, err)
				all := strings.Join(plan, "\n")
				if !strings.Contains(all, "asset_jobs_pending_kind (stage=? AND kind=?") || strings.Contains(all, "TEMP B-TREE") {
					t.Fatalf("kind query does not bound the queue by kind in date order:\n%s", all)
				}
				t.Log(all)
			}
			a, b := item(src.ID, `C:\Media\a.jpg`, "a"), item(src.ID, `C:\Media\b.jpg`, "b")
			a.Kind, b.Kind = kind, kind
			check(t, errOf(st.Ingest(ctx, []IngestItem{a, b})))
			jobs, err = st.PendingJobsKind(ctx, core.StageMetadata, kind, nil, 10)
			check(t, err)
			if len(jobs) != 2 || jobs[0].Path != b.Path || jobs[1].Path != a.Path {
				t.Fatalf("kind query changed capture-time tie ordering: %+v", jobs)
			}
			for _, within := range []*TimeRange{nil, {From: a.TakenAt, To: a.TakenAt + 1}} {
				got, err := st.PendingJobsKind(ctx, core.StageMetadata, kind, within, 1)
				check(t, err)
				if len(got) != 1 || got[0] != jobs[0] {
					t.Fatalf("kind query with range %v and limit 1 = %+v, want %+v", within, got, jobs[:1])
				}
			}
			check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: jobs[0].AssetID}, {AssetID: jobs[1].AssetID}}))
			check(t, st.SaveThumbnails(ctx, "t", []ThumbResult{{AssetID: jobs[0].AssetID}, {AssetID: jobs[1].AssetID}}))
			got, err := st.PendingJobsKind(ctx, core.StageEmbedding, kind, nil, 10)
			check(t, err)
			if len(got) != 2 || got[0] != jobs[0] || got[1] != jobs[1] {
				t.Fatalf("runnable embedding jobs changed: %+v, want %+v", got, jobs)
			}
			var empty int
			check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM asset_jobs WHERE kind = ''").Scan(&empty))
			if empty != 0 {
				t.Fatalf("%d ingested jobs have no kind", empty)
			}
		})
	}
}

func TestPendingJobsOfLooksUpCandidatesByKey(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	q, args := runnableQuery("json_each(?) ids CROSS JOIN asset_jobs j ON j.asset_id = ids.value", []any{"[1,2]"}, core.StageFaces)
	plan, err := queryAll(ctx, st.r, func(r *sql.Rows) (d string, err error) {
		var id, parent, unused int
		err = r.Scan(&id, &parent, &unused, &d)
		return d, err
	}, "EXPLAIN QUERY PLAN "+q+" ORDER BY j.taken_at DESC, j.asset_id DESC", args...)
	check(t, err)
	all := strings.Join(plan, "\n")
	if !strings.Contains(all, "SEARCH j USING PRIMARY KEY (asset_id=? AND stage=?)") {
		t.Fatalf("PendingJobsOf does not look up candidates by key:\n%s", all)
	}
	t.Log(all)
}

func TestUpgradeBackfillsJobKinds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(path)
	check(t, err)
	ctx := t.Context()
	src, err := st.AddSource(ctx, `C:\Media`)
	check(t, err)
	photo, video := item(src.ID, `C:\Media\a.jpg`, "a"), item(src.ID, `C:\Media\b.mp4`, "b")
	video.Kind = core.KindVideo
	check(t, errOf(st.Ingest(ctx, []IngestItem{photo, video})))
	check(t, errOf(st.w.ExecContext(ctx, "DROP INDEX asset_jobs_pending_kind")))
	check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE asset_jobs DROP COLUMN kind")))
	downgradeToV18(t, st)
	check(t, errOf(st.w.ExecContext(ctx, "PRAGMA user_version = 16")))
	check(t, st.Close())
	st, err = Open(path)
	check(t, err)
	defer st.Close()
	var wrong int
	check(t, st.r.QueryRowContext(ctx, `SELECT count(*) FROM asset_jobs j JOIN assets a ON a.id = j.asset_id
		WHERE j.kind != a.kind OR j.kind = ''`).Scan(&wrong))
	if wrong != 0 {
		t.Fatalf("%d upgraded jobs have the wrong kind", wrong)
	}
	jobs, err := st.PendingJobsKind(ctx, core.StageMetadata, core.KindVideo, nil, 10)
	check(t, err)
	if len(jobs) != 1 || jobs[0].Path != video.Path {
		t.Fatalf("upgraded video jobs: %+v", jobs)
	}
}

func TestKindVersionsAdoptPhotosAndRetryOldVideos(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	video := item(src.ID, `C:\Photos\video.mp4`, "video")
	video.Kind = core.KindVideo
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\photo.jpg`, "photo"),
		video,
	})))
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE asset_jobs SET status = 'done', model_version = 'model+video-frames-1'
		WHERE stage = 'embedding'`)))
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE asset_jobs SET status = 'skipped', attempts = 3
		WHERE stage = 'embedding' AND asset_id IN (SELECT id FROM assets WHERE kind = 'video')`)))
	versions := map[core.Stage]StageVersion{core.StageEmbedding: {
		Photo: "model", Video: "model+video-frames-2", LegacyPhoto: "model+video-frames-1",
	}}
	n, err := st.RequeueVersions(ctx, versions)
	check(t, err)
	if n != 1 {
		t.Fatalf("requeued %d jobs, want only the video", n)
	}
	var status, version string
	check(t, st.r.QueryRowContext(ctx, `SELECT status, model_version FROM asset_jobs
		WHERE stage = 'embedding' AND asset_id IN (SELECT id FROM assets WHERE kind = 'photo')`).Scan(&status, &version))
	if status != "done" || version != "model" {
		t.Fatalf("photo = %s at %s, want done at model", status, version)
	}
	var attempts int
	check(t, st.r.QueryRowContext(ctx, `SELECT status, attempts FROM asset_jobs
		WHERE stage = 'embedding' AND asset_id IN (SELECT id FROM assets WHERE kind = 'video')`).Scan(&status, &attempts))
	if status != "waiting" || attempts != 0 {
		t.Fatalf("video = %s with %d attempts, want waiting with fresh retries", status, attempts)
	}
}

func TestJobsCarryTheirAssetsCaptureTime(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	id := jobs[0].AssetID
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: id, TakenAt: 1_600_000_000}}))
	var n int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM asset_jobs WHERE asset_id = ? AND taken_at = 1600000000", id).Scan(&n))
	if n != len(core.Stages) {
		t.Fatalf("%d of %d jobs carry the new capture time", n, len(core.Stages))
	}
	within := &TimeRange{From: 1_599_999_999, To: 1_600_000_001}
	if got, err := st.PendingJobs(ctx, core.StageThumbnail, within, 10); err != nil || len(got) != 1 {
		t.Fatalf("PendingJobs in the new day = %v, %v; want the asset", got, err)
	}
}

func TestLaterStagesWaitForEveryEarlierStage(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	id := jobs[0].AssetID
	check(t, st.SaveMetadata(ctx, "m1", []MetadataResult{{AssetID: id, Orientation: 1}}))
	check(t, st.SaveThumbnails(ctx, "t1", []ThumbResult{{AssetID: id}}))

	// An upgrade re-runs metadata: faces must wait for it, not only for thumbnails.
	check(t, errOf(st.RequeueOutdated(ctx, map[core.Stage]string{core.StageMetadata: "m2"})))
	if got, err := st.PendingJobs(ctx, core.StageFaces, nil, 1); err != nil || len(got) != 0 {
		t.Fatalf("faces ran before metadata: %v, %v", got, err)
	}

	// The photo turns out to be sideways: everything made from it is redone.
	check(t, st.SaveMetadata(ctx, "m2", []MetadataResult{{AssetID: id, Orientation: 6}}))
	if s, _ := readJob(t, st, id, core.StageThumbnail); s != core.JobPending {
		t.Fatalf("thumbnail after an orientation change is %s, want pending", s)
	}
	// Seen again with the same orientation: nothing is redone.
	check(t, st.SaveThumbnails(ctx, "t1", []ThumbResult{{AssetID: id}}))
	check(t, st.SaveMetadata(ctx, "m2", []MetadataResult{{AssetID: id, Orientation: 6}}))
	if s, _ := readJob(t, st, id, core.StageThumbnail); s != core.JobDone {
		t.Fatalf("thumbnail after the same orientation is %s, want done", s)
	}
}

func TestFailedJobsRetryAFewTimes(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	id := jobs[0].AssetID
	for try := 1; try <= MaxAttempts+1; try++ {
		check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: id, Err: "drive not ready"}}))
		n, err := st.RetryFailed(ctx, src.ID)
		check(t, err)
		if want := try < MaxAttempts; (n == 1) != want {
			t.Fatalf("after %d failures RetryFailed requeued %d jobs, want requeued=%v", try, n, want)
		}
	}
	if s, attempts := readJob(t, st, id, core.StageMetadata); s != core.JobError || attempts != MaxAttempts+1 {
		t.Fatalf("job is %s after %d attempts", s, attempts)
	}
	// A new version starts the count again.
	check(t, errOf(st.RequeueOutdated(ctx, map[core.Stage]string{core.StageMetadata: "m2"})))
	check(t, st.SaveMetadata(ctx, "m2", []MetadataResult{{AssetID: id}}))
	if s, attempts := readJob(t, st, id, core.StageMetadata); s != core.JobDone || attempts != 0 {
		t.Fatalf("job is %s with %d attempts, want done with 0", s, attempts)
	}
}

func TestKnownFilesFoldsCaseLikeWindows(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	// The folder was renamed from ÉTÉ to été: one file, under its new spelling.
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\ÉTÉ\a.jpg`, "a"),
		item(src.ID, `C:\Photos\été\a.jpg`, "a"),
		item(src.ID, `C:\Photos\misc\b.jpg`, "b"),
	})))
	known, err := st.KnownFiles(ctx, src.ID, `C:\Photos\ÉTÉ`)
	check(t, err)
	if _, ok := known[core.Fold(`C:\Photos\été\a.jpg`)]; !ok || len(known) != 1 {
		t.Fatalf("KnownFiles(ÉTÉ) = %v; want the file once", known)
	}
	var path string
	check(t, st.r.QueryRowContext(ctx, `SELECT path FROM files WHERE path LIKE '%a.jpg'`).Scan(&path))
	if path != `C:\Photos\été\a.jpg` {
		t.Fatalf("stored spelling %q, want the newest", path)
	}
}

// A file renamed in letters NOCASE doesn't fold, with new content, stays one
// file: its row moves to the new content and the old asset is left missing.
// Before files_path_fold it got a second row, and the next scan dropped the
// newer one, so the file switched between the two assets on every scan.
func TestFileRenamedInOtherLettersWithNewContentStaysOneFile(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\Été.jpg`, "old")})))
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\été.jpg`, "new")})))
	known, err := st.KnownFiles(ctx, src.ID, "")
	check(t, err)
	if len(known) != 1 {
		t.Fatalf("KnownFiles = %v; want one file", known)
	}
	var files, missing, present int
	check(t, st.r.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM files),
		(SELECT count(*) FROM assets WHERE missing_since IS NOT NULL),
		(SELECT count(*) FROM assets WHERE missing_since IS NULL)`).Scan(&files, &missing, &present))
	if files != 1 || missing != 1 || present != 1 {
		t.Fatalf("files %d, missing assets %d, present assets %d; want 1, 1, 1", files, missing, present)
	}
}

// Migration 0018 keeps the newest row of each file named in several cases,
// leaves the displaced asset missing, and makes the fold key unique.
func TestUpgradeKeepsOneRowPerFileInAnyCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(path)
	check(t, err)
	ctx := t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\Été.jpg`, "old"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
	// A schema 17 library could hold a second spelling with other content.
	check(t, errOf(st.w.ExecContext(ctx, "DROP INDEX files_path_fold")))
	downgradeToV18(t, st)
	check(t, errOf(st.w.ExecContext(ctx, "INSERT INTO assets (content_hash, kind, ext, taken_at, date_from, added_at) VALUES (x'6e6577', 'photo', '.jpg', 0, 'file', 0)")))
	check(t, errOf(st.w.ExecContext(ctx, `INSERT INTO files (asset_id, source_id, path, size, mtime) VALUES (last_insert_rowid(), ?, 'C:\Photos\été.jpg', 1, 1)`, src.ID)))
	check(t, errOf(st.w.ExecContext(ctx, "PRAGMA user_version = 17")))
	check(t, st.Close())

	st, err = Open(path)
	check(t, err)
	defer st.Close()
	rows, err := st.r.QueryContext(ctx, "SELECT f.path, a.missing_since IS NOT NULL FROM files f JOIN assets a ON a.id = f.asset_id ORDER BY f.path")
	check(t, err)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var p string
		var missing bool
		check(t, rows.Scan(&p, &missing))
		got = append(got, fmt.Sprintf("%s missing=%v", p, missing))
	}
	check(t, rows.Err())
	if want := []string{`C:\Photos\b.jpg missing=false`, `C:\Photos\été.jpg missing=false`}; !slices.Equal(got, want) {
		t.Fatalf("files after upgrade: %v, want %v", got, want)
	}
	var displacedMissing int
	check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE content_hash = ? AND missing_since IS NOT NULL", []byte("old")).Scan(&displacedMissing))
	if displacedMissing != 1 {
		t.Fatal("the displaced asset is not marked missing")
	}
	if _, err := st.w.ExecContext(ctx, `INSERT INTO files (asset_id, source_id, path, size, mtime)
		SELECT asset_id, source_id, 'C:\Photos\ÉTÉ.JPG', 1, 1 FROM files LIMIT 1`); err == nil {
		t.Fatal("a second spelling of one file was accepted after the upgrade")
	}
}

// A job an earlier stage holds back is 'waiting', out of the pending index,
// and becomes 'pending' once that stage is saved; progress counts both.
func TestJobsWaitOutsideTheQueueUntilTheirTurn(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	id := jobs[0].AssetID
	want := func(when string, statuses map[core.Stage]core.JobStatus) {
		t.Helper()
		for stage, w := range statuses {
			if s, _ := readJob(t, st, id, stage); s != w {
				t.Errorf("%s: %s is %s, want %s", when, stage, s, w)
			}
		}
	}
	want("new", map[core.Stage]core.JobStatus{core.StageMetadata: core.JobPending,
		core.StageThumbnail: core.JobWaiting, core.StageEmbedding: core.JobWaiting, core.StageFaces: core.JobWaiting})
	pending, _, _, err := st.JobCounts(ctx)
	check(t, err)
	if pending[core.StageFaces] != 1 {
		t.Errorf("JobCounts = %v, want the waiting faces job counted", pending)
	}
	if n, err := st.CountPending(ctx, core.StageEmbedding); err != nil || n != 1 {
		t.Errorf("CountPending(embedding) = %d, %v; want 1", n, err)
	}

	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: id, Orientation: 1}}))
	want("after metadata", map[core.Stage]core.JobStatus{core.StageThumbnail: core.JobPending,
		core.StageEmbedding: core.JobWaiting, core.StageFaces: core.JobWaiting})
	// A failed thumbnail doesn't hold the AI stages back: they read the
	// original and record their own failure.
	check(t, st.SaveThumbnails(ctx, "t", []ThumbResult{{AssetID: id, Err: "broken"}}))
	want("after thumbnail", map[core.Stage]core.JobStatus{core.StageEmbedding: core.JobPending, core.StageFaces: core.JobPending})

	// The file is seen again: the thumbnail retries, and the AI stages wait again.
	check(t, errOf(st.RetryFailed(ctx, src.ID)))
	want("retry", map[core.Stage]core.JobStatus{core.StageThumbnail: core.JobPending,
		core.StageEmbedding: core.JobWaiting, core.StageFaces: core.JobWaiting})
	// A new metadata version holds back everything after it.
	check(t, st.SaveThumbnails(ctx, "t", []ThumbResult{{AssetID: id}}))
	check(t, errOf(st.RequeueOutdated(ctx, map[core.Stage]string{core.StageMetadata: "m2"})))
	want("requeue", map[core.Stage]core.JobStatus{core.StageMetadata: core.JobPending,
		core.StageThumbnail: core.JobDone, core.StageEmbedding: core.JobWaiting, core.StageFaces: core.JobWaiting})
	check(t, st.SaveMetadata(ctx, "m2", []MetadataResult{{AssetID: id, Orientation: 1}}))
	want("metadata again", map[core.Stage]core.JobStatus{core.StageEmbedding: core.JobPending, core.StageFaces: core.JobPending})
}

// Schema v8 moves jobs a v7 library left pending behind an earlier stage to
// waiting.
func TestUpgradeMovesBlockedJobsToWaiting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(path)
	check(t, err)
	ctx := context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 2)
	check(t, err)
	a, b := jobs[0].AssetID, jobs[1].AssetID
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: b, Orientation: 1}}))
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE asset_jobs SET status = 'pending' WHERE status = 'waiting'")))
	downgradeToV18(t, st)
	check(t, errOf(st.w.ExecContext(ctx, "DROP INDEX asset_jobs_pending_kind")))
	check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE asset_jobs DROP COLUMN kind")))          // added by v17
	check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE asset_jobs DROP COLUMN prior_version"))) // added by v9
	check(t, errOf(st.w.ExecContext(ctx, "DROP INDEX assets_by_place")))                       // added by v10
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE places")))
	check(t, errOf(st.w.ExecContext(ctx, "DROP INDEX assets_pending_place")))
	check(t, errOf(st.w.ExecContext(ctx, "DROP INDEX assets_event_cover")))
	for _, trigger := range []string{"event_asset_added", "event_asset_changed", "event_asset_deleted", "event_name_added", "event_name_changed", "event_name_deleted", "event_cover_changed", "event_block_added", "event_block_changed", "event_block_deleted", "event_block_face_added", "event_block_face_changed", "event_block_face_deleted", "event_block_tag_added", "event_block_tag_changed", "event_block_tag_deleted", "event_block_album_added", "event_block_album_deleted", "event_block_album_rule_changed", "event_block_favourite_changed", "event_block_confidence_changed", "event_block_confidence_added", "event_block_file_added", "event_block_file_changed", "event_block_file_deleted", "block_asset_deleted", "block_person_deleted", "block_tag_deleted", "block_place_deleted", "block_album_deleted",
		"visible_asset_added", "visible_asset_deleted", "visible_asset_changed", "visible_block_place_changed", "visible_block_date_changed",
		"face_block_removed", "visible_face_block_added", "visible_face_block_deleted"} {
		check(t, errOf(st.w.ExecContext(ctx, "DROP TRIGGER IF EXISTS "+trigger))) // added after v7
	}
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE blocks")))
	check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE tags DROP COLUMN is_sink")))
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE event_revision")))
	check(t, errOf(st.w.ExecContext(ctx, "DROP TABLE event_names"))) // added by v11
	check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE assets DROP COLUMN place_checked")))
	check(t, errOf(st.w.ExecContext(ctx, "PRAGMA user_version = 7")))
	check(t, st.Close())

	st, err = Open(path)
	check(t, err)
	defer st.Close()
	for _, c := range []struct {
		asset int64
		stage core.Stage
		want  core.JobStatus
	}{
		{a, core.StageMetadata, core.JobPending}, {a, core.StageThumbnail, core.JobWaiting}, {a, core.StageFaces, core.JobWaiting},
		{b, core.StageThumbnail, core.JobPending}, {b, core.StageEmbedding, core.JobWaiting}, {b, core.StageFaces, core.JobWaiting},
	} {
		if s, _ := readJob(t, st, c.asset, c.stage); s != c.want {
			t.Errorf("asset %d %s is %s, want %s", c.asset, c.stage, s, c.want)
		}
	}
}

func TestUpgradeRemembersWhatMadeTheOutput(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b")})))
	var a, b int64
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte("a")).Scan(&a))
	check(t, st.r.QueryRowContext(ctx, "SELECT id FROM assets WHERE content_hash = ?", []byte("b")).Scan(&b))
	check(t, st.SaveMetadata(ctx, "m1", []MetadataResult{{AssetID: a, Orientation: 1}, {AssetID: b, Orientation: 1}}))
	ph := uint64(42)
	check(t, st.SaveThumbnails(ctx, "t1", []ThumbResult{{AssetID: a, PHash: &ph}, {AssetID: b, Err: "unreadable"}}))
	check(t, st.SaveFaces(ctx, "f", "f1", []FaceResult{
		{AssetID: a, Faces: []DetectedFace{{X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Vec: []float32{1, 0}}}},
		{AssetID: b},
	}))
	thumbOf := func(id int64) (phash int64, version int) {
		t.Helper()
		check(t, st.r.QueryRowContext(ctx, "SELECT COALESCE(phash, 0), thumb_version FROM assets WHERE id = ?", id).Scan(&phash, &version))
		return phash, version
	}
	_, before := thumbOf(a)

	check(t, errOf(st.RequeueOutdated(ctx, map[core.Stage]string{core.StageThumbnail: "t2", core.StageFaces: "f2"})))
	jobs, err := st.PendingJobs(ctx, core.StageThumbnail, nil, 10)
	check(t, err)
	prior := map[int64]string{}
	for _, j := range jobs {
		prior[j.AssetID] = j.Prior
	}
	// Only a job that had succeeded has output to keep.
	if len(jobs) != 2 || prior[a] != "t1" || prior[b] != "" {
		t.Fatalf("requeued thumbnails = %+v, want a from t1 and b from nothing", jobs)
	}
	if got, err := st.WithFaces(ctx, []int64{a, b}); err != nil || !got[a] || got[b] {
		t.Fatalf("WithFaces = %v, %v", got, err)
	}

	// Output that stands keeps everything but the version.
	check(t, st.SaveThumbnails(ctx, "t2", []ThumbResult{{AssetID: a, Unchanged: true}}))
	if p, v := thumbOf(a); p != 42 || v != before {
		t.Fatalf("an unchanged thumbnail has phash %d, version %d; want 42, %d", p, v, before)
	}
	check(t, st.SaveFaces(ctx, "f", "f2", []FaceResult{{AssetID: a, Unchanged: true}}))
	if got, _ := st.WithFaces(ctx, []int64{a}); !got[a] {
		t.Fatal("unchanged faces were dropped")
	}
	var version string
	var kept sql.NullString
	check(t, st.r.QueryRowContext(ctx, "SELECT model_version, prior_version FROM asset_jobs WHERE asset_id = ? AND stage = 'thumbnail'", a).
		Scan(&version, &kept))
	if s, _ := readJob(t, st, a, core.StageThumbnail); s != core.JobDone || version != "t2" || kept.Valid {
		t.Fatalf("closed job is %s at %q with prior %v", s, version, kept)
	}

	// A requeue for another reason (the photo turned out sideways) redoes it in full.
	check(t, errOf(st.RequeueOutdated(ctx, map[core.Stage]string{core.StageThumbnail: "t3"})))
	check(t, st.SaveMetadata(ctx, "m1", []MetadataResult{{AssetID: a, Orientation: 6}}))
	jobs, err = st.PendingJobsOf(ctx, core.StageThumbnail, []int64{a})
	check(t, err)
	if len(jobs) != 1 || jobs[0].Prior != "" {
		t.Fatalf("thumbnail after an orientation change = %+v, want no prior version", jobs)
	}
}

// Thumbnails made afresh (an upgrade) re-run the image model, which reads
// them; thumbnails that stand leave the embedding as it is.
func TestNewThumbnailsRequeueTheEmbedding(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	id := jobs[0].AssetID
	check(t, st.SaveMetadata(ctx, "m", []MetadataResult{{AssetID: id, Orientation: 1}}))
	check(t, st.SaveThumbnails(ctx, "t1", []ThumbResult{{AssetID: id}}))
	check(t, st.SaveEmbeddings(ctx, "model", "e", []EmbedResult{{AssetID: id, Vec: make([]int8, 4), Scale: 1}}))
	if status, _ := readJob(t, st, id, core.StageEmbedding); status != core.JobDone {
		t.Fatalf("embedding before: %s", status)
	}
	check(t, st.SaveThumbnails(ctx, "t2", []ThumbResult{{AssetID: id, Unchanged: true}}))
	if status, _ := readJob(t, st, id, core.StageEmbedding); status != core.JobDone {
		t.Fatalf("embedding after standing thumbnails: %s", status)
	}
	check(t, st.SaveThumbnails(ctx, "t3", []ThumbResult{{AssetID: id}}))
	if status, _ := readJob(t, st, id, core.StageEmbedding); status != core.JobPending {
		t.Fatalf("embedding after new thumbnails: %s", status)
	}
}

func TestCPUThreadsAreHeldToTheMachines(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if n, err := st.CPUThreads(ctx); err != nil || n != core.DefaultCPUThreads() {
		t.Fatalf("default threads = %d (%v), want %d", n, err, core.DefaultCPUThreads())
	}
	most := core.MaxCPUThreads()
	for _, tc := range []struct{ set, want int }{
		{1, 1},
		{most, most},
		{0, 1},
		{most * 4, most},
	} {
		stored, err := st.SetCPUThreads(ctx, tc.set)
		check(t, err)
		got, err := st.CPUThreads(ctx)
		check(t, err)
		if stored != tc.want || got != tc.want {
			t.Errorf("set %d: stored %d, read %d, want %d", tc.set, stored, got, tc.want)
		}
	}
	// A library from a bigger machine runs on this one's threads.
	check(t, st.SetInt(ctx, core.SettingCPUThreads, most+8))
	if n, _ := st.CPUThreads(ctx); n != most {
		t.Errorf("a bigger machine's choice reads as %d, want %d", n, most)
	}
}
