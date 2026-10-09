//go:build windows

package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lumen/internal/core"
	"lumen/internal/fileops"
	"lumen/internal/store"
)

func TestCompressionKeepsVerifiedOutputAndSidecarStableUntilRegistration(t *testing.T) {
	ctx := t.Context()
	database := filepath.Join(t.TempDir(), "library.db")
	st, err := store.Open(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	photos := t.TempDir()
	source, err := st.AddSource(ctx, photos)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Ingest(ctx, []store.IngestItem{{SourceID: source.ID, Path: filepath.Join(photos, "original.jpg"), Hash: []byte("source"), Kind: core.KindPhoto, Ext: ".jpg", TakenAt: 1, DateFrom: core.DateFromFile}})
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "saved.jpg")
	for _, path := range []string{output, output + fileops.SidecarExt} {
		if err := os.WriteFile(path, []byte("verified bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest, size, err := fileops.FileDigest(ctx, output)
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.SaveCompressionPlan(ctx, core.DefaultCompressionOptions(), []core.CompressionJob{{AssetID: 1, To: output, State: core.CompressionCommitting}})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := st.CompressionJobs(ctx, run)
	if err != nil {
		t.Fatal(err)
	}
	jobs[0].OutputDigest, jobs[0].OutputSize = digest, size
	jobs[0].SidecarDigest, jobs[0].SidecarSize = digest, size
	if err := st.SetCompressionJob(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	// Occupy SQLite's writer so verification and registration cannot finish
	// together. This is fault injection against the scratch store only.
	locker, err := sql.Open("sqlite3_lumen", database+"?_txlock=immediate&_busy_timeout=10000")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locker.Close() }()
	transaction, err := locker.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback() }()
	done := make(chan error, 1)
	go func() { done <- (&fileops.CompressionPublisher{Store: st}).Settle(ctx, jobs[0]) }()
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-done:
		t.Fatalf("registration did not wait for the writer: %v", err)
	case <-timer.C:
	}
	for _, path := range []string{output, output + fileops.SidecarExt} {
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			_ = file.Close()
			t.Error("verified bytes became writable while registration was pending")
		}
	}
	if err := transaction.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("registration did not finish after releasing the writer")
	}
}
