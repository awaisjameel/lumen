// Package store owns the SQLite database: schema, migrations and every query.
// No other package writes SQL.
//
// The database runs in WAL mode with two handles: a single-connection writer,
// so writes are serialised without "database is locked" errors, and a small
// read-only pool for the UI and the indexer's reads, so browsing never waits
// for indexing. Callers batch their writes (hundreds of rows per
// transaction), never row by row.
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
	"sync"

	"github.com/mattn/go-sqlite3"

	"lumen/internal/core"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// driverName is go-sqlite3 with Lumen's SQL functions on every connection:
// fold(text) is core.Fold, the case-insensitive key names and paths are
// compared by (SQLite's own NOCASE and LIKE fold only A-Z).
const driverName = "sqlite3_lumen"

func init() {
	sql.Register(driverName, &sqlite3.SQLiteDriver{
		ConnectHook: func(c *sqlite3.SQLiteConn) error {
			return c.RegisterFunc("fold", foldSQL, true)
		},
	})
}

// foldSQL is fold(x): core.Fold of text, NULL for NULL (an unnamed person).
func foldSQL(v any) any {
	switch x := v.(type) {
	case string:
		return core.Fold(x)
	case []byte:
		return core.Fold(string(x))
	}
	return nil
}

// Store is the handle every other package uses.
type Store struct {
	w           *sql.DB // single writer connection
	r           *sql.DB // read-only pool
	eventsCache struct {
		sync.Mutex
		revision      int64
		coverRevision int64
		gap           int
		loaded        bool
		items         []core.Event
	}

	// settings caches every setting read or written: only the Store writes
	// them, and some (the tag confidence) are read on every scroll.
	settings sync.Map // key -> string
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

	w, err := sql.Open(driverName, dsn("&_txlock=immediate"))
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	w.SetMaxOpenConns(1)
	if err := w.PingContext(context.Background()); err != nil {
		w.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	s := &Store{w: w}
	if err := s.migrate(context.Background()); err != nil {
		w.Close()
		return nil, err
	}

	r, err := sql.Open(driverName, dsn("&_query_only=1"))
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	// Idle connections stay open: each new one re-runs the pragmas and starts
	// with an empty page cache.
	readers := max(4, runtime.NumCPU()/2)
	r.SetMaxOpenConns(readers)
	r.SetMaxIdleConns(readers)
	s.r = r
	return s, nil
}

// Close checkpoints and closes both handles.
func (s *Store) Close() error {
	if s.r != nil {
		s.r.Close()
	}
	_, _ = s.w.Exec("PRAGMA optimize") //nolint:noctx // best effort while closing
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
	previous := 0
	for _, name := range names {
		base := filepath.Base(name)
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number", base)
		}
		if version == previous {
			return fmt.Errorf("migration %s: another migration has version %d", base, version)
		}
		previous = version
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

// read runs fn in a read-only transaction: every query in it sees the same
// snapshot of the database (WAL snapshot isolation), so counts and covers
// agree while the indexer writes.
func (s *Store) read(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.r.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // read-only: there is nothing to commit
	return fn(tx)
}

// tx runs fn in a write transaction, committing on success.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback() // fn's error is the one that matters
		return err
	}
	return tx.Commit()
}

// querier is a connection pool or a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// queryAll runs a read query and scans every row with scan. The result is
// never nil, so an empty list reaches the UI as [] rather than null.
func queryAll[T any](ctx context.Context, db querier, scan func(*sql.Rows) (T, error), q string, args ...any) ([]T, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	return collect(rows, err, scan)
}

// scanID scans a row holding one id.
func scanID(r *sql.Rows) (id int64, err error) {
	err = r.Scan(&id)
	return id, err
}

// collect scans every row of a query's result (rows, err) and closes it.
func collect[T any](rows *sql.Rows, err error, scan func(*sql.Rows) (T, error)) ([]T, error) {
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

// Which assets a query sees. Each query picks one on purpose:
//
//   - visible: what the user browses (grid, counts, search results, albums,
//     People and Things). It is the WHERE clause of the assets_timeline
//     partial index, and must be repeated exactly for SQLite to use it.
//   - present: every asset with a file on disk, hidden or trashed ones too.
//     The indexer processes these (so a restored photo is ready at once), and
//     their details and files can still be opened.
const (
	visible  = "hidden = 0 AND trashed_at IS NULL AND missing_since IS NULL"
	visibleA = "a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL" // assets joined as "a"
	present  = "missing_since IS NULL"
)

// What a smart album's rule selects by (albums.rule_json holds a core.AlbumRule).
const (
	ruleTag    = "json_extract(rule_json, '$.tag')"
	rulePerson = "json_extract(rule_json, '$.person')"
)

// setting reads a raw setting value; ok is false if it has never been set.
func (s *Store) setting(ctx context.Context, key string) (v string, ok bool, err error) {
	if v, ok := s.settings.Load(key); ok {
		return v.(string), true, nil // only strings are stored
	}
	err = s.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	s.settings.Store(key, v)
	return v, true, nil
}

func (s *Store) setSetting(ctx context.Context, key, value string) error {
	_, err := s.w.ExecContext(ctx,
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	if err == nil {
		s.settings.Store(key, value)
	}
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
	if f, perr := strconv.ParseFloat(v, 64); perr == nil {
		return f, nil
	}
	return def, nil // unreadable: as if never set
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
	if n, perr := strconv.Atoi(v); perr == nil {
		return n, nil
	}
	return def, nil // unreadable: as if never set
}

// SetInt stores a whole-number setting.
func (s *Store) SetInt(ctx context.Context, key string, v int) error {
	return s.setSetting(ctx, key, strconv.Itoa(v))
}
