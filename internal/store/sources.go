package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"lumen/internal/core"
)

// ErrAlreadyCovered means the folder is already inside a watched source.
var ErrAlreadyCovered = errors.New("this folder is already part of your library")

// ListSources returns every source folder, alphabetically.
func (s *Store) ListSources(ctx context.Context) ([]core.Source, error) {
	rows, err := s.r.QueryContext(ctx, sourcesQuery)
	return collect(rows, err, scanSource)
}

const sourcesQuery = "SELECT id, path, enabled FROM sources ORDER BY path"

func scanSource(r *sql.Rows) (src core.Source, err error) {
	err = r.Scan(&src.ID, &src.Path, &src.Enabled)
	return src, err
}

// AddSource registers a folder. Sources never overlap: adding a folder inside
// an existing source returns ErrAlreadyCovered, and adding a parent of existing
// sources absorbs them (their files move to the new source, nothing is lost).
func (s *Store) AddSource(ctx context.Context, path string) (core.Source, error) {
	path = filepath.Clean(path)
	var src core.Source
	err := s.tx(ctx, func(tx *sql.Tx) error {
		// Check under the writer lock so concurrent additions cannot overlap.
		rows, err := tx.QueryContext(ctx, sourcesQuery)
		existing, err := collect(rows, err, scanSource)
		if err != nil {
			return err
		}
		for _, e := range existing {
			if core.IsWithin(path, e.Path) {
				src = e
				return ErrAlreadyCovered
			}
		}
		res, err := tx.ExecContext(ctx, "INSERT INTO sources (path, added_at) VALUES (?, ?)", path, time.Now().Unix())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		src = core.Source{ID: id, Path: path, Enabled: true}
		for _, e := range existing {
			if !core.IsWithin(e.Path, path) {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE files SET source_id = ? WHERE source_id = ?", id, e.ID); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", e.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return src, fmt.Errorf("add source: %w", err)
	}
	return src, nil
}

// RemoveSource stops watching a folder. Its files leave the library; assets
// that no longer have any file are marked missing rather than deleted, so if
// the folder comes back, names, favourites and albums are still there.
func (s *Store) RemoveSource(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		// Only the assets of the files removed can have lost their last file.
		rows, err := tx.QueryContext(ctx, "DELETE FROM files WHERE source_id = ? RETURNING asset_id", id)
		assets, err := collect(rows, err, scanID)
		if err != nil {
			return err
		}
		if err := affected(tx.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", id)); err != nil {
			return err
		}
		return markOrphans(ctx, tx, assets, time.Now().Unix())
	})
}
