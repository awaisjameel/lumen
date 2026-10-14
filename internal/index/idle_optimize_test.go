package index

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/store"
)

type idleEvents struct{ changed chan struct{} }

func (*idleEvents) Progress(core.Progress)        {}
func (e *idleEvents) LibraryChanged(core.Changes) { e.changed <- struct{}{} }

type idleLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *idleLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}
func (l *idleLog) text() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buf.String() }

func TestReportLoopOptimisesOnceWhenIndexingBecomesIdle(t *testing.T) {
	paths := core.PathsIn(t.TempDir())
	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SetInt(t.Context(), core.SettingCPUThreads, 2); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3_lumen", paths.DBPath+"?_query_only=1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	stats := func() int {
		var n int
		if err := db.QueryRowContext(t.Context(), "SELECT count(*) FROM sqlite_master WHERE name='sqlite_stat1'").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if stats() != 0 {
		t.Fatal("fixture already has statistics")
	}
	var logs idleLog
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	events := &idleEvents{changed: make(chan struct{}, 4)}
	ix := quiet(New(st, paths, nil, Models{}, events))
	ix.scanning = true
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); ix.reportLoop(ctx) }()
	defer func() { cancel(); <-done }()
	wait := func() {
		select {
		case <-events.changed:
		case <-time.After(10 * time.Second):
			t.Fatal("report loop did not report the change")
		}
	}
	ix.markChanged(core.Changes{Assets: true})
	wait()
	if stats() != 0 || strings.Contains(logs.text(), "optimise idle library") {
		t.Fatal("statistics ran during scanning")
	}
	ix.mu.Lock()
	ix.scanning = false
	ix.mu.Unlock()
	ix.markChanged(core.Changes{Assets: true})
	wait()
	if stats() != 1 || strings.Count(logs.text(), "optimise idle library") != 1 {
		t.Fatalf("idle statistics missing: %s", logs.text())
	}
	ix.markChanged(core.Changes{Assets: true})
	wait()
	if strings.Count(logs.text(), "optimise idle library") != 1 {
		t.Fatal("unchanged idle period repeated statistics maintenance")
	}
}
