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

// ErrInvalidBlock means the requested kind, mode or target cannot be blocked.
var ErrInvalidBlock = errors.New("invalid block")

// ErrBlockChangeLocked rejects unauthenticated changes to an existing rule.
var ErrBlockChangeLocked = errors.New("changing an existing block requires PIN unlock")

// Blocks lists the user's visibility rules. This is private UI data and is
// deliberately not joined into the ordinary People/Things views.
func (s *Store) Blocks(ctx context.Context) ([]core.Block, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (b core.Block, err error) {
		err = r.Scan(&b.ID, &b.Kind, &b.TargetID, &b.Path, &b.Mode, &b.Name)
		return b, err
	}, `SELECT b.id, b.kind, COALESCE(b.target_id, 0), COALESCE(b.path, ''), b.mode,
		COALESCE(CASE b.kind
			WHEN 'asset' THEN (SELECT path FROM files WHERE asset_id = b.target_id ORDER BY id LIMIT 1)
			WHEN 'person' THEN (SELECT COALESCE(name, 'Unnamed person') FROM people WHERE id = b.target_id)
			WHEN 'tag' THEN (SELECT name FROM tags WHERE id = b.target_id)
			WHEN 'place' THEN (SELECT name FROM places WHERE id = b.target_id)
			WHEN 'album' THEN (SELECT name FROM albums WHERE id = b.target_id)
		END, b.name)
		FROM blocks b ORDER BY b.created_at DESC, b.id DESC`)
}

// SetBlock adds or changes one rule. Target validation and the write share the
// writer transaction so a deleted item never leaves a dangling new block.
func (s *Store) SetBlock(ctx context.Context, kind core.BlockKind, targetID int64, path string, mode core.BlockMode) (int64, error) {
	id, _, err := s.SetBlockWithCreated(ctx, kind, targetID, path, mode, true)
	return id, err
}

// SetBlockWithCreated also reports whether this call inserted the rule. The
// service uses that fact to offer Undo only for a genuinely new block.
// Existing rules can only be updated when allowChange is true.
func (s *Store) SetBlockWithCreated(ctx context.Context, kind core.BlockKind, targetID int64, path string, mode core.BlockMode, allowChange bool) (id int64, created bool, err error) {
	if mode != core.BlockLabel && mode != core.BlockPhotos {
		return 0, false, ErrInvalidBlock
	}
	if kind == core.BlockAsset || kind == core.BlockFolder {
		mode = core.BlockPhotos
	}
	if kind == core.BlockFolder {
		if targetID != 0 || path == "" || !filepath.IsAbs(path) {
			return 0, false, ErrInvalidBlock
		}
		path = filepath.Clean(path)
	} else if targetID <= 0 || path != "" {
		return 0, false, ErrInvalidBlock
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var name string
		var err error
		switch kind {
		case core.BlockAsset:
			err = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT path FROM files WHERE asset_id = assets.id ORDER BY id LIMIT 1), '')
				FROM assets WHERE id = ? AND `+present, targetID).Scan(&name)
		case core.BlockPerson:
			err = tx.QueryRowContext(ctx, "SELECT COALESCE(name, 'Unnamed person') FROM people WHERE id = ?", targetID).Scan(&name)
		case core.BlockTag:
			err = tx.QueryRowContext(ctx, "SELECT name FROM tags WHERE id = ?", targetID).Scan(&name)
		case core.BlockPlace:
			err = tx.QueryRowContext(ctx, "SELECT name FROM places WHERE id = ?", targetID).Scan(&name)
		case core.BlockAlbum:
			err = tx.QueryRowContext(ctx, "SELECT name FROM albums WHERE id = ?", targetID).Scan(&name)
		case core.BlockFolder:
			var sources []string
			sources, err = queryAll(ctx, tx, func(r *sql.Rows) (p string, e error) { e = r.Scan(&p); return p, e },
				"SELECT path FROM sources")
			if err == nil {
				err = ErrNotFound
				for _, source := range sources {
					if core.IsWithin(path, source) {
						name, err = path, nil
						break
					}
				}
			}
		default:
			return ErrInvalidBlock
		}
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if name == "" {
			name = fmt.Sprintf("%s %d", kind, targetID)
		}
		if kind == core.BlockFolder {
			err = tx.QueryRowContext(ctx, "SELECT id FROM blocks WHERE kind = 'folder' AND fold(path) = fold(?)", path).Scan(&id)
		} else {
			err = tx.QueryRowContext(ctx, "SELECT id FROM blocks WHERE kind = ? AND target_id = ?", kind, targetID).Scan(&id)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id != 0 {
			if !allowChange {
				return ErrBlockChangeLocked
			}
			_, err = tx.ExecContext(ctx, "UPDATE blocks SET mode = ?, name = ? WHERE id = ?", mode, name, id)
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO blocks(kind, target_id, path, mode, name, created_at)
			VALUES (?, NULLIF(?, 0), NULLIF(?, ''), ?, ?, ?)`, kind, targetID, path, mode, name, time.Now().Unix())
		if err == nil {
			id, err = res.LastInsertId()
			created = err == nil
		}
		return err
	})
	if err != nil {
		return 0, false, err
	}
	return id, created, nil
}

// RemoveBlock restores an item's ordinary visibility without touching media.
func (s *Store) RemoveBlock(ctx context.Context, id int64) error {
	return affected(s.w.ExecContext(ctx, "DELETE FROM blocks WHERE id = ?", id))
}

// IsBlocked checks whether a named view target has been removed from browsing.
func (s *Store) IsBlocked(ctx context.Context, kind core.BlockKind, targetID int64) (bool, error) {
	var blocked bool
	err := s.r.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM blocks WHERE kind = ? AND target_id = ?)", kind, targetID).Scan(&blocked)
	return blocked, err
}
