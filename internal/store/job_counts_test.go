package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"lumen/internal/core"
)

// jobCountsReference preserves the original progress query independently of
// the maintained table, including distinct assets for each terminal outcome.
func jobCountsReference(ctx context.Context, db querier) (QueueCounts, error) {
	rows, err := db.QueryContext(ctx, `SELECT j.stage, j.kind, j.status, count(*) FROM asset_jobs j
		JOIN assets a ON a.id = j.asset_id
		WHERE j.status IN ('pending', 'waiting') AND `+visibleA+`
		GROUP BY j.stage, j.kind, j.status
		UNION ALL
		SELECT '', '', j.status, count(DISTINCT j.asset_id) FROM asset_jobs j
		JOIN assets a ON a.id = j.asset_id
		WHERE j.status IN ('error', 'skipped') AND `+visibleA+`
			GROUP BY j.status`)
	if err != nil {
		return QueueCounts{}, err
	}
	defer rows.Close()
	counts := QueueCounts{Pending: map[core.Stage]int{}, Videos: map[core.Stage]int{}}
	for rows.Next() {
		var stage core.Stage
		var kind core.Kind
		var status core.JobStatus
		var n int
		if err := rows.Scan(&stage, &kind, &status, &n); err != nil {
			return QueueCounts{}, err
		}
		switch status {
		case core.JobPending, core.JobWaiting:
			counts.Pending[stage] += n
			if kind == core.KindVideo {
				counts.Videos[stage] += n
			}
		case core.JobError:
			counts.Failed += n
		case core.JobSkipped:
			counts.Skipped += n
		case core.JobDone:
		}
	}
	return counts, rows.Err()
}

func compareJobCounts(ctx context.Context, db querier) error {
	got, err := jobCounts(ctx, db)
	if err != nil {
		return err
	}
	want, err := jobCountsReference(ctx, db)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(got, want) {
		return fmt.Errorf("kept counts %+v differ from reference %+v", got, want)
	}
	return nil
}

func seedJobCounts(ctx context.Context, t *testing.T, st *Store) {
	t.Helper()
	seedBlockedOracle(ctx, t, st)
	writeInTx(t, st, `INSERT INTO asset_jobs(asset_id, stage, status, kind, updated_at)
  SELECT id, stage, CASE (id + ordinal)%5 WHEN 0 THEN 'pending' WHEN 1 THEN 'waiting'
   WHEN 2 THEN 'done' WHEN 3 THEN 'error' ELSE 'skipped' END,
   CASE id%2 WHEN 0 THEN 'video' ELSE 'photo' END, 0
  FROM assets CROSS JOIN (SELECT 'metadata' stage, 0 ordinal UNION ALL SELECT 'thumbnail', 1
   UNION ALL SELECT 'embedding', 2 UNION ALL SELECT 'faces', 3 UNION ALL SELECT 'ocr', 4)`)
}

func jobCountsChanges() []blockedOracleChange {
	changes := keptCountChanges()
	args := func(r *rand.Rand) []any { return []any{int64(2 + r.IntN(14)), queuedStages[r.IntN(len(queuedStages))]} }
	changes = append(changes,
		blockedOracleChange{"job insert", `INSERT OR IGNORE INTO asset_jobs(asset_id, stage, status, kind, updated_at)
   SELECT id, ?, 'pending', kind, 0 FROM assets WHERE id = ?`, func(r *rand.Rand) []any {
			return []any{queuedStages[r.IntN(len(queuedStages))], int64(2 + r.IntN(15))}
		}},
		blockedOracleChange{"job delete", `DELETE FROM asset_jobs WHERE asset_id = ? AND stage = ?`, args},
		blockedOracleChange{"job stage", `UPDATE OR IGNORE asset_jobs SET stage = CASE stage WHEN 'ocr' THEN 'faces' ELSE 'ocr' END WHERE asset_id = ? AND stage = ?`, args},
		blockedOracleChange{"job kind", `UPDATE asset_jobs SET kind = CASE kind WHEN 'photo' THEN 'video' ELSE 'photo' END WHERE asset_id = ? AND stage = ?`, args},
		blockedOracleChange{"job owner", `UPDATE OR IGNORE asset_jobs SET asset_id = 17-asset_id WHERE asset_id = ? AND stage = ?`, args},
		blockedOracleChange{"job metadata", `UPDATE asset_jobs SET taken_at = taken_at + 1, updated_at = updated_at + 1, attempts = attempts + 1 WHERE asset_id = ? AND stage = ?`, args},
		blockedOracleChange{"asset restore", `UPDATE assets SET hidden = 0, trashed_at = NULL, missing_since = NULL WHERE id = ?`, func(r *rand.Rand) []any { return []any{int64(2 + r.IntN(14))} }},
	)
	for _, status := range []core.JobStatus{core.JobPending, core.JobWaiting, core.JobDone, core.JobError, core.JobSkipped} {
		changes = append(changes, blockedOracleChange{string(status), `UPDATE asset_jobs SET status = ? WHERE asset_id = ? AND stage = ?`, func(r *rand.Rand) []any {
			return []any{status, int64(2 + r.IntN(14)), queuedStages[r.IntN(len(queuedStages))]}
		}})
	}
	return changes
}

