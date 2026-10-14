package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedOptimize(t *testing.T, st *Store) {
	t.Helper()
	check(t, st.tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<1000)
   INSERT INTO assets(id,content_hash,kind,ext,taken_at,added_at) SELECT x,printf('%016x',x),'photo','.jpg',x*86400,0 FROM n`)
		return err
	}))
}

func TestOptimizeCreatesStatisticsBeforeTheLibraryCloses(t *testing.T) {
	st := openTest(t)
	seedOptimize(t, st)
	var before int
	check(t, st.r.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name='sqlite_stat1'").Scan(&before))
	if before != 0 {
		t.Fatal("fixture already has statistics")
	}
	completed, err := st.Optimize(t.Context())
	check(t, err)
	var after int
	check(t, st.r.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_stat1 WHERE tbl='assets'").Scan(&after))
	if !completed || after == 0 {
		t.Fatal("optimisation left the open library without statistics")
	}
	var limit int
	check(t, st.w.QueryRowContext(t.Context(), "PRAGMA analysis_limit").Scan(&limit))
	if limit != 0 {
		t.Fatalf("maintenance changed the writer analysis limit: %d", limit)
	}
}

func TestOptimizeYieldsToAnActiveWriterAndReportsCancellation(t *testing.T) {
	st := openTest(t)
	seedOptimize(t, st)
	writer, err := st.w.BeginTx(t.Context(), nil)
	check(t, err)
	defer func() { _ = writer.Rollback() }()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	completed, err := st.Optimize(ctx)
	check(t, err)
	if completed {
		t.Fatal("optimisation did not yield to the active writer")
	}
	check(t, writer.Rollback())
	completed, err = st.Optimize(t.Context())
	check(t, err)
	if !completed {
		t.Fatal("optimisation did not retry after the writer finished")
	}
	cancelled, stop := context.WithCancel(t.Context())
	stop()
	_, err = st.Optimize(cancelled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled optimisation=%v", err)
	}
	t.Logf("busy optimisation and retry=%s; SQLite busy wait is zero", time.Since(started))
}

func TestOptimizeKeepsReaderSnapshotsAndTheLiteralLibraryPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "literal # % folder")
	check(t, os.Mkdir(root, 0o750))
	name := filepath.Join(root, "library.db")
	st, err := Open(name)
	check(t, err)
	defer func() { _ = st.Close() }()
	seedOptimize(t, st)
	reader, err := st.r.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	check(t, err)
	defer func() { _ = reader.Rollback() }()
	var n int
	check(t, reader.QueryRowContext(t.Context(), "SELECT count(*) FROM assets").Scan(&n))
	completed, err := st.Optimize(t.Context())
	check(t, err)
	if !completed {
		t.Fatal("reader prevented optimisation in WAL mode")
	}
	check(t, st.tx(t.Context(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(t.Context(), "DELETE FROM assets WHERE id=1")
		return err
	}))
	check(t, reader.QueryRowContext(t.Context(), "SELECT count(*) FROM assets").Scan(&n))
	if n != 1000 {
		t.Fatalf("reader lost its snapshot: %d", n)
	}
	check(t, reader.Rollback())
	check(t, st.Close())
	check(t, os.Remove(name))
	if _, err := st.Optimize(t.Context()); err == nil {
		t.Fatal("closed library was optimised")
	}
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing library changed: %v", err)
	}
}
