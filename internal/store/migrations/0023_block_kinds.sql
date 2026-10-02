-- Phase 5: events and single faces can be blocked too. Their targets are not
-- rows that already exist (an event is regrouped on read; a face is found
-- again by its place in the photo after a re-analysis), so each block points
-- at a target row of its own: an event tag of kind 'block' (migration 0019),
-- or a face_blocks row below. The covered photos are kept in membership
-- tables, so the visibility predicate reads them with one indexed lookup.

-- Safe to run again: the upgrade tests replay migrations over a current schema.
-- The rebuild drops blocks' own indexes and triggers with it, so they are made
-- afresh; everything else is IF NOT EXISTS.

-- blocks.kind has a CHECK constraint, so the table is rebuilt to allow the two
-- new kinds. Triggers on other tables name blocks in their bodies; with
-- legacy_alter_table on, the rename leaves those names alone (the default
-- would refuse it while the old table is gone), and they read the new table.
PRAGMA legacy_alter_table = ON;
CREATE TABLE blocks_new (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('asset', 'person', 'tag', 'place', 'folder', 'album', 'event', 'face')),
  target_id  INTEGER,
  path       TEXT,
  mode       TEXT NOT NULL CHECK (mode IN ('label', 'photos')),
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  CHECK ((kind = 'folder' AND target_id IS NULL AND path IS NOT NULL AND mode = 'photos')
      OR (kind != 'folder' AND target_id > 0 AND path IS NULL))
);
INSERT INTO blocks_new (id, kind, target_id, path, mode, name, created_at)
  SELECT id, kind, target_id, path, mode, name, created_at FROM blocks;
DROP TABLE blocks; -- its indexes and its own triggers go with it
ALTER TABLE blocks_new RENAME TO blocks;
PRAGMA legacy_alter_table = OFF;

CREATE UNIQUE INDEX blocks_target ON blocks(kind, target_id) WHERE target_id IS NOT NULL;
CREATE UNIQUE INDEX blocks_folder ON blocks(fold(path)) WHERE kind = 'folder';
CREATE INDEX blocks_photos ON blocks(kind, target_id) WHERE mode = 'photos';

-- As migration 0016's: any change to a block changes grouping and visibility.
CREATE TRIGGER event_block_added AFTER INSERT ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_changed AFTER UPDATE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_deleted AFTER DELETE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;

-- A blocked face: where it was, so a re-analysis (a new face model, a moved
-- box) finds it again, and whether its look-alikes are covered too.
CREATE TABLE IF NOT EXISTS face_blocks (
  id         INTEGER PRIMARY KEY,
  asset_id   INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  at_ms      INTEGER NOT NULL,              -- the video frame; 0 for photos
  x          REAL    NOT NULL,              -- the box, as fractions of the upright photo
  y          REAL    NOT NULL,
  w          REAL    NOT NULL,
  h          REAL    NOT NULL,
  face_id    INTEGER REFERENCES faces (id) ON DELETE SET NULL, -- the face at the box now; NULL while none is
  lookalikes INTEGER NOT NULL               -- 1: faces like it are covered too
);
CREATE INDEX IF NOT EXISTS face_blocks_asset ON face_blocks (asset_id);
CREATE INDEX IF NOT EXISTS face_blocks_face ON face_blocks (face_id);

-- The blocked face's identity vector, per face model: faces are compared only
-- with a vector of their own model.
CREATE TABLE IF NOT EXISTS face_block_vectors (
  face_block_id INTEGER NOT NULL REFERENCES face_blocks (id) ON DELETE CASCADE,
  model         TEXT    NOT NULL,
  vec           BLOB    NOT NULL,           -- float32, as faces.vec
  PRIMARY KEY (face_block_id, model)
) WITHOUT ROWID;

-- The faces a face block covers (the blocked face, and with look-alikes the
-- faces like it), with each face's photo, which never changes.
CREATE TABLE IF NOT EXISTS face_block_faces (
  face_block_id INTEGER NOT NULL REFERENCES face_blocks (id) ON DELETE CASCADE,
  face_id       INTEGER NOT NULL REFERENCES faces (id) ON DELETE CASCADE,
  asset_id      INTEGER NOT NULL,
  PRIMARY KEY (face_block_id, face_id)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS face_block_faces_asset ON face_block_faces (asset_id, face_block_id);
CREATE INDEX IF NOT EXISTS face_block_faces_face ON face_block_faces (face_id);

-- A block and its target come and go together.
CREATE TRIGGER IF NOT EXISTS block_event_target_removed AFTER DELETE ON blocks WHEN OLD.kind = 'event' BEGIN
  DELETE FROM event_tags WHERE id = OLD.target_id AND kind = 'block';
END;
CREATE TRIGGER IF NOT EXISTS block_face_target_removed AFTER DELETE ON blocks WHEN OLD.kind = 'face' BEGIN
  DELETE FROM face_blocks WHERE id = OLD.target_id;
END;
CREATE TRIGGER IF NOT EXISTS event_block_tag_removed AFTER DELETE ON event_tags WHEN OLD.kind = 'block' BEGIN
  DELETE FROM blocks WHERE kind = 'event' AND target_id = OLD.id;
END;
CREATE TRIGGER IF NOT EXISTS face_block_removed AFTER DELETE ON face_blocks BEGIN
  DELETE FROM blocks WHERE kind = 'face' AND target_id = OLD.id;
END;

-- What a photo-mode event or face block covers changes visibility (and so
-- grouping); migration 0019's triggers already count event members for
-- grouping alone.
CREATE TRIGGER IF NOT EXISTS visible_event_member_added AFTER INSERT ON event_members
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'event' AND target_id = NEW.tag_id AND mode = 'photos') BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS visible_event_member_deleted AFTER DELETE ON event_members
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'event' AND target_id = OLD.tag_id AND mode = 'photos') BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS visible_face_block_added AFTER INSERT ON face_block_faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'face' AND target_id = NEW.face_block_id AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS visible_face_block_deleted AFTER DELETE ON face_block_faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'face' AND target_id = OLD.face_block_id AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
