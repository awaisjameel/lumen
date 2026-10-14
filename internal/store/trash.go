package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// TrashedFile is one file of an asset in the Trash, for moving it to the
// Recycle Bin.
type TrashedFile struct {
	ID      int64 // the files row, for RemoveFiles once the file is gone
	AssetID int64
	Path    string
}

// TrashAssets moves present assets to the Trash. Their files are untouched;
// the assets leave every view the shared visibility predicate feeds
// (migration 0016's trigger moves the visibility revision). Trashing an asset
// that is already there changes nothing. If any id names no present asset the
// call fails with ErrNotFound and trashes none.
func (s *Store) TrashAssets(ctx context.Context, ids []int64) error {
	return s.setTrashed(ctx, ids, true)
}

// RestoreAssets takes assets out of the Trash, whether or not their files are
// still on disk. Restoring one that is not there changes nothing; an id that
// names no asset fails the call with ErrNotFound and restores none.
func (s *Store) RestoreAssets(ctx context.Context, ids []int64) error {
	return s.setTrashed(ctx, ids, false)
}

func (s *Store) setTrashed(ctx context.Context, ids []int64, trash bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error { return setTrashed(ctx, tx, ids, trash) })
}

func setTrashed(ctx context.Context, tx *sql.Tx, ids []int64, trash bool) error {
	if len(ids) == 0 {
		return nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	// Trashing needs a file on disk (present); a restore only needs the row,
	// so an asset whose file was recycled can leave the Trash too.
	exists, change, update, args := "1", "trashed_at IS NOT NULL", "UPDATE assets SET trashed_at = NULL WHERE id IN (SELECT value FROM json_each(?)) AND trashed_at IS NOT NULL",
		[]any{string(list)}
	if trash {
		exists = present
		change = "trashed_at IS NULL AND " + present
		update = "UPDATE assets SET trashed_at = ? WHERE id IN (SELECT value FROM json_each(?)) AND trashed_at IS NULL AND " + present
		args = []any{time.Now().Unix(), string(list)}
	}
	if err := requireAll(ctx, tx, exists, string(list)); err != nil {
		return err
	}
	if int64(len(ids)) >= keptCountsLimit(ctx) {
		var changed int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+change+" AND id IN (SELECT value FROM json_each(?))", string(list)).Scan(&changed); err != nil {
			return err
		}
		if err := markKeptCountsForChange(ctx, tx, changed); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, update, args...)
	return err
}

// requireAll fails with ErrNotFound unless every id of the JSON list names an
// asset matching the condition.
func requireAll(ctx context.Context, q querier, condition, list string) error {
	var distinct, matching int
	if err := q.QueryRowContext(ctx, "SELECT count(DISTINCT value) FROM json_each(?)", list).Scan(&distinct); err != nil {
		return err
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE ("+condition+") AND id IN (SELECT value FROM json_each(?))",
		list).Scan(&matching); err != nil {
		return err
	}
	if matching != distinct {
		return ErrNotFound
	}
	return nil
}

// RestoreAllTrash takes everything the Trash shows out of it. Assets whose
// files were recycled stay marked: should the file come back from the
// Recycle Bin it returns to the Trash, not the library.
func (s *Store) RestoreAllTrash(ctx context.Context) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var changed int64
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+trashed).Scan(&changed); err != nil {
			return err
		}
		if err := markKeptCountsForChange(ctx, tx, changed); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE assets SET trashed_at = NULL WHERE "+trashed)
		return err
	})
}

// RemoveRecycledFiles forgets files that were moved to the Recycle Bin, so
// their assets become missing and keep their names, tags and albums (as
// RemoveFiles does). A row goes only if it still holds the path that was
// recycled: the scanner may have dropped it meanwhile, and SQLite may have
// given its id to another file.
func (s *Store) RemoveRecycledFiles(ctx context.Context, files []TrashedFile) error {
	if len(files) == 0 {
		return nil
	}
	type pair struct {
		ID   int64  `json:"id"`
		Path string `json:"path"`
	}
	pairs := make([]pair, len(files))
	for i, f := range files {
		pairs[i] = pair{f.ID, f.Path}
	}
	list, err := json.Marshal(pairs)
	if err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		// The id list picks the rows by primary key; the path check runs only on those.
		assets, err := queryAll(ctx, tx, scanID, `DELETE FROM files
			WHERE id IN (SELECT json_extract(value, '$.id') FROM json_each(?1)) AND EXISTS (SELECT 1 FROM json_each(?1) j
			WHERE json_extract(j.value, '$.id') = files.id AND `+foldKey("json_extract(j.value, '$.path')")+` = `+foldKey("files.path")+`) RETURNING asset_id`, string(list))
		if err != nil {
			return err
		}
		return markOrphans(ctx, tx, assets, time.Now().Unix())
	})
}

// TrashCount is how many assets the Trash shows.
func (s *Store) TrashCount(ctx context.Context) (int, error) {
	var n int
	err := s.r.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE "+trashed).Scan(&n)
	return n, err
}

// TrashedFiles returns every file of the assets ids, which must all be in the
// Trash (what TrashCount counts): an id that is not fails the call with
// ErrNotFound, so deleting from disk can never reach a photo the user did not
// trash. Files come in path order.
func (s *Store) TrashedFiles(ctx context.Context, ids []int64) ([]TrashedFile, error) {
	if len(ids) == 0 {
		return []TrashedFile{}, nil
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	var files []TrashedFile
	err = s.read(ctx, func(tx *sql.Tx) error {
		if err := requireAll(ctx, tx, trashed, string(list)); err != nil {
			return err
		}
		files, err = trashedFiles(ctx, tx, " AND asset_id IN (SELECT value FROM json_each(?))", string(list))
		return err
	})
	return files, err
}

// AllTrashedFiles returns every file of every asset in the Trash.
func (s *Store) AllTrashedFiles(ctx context.Context) ([]TrashedFile, error) {
	var files []TrashedFile
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		files, err = trashedFiles(ctx, tx, "")
		return err
	})
	return files, err
}

// trashedFiles reads the files of Trash assets, narrowed by extra (a clause
// that starts with " AND ").
func trashedFiles(ctx context.Context, q querier, extra string, args ...any) ([]TrashedFile, error) {
	return queryAll(ctx, q, func(r *sql.Rows) (f TrashedFile, err error) {
		err = r.Scan(&f.ID, &f.AssetID, &f.Path)
		return f, err
	}, `SELECT id, asset_id, path FROM files WHERE asset_id IN (SELECT id FROM assets WHERE `+trashed+`)`+extra+` ORDER BY path, id`, args...)
}
