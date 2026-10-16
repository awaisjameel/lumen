package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"

	"lumen/internal/core"
)

func settleCountInputs(ctx context.Context, tx *sql.Tx) error {
	if err := settleDirty(ctx, tx); err != nil {
		return err
	}
	if err := settleBlockedAssets(ctx, tx); err != nil {
		return err
	}
	return settleKeptCounts(ctx, tx)
}

func keptCountChanges() []blockedOracleChange {
	changes := blockedOracleChanges()
	// Production uses UPSERT: REPLACE suppresses SQLite's delete triggers.
	changes[0].sql = `INSERT INTO asset_tags(asset_id, tag_id, source, score) VALUES (?, 1, 'ai', 0.03)
		ON CONFLICT(asset_id, tag_id) DO UPDATE SET source = excluded.source, score = excluded.score`
	return changes
}

var errKeptCountRollback = errors.New("kept counts rollback")

func applyKeptCountChange(ctx context.Context, t *testing.T, st *Store, query string, args []any, rollback bool) error {
	t.Helper()
	return st.tx(ctx, func(tx *sql.Tx) error {
		if err := markKeptCountsForChange(ctx, tx, 1); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
		if err := settleCountInputs(ctx, tx); err != nil {
			return err
		}
		compareKeptCounts(ctx, t, tx)
		if rollback {
			return errKeptCountRollback
		}
		return nil
	})
}

func compareKeptCounts(ctx context.Context, t *testing.T, tx *sql.Tx) {
	t.Helper()
	compareDayCounts(ctx, t, tx)
	assertTagCountValues(ctx, t, tx)
	check(t, comparePersonCounts(ctx, tx))
	check(t, compareJobCounts(ctx, tx))
	var keptTrash, countedTrash int
	check(t, tx.QueryRowContext(ctx, "SELECT n FROM trash_count WHERE id = 1").Scan(&keptTrash))
	check(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+trashed).Scan(&countedTrash))
	if keptTrash != countedTrash {
		t.Fatalf("kept Trash %d, counted %d", keptTrash, countedTrash)
	}
	var marks int
	check(t, tx.QueryRowContext(ctx, "SELECT count(*) FROM count_rebuild").Scan(&marks))
	if marks != 0 {
		t.Fatalf("settlement left %d count marks", marks)
	}
}

func refuseKeptCountWrites(t *testing.T, st *Store, table string) {
	t.Helper()
	for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
		writeInTx(t, st, "CREATE TRIGGER refuse_"+table+"_"+operation+" BEFORE "+operation+" ON "+table+
			" BEGIN SELECT RAISE(ABORT, 'unexpected count write'); END")
	}
}

func seedKeptCounts(t *testing.T, st *Store) {
	t.Helper()
	seedJobCounts(t.Context(), t, st)
	writeInTx(t, st, `INSERT INTO asset_tags(asset_id, tag_id, source, score) SELECT id, 1, 'ai', 0.5 FROM assets;
		UPDATE assets SET thumb_version = 1, width = 1000, height = 1000, favourite = id%2`)
}

// Recording whether the table was marked distinguishes a recount from row
// maintenance without relying on timings or a second production journal.
func recordKeptCountWrites(t *testing.T, st *Store) {
	t.Helper()
	writeInTx(t, st, "CREATE TABLE count_test_writes (name TEXT, recount INTEGER)")
	for _, table := range []string{"day_counts", "tag_counts", "person_counts", "person_counts_faces", "job_counts"} {
		mark := table
		if table == "person_counts_faces" {
			mark = "person_counts"
		}
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			writeInTx(t, st, "CREATE TRIGGER record_"+table+"_"+operation+" AFTER "+operation+" ON "+table+
				" BEGIN INSERT INTO count_test_writes VALUES ('"+table+"', EXISTS (SELECT 1 FROM count_rebuild WHERE name = '"+mark+"')); END")
		}
	}
}

func assertKeptCountPath(t *testing.T, st *Store, recount bool) {
	t.Helper()
	var writes, wrong int
	check(t, st.r.QueryRowContext(t.Context(), "SELECT count(*), COALESCE(sum(recount <> ?), 0) FROM count_test_writes", recount).Scan(&writes, &wrong))
	if writes == 0 || wrong != 0 {
		t.Fatalf("count writes = %d, wrong path = %d, want recount=%t", writes, wrong, recount)
	}
	check(t, st.read(t.Context(), func(tx *sql.Tx) error { compareKeptCounts(t.Context(), t, tx); return nil }))
	writeInTx(t, st, "DELETE FROM count_test_writes")
}

