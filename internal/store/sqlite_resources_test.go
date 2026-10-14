package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCheckpointTruncatesTheWALWithoutLosingCommittedData(t *testing.T) {
	name := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(name)
	check(t, err)
	defer func() { _ = st.Close() }()
	check(t, st.setSetting(t.Context(), "checkpoint-value", strings.Repeat("x", 128*1024)))
	before := sqliteFileSize(t, name+"-wal")
	if before == 0 {
		t.Fatal("fixture has no WAL pages")
	}
	start := time.Now()
	completed, err := st.Checkpoint(t.Context())
	check(t, err)
	after := sqliteFileSize(t, name+"-wal")
	t.Logf("WAL before=%d bytes after=%d bytes checkpoint=%s", before, after, time.Since(start))
	if !completed || after != 0 {
		t.Fatalf("WAL was not truncated: completed=%v bytes=%d", completed, after)
	}
	var value string
	check(t, st.r.QueryRowContext(t.Context(), "SELECT value FROM settings WHERE key = 'checkpoint-value'").Scan(&value))
	if value != strings.Repeat("x", 128*1024) {
		t.Fatal("checkpoint lost the committed value")
	}
}

func TestCheckpointDoesNotWaitForAReaderOrHoldUpTheWriter(t *testing.T) {
	name := filepath.Join(t.TempDir(), "library.db")
	st, err := Open(name)
	check(t, err)
	defer func() { _ = st.Close() }()
	ctx := t.Context()
	check(t, st.setSetting(ctx, "snapshot", "before"))
	reader, err := st.r.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	check(t, err)
	defer func() { _ = reader.Rollback() }()
	var value string
	check(t, reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'snapshot'").Scan(&value))
	check(t, st.setSetting(ctx, "snapshot", "after"))
	before := sqliteFileSize(t, name+"-wal")
	started, finished := make(chan struct{}), make(chan struct{})
	var completed bool
	var checkpointErr error
	at := time.Now()
	go func() { close(started); completed, checkpointErr = st.Checkpoint(ctx); close(finished) }()
	<-started
	writerCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	check(t, st.setSetting(writerCtx, "writer-during-checkpoint", "written"))
	writerTime := time.Since(at)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("checkpoint waited for the reader")
	}
	check(t, checkpointErr)
	if completed {
		t.Fatal("checkpoint truncated a reader's snapshot")
	}
	if size := sqliteFileSize(t, name+"-wal"); size < before {
		t.Fatalf("pinned WAL shrank from %d to %d bytes", before, size)
	}
	check(t, reader.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'snapshot'").Scan(&value))
	if value != "before" {
		t.Fatalf("reader lost its snapshot: %q", value)
	}
	t.Logf("pinned checkpoint=%s writer=%s; SQLite busy wait is zero, wall-clock ceiling is 1 s", time.Since(at), writerTime)
	check(t, reader.Rollback())
	completed, err = st.Checkpoint(ctx)
	check(t, err)
	if !completed || sqliteFileSize(t, name+"-wal") != 0 {
		t.Fatal("WAL did not truncate after the reader finished")
	}
}

func TestCheckpointYieldsToAnActiveWriterAndReportsCancellation(t *testing.T) {
	st := openTest(t)
	ctx := t.Context()
	writer, err := st.w.BeginTx(ctx, nil)
	check(t, err)
	defer func() { _ = writer.Rollback() }()
	maintenanceCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	completed, err := st.Checkpoint(maintenanceCtx)
	check(t, err)
	if completed {
		t.Fatal("checkpoint did not yield to the active writer")
	}
	check(t, writer.Rollback())
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = st.Checkpoint(cancelled)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled checkpoint = %v", err)
	}
}

func sqliteFileSize(t *testing.T, name string) int64 {
	t.Helper()
	info, err := os.Stat(name)
	check(t, err)
	return info.Size()
}

func TestTheReadPoolBoundsItsIdleConnectionCaches(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if readers := st.r.Stats().MaxOpenConnections; readers != max(4, runtime.NumCPU()/2) {
		t.Fatalf("active reader limit lost concurrency: %d", readers)
	}
	conns := make([]*sql.Conn, st.r.Stats().MaxOpenConnections)
	for i := range conns {
		conn, err := st.r.Conn(ctx)
		check(t, err)
		conns[i] = conn
		t.Cleanup(func() { _ = conn.Close() })
		var size, mapped int
		check(t, conn.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&size))
		check(t, conn.QueryRowContext(ctx, "PRAGMA mmap_size").Scan(&mapped))
		if size != -32000 || mapped != 0 {
			t.Fatalf("reader settings: cache=%d KiB mmap=%d bytes", size, mapped)
		}
	}
	for _, conn := range conns {
		check(t, conn.Close())
	}
	if idle := st.r.Stats().Idle; idle != 4 {
		t.Fatalf("idle readers = %d, want four", idle)
	}
	var size int
	check(t, st.w.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&size))
	if size != -32000 {
		t.Fatalf("writer cache = %d KiB", size)
	}
}

func TestIdleReadersCloseAndTheNextReadReopensThem(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	check(t, errOf(st.CountVisible(ctx)))
	if st.r.Stats().OpenConnections == 0 {
		t.Fatal("fixture has no idle reader")
	}
	deadline := time.NewTimer(25 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for st.r.Stats().OpenConnections != 0 {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatal("idle readers retained their caches until the next 30-second queue poll")
		case <-tick.C:
		}
	}
	if st.r.Stats().MaxIdleTimeClosed == 0 {
		t.Fatal("reader was not closed by its idle lifetime")
	}
	check(t, errOf(st.CountVisible(ctx)))
	if st.r.Stats().OpenConnections != 1 {
		t.Fatal("next read did not reopen a reader")
	}
}

func TestCheckpointOpensOnlyTheExistingLibraryAtItsLiteralPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "literal # % folder")
	check(t, os.Mkdir(root, 0o750))
	name := filepath.Join(root, "library.db")
	st, err := Open(name)
	check(t, err)
	defer func() { _ = st.Close() }()
	check(t, st.setSetting(t.Context(), "literal-checkpoint", "written"))
	completed, err := st.Checkpoint(t.Context())
	check(t, err)
	if !completed || sqliteFileSize(t, name+"-wal") != 0 {
		t.Fatal("literal path was not checkpointed")
	}
	check(t, st.Close())
	check(t, os.Remove(name))
	_, err = st.Checkpoint(t.Context())
	if err == nil {
		t.Fatal("checkpoint recreated a missing library")
	}
	if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing library changed: %v", err)
	}
}
