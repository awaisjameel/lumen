package store

import (
	"context"
	"database/sql"
	"path/filepath"
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
	// The folder was renamed from ÉTÉ to été: both spellings were recorded.
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\ÉTÉ\a.jpg`, "a"),
		item(src.ID, `C:\Photos\été\a.jpg`, "a"),
		item(src.ID, `C:\Photos\misc\b.jpg`, "b"),
	})))
	known, extra, err := st.KnownFiles(ctx, src.ID, `C:\Photos\été`)
	check(t, err)
	if _, ok := known[core.Fold(`C:\Photos\ÉTÉ\a.jpg`)]; !ok || len(known) != 1 || len(extra) != 1 {
		t.Fatalf("KnownFiles(été) = %v, extra %v; want the file once and its other spelling as extra", known, extra)
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
	check(t, errOf(st.w.ExecContext(ctx, "ALTER TABLE asset_jobs DROP COLUMN prior_version"))) // added by v9
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