func TestJobCountsEqualTheReferenceThroughEveryRandomisedChange(t *testing.T) {
	t.Parallel()
	for _, limit := range []int64{keptCountsThreshold, 1} {
		for _, seed := range []uint64{1, 42, 20261014} {
			t.Run(fmt.Sprintf("limit=%d/seed=%d", limit, seed), func(t *testing.T) {
				t.Parallel()
				st := openTest(t)
				ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, limit)
				seedJobCounts(ctx, t, st)
				rng := rand.New(rand.NewPCG(seed, seed+1))
				changes := jobCountsChanges()
				t.Logf("checking %d mutations across %d operations", 12*len(changes), len(changes))
				for round := range 12 {
					for _, index := range rng.Perm(len(changes)) {
						change := changes[index]
						var args []any
						if change.args != nil {
							args = change.args(rng)
						}
						rollback := round%4 == 3 && index%7 == 0
						errRollback := errKeptCountRollback
						err := applyKeptCountChange(ctx, t, st, change.sql, args, rollback)
						if err != nil && (!rollback || !errors.Is(err, errRollback)) {
							t.Fatalf("seed %d round %d %s: %v", seed, round, change.name, err)
						}
						check(t, st.read(ctx, func(tx *sql.Tx) error { return compareJobCounts(ctx, tx) }))
					}
				}
			})
		}
	}
}

func TestJobCountsSubtractEachDeletedAssetBeforeItsChildrenCascade(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedJobCounts(ctx, t, st)
	writeInTx(t, st, "UPDATE asset_jobs SET status = 'error' WHERE asset_id = 1")
	check(t, errOf(st.SetBlock(ctx, core.BlockAsset, 2, "", core.BlockPhotos)))
	writeInTx(t, st, "DELETE FROM assets WHERE id IN (1, 2, 3)")
	check(t, st.read(ctx, func(tx *sql.Tx) error { return compareJobCounts(ctx, tx) }))
}

func TestJobCountsMatchTheReferenceInSnapshotsBesideAWriter(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedJobCounts(ctx, t, st)
	var readers sync.WaitGroup
	done := make(chan struct{})
	for range 3 {
		readers.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				if err := st.read(ctx, func(tx *sql.Tx) error { return compareJobCounts(ctx, tx) }); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	defer func() { close(done); readers.Wait() }()
	for step := range 250 {
		writeInTx(t, st, `UPDATE assets SET hidden = 1-hidden WHERE id = ?;
   UPDATE asset_jobs SET status = CASE status WHEN 'pending' THEN 'error' WHEN 'error' THEN 'skipped'
    WHEN 'skipped' THEN 'waiting' ELSE 'pending' END WHERE asset_id = ?`, 1+step%16, 1+step%16)
		if step%2 == 0 {
			check(t, errOf(st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)))
		} else {
			writeInTx(t, st, "DELETE FROM blocks")
		}
	}
}

func TestJobCountsReadOnlyTheMaintainedRows(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedJobCounts(ctx, t, st)
	st.r.SetMaxOpenConns(1)
	conn, err := st.r.Conn(ctx)
	check(t, err)
	defer func() { _ = conn.Close() }()
	check(t, errOf(conn.ExecContext(ctx, `PRAGMA query_only = OFF;
  CREATE TEMP VIEW asset_jobs AS SELECT * FROM main.asset_jobs WHERE json_extract('invalid', '$') = 1;
  PRAGMA query_only = ON;`)))
	check(t, conn.Close())
	check(t, errOf(st.JobCounts(ctx)))
}

