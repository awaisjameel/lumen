package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"lumen/internal/core"
)

// FileStat is what the scanner compares to decide whether a file changed.
type FileStat struct {
	ID    int64
	Size  int64
	MTime int64
}

// KnownFiles returns the files recorded for a source, keyed by core.Fold of
// their path (one row per file in any case: files_path_fold). If under is not
// empty, only files inside that folder are returned.
func (s *Store) KnownFiles(ctx context.Context, sourceID int64, under string) (known map[string]FileStat, err error) {
	q := "SELECT id, path, size, mtime FROM files WHERE source_id = ?"
	args := []any{sourceID}
	var prefix string
	if under != "" {
		dir := strings.TrimRight(filepath.Clean(under), `\/`)
		prefix = core.Fold(dir + `\`)
		// A range scan instead of LIKE: every path inside dir sorts between
		// dir+`\` and dir+`]` (']' is the character after '\'). NOCASE folds
		// only A-Z, so a folder named in other letters is filtered in Go.
		// +source_id keeps SQLite on the path range rather than reading every
		// file of the source through files_source.
		if isASCII(dir) {
			q = "SELECT id, path, size, mtime FROM files WHERE +source_id = ? AND path >= ? AND path < ?"
			args = append(args, dir+`\`, dir+`]`)
		}
	}
	rows, err := s.r.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	known = make(map[string]FileStat)
	for rows.Next() {
		var st FileStat
		var path string
		if err := rows.Scan(&st.ID, &path, &st.Size, &st.MTime); err != nil {
			return nil, err
		}
		if key := core.Fold(path); strings.HasPrefix(key, prefix) {
			known[key] = st
		}
	}
	return known, rows.Err()
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// IngestItem is one new or changed file, already hashed by the scanner.
type IngestItem struct {
	SourceID int64
	Path     string
	Size     int64
	MTime    int64 // unix nanoseconds
	Hash     []byte
	Kind     core.Kind
	Ext      string
	TakenAt  int64           // best guess until the metadata stage reads EXIF
	DateFrom core.DateSource // DateFromFilename or DateFromFile
}

// Ingest records a batch of files in one transaction. A file whose content is
// already known joins the existing asset (a move, rename or duplicate), so no
// work is repeated. New content creates an asset plus pending jobs for every
// stage. It returns how many new assets were created.
func (s *Store) Ingest(ctx context.Context, items []IngestItem) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	created := 0
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().Unix()
		findAsset, err := tx.PrepareContext(ctx, "SELECT id FROM assets WHERE content_hash = ?")
		if err != nil {
			return err
		}
		defer findAsset.Close()
		revive, err := tx.PrepareContext(ctx, "UPDATE assets SET missing_since = NULL WHERE id = ? AND missing_since IS NOT NULL")
		if err != nil {
			return err
		}
		defer revive.Close()
		// A file found again (moved, or back on a drive plugged in again) may
		// be readable now where its jobs failed.
		retry, err := tx.PrepareContext(ctx, `UPDATE asset_jobs SET status = 'pending', error = NULL, updated_at = ?
			WHERE asset_id = ? AND status = 'error' AND attempts < ?`)
		if err != nil {
			return err
		}
		defer retry.Close()
		settleAsset, err := tx.PrepareContext(ctx, settleSQL+"asset_id = ?")
		if err != nil {
			return err
		}
		defer settleAsset.Close()
		// Until EXIF is read, the best date guess across all copies wins, whatever
		// order they were found in: a date in a file name beats a modified time,
		// and an older modified time beats a newer one (copies are usually newer).
		betterGuess, err := tx.PrepareContext(ctx, `UPDATE assets SET taken_at = ?1, date_from = ?2
			WHERE id = ?3 AND date_from = 'file' AND (?2 = 'filename' OR ?1 < taken_at)`)
		if err != nil {
			return err
		}
		defer betterGuess.Close()
		insAsset, err := tx.PrepareContext(ctx, `INSERT INTO assets (content_hash, kind, ext, taken_at, date_from, added_at)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insAsset.Close()
		// Jobs start with the asset's capture time (see migration 0007), and
		// waiting if an earlier stage has to run first.
		insJob, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO asset_jobs (asset_id, stage, status, updated_at, taken_at, kind)
			VALUES (?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer insJob.Close()
		// A file is the same file in any case, as Windows treats "ÉTÉ" and
		// "été" as one name: rows are found by their folded path (the
		// files_path_fold index), and an update also stores the new spelling.
		oldFile, err := tx.PrepareContext(ctx, "SELECT id, asset_id FROM files WHERE "+foldPath+" = "+foldKey("?1"))
		if err != nil {
			return err
		}
		defer oldFile.Close()
		updFile, err := tx.PrepareContext(ctx, "UPDATE files SET asset_id = ?, source_id = ?, path = ?, size = ?, mtime = ? WHERE id = ?")
		if err != nil {
			return err
		}
		defer updFile.Close()
		insFile, err := tx.PrepareContext(ctx, "INSERT INTO files (asset_id, source_id, path, size, mtime) VALUES (?, ?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer insFile.Close()

		var orphanCandidates []int64
		for _, it := range items {
			var assetID int64
			switch err := findAsset.QueryRowContext(ctx, it.Hash).Scan(&assetID); {
			case err == nil:
				if _, err := revive.ExecContext(ctx, assetID); err != nil {
					return err
				}
				res, err := retry.ExecContext(ctx, now, assetID, MaxAttempts)
				if err != nil {
					return err
				}
				if n, _ := res.RowsAffected(); n > 0 {
					if _, err := settleAsset.ExecContext(ctx, assetID); err != nil {
						return err
					}
				}
				if _, err := betterGuess.ExecContext(ctx, it.TakenAt, it.DateFrom, assetID); err != nil {
					return err
				}
			case errors.Is(err, sql.ErrNoRows):
				res, err := insAsset.ExecContext(ctx, it.Hash, it.Kind, strings.ToLower(it.Ext), it.TakenAt, it.DateFrom, now)
				if err != nil {
					return err
				}
				assetID, _ = res.LastInsertId()
				for _, st := range core.Stages {
					status := core.JobPending
					if len(waitsFor[st]) > 0 {
						status = core.JobWaiting
					}
					if _, err := insJob.ExecContext(ctx, assetID, st, status, now, it.TakenAt, it.Kind); err != nil {
						return err
					}
				}
				created++
			default:
				return err
			}

			var fileID, prev int64
			switch err := oldFile.QueryRowContext(ctx, it.Path).Scan(&fileID, &prev); {
			case err == nil:
				if prev != assetID {
					orphanCandidates = append(orphanCandidates, prev) // the file's content changed
				}
				if _, err := updFile.ExecContext(ctx, assetID, it.SourceID, it.Path, it.Size, it.MTime, fileID); err != nil {
					return err
				}
			case errors.Is(err, sql.ErrNoRows):
				if _, err := insFile.ExecContext(ctx, assetID, it.SourceID, it.Path, it.Size, it.MTime); err != nil {
					return err
				}
			default:
				return err
			}
		}
		return markOrphans(ctx, tx, orphanCandidates, now)
	})
	return created, err
}

