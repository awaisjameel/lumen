package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mattn/go-sqlite3"
)

// Backup takes a consistent online SQLite snapshot, including committed WAL pages.
// The destination must be new; an existing recovery copy is never overwritten.
func (s *Store) Backup(ctx context.Context, name string) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = os.Remove(name)
		}
	}()
	db, err := sql.Open(driverName, name+"?_journal_mode=DELETE&_synchronous=FULL")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	dst, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = dst.Close() }()
	src, err := s.r.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	err = src.Raw(func(source any) error {
		from, ok := source.(*sqlite3.SQLiteConn)
		if !ok {
			return errors.New("unexpected SQLite source driver")
		}
		return dst.Raw(func(destination any) error {
			to, ok := destination.(*sqlite3.SQLiteConn)
			if !ok {
				return errors.New("unexpected SQLite backup driver")
			}
			backup, err := to.Backup("main", from, "main")
			if err != nil {
				return err
			}
			// In steps, so that a cancelled backup stops; without a pause
			// between them, because nothing else is waiting for the library
			// when one is taken.
			for {
				if err := ctx.Err(); err != nil {
					_ = backup.Finish()
					return err
				}
				done, err := backup.Step(4096)
				if err != nil {
					_ = backup.Finish()
					return err
				}
				if done {
					return backup.Finish()
				}
			}
		})
	})
	if err != nil {
		return fmt.Errorf("backup library: %w", err)
	}
	var result string
	if err := dst.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("backup integrity: %s", result)
	}
	if err := dst.Close(); err != nil {
		return err
	}
	if err := db.Close(); err != nil {
		return err
	}
	file, err := os.OpenFile(name, os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	done = true
	return nil
}

// Integrity checks the opened library without changing records.
func (s *Store) Integrity(ctx context.Context) error {
	var result string
	if err := s.r.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("library integrity: %s", result)
	}
	return nil
}

// HoldWrites reserves the sole writer connection until bootstrap health is durable.
func (s *Store) HoldWrites(ctx context.Context) (func() error, error) {
	conn, err := s.w.Conn(ctx)
	if err != nil {
		return nil, err
	}
	return conn.Close, nil
}

// RestoreBackup runs only before opening a library, under an installation lease.
// An online SQLite copy replaces the closed database and its stale WAL together.
func RestoreBackup(ctx context.Context, backup, destination string) error {
	if !filepath.IsAbs(backup) || !filepath.IsAbs(destination) || backup == destination {
		return errors.New("invalid library recovery path")
	}
	// The driver drops "mode=ro" from a plain path, so opening a backup that
	// is not there would create it, empty, and restore that.
	if _, err := os.Stat(backup); err != nil {
		return err
	}
	source, err := sql.Open(driverName, backup+"?mode=ro&_query_only=1")
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	s := &Store{r: source}
	if err := s.Integrity(ctx); err != nil {
		return err
	}
	work, err := os.MkdirTemp(filepath.Dir(destination), ".lumen-recovery-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()
	temp := filepath.Join(work, "library.db")
	if err := s.Backup(ctx, temp); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(destination + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return os.Rename(temp, destination)
}
