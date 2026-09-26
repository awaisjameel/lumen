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

	"github.com/mattn/go-sqlite3"
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

// queryAll runs a read query and scans every row with scan. The result is
// never nil, so an empty list reaches the UI as [] rather than null.
func queryAll[T any](ctx context.Context, db *sql.DB, scan func(*sql.Rows) (T, error), q string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// isUnique reports whether err is a UNIQUE constraint violation.
func isUnique(err error) bool {
	var se sqlite3.Error
	return errors.As(err, &se) && se.ExtendedCode == sqlite3.ErrConstraintUnique
}

// visible is the WHERE clause of the assets_timeline partial index.
const visible = "hidden = 0 AND trashed_at IS NULL AND missing_since IS NULL"

// visibleAs is visible for the assets table under an alias, for joins.
func visibleAs(alias string) string {
	return alias + ".hidden = 0 AND " + alias + ".trashed_at IS NULL AND " + alias + ".missing_since IS NULL"
}

// setting reads a raw setting value; ok is false if it has never been set.
func (s *Store) setting(ctx context.Context, key string) (v string, ok bool, err error) {
	err = s.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) setSetting(ctx context.Context, key, value string) error {
	_, err := s.w.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// Bool returns a boolean setting, or def if it has never been set.
func (s *Store) Bool(ctx context.Context, key string, def bool) (bool, error) {
	v, ok, err := s.setting(ctx, key)
	if !ok {
		return def, err
	}
	return v == "1", nil
}

// SetBool stores a boolean setting.
func (s *Store) SetBool(ctx context.Context, key string, v bool) error {
	value := "0"
	if v {
		value = "1"
	}
	return s.setSetting(ctx, key, value)
}

// Float returns a number setting, or def if it has never been set (or is
// unreadable).
func (s *Store) Float(ctx context.Context, key string, def float64) (float64, error) {
	v, ok, err := s.setting(ctx, key)
	if !ok {
		return def, err
	}
	f, perr := strconv.ParseFloat(v, 64)
	if perr != nil {
		return def, nil
	}
	return f, nil
}

// SetFloat stores a number setting.
func (s *Store) SetFloat(ctx context.Context, key string, v float64) error {
	return s.setSetting(ctx, key, strconv.FormatFloat(v, 'g', -1, 64))
}

// Int returns a whole-number setting, or def if it has never been set (or
// is unreadable).
func (s *Store) Int(ctx context.Context, key string, def int) (int, error) {
	v, ok, err := s.setting(ctx, key)
	if !ok {
		return def, err
	}
	n, perr := strconv.Atoi(v)
	if perr != nil {
		return def, nil
	}
	return n, nil
}

// SetInt stores a whole-number setting.
func (s *Store) SetInt(ctx context.Context, key string, v int) error {
	return s.setSetting(ctx, key, strconv.Itoa(v))
}