func TestKeptCountsChooseTheirPathFromTheActualCoverageDelta(t *testing.T) {
	for _, limit := range []int64{8, 32} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			st := openTest(t)
			ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, limit)
			seedKeptCounts(t, st)
			recordKeptCountWrites(t, st)
			id, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)
			check(t, err)
			assertKeptCountPath(t, st, limit == 8)
			// A rebuild retaining all sixteen rows must still use row maintenance
			// when one asset escapes an existing large folder block.
			check(t, errOf(st.w.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\Open\1.jpg' WHERE asset_id = 1`)))
			assertKeptCountPath(t, st, false)
			check(t, errOf(st.w.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\Private\1.jpg' WHERE asset_id = 1`)))
			assertKeptCountPath(t, st, false)
			// The global path evaluates sixteen existing rows but changes none.
			check(t, errOf(st.w.ExecContext(ctx, "INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1)")))
			var writes int
			check(t, st.r.QueryRowContext(ctx, "SELECT count(*) FROM count_test_writes").Scan(&writes))
			if writes != 0 {
				t.Fatalf("unchanged coverage wrote %d counts", writes)
			}
			check(t, st.RemoveBlock(ctx, id))
			assertKeptCountPath(t, st, limit == 8)
		})
	}
}

func TestKeptCountsRecountAboveTheMeasuredThresholdAndMaintainOnePhotoBelowIt(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		if err := markKeptCountsStale(ctx, tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO sources(id,path,added_at) VALUES (1,'C:\Photos',0);
			INSERT INTO tags(id,name,kind,created_at) VALUES (1,'All','manual',0);
			INSERT INTO people(id,name,created_at) VALUES (1,'All',0);
			WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?)
			INSERT INTO assets(id,content_hash,kind,ext,taken_at,added_at,thumb_version,width,height)
			SELECT x, printf('%016x',x), 'photo', '.jpg', 1700000000, 0, 1, 100, 100 FROM n;
			INSERT INTO files(asset_id,source_id,path,size,mtime) SELECT id,1,'C:\Photos\Private\'||id||'.jpg',1,0 FROM assets;
			INSERT INTO asset_tags(asset_id,tag_id,source) SELECT id,1,'user' FROM assets;
			INSERT INTO asset_jobs(asset_id,stage,status,kind,updated_at) SELECT id,'metadata','pending','photo',0 FROM assets;
			INSERT INTO faces(asset_id,person_id,x,y,w,h,score,sharpness,yaw,model,vec)
			SELECT id,1,0,0,0.2,0.2,1,20,0,'test',zeroblob(512) FROM assets WHERE id<=16`, keptCountsThreshold+1)
		return err
	}))
	recordKeptCountWrites(t, st)
	id, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)
	check(t, err)
	assertKeptCountPath(t, st, true)
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE files SET path='C:\Photos\Open\1.jpg' WHERE asset_id=1`)))
	assertKeptCountPath(t, st, false)
	check(t, st.RemoveBlock(ctx, id))
	assertKeptCountPath(t, st, true)
}

func TestKeptCountsRecountBulkTrashRestoreAndSourceRemoval(t *testing.T) {
	st := openTest(t)
	ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, int64(8))
	seedKeptCounts(t, st)
	ids := make([]int64, 16)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	recordKeptCountWrites(t, st)
	check(t, st.TrashAssets(ctx, ids))
	assertKeptCountPath(t, st, true)
	check(t, st.RestoreAssets(ctx, ids))
	assertKeptCountPath(t, st, true)
	check(t, st.TrashAssets(ctx, ids))
	assertKeptCountPath(t, st, true)
	check(t, st.RestoreAllTrash(ctx))
	assertKeptCountPath(t, st, true)
	check(t, st.RemoveSource(ctx, 1))
	assertKeptCountPath(t, st, true)
}

func TestKeptCountsIncludeBothSidesOfALocalCoverageChange(t *testing.T) {
	st := openTest(t)
	ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, int64(12))
	seedKeptCounts(t, st)
	writeInTx(t, st, `UPDATE files SET path = 'C:\Photos\Open\'||id||'.jpg' WHERE asset_id>8`)
	check(t, errOf(st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)))
	recordKeptCountWrites(t, st)
	// Neither side reaches twelve alone; their sixteen real changes do.
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\'||
		CASE WHEN asset_id<=8 THEN 'Open\' ELSE 'Private\' END||id||'.jpg'`)))
	assertKeptCountPath(t, st, true)
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\Private\'||id||'.jpg'`)))
	assertKeptCountPath(t, st, false)
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\Open\'||id||'.jpg'`)))
	assertKeptCountPath(t, st, true)
	// With no dirty asset covered, the direct insertion path must also mark.
	check(t, errOf(st.w.ExecContext(ctx, `UPDATE files SET path = 'C:\Photos\Private\'||id||'.jpg'`)))
	assertKeptCountPath(t, st, true)
}

