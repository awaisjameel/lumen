package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestBackupIncludesCommittedWALSettingsAndRefusesOverwrite(t *testing.T) {
	s := openTest(t)
	if err := s.SetBool(t.Context(), "backup-test", true); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(t.TempDir(), "recovery.db")
	if err := s.Backup(t.Context(), name); err != nil {
		t.Fatal(err)
	}
	backup, err := Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = backup.Close() }()
	on, err := backup.Bool(t.Context(), "backup-test", false)
	if err != nil || !on {
		t.Fatalf("WAL setting absent: %v %v", on, err)
	}
	if err := s.Backup(t.Context(), name); err == nil {
		t.Fatal("existing recovery copy overwritten")
	}
}

func TestBootstrapWriteHoldKeepsReadsAvailableAndReleasesWaitingWrites(t *testing.T) {
	s := openTest(t)
	release, err := s.HoldWrites(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Integrity(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := s.SetBool(ctx, "blocked-before-health", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write admitted before health: %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBool(t.Context(), "after-health", true); err != nil {
		t.Fatal(err)
	}
}

func TestPairedBackupRestorationIsRepeatableBeforeLibraryOpen(t *testing.T) {
	root := t.TempDir()
	source, err := Open(filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := source.SetBool(t.Context(), "before-update", true); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(root, "paired.db")
	if err := source.Backup(t.Context(), backup); err != nil {
		t.Fatal(err)
	}
	if err := source.SetBool(t.Context(), "after-update", true); err != nil {
		t.Fatal(err)
	}
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RestoreBackup(t.Context(), backup, filepath.Join(root, "library.db")); err != nil {
			t.Fatal(err)
		}
	}
	restored, err := Open(filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restored.Close() }()
	before, err := restored.Bool(t.Context(), "before-update", false)
	if err != nil || !before {
		t.Fatal("paired state missing", err)
	}
	after, err := restored.Bool(t.Context(), "after-update", false)
	if err != nil || after {
		t.Fatal("newer state survived pre-health recovery", err)
	}
}