func TestJobCountsCountOnlyTheFirstAndLastFailedOrSkippedStage(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedJobCounts(ctx, t, st)
	writeInTx(t, st, "UPDATE asset_jobs SET status = 'done'")
	for _, status := range []core.JobStatus{core.JobError, core.JobSkipped} {
		for _, stage := range queuedStages {
			writeInTx(t, st, "UPDATE asset_jobs SET status = ? WHERE asset_id = 1 AND stage = ?", status, stage)
			counts, err := st.JobCounts(ctx)
			check(t, err)
			if counts.Failed+counts.Skipped != 1 {
				t.Fatalf("multiple stages counted the same asset: %+v", counts)
			}
		}
		for i, stage := range queuedStages {
			writeInTx(t, st, "UPDATE asset_jobs SET status = 'done' WHERE asset_id = 1 AND stage = ?", stage)
			counts, err := st.JobCounts(ctx)
			check(t, err)
			want := 1
			if i == len(queuedStages)-1 {
				want = 0
			}
			if counts.Failed+counts.Skipped != want {
				t.Fatalf("removing stage %s changed the distinct count too soon: %+v", stage, counts)
			}
		}
	}
}

func TestJobCountsDoNotWriteForEquivalentQueueStatesOrUnchangedVisibility(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedJobCounts(ctx, t, st)
	writeInTx(t, st, "UPDATE asset_jobs SET status = 'pending' WHERE asset_id = 1 AND stage = 'metadata'")
	check(t, st.runTx(ctx, false, func(tx *sql.Tx) error {
		// A count write aborts this transaction, even if it would preserve the value.
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TRIGGER reject_job_count_update BEFORE UPDATE ON job_counts
			BEGIN SELECT RAISE(ABORT, 'unnecessary count update'); END;
			CREATE TEMP TRIGGER reject_job_count_insert BEFORE INSERT ON job_counts
			BEGIN SELECT RAISE(ABORT, 'unnecessary count insert'); END;`); err != nil {
			return err
		}
		for _, q := range []string{
			"UPDATE asset_jobs SET status = 'waiting' WHERE asset_id = 1 AND stage = 'metadata'",
			"UPDATE asset_jobs SET status = 'pending' WHERE asset_id = 1 AND stage = 'metadata'",
			"UPDATE asset_jobs SET status = status, stage = stage, kind = kind, asset_id = asset_id",
			"UPDATE asset_jobs SET updated_at = updated_at + 1, taken_at = taken_at + 1",
			"UPDATE assets SET hidden = hidden, missing_since = missing_since, trashed_at = trashed_at",
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "DROP TRIGGER reject_job_count_update; DROP TRIGGER reject_job_count_insert")
		return err
	}))
	check(t, st.read(ctx, func(tx *sql.Tx) error { return compareJobCounts(ctx, tx) }))
}

func TestJobCountsOutcomeMaintenanceProbesOnlyTheAssetsOwnJobs(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	body, err := migrationFS.ReadFile("migrations/0037_kept_counts.sql")
	check(t, err)
	// Explain the migration's actual existence checks before statistics can
	// conceal a planner choice that scans all failed jobs for each write.
	checks := regexp.MustCompile(`(?s)SELECT 1 FROM asset_jobs j(?: INDEXED BY sqlite_autoindex_asset_jobs_1)?\s+WHERE j.asset_id = (?:OLD|NEW)\.asset_id\s+AND j.status = (?:OLD|NEW)\.status(?: AND j.stage != (?:OLD|NEW)\.stage)?`).FindAllString(string(body), -1)
	if len(checks) != 6 {
		t.Fatalf("found %d outcome checks, want 6", len(checks))
	}
	replace := strings.NewReplacer("OLD.asset_id", "1", "NEW.asset_id", "1", "OLD.status", "'error'", "NEW.status", "'error'", "NEW.stage", "'faces'")
	for _, q := range checks {
		plan, err := queryAll(ctx, st.r, func(r *sql.Rows) (detail string, err error) {
			var id, parent, unused int
			err = r.Scan(&id, &parent, &unused, &detail)
			return detail, err
		}, "EXPLAIN QUERY PLAN "+replace.Replace(q))
		check(t, err)
		if len(plan) != 1 || !strings.Contains(plan[0], "USING PRIMARY KEY (asset_id=?)") {
			t.Fatalf("outcome check scans unrelated jobs: %s: %v", q, plan)
		}
	}
}
