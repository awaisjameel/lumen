package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"lumen/internal/core"
)

// SelectedFiles reads one primary file per visible asset, without loading
// the full library or deriving event names for explicit destinations.
func (s *Store) SelectedFiles(ctx context.Context, ids []int64) (OrganiseSnapshot, error) {
	var snap OrganiseSnapshot
	data, err := json.Marshal(ids)
	if err != nil {
		return snap, err
	}
	err = s.read(ctx, func(tx *sql.Tx) error {
		var err error
		snap.Files, err = queryAll(ctx, tx, func(r *sql.Rows) (f OrganiseFile, err error) {
			err = r.Scan(&f.FileID, &f.AssetID, &f.SourceID, &f.Root, &f.Path, &f.Size, &f.MTime)
			return f, err
		}, `SELECT f.id, f.asset_id, f.source_id, s.path, f.path, f.size, f.mtime
			FROM files f JOIN assets a ON a.id = f.asset_id JOIN sources s ON s.id = f.source_id
			WHERE a.id IN (SELECT value FROM json_each(?)) AND s.enabled = 1 AND `+visibleA+`
			AND f.id = (SELECT min(f2.id) FROM files f2 JOIN sources s2 ON s2.id = f2.source_id WHERE f2.asset_id = a.id AND s2.enabled = 1)
			ORDER BY a.id`, string(data))
		if err != nil {
			return err
		}
		snap.BlockedDirs, err = blockedFolders(ctx, tx)
		return err
	})
	return snap, err
}

// KnownDestinations checks candidate paths against the canonical folded
// index, including indexed files that happen to be absent from disk.
func (s *Store) KnownDestinations(ctx context.Context, paths []string) (map[string]bool, error) {
	known := make(map[string]bool, len(paths))
	err := s.read(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, "SELECT EXISTS (SELECT 1 FROM files WHERE "+foldPath+" = "+foldKey("?1")+")")
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, path := range paths {
			var exists bool
			if err := stmt.QueryRowContext(ctx, path).Scan(&exists); err != nil {
				return err
			}
			if exists {
				known[core.Fold(path)] = true
			}
		}
		return nil
	})
	return known, err
}