func TestConfidenceChangesDeferOnlyTheTagCountsUntilSettlement(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedKeptCounts(t, st)
	recordKeptCountWrites(t, st)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES ('tag_confidence', '0.8');
			UPDATE settings SET value='0.7' WHERE key='tag_confidence';
			UPDATE asset_tags SET score=0.75`); err != nil {
			return err
		}
		var marks, writes int
		var name string
		if err := tx.QueryRowContext(ctx, "SELECT count(*), min(name) FROM count_rebuild").Scan(&marks, &name); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM count_test_writes").Scan(&writes); err != nil {
			return err
		}
		if marks != 1 || name != "tag_counts" || writes != 0 {
			t.Fatalf("deferred confidence: %d marks, table %q, %d writes", marks, name, writes)
		}
		return nil
	}))
	assertKeptCountPath(t, st, true)
}

func TestMarkedKeptCountsDoNoRowMaintenanceBeforeSettlement(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedKeptCounts(t, st)
	for _, table := range []string{"day_counts", "tag_counts", "person_counts", "person_counts_faces", "job_counts"} {
		refuseKeptCountWrites(t, st, table)
	}
	sentinel := errors.New("rollback marked inputs")
	err := st.tx(ctx, func(tx *sql.Tx) error {
		if err := markKeptCountsStale(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE assets SET hidden=1, thumb_version=0 WHERE id=2;
			UPDATE faces SET person_id=2, score=0.8 WHERE id=3;
			UPDATE asset_tags SET source='rejected' WHERE asset_id=4;
			INSERT INTO asset_tags(asset_id, tag_id, source) VALUES (5,2,'user');
			UPDATE asset_jobs SET status='pending', kind='video' WHERE asset_id=6;
			UPDATE asset_jobs SET status='done' WHERE asset_id=7;
			INSERT INTO blocked_assets(asset_id) VALUES (8);
			DELETE FROM blocked_assets WHERE asset_id=8;
			INSERT INTO tags(id,name,kind,created_at) VALUES (3,'New','manual',0);
			INSERT INTO people(id,name,created_at) VALUES (3,'New',0)`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("marked inputs wrote kept rows: %v", err)
	}
	check(t, st.read(ctx, func(tx *sql.Tx) error { compareKeptCounts(ctx, t, tx); return nil }))
}

func TestKeptCountsDoNotRecountDuplicateOrUnchangedBulkCandidates(t *testing.T) {
	st := openTest(t)
	ctx := context.WithValue(t.Context(), keptCountsThresholdKey{}, int64(8))
	seedKeptCounts(t, st)
	recordKeptCountWrites(t, st)
	ids := make([]int64, 16)
	for i := range ids {
		ids[i] = 2
	}
	check(t, st.TrashAssets(ctx, ids))
	assertKeptCountPath(t, st, false)
	check(t, st.TrashAssets(ctx, ids))
	refuseKeptCountWrites(t, st, "day_counts")
	check(t, st.TrashAssets(ctx, ids))
	check(t, st.tx(ctx, func(tx *sql.Tx) error { return markOrphans(ctx, tx, ids, 1) }))
}

