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
	"time"

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
	w               *storeWriter // single writer connection, with settlement for standalone writes
	r               *sql.DB      // read-only pool
	duplicatesCache duplicateCache
	eventsCache     struct {
		sync.Mutex
		revision      int64
		coverRevision int64
		gap           int
		loaded        bool
		items         []core.Event
	}

	// settings caches every setting read or written: only the Store writes
	// them, and some (the tag confidence) are read on every scroll. settingsMu
	// orders each write with its cache update, so the cache ends as the
	// database does whichever of two writes commits last.
	settings   sync.Map // key -> string, or nil for a setting that has never been set
	settingsMu sync.Mutex
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
	s := &Store{}
	s.w = &storeWriter{DB: w, execContext: s.execContext}
	if err := s.migrate(context.Background()); err != nil {
		w.Close()
		return nil, err
	}
	// A migrated library must be backfilled before any reader can browse it.
	if err := s.tx(context.Background(), func(*sql.Tx) error { return nil }); err != nil {
		w.Close()
		return nil, fmt.Errorf("backfill derived tables: %w", err)
	}

	r, err := sql.Open(driverName, dsn("&_query_only=1"))
	if err != nil {
		w.Close()
		return nil, fmt.Errorf("open read pool: %w", err)
	}
	// Keep concurrency for browsing alongside the stages, but retain only four
	// idle caches. Expire them between the indexer's 30-second idle queue polls.
	readers := max(4, runtime.NumCPU()/2)
	r.SetMaxOpenConns(readers)
	r.SetMaxIdleConns(4)
	r.SetConnMaxIdleTime(15 * time.Second)
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

// Checkpoint truncates the WAL if no reader or writer currently pins it.
// Its separate connection never reserves the writer, and its zero busy timeout
// makes an occupied WAL a harmless no-op instead of waiting for a long reader.
func (s *Store) Checkpoint(ctx context.Context) (bool, error) {
	// SQLite names the file its readers hold open, so nothing is created: a
	// closed store fails here, and Windows keeps an open database in place.
	// The path is plain, as in Open, so it needs no escaping.
	var seq int
	var name, file string
	if err := s.r.QueryRowContext(ctx, "PRAGMA database_list").Scan(&seq, &name, &file); err != nil {
		return false, fmt.Errorf("checkpoint WAL: %w", err)
	}
	db, err := sql.Open(driverName, file+"?_busy_timeout=0&_cache_size=-2000")
	if err != nil {
		return false, err
	}
	defer func() { _ = db.Close() }()
	var busy, frames, copied int
	err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &frames, &copied)
	var se sqlite3.Error
	if errors.As(err, &se) && se.Code == sqlite3.ErrBusy {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checkpoint WAL: %w", err)
	}
	return busy == 0, nil
}

