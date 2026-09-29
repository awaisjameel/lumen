package store

import (
	"context"
	"database/sql"
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
