package store

import (
	"context"
	"database/sql"
	"encoding/json"
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

// Blocks lists the user's visibility rules, each with how many photos it
// covers and what to show it by. This is private UI data and is deliberately
// not joined into the ordinary People/Things views.
func (s *Store) Blocks(ctx context.Context) ([]core.Block, error) {
	var blocks []core.Block
	err := s.read(ctx, func(tx *sql.Tx) (err error) {
		blocks, err = queryAll(ctx, tx, func(r *sql.Rows) (b core.Block, err error) {
			err = r.Scan(&b.ID, &b.Kind, &b.TargetID, &b.Path, &b.Mode, &b.Name)
			return b, err
		}, `SELECT b.id, b.kind, COALESCE(b.target_id, 0), COALESCE(b.path, ''), b.mode, `+blockName("b")+`
			FROM blocks b ORDER BY b.created_at DESC, b.id DESC`)
		if err != nil {
			return err
		}
		for i := range blocks {
			if err := addBlockCover(ctx, tx, &blocks[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return blocks, nil
}

// blockCandidates lists, for the block ?1, assets that may be covered: by the
// target's own index where its kind has one, else every asset. It is only a
// narrowing; blockMatch stays the one rule of what a block covers. Each part
// joins the block first, by its primary key, so the parts of other kinds read
// nothing.
const blockCandidates = `SELECT c.target_id FROM blocks c WHERE c.id = ?1 AND c.kind = 'asset'
	UNION ALL SELECT f.asset_id FROM blocks c JOIN faces f ON f.person_id = c.target_id WHERE c.id = ?1 AND c.kind = 'person'
	UNION ALL SELECT t.asset_id FROM blocks c JOIN asset_tags t ON t.tag_id = c.target_id WHERE c.id = ?1 AND c.kind = 'tag'
	UNION ALL SELECT a.id FROM blocks c JOIN assets a ON a.place_id = c.target_id WHERE c.id = ?1 AND c.kind = 'place'
	UNION ALL SELECT m.asset_id FROM blocks c JOIN event_members m ON m.tag_id = c.target_id WHERE c.id = ?1 AND c.kind = 'event'
	UNION ALL SELECT x.asset_id FROM blocks c JOIN face_block_faces x ON x.face_block_id = c.target_id WHERE c.id = ?1 AND c.kind = 'face'
	UNION ALL SELECT i.asset_id FROM blocks c JOIN albums al ON al.id = c.target_id AND al.kind = 'manual'
		JOIN album_items i ON i.album_id = al.id WHERE c.id = ?1 AND c.kind = 'album'
	UNION ALL SELECT a.id FROM blocks c JOIN assets a WHERE c.id = ?1 AND (c.kind = 'folder'
		OR (c.kind = 'album' AND NOT EXISTS (SELECT 1 FROM albums al WHERE al.id = c.target_id AND al.kind = 'manual')))`

// blockCoverSQL reads what the block ?1 covers as BlockCoverage lists it: the
// count, and the newest asset, preferring one whose thumbnail is made.
var blockCoverSQL = `SELECT count(*) OVER (), id, CASE WHEN thumb_version > 0 THEN thumb_version + display_version ELSE 0 END
	FROM assets WHERE id IN (` + blockCandidates + `) AND ` + baseVisibleOf("") + `
	AND EXISTS (SELECT 1 FROM blocks target WHERE target.id = ?1 AND ` + blockMatch("assets", "target") + `)
	ORDER BY thumb_version > 0 DESC, taken_at DESC, id DESC LIMIT 1`

// addBlockCover fills in b's count and cover, and for a person or a face the
// face to show it by: the person's clearest face (the user's pick first), or
// the blocked face as it is found now.
func addBlockCover(ctx context.Context, tx *sql.Tx, b *core.Block) error {
	err := tx.QueryRowContext(ctx, blockCoverSQL, b.ID).Scan(&b.Count, &b.Cover, &b.CoverThumb)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var where string
	switch b.Kind {
	case core.BlockPerson:
		where = " WHERE f.person_id = ? AND " + baseVisibleOf("a") + " AND a.thumb_version > 0 ORDER BY " + clearest
	case core.BlockFace:
		where = " WHERE f.id = (SELECT face_id FROM face_blocks WHERE id = ?) AND " + baseVisibleOf("a") + " AND a.thumb_version > 0"
	default:
		return nil
	}
	faces, err := queryAll(ctx, tx, scanFace, faceSelect+where+" LIMIT 1", b.TargetID)
	if err != nil {
		return err
	}
	if len(faces) > 0 {
		b.Face = faces[0]
	}
	return nil
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
			target, name, err = s.faceBlockTarget(ctx, tx, targetID, lookalikes, allowChange)
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

// BlockRemoval is an opaque receipt for an unblocked rule. Keeping the resolved
// target matters: event and face targets are durable anchors, not live IDs.
type BlockRemoval struct {
	block     core.Block
	createdAt int64
	members   []removedEventMember
	face      *removedFaceBlock
}

// RemoveBlockForUndo removes a rule and captures its exact persisted target in
// the same transaction. Only the service keeps the receipt; clients cannot
// manufacture an anchor or restore an arbitrary rule through it.
func (s *Store) RemoveBlockForUndo(ctx context.Context, id int64) (*BlockRemoval, error) {
	r := &BlockRemoval{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		b := &r.block
		err := tx.QueryRowContext(ctx, `SELECT id, kind, COALESCE(target_id, 0), COALESCE(path, ''), mode, name, created_at
			FROM blocks WHERE id = ?`, id).Scan(&b.ID, &b.Kind, &b.TargetID, &b.Path, &b.Mode, &b.Name, &r.createdAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		switch b.Kind {
		case core.BlockEvent:
			r.members, err = queryAll(ctx, tx, func(rows *sql.Rows) (m removedEventMember, err error) {
				err = rows.Scan(&m.ID, &m.TakenAt, &m.Lat, &m.Lon, &m.Moved)
				return m, err
			}, `SELECT a.id, a.taken_at, a.lat, a.lon, m.moved
				FROM event_members m JOIN assets a ON a.id = m.asset_id WHERE m.tag_id = ?`, b.TargetID)
		case core.BlockFace:
			r.face, err = captureFaceBlock(ctx, tx, b.TargetID)
		default:
		}
		if err != nil {
			return err
		}
		return affected(tx.ExecContext(ctx, "DELETE FROM blocks WHERE id = ?", id))
	})
	return r, err
}

// RestoreRemovedBlock re-blocks the captured target. A rule made since removal
// is retained as it is; Undo must never weaken a newer block's scope.
func (s *Store) RestoreRemovedBlock(ctx context.Context, r *BlockRemoval) error {
	if r == nil {
		return ErrNotFound
	}
	b := r.block
	return s.tx(ctx, func(tx *sql.Tx) error {
		switch b.Kind {
		case core.BlockEvent:
			result, err := tx.ExecContext(ctx, "INSERT INTO event_tags(kind, created_at) VALUES ('block', ?)", r.createdAt)
			if err != nil {
				return err
			}
			b.TargetID, err = result.LastInsertId()
			if err != nil {
				return err
			}
			members, err := json.Marshal(r.members)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO event_members(tag_id, asset_id, moved)
				SELECT ?, a.id, json_extract(j.value, '$.moved') OR NOT
				(a.taken_at IS json_extract(j.value, '$.takenAt') AND a.lat IS json_extract(j.value, '$.lat')
				 AND a.lon IS json_extract(j.value, '$.lon'))
				FROM json_each(?) j JOIN assets a ON a.id = json_extract(j.value, '$.id')`, b.TargetID, string(members)); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO event_dirty(at)
				SELECT a.taken_at FROM json_each(?1) j JOIN assets a ON a.id = json_extract(j.value, '$.id')
				UNION SELECT json_extract(value, '$.takenAt') FROM json_each(?1)`, string(members)); err != nil {
				return err
			}
		case core.BlockFace:
			var err error
			b.TargetID, err = restoreFaceBlock(ctx, tx, r.face)
			if err != nil {
				return err
			}
		default:
		}
		_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO blocks(kind, target_id, path, mode, name, created_at)
			VALUES (?, NULLIF(?, 0), NULLIF(?, ''), ?, ?, ?)`, b.Kind, b.TargetID, b.Path, b.Mode, b.Name, r.createdAt)
		return err
	})
}

// IsBlocked checks whether a named view target has been removed from browsing.
func (s *Store) IsBlocked(ctx context.Context, kind core.BlockKind, targetID int64) (bool, error) {
	var blocked bool
	err := s.r.QueryRowContext(ctx, "SELECT "+labelBlocked(kind, "?"), targetID).Scan(&blocked)
	return blocked, err
}
