-- User blocks are independent of the media and labels they cover. Removing a
-- block restores the current view without reindexing or moving original files.
CREATE TABLE blocks (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('asset', 'person', 'tag', 'place', 'folder', 'album')),
  target_id  INTEGER,
  path       TEXT,
  mode       TEXT NOT NULL CHECK (mode IN ('label', 'photos')),
  name       TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  CHECK ((kind = 'folder' AND target_id IS NULL AND path IS NOT NULL AND mode = 'photos')
      OR (kind != 'folder' AND target_id > 0 AND path IS NULL))
);
CREATE UNIQUE INDEX blocks_target ON blocks(kind, target_id) WHERE target_id IS NOT NULL;
CREATE UNIQUE INDEX blocks_folder ON blocks(fold(path)) WHERE kind = 'folder';
CREATE INDEX blocks_photos ON blocks(kind, target_id) WHERE mode = 'photos';

-- Existing user-hidden labels become photo blocks. The vocabulary's one sink
-- kind was hidden by the application rather than the user.
ALTER TABLE tags ADD COLUMN is_sink INTEGER NOT NULL DEFAULT 0;
UPDATE tags SET is_sink = 1 WHERE kind = 'builtin' AND name = 'Other objects';
INSERT INTO blocks(kind, target_id, mode, name, created_at)
  SELECT 'person', id, 'photos', COALESCE(name, 'Unnamed person'), CAST(strftime('%s') AS INTEGER)
  FROM people WHERE hidden = 1;
INSERT INTO blocks(kind, target_id, mode, name, created_at)
  SELECT 'tag', id, 'photos', name, CAST(strftime('%s') AS INTEGER)
  FROM tags WHERE hidden = 1 AND is_sink = 0;
UPDATE people SET hidden = 0 WHERE hidden = 1;
UPDATE tags SET hidden = 0 WHERE hidden = 1 AND is_sink = 0;

CREATE TRIGGER event_block_added AFTER INSERT ON blocks BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_changed AFTER UPDATE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_deleted AFTER DELETE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

-- A new face, tag, album membership or file path can make an existing block
-- cover an asset without changing that asset's own row.
CREATE TRIGGER event_block_face_added AFTER INSERT ON faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'person' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_face_changed AFTER UPDATE OF person_id ON faces
WHEN NEW.person_id IS NOT OLD.person_id AND EXISTS (SELECT 1 FROM blocks WHERE kind = 'person' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_face_deleted AFTER DELETE ON faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'person' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_tag_added AFTER INSERT ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'tag' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_tag_changed AFTER UPDATE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'tag' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_tag_deleted AFTER DELETE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'tag' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_album_added AFTER INSERT ON album_items
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_album_deleted AFTER DELETE ON album_items
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_album_rule_changed AFTER UPDATE OF rule_json ON albums
WHEN NEW.rule_json IS NOT OLD.rule_json AND EXISTS
  (SELECT 1 FROM blocks WHERE kind = 'album' AND target_id = NEW.id AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_favourite_changed AFTER UPDATE OF favourite ON assets
WHEN NEW.favourite IS NOT OLD.favourite AND EXISTS
  (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_confidence_changed AFTER UPDATE OF value ON settings
WHEN NEW.key = 'tag_confidence' AND NEW.value IS NOT OLD.value AND EXISTS
  (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_confidence_added AFTER INSERT ON settings
WHEN NEW.key = 'tag_confidence' AND EXISTS
  (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_file_added AFTER INSERT ON files
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_file_changed AFTER UPDATE OF path ON files
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_file_deleted AFTER DELETE ON files
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder' AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER block_asset_deleted AFTER DELETE ON assets BEGIN
  DELETE FROM blocks WHERE kind = 'asset' AND target_id = OLD.id;
END;
CREATE TRIGGER block_person_deleted AFTER DELETE ON people BEGIN
  DELETE FROM blocks WHERE kind = 'person' AND target_id = OLD.id;
END;
CREATE TRIGGER block_tag_deleted AFTER DELETE ON tags BEGIN
  DELETE FROM blocks WHERE kind = 'tag' AND target_id = OLD.id;
END;
CREATE TRIGGER block_place_deleted AFTER DELETE ON places BEGIN
  DELETE FROM blocks WHERE kind = 'place' AND target_id = OLD.id;
END;
CREATE TRIGGER block_album_deleted AFTER DELETE ON albums BEGIN
  DELETE FROM blocks WHERE kind = 'album' AND target_id = OLD.id;
END;