// SchemaVersion is the applied migration version, for a local problem report.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := s.r.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version)
	return version, err
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
		if err := s.runTx(ctx, false, func(tx *sql.Tx) error {
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

// tx runs fn in a write transaction, committing on success. The deferred
// rollback also runs if fn panics, so the single writer connection is never
// left inside a transaction (every later write would wait on it). Before it
// commits, the transaction settles event tags, blocked assets and counts, so
// the derived sets and kept counts agree with the photos on disk.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.runTx(ctx, true, fn)
}

// runTx is tx, with the settling optional: a migration must not settle,
// because the tables it needs may not exist yet.
func (s *Store) runTx(ctx context.Context, settle bool, fn func(*sql.Tx) error) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // after Commit: sql.ErrTxDone, ignored; else fn's error is the one that matters
	if err := fn(tx); err != nil {
		return err
	}
	if settle {
		if err := settleDirty(ctx, tx); err != nil {
			return fmt.Errorf("settle event tags: %w", err)
		}
		if err := settleBlockedAssets(ctx, tx); err != nil {
			return fmt.Errorf("settle blocked assets: %w", err)
		}
		if err := settleKeptCounts(ctx, tx); err != nil {
			return fmt.Errorf("settle kept counts: %w", err)
		}
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
//     People and Things): not hidden, trashed or missing, and not covered by
//     a block. Its first three terms are the WHERE clause of the
//     assets_timeline partial index; baseVisibleOf spells them the one way
//     SQLite must see them to use it.
//   - present: every asset with a file on disk, hidden, trashed or blocked
//     ones too. The indexer processes these (so a restored or unblocked
//     photo is ready at once).
const present = "missing_since IS NULL"

var (
	visible  = baseVisibleOf("") + " AND " + blockVisible("assets")
	visibleA = baseVisibleOf("a") + " AND " + blockVisible("a")
	visibleX = baseVisibleOf("x") + " AND " + blockVisible("x")
	// baseVisibleA is visibleA without blocks: face clustering must see a
	// blocked person's photos, so new faces of them are still recognised.
	baseVisibleA = baseVisibleOf("a")
)

// baseVisibleOf is the index's visibility clause for the assets row alias
// ("" for the bare table).
func baseVisibleOf(alias string) string {
	if alias != "" {
		alias += "."
	}
	return alias + "hidden = 0 AND " + alias + "trashed_at IS NULL AND " + alias + "missing_since IS NULL"
}

// blockVisible is true when no photo block covers the assets row alias.
func blockVisible(asset string) string {
	// An empty materialisation is common. Check it once per query so those
	// libraries need no asset-by-asset primary-key seeks.
	return "(NOT EXISTS (SELECT 1 FROM blocked_assets) OR NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = " + asset + ".id))"
}

// blockMatch is true when the blocks row alias blk covers the assets row alias
// asset, whatever the block's mode: visibility and the Blocked screen's
// browser ask the same question with the same SQL. Settlement evaluates current
// metadata, membership and file paths before commit, so newly indexed media is
// covered immediately; event and face blocks read the members that event
// settling and face saving keep current (migration 0023). Its own
// aliases (blk_*) never collide with the caller's.
//
// The revision triggers of migrations 0014-0016 and 0023 name these inputs.
// Materialisation's dirty triggers begin in migration 0032 and must cover
// every input read here too.
func blockMatch(asset, blk string) string {
	id := asset + ".id"
	takenDay := "strftime('%Y-%m-%d', " + asset + ".taken_at, 'unixepoch')"
	rule := func(field string) string { return "json_extract(blk_album.rule_json, '$." + field + "')" }
	return `(
		(` + blk + `.kind = 'asset' AND ` + blk + `.target_id = ` + id + `)
		OR (` + blk + `.kind = 'person' AND EXISTS (SELECT 1 FROM faces blk_face
			WHERE blk_face.asset_id = ` + id + ` AND blk_face.person_id = ` + blk + `.target_id))
		OR (` + blk + `.kind = 'tag' AND EXISTS (SELECT 1 FROM asset_tags blk_tag
			WHERE blk_tag.asset_id = ` + id + ` AND blk_tag.tag_id = ` + blk + `.target_id AND ` + tagAppliesTo("blk_tag", tagConfidenceSQL) + `))
		OR (` + blk + `.kind = 'place' AND ` + blk + `.target_id = ` + asset + `.place_id)
		OR (` + blk + `.kind = 'folder' AND EXISTS (SELECT 1 FROM files blk_file WHERE blk_file.asset_id = ` + id + ` AND (
			` + foldKey("blk_file.path") + ` = ` + foldKey(blk+".path") + `
			OR instr(` + foldKey("blk_file.path") + `, ` + foldKey(`rtrim(`+blk+`.path, '\') || '\'`) + `) = 1)))
		OR (` + blk + `.kind = 'album' AND EXISTS (SELECT 1 FROM albums blk_album WHERE blk_album.id = ` + blk + `.target_id AND (
			(blk_album.kind = 'manual' AND EXISTS (SELECT 1 FROM album_items blk_item
				WHERE blk_item.asset_id = ` + id + ` AND blk_item.album_id = blk_album.id))
			OR (blk_album.kind = 'smart'
				AND (COALESCE(` + rule("kind") + `, '') = '' OR ` + rule("kind") + ` = ` + asset + `.kind)
				AND (COALESCE(` + rule("favourites") + `, 0) = 0 OR ` + asset + `.favourite = 1)
				AND (COALESCE(` + rule("from") + `, '') = '' OR ` + takenDay + ` >= ` + rule("from") + `)
				AND (COALESCE(` + rule("to") + `, '') = '' OR ` + takenDay + ` <= ` + rule("to") + `)
				AND (COALESCE(` + rule("tag") + `, 0) = 0 OR EXISTS (SELECT 1 FROM asset_tags blk_rule_tag
					WHERE blk_rule_tag.asset_id = ` + id + ` AND blk_rule_tag.tag_id = ` + rule("tag") + `
					AND ` + tagAppliesTo("blk_rule_tag", tagConfidenceSQL) + `))
				AND (COALESCE(` + rule("person") + `, 0) = 0 OR EXISTS (SELECT 1 FROM faces blk_rule_face
					WHERE blk_rule_face.asset_id = ` + id + ` AND blk_rule_face.person_id = ` + rule("person") + `
					AND NOT EXISTS (SELECT 1 FROM face_block_faces blk_rule_cover WHERE blk_rule_cover.face_id = blk_rule_face.id)))
			)
		)))
		OR (` + blk + `.kind = 'event' AND EXISTS (SELECT 1 FROM event_members blk_event
			WHERE blk_event.tag_id = ` + blk + `.target_id AND blk_event.asset_id = ` + id + `))
		OR (` + blk + `.kind = 'face' AND EXISTS (SELECT 1 FROM face_block_faces blk_face_block
			WHERE blk_face_block.asset_id = ` + id + ` AND blk_face_block.face_block_id = ` + blk + `.target_id))
	)`
}

// tagAppliesTo is the condition for the asset_tags row alias to count: the
// user put it there, or the AI did with at least threshold (an SQL
// expression) confidence.
func tagAppliesTo(alias, threshold string) string {
	return "(" + alias + ".source = 'user' OR (" + alias + ".source = 'ai' AND " + alias + ".score >= " + threshold + "))"
}

// tagApplies is tagAppliesTo for alias x, with the confidence as an argument.
// Callers pass the setting as read by tagConfidence (a float64, as SQLite
// stores it, so both spellings of the threshold agree to the last bit).
var tagApplies = tagAppliesTo("x", "?")

// tagConfidenceSQL is the user's tag confidence, for predicates that cannot
// take it as an argument (the block predicate is part of visible).
var tagConfidenceSQL = "COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = '" + core.SettingTagConfidence + "'), " +
	strconv.FormatFloat(core.TagConfidenceDefault, 'g', -1, 64) + ")"

// foldPath is files.path's fold key, the expression of the unique index
// files_path_fold (migration 0018): queries must use it exactly for SQLite
// to use that index.
var foldPath = foldKey("path")

// foldKey is the SQL for core.Fold(expr). fold() is a call into Go for every
// row, so ASCII text (a byte per character) and NULL use SQLite's lower(),
// which is what core.Fold does to them.
func foldKey(expr string) string {
	return "(CASE WHEN octet_length(" + expr + ") != length(" + expr + ") THEN fold(" + expr + ") ELSE lower(" + expr + ") END)"
}

// What a smart album's rule selects by (albums.rule_json holds a core.AlbumRule).
const (
	ruleTag    = "json_extract(rule_json, '$.tag')"
	rulePerson = "json_extract(rule_json, '$.person')"
)

// setting reads a raw setting value; ok is false if it has never been set.
func (s *Store) setting(ctx context.Context, key string) (v string, ok bool, err error) {
	if cached, loaded := s.settings.Load(key); loaded {
		if cached == nil {
			return "", false, nil
		}
		return cached.(string), true, nil // only strings and nil are stored
	}
	err = s.r.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	var value any = v
	if errors.Is(err, sql.ErrNoRows) {
		value = nil
	} else if err != nil {
		return "", false, err
	}
	// A write that raced this read has cached the newer value: keep it, even
	// when the read's snapshot held no setting. Cache absence, not its default.
	cached, _ := s.settings.LoadOrStore(key, value)
	if cached == nil {
		return "", false, nil
	}
	return cached.(string), true, nil
}

func (s *Store) setSetting(ctx context.Context, key, value string) error {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
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
