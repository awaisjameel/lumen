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

// labelBlocked is true when a block of kind (in either mode) targets id, an
// SQL expression: the label is left out of lists, suggestions and details.
func labelBlocked(kind core.BlockKind, id string) string {
	return "EXISTS (SELECT 1 FROM blocks blk_label WHERE blk_label.kind = '" + string(kind) + "' AND blk_label.target_id = " + id + ")"
}

// placeBlocked is labelBlocked for places.
func placeBlocked(id string) string { return labelBlocked(core.BlockPlace, id) }

// blockName is the name a block is listed under, an SQL expression over the
// blocks row alias b: the live name of what it targets, so a renamed person
// or album shows its new name, else the name stored with the block.
func blockName(b string) string {
	return `COALESCE(CASE ` + b + `.kind
			WHEN 'asset' THEN (SELECT path FROM files WHERE asset_id = ` + b + `.target_id ORDER BY id LIMIT 1)
			WHEN 'person' THEN (SELECT COALESCE(name, 'Unnamed person') FROM people WHERE id = ` + b + `.target_id)
			WHEN 'tag' THEN (SELECT name FROM tags WHERE id = ` + b + `.target_id)
			WHEN 'place' THEN (SELECT name FROM places WHERE id = ` + b + `.target_id)
			WHEN 'album' THEN (SELECT name FROM albums WHERE id = ` + b + `.target_id)
		END, ` + b + `.name)`
}

// Blocks lists the user's visibility rules. This is private UI data and is
// deliberately not joined into the ordinary People/Things views.
func (s *Store) Blocks(ctx context.Context) ([]core.Block, error) {
	return queryAll(ctx, s.r, func(r *sql.Rows) (b core.Block, err error) {
		err = r.Scan(&b.ID, &b.Kind, &b.TargetID, &b.Path, &b.Mode, &b.Name)
		return b, err
	}, `SELECT b.id, b.kind, COALESCE(b.target_id, 0), COALESCE(b.path, ''), b.mode, `+blockName("b")+`
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
// Existing rules can only be updated when allowChange is true. An event is
// named by one of its assets; a face block made here covers that face only
// (SetFaceBlock chooses look-alikes too).
func (s *Store) SetBlockWithCreated(ctx context.Context, kind core.BlockKind, targetID int64, path string, mode core.BlockMode, allowChange bool) (id int64, created bool, err error) {
	return s.setBlock(ctx, kind, targetID, path, mode, allowChange, false)
}

// SetFaceBlock blocks the face faceID, and with lookalikes the faces like it
// too, in existing and future photos.
func (s *Store) SetFaceBlock(ctx context.Context, faceID int64, mode core.BlockMode, lookalikes, allowChange bool) (id int64, created bool, err error) {
	return s.setBlock(ctx, core.BlockFace, faceID, "", mode, allowChange, lookalikes)
}

// setBlock is SetBlockWithCreated with the face-block choice. Event and face
// blocks point at a target row of their own, which their target function
// finds (an existing block's) or makes inside the transaction, so it is in
// the same commit as the block.
func (s *Store) setBlock(ctx context.Context, kind core.BlockKind, targetID int64, path string, mode core.BlockMode, allowChange, lookalikes bool) (id int64, created bool, err error) {
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
		target := targetID // the blocks row's target, once an event or face is resolved
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
		case core.BlockEvent:
			target, name, err = s.eventBlockTarget(ctx, tx, targetID)
		case core.BlockFace:
			target, name, err = s.faceBlockTarget(ctx, tx, targetID, lookalikes)
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
		var current core.BlockMode
		if kind == core.BlockFolder {
			err = tx.QueryRowContext(ctx, "SELECT id, mode FROM blocks WHERE kind = 'folder' AND fold(path) = fold(?)", path).Scan(&id, &current)
		} else {
			err = tx.QueryRowContext(ctx, "SELECT id, mode FROM blocks WHERE kind = ? AND target_id = ?", kind, target).Scan(&id, &current)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id != 0 {
			if current == mode {
				return nil // already so: nothing changes, so nothing needs the PIN
			}
			if !allowChange {
				return ErrBlockChangeLocked
			}
			_, err = tx.ExecContext(ctx, "UPDATE blocks SET mode = ?, name = ? WHERE id = ?", mode, name, id)
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO blocks(kind, target_id, path, mode, name, created_at)
			VALUES (?, NULLIF(?, 0), NULLIF(?, ''), ?, ?, ?)`, kind, target, path, mode, name, time.Now().Unix())
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

// SetBlockMode changes what the block blockID hides, by the block's own id:
// every kind's target is read by SetBlock as something else (an event's is a
// tag of its own, a face's a face_blocks row), so a mode switch must not go
// through it. Setting the mode a block has changes nothing, and so moves no
// revision. Photos and folders are always blocked with their photos.
func (s *Store) SetBlockMode(ctx context.Context, blockID int64, mode core.BlockMode) error {
	if mode != core.BlockLabel && mode != core.BlockPhotos {
		return ErrInvalidBlock
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		var kind core.BlockKind
		var current core.BlockMode
		err := tx.QueryRowContext(ctx, "SELECT kind, mode FROM blocks WHERE id = ?", blockID).Scan(&kind, &current)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if (kind == core.BlockAsset || kind == core.BlockFolder) && mode != core.BlockPhotos {
			return ErrInvalidBlock
		}
		if current == mode {
			return nil
		}
		_, err = tx.ExecContext(ctx, "UPDATE blocks SET mode = ? WHERE id = ?", mode, blockID)
		return err
	})
}

// RemoveBlock restores an item's ordinary visibility without touching media.
func (s *Store) RemoveBlock(ctx context.Context, id int64) error {
	return affected(s.w.ExecContext(ctx, "DELETE FROM blocks WHERE id = ?", id))
}

// IsBlocked checks whether a named view target has been removed from browsing.
func (s *Store) IsBlocked(ctx context.Context, kind core.BlockKind, targetID int64) (bool, error) {
	var blocked bool
	err := s.r.QueryRowContext(ctx, "SELECT "+labelBlocked(kind, "?"), targetID).Scan(&blocked)
	return blocked, err
}
