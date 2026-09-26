// Package store owns the SQLite database: schema, migrations and every query.
// No other package writes SQL.
//
// The database runs in WAL mode with two handles: a single-connection writer,
// so writes are serialised without "database is locked" errors, and a small
// read-only pool for the UI, so browsing never waits for the indexer. Callers
// batch their writes (hundreds of rows per transaction), never row by row.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store is the handle every other package uses.
type Store struct {
	w *sql.DB // single writer connection
	r *sql.DB // read-only pool
}

// Open opens (creating if needed) and migrates the database at path.
func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	// A plain path (not a file: URI) so spaces in the user's profile path need
	// no escaping; go-sqlite3 strips everything after '?' as driver options.
	dsn := func(extra string) string {
		return abs + "?_journal_mode=WAL&_busy_timeout=10000&_foreign_keys=1&_synchronous=NORMAL&_cache_size=-32000" + extra
	}

	w, err := sql.Open("sqlite3", dsn("&_txlock=immediate"))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	w.SetMaxOpenConns(1)
	if err := w.Ping(); err != nil {
		w.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{w: w}
	if err := s.migrate(context.Background()); err != nil {
		w.Close()
		return nil, err
	}

	r, err := sql.Open("sqlite3", dsn("&_query_only=1"))
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	r.SetMaxOpenConns(max(4, runtime.NumCPU()/2))
	s.r = r
	return s, nil
}

// Close checkpoints and closes both handles.
func (s *Store) Close() error {
	if s.r != nil {
		s.r.Close()
	}
	s.w.Exec("PRAGMA optimize")
	return s.w.Close()
}

// migrate applies every embedded migration newer than PRAGMA user_version.
// Files are named NNNN_description.sql; NNNN becomes the schema version.
func (s *Store) migrate(ctx context.Context) error {
	var current int
	if err := s.w.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		base := filepath.Base(name)
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number", base)
		}
		if version <= current {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		if err := s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version))
			return err
		}); err != nil {
			return fmt.Errorf("migration %s: %w", base, err)
		}
		current = version
	}
	return nil
}

// tx runs fn in a write transaction, committing on success.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// visible is the WHERE clause of the assets_timeline partial index.
const visible = "hidden = 0 AND trashed_at IS NULL AND missing_since IS NULL"

// Bool returns a boolean setting, or def if it has never been set.
func (s *Store) Bool(ctx context.Context, key string, def bool) (bool, error) {
	var v string
	err := s.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return def, nil
	}
	return v == "1", err
}

// SetBool stores a boolean setting.
func (s *Store) SetBool(ctx context.Context, key string, v bool) error {
	value := "0"
	if v {
		value = "1"
	}
	_, err := s.w.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}