// RemoveFiles forgets files that are gone from disk.
func (s *Store) RemoveFiles(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		sel, err := tx.PrepareContext(ctx, "SELECT asset_id FROM files WHERE id = ?")
		if err != nil {
			return err
		}
		defer sel.Close()
		del, err := tx.PrepareContext(ctx, "DELETE FROM files WHERE id = ?")
		if err != nil {
			return err
		}
		defer del.Close()
		var assets []int64
		for _, id := range ids {
			var a int64
			switch err := sel.QueryRowContext(ctx, id).Scan(&a); {
			case err == nil:
				assets = append(assets, a)
			case !errors.Is(err, sql.ErrNoRows):
				return err
			}
			if _, err := del.ExecContext(ctx, id); err != nil {
				return err
			}
		}
		return markOrphans(ctx, tx, assets, time.Now().Unix())
	})
}

// markOrphans flags assets that no file points to any more. They drop out of
// the grid but keep their data, and come back if the content reappears.
func markOrphans(ctx context.Context, tx *sql.Tx, assetIDs []int64, now int64) error {
	if len(assetIDs) == 0 {
		return nil
	}
	list, err := json.Marshal(assetIDs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE assets SET missing_since = ?
		WHERE id IN (SELECT value FROM json_each(?)) AND missing_since IS NULL
		AND NOT EXISTS (SELECT 1 FROM files WHERE asset_id = assets.id)`, now, string(list))
	return err
}