func TestKeptCountsMigrationFillsAnExistingLibraryAndCanBeReplayed(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open(driverName, path+"?_foreign_keys=1")
	check(t, err)
	t.Cleanup(func() { _ = db.Close() })
	names, err := migrationFS.ReadDir("migrations")
	check(t, err)
	for _, name := range names {
		if name.Name() >= "0037" {
			break
		}
		body, err := migrationFS.ReadFile("migrations/" + name.Name())
		check(t, err)
		check(t, errOf(db.ExecContext(ctx, string(body))))
	}
	check(t, errOf(db.ExecContext(ctx, `INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0);
		INSERT INTO tags(id, name, kind, created_at) VALUES (1, 'Old', 'manual', 0), (2, 'Empty', 'manual', 0);
		INSERT INTO people(id, name, created_at) VALUES (1, 'Old', 0), (2, 'Empty', 0);
		WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<16)
		INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, thumb_version, width, height, favourite)
		SELECT x, printf('%016x', x), CASE WHEN x%3=0 THEN 'video' ELSE 'photo' END, '.jpg', 1700000000+x*172800, 0, 1, 100, 100, x%2 FROM n;
		INSERT INTO files(asset_id, source_id, path, size, mtime) SELECT id, 1, 'C:\Photos\'||id||'.jpg', 1, 0 FROM assets;
		INSERT INTO asset_tags(asset_id, tag_id, source, score) SELECT id, 1, CASE WHEN id%2=0 THEN 'user' ELSE 'ai' END, 0.03 FROM assets;
		INSERT INTO faces(id, asset_id, person_id, x, y, w, h, score, sharpness, yaw, model, vec)
		SELECT id AS face_id, id AS asset_id, 1, 0, 0, 0.2, 0.2, 1, 20, 0, 'old', zeroblob(512) FROM assets;
		INSERT INTO faces(id, asset_id, person_id, x, y, w, h, score, sharpness, yaw, model, vec)
		VALUES (100, 1, 1, 0.3, 0, 0.2, 0.2, 1, 20, 0, 'old', zeroblob(512));
		INSERT INTO asset_jobs(asset_id, stage, status, kind, updated_at)
		SELECT id, stage, CASE (id+ordinal)%5 WHEN 0 THEN 'pending' WHEN 1 THEN 'waiting' WHEN 2 THEN 'done' WHEN 3 THEN 'error' ELSE 'skipped' END, kind, 0
		FROM assets CROSS JOIN (SELECT 'metadata' stage, 0 ordinal UNION ALL SELECT 'thumbnail', 1 UNION ALL SELECT 'embedding', 2 UNION ALL SELECT 'faces', 3);
		INSERT INTO settings(key, value) VALUES ('tag_confidence', '0.025');
		INSERT INTO blocks(kind, target_id, mode, name, created_at) VALUES ('asset', 2, 'photos', 'Old', 0);
		UPDATE assets SET hidden = 1 WHERE id=4;
		UPDATE assets SET trashed_at = 1 WHERE id=5;
		UPDATE assets SET missing_since = 1 WHERE id=6;
		PRAGMA user_version = 36`)))
	check(t, db.Close())
	st, err := Open(path)
	check(t, err)
	t.Cleanup(func() { check(t, st.Close()) })
	check(t, st.read(ctx, func(tx *sql.Tx) error { compareKeptCounts(ctx, t, tx); return nil }))
	body, err := migrationFS.ReadFile("migrations/0037_kept_counts.sql")
	check(t, err)
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return err
		}
		var marks int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM count_rebuild").Scan(&marks); err != nil {
			return err
		}
		if marks != 4 {
			t.Fatalf("migration marked %d tables, want four", marks)
		}
		_, err := tx.ExecContext(ctx, `UPDATE assets SET hidden=1-hidden, favourite=1-favourite;
			UPDATE settings SET value='0.04' WHERE key='tag_confidence'`)
		return err
	}))
	check(t, st.read(ctx, func(tx *sql.Tx) error { compareKeptCounts(ctx, t, tx); return nil }))
	writeInTx(t, st, "DELETE FROM faces WHERE id IN (1,100); DELETE FROM assets WHERE id=2")
	check(t, st.read(ctx, func(tx *sql.Tx) error { compareKeptCounts(ctx, t, tx); return nil }))
}

func TestKeptCountTriggersUseTheCoreConfidenceDefault(t *testing.T) {
	body, err := migrationFS.ReadFile("migrations/0037_kept_counts.sql")
	check(t, err)
	defaults := regexp.MustCompile(`COALESCE\(\(SELECT CAST\(value AS REAL\) FROM settings WHERE key = 'tag_confidence'\), ([0-9.eE+-]+)\)`).FindAllStringSubmatch(string(body), -1)
	if len(defaults) == 0 {
		t.Fatal("confidence defaults were not found")
	}
	for _, match := range defaults {
		value, err := strconv.ParseFloat(match[1], 64)
		check(t, err)
		if value != core.TagConfidenceDefault {
			t.Fatalf("migration default %g, core default %g", value, core.TagConfidenceDefault)
		}
	}
}

func TestFailedKeptCountSettlementRollsBackInputsAndMarkers(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedKeptCounts(t, st)
	refuseKeptCountWrites(t, st, "tag_counts")
	err := st.tx(ctx, func(tx *sql.Tx) error {
		if err := markKeptCountsStale(ctx, tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = 1")
		return err
	})
	if err == nil {
		t.Fatal("failed recount committed its inputs")
	}
	var hidden bool
	check(t, st.r.QueryRowContext(ctx, "SELECT hidden FROM assets WHERE id=1").Scan(&hidden))
	if hidden {
		t.Fatal("failed recount left its input changed")
	}
	check(t, st.read(ctx, func(tx *sql.Tx) error { compareKeptCounts(ctx, t, tx); return nil }))
	// Cancellation must also roll back the marker that would silence triggers.
	cancelled, cancel := context.WithCancel(ctx)
	err = st.tx(cancelled, func(tx *sql.Tx) error {
		if err := markKeptCountsStale(cancelled, tx); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled settlement: %v", err)
	}
}
