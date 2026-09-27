package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"lumen/internal/core"
)

// ErrAlreadyCovered means the folder is already inside a watched source.
var ErrAlreadyCovered = errors.New("this folder is already part of your library")

// ListSources returns every source folder, alphabetically.
func (s *Store) ListSources(ctx context.Context) ([]core.Source, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (src core.Source, err error) {
		err = r.Scan(&src.ID, &src.Path, &src.Enabled)
		return src, err
	}, "SELECT id, path, enabled FROM sources ORDER BY path")
}

// AddSource registers a folder. Sources never overlap: adding a folder inside
// an existing source returns ErrAlreadyCovered, and adding a parent of existing
// sources absorbs them (their files move to the new source, nothing is lost).
func (s *Store) AddSource(ctx context.Context, path string) (core.Source, error) {
	path = filepath.Clean(path)
	existing, err := s.ListSources(ctx)
	if err != nil {
		return core.Source{}, err
	}
	for _, e := range existing {
		if IsWithin(path, e.Path) {
			return e, ErrAlreadyCovered
		}
	}

	var src core.Source
	err = s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "INSERT INTO sources (path, added_at) VALUES (?, ?)", path, time.Now().Unix())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		src = core.Source{ID: id, Path: path, Enabled: true}
		for _, e := range existing {
			if !IsWithin(e.Path, path) {
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
		return core.Source{}, fmt.Errorf("add source: %w", err)
	}
	return src, nil
}

// RemoveSource stops watching a folder. Its files leave the library; assets
// that no longer have any file are marked missing rather than deleted, so if
// the folder comes back, names, favourites and albums are still there.
func (s *Store) RemoveSource(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM files WHERE source_id = ?", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM sources WHERE id = ?", id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE assets SET missing_since = ?
			WHERE missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM files f WHERE f.asset_id = assets.id)`,
			time.Now().Unix())
		return err
	})
}

// IsWithin reports whether path is dir or inside it (case-insensitive, as on Windows).
func IsWithin(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if strings.EqualFold(path, dir) {
		return true
	}
	if !strings.HasSuffix(dir, string(filepath.Separator)) {
		dir += string(filepath.Separator)
	}
	return len(path) > len(dir) && strings.EqualFold(path[:len(dir)], dir)
}
