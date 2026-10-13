-- The original revision guards already decide whether most indexing rows can
-- affect a block. Record dirty assets and accumulate the original revision
-- increments together, so indexing needs one trigger write rather than two.
-- Store.tx pays those increments after event tags and before committing.
-- The only coverage rule is store.blockMatch.
CREATE TABLE IF NOT EXISTS blocked_assets (
  asset_id INTEGER PRIMARY KEY REFERENCES assets(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS blocked_dirty (
  asset_id INTEGER PRIMARY KEY,
  is_blocked INTEGER NOT NULL DEFAULT 0 CHECK (is_blocked IN (0, 1)),
  event_increments INTEGER NOT NULL DEFAULT 0 CHECK (event_increments >= 0),
  visible_increments INTEGER NOT NULL DEFAULT 0 CHECK (visible_increments >= 0)
);
CREATE TABLE IF NOT EXISTS blocked_rebuild (id INTEGER PRIMARY KEY CHECK (id = 1));
INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);

DROP TRIGGER IF EXISTS event_block_added;
CREATE TRIGGER event_block_added AFTER INSERT ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

DROP TRIGGER IF EXISTS event_block_changed;
CREATE TRIGGER event_block_changed AFTER UPDATE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

DROP TRIGGER IF EXISTS event_block_deleted;
CREATE TRIGGER event_block_deleted AFTER DELETE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

DROP TRIGGER IF EXISTS visible_asset_added;
CREATE TRIGGER visible_asset_added AFTER INSERT ON assets
WHEN EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos') BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.id, 0, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS visible_asset_deleted;
CREATE TRIGGER visible_asset_deleted AFTER DELETE ON assets
WHEN EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos') BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.id, 0, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

-- With no photo block, an asset cannot need coverage. Keep the original
-- revision write without allocating a journal row for each indexed photo.
CREATE TRIGGER blocked_revision_asset_added AFTER INSERT ON assets
WHEN NOT EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos') BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;

CREATE TRIGGER blocked_revision_asset_deleted AFTER DELETE ON assets
WHEN NOT EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos') BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;

DROP TRIGGER IF EXISTS visible_block_place_changed;
CREATE TRIGGER visible_block_place_changed AFTER UPDATE OF place_id ON assets
WHEN NEW.place_id IS NOT OLD.place_id AND EXISTS
  (SELECT 1 FROM blocks WHERE kind = 'place' AND mode = 'photos' AND target_id IN (OLD.place_id, NEW.place_id)) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.id, 0, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS visible_block_date_changed;
CREATE TRIGGER visible_block_date_changed AFTER UPDATE OF taken_at ON assets
WHEN NEW.taken_at IS NOT OLD.taken_at AND EXISTS (SELECT 1 FROM blocks b JOIN albums a ON a.id = b.target_id
  WHERE b.kind = 'album' AND b.mode = 'photos' AND a.kind = 'smart'
    AND (COALESCE(json_extract(a.rule_json, '$.from'), '') != '' OR COALESCE(json_extract(a.rule_json, '$.to'), '') != '')) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.id, 0, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_favourite_changed;
CREATE TRIGGER event_block_favourite_changed AFTER UPDATE OF favourite ON assets
WHEN NEW.favourite IS NOT OLD.favourite AND EXISTS (SELECT 1 FROM blocks b JOIN albums a ON a.id = b.target_id
  WHERE b.kind = 'album' AND b.mode = 'photos' AND a.kind = 'smart'
    AND COALESCE(json_extract(a.rule_json, '$.favourites'), 0) != 0) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_tag_added;
CREATE TRIGGER event_block_tag_added AFTER INSERT ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'tag' AND b.target_id = NEW.tag_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.tag') = NEW.tag_id)))) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_tag_deleted;
CREATE TRIGGER event_block_tag_deleted AFTER DELETE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'tag' AND b.target_id = OLD.tag_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.tag') = OLD.tag_id)))) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_tag_changed;
CREATE TRIGGER event_block_tag_changed AFTER UPDATE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'tag' AND b.target_id IN (OLD.tag_id, NEW.tag_id))
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.tag') IN (OLD.tag_id, NEW.tag_id))))) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1), (NEW.asset_id, 0, 0)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_face_added;
CREATE TRIGGER event_block_face_added AFTER INSERT ON faces
WHEN NEW.person_id IS NOT NULL AND EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'person' AND b.target_id = NEW.person_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.person') = NEW.person_id)))) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_face_deleted;
CREATE TRIGGER event_block_face_deleted AFTER DELETE ON faces
WHEN OLD.person_id IS NOT NULL AND EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'person' AND b.target_id = OLD.person_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.person') = OLD.person_id)))) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_face_changed;
CREATE TRIGGER event_block_face_changed AFTER UPDATE OF person_id, asset_id, id ON faces
WHEN (NEW.person_id IS NOT OLD.person_id OR NEW.asset_id IS NOT OLD.asset_id OR NEW.id IS NOT OLD.id) AND EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'person' AND b.target_id IN (OLD.person_id, NEW.person_id))
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.person') IN (OLD.person_id, NEW.person_id))))) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1), (NEW.asset_id, 0, 0)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_file_added;
CREATE TRIGGER event_block_file_added AFTER INSERT ON files
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder') BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_file_deleted;
CREATE TRIGGER event_block_file_deleted AFTER DELETE ON files
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder') BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_file_changed;
CREATE TRIGGER event_block_file_changed AFTER UPDATE OF path, asset_id ON files
WHEN (NEW.path IS NOT OLD.path OR NEW.asset_id IS NOT OLD.asset_id)
  AND EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder') BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1), (NEW.asset_id, 0, 0)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_album_added;
CREATE TRIGGER event_block_album_added AFTER INSERT ON album_items
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos' AND target_id = NEW.album_id) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_album_deleted;
CREATE TRIGGER event_block_album_deleted AFTER DELETE ON album_items
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos' AND target_id = OLD.album_id) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS event_block_album_rule_changed;
CREATE TRIGGER event_block_album_rule_changed AFTER UPDATE OF kind, rule_json, id ON albums
WHEN (NEW.rule_json IS NOT OLD.rule_json OR NEW.kind IS NOT OLD.kind OR NEW.id IS NOT OLD.id) AND EXISTS
  (SELECT 1 FROM blocks WHERE kind = 'album' AND target_id IN (OLD.id, NEW.id) AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

DROP TRIGGER IF EXISTS event_block_confidence_added;
CREATE TRIGGER event_block_confidence_added AFTER INSERT ON settings
WHEN NEW.key = 'tag_confidence' AND EXISTS
  (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

DROP TRIGGER IF EXISTS event_block_confidence_changed;
CREATE TRIGGER event_block_confidence_changed AFTER UPDATE OF value ON settings
WHEN NEW.key = 'tag_confidence' AND NEW.value IS NOT OLD.value AND EXISTS
  (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

DROP TRIGGER IF EXISTS visible_event_member_added;
CREATE TRIGGER visible_event_member_added AFTER INSERT ON event_members
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'event' AND target_id = NEW.tag_id AND mode = 'photos') BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.asset_id, 0, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS visible_face_block_added;
CREATE TRIGGER visible_face_block_added AFTER INSERT ON face_block_faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos' AND ((kind = 'face' AND target_id = NEW.face_block_id) OR kind = 'album')) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (NEW.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
  INSERT OR IGNORE INTO blocked_dirty(asset_id) SELECT asset_id FROM faces WHERE id = NEW.face_id;
END;

DROP TRIGGER IF EXISTS visible_event_member_deleted;
CREATE TRIGGER visible_event_member_deleted AFTER DELETE ON event_members
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'event' AND target_id = OLD.tag_id AND mode = 'photos') BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 0, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
END;

DROP TRIGGER IF EXISTS visible_face_block_deleted;
CREATE TRIGGER visible_face_block_deleted AFTER DELETE ON face_block_faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos' AND ((kind = 'face' AND target_id = OLD.face_block_id) OR kind = 'album')) BEGIN
  INSERT INTO blocked_dirty(asset_id, event_increments, visible_increments)
    VALUES (OLD.asset_id, 1, 1)
    ON CONFLICT(asset_id) DO UPDATE SET
      event_increments = blocked_dirty.event_increments + excluded.event_increments,
      visible_increments = blocked_dirty.visible_increments + excluded.visible_increments;
  INSERT OR IGNORE INTO blocked_dirty(asset_id) SELECT asset_id FROM faces WHERE id = OLD.face_id;
END;

CREATE TRIGGER IF NOT EXISTS blocked_dirty_asset_kind AFTER UPDATE OF kind ON assets
WHEN NEW.kind IS NOT OLD.kind AND EXISTS (SELECT 1 FROM blocks b JOIN albums a ON a.id = b.target_id
  WHERE b.kind = 'album' AND b.mode = 'photos' AND a.kind = 'smart'
    AND COALESCE(json_extract(a.rule_json, '$.kind'), '') != '') BEGIN
  INSERT OR IGNORE INTO blocked_dirty(asset_id) VALUES (NEW.id);
END;

CREATE TRIGGER IF NOT EXISTS blocked_dirty_asset_identity AFTER UPDATE OF id ON assets
WHEN NEW.id IS NOT OLD.id AND EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos') BEGIN
  INSERT OR IGNORE INTO blocked_dirty(asset_id) VALUES (OLD.id), (NEW.id);
END;

CREATE TRIGGER IF NOT EXISTS blocked_dirty_album_items_update AFTER UPDATE OF asset_id, album_id ON album_items
WHEN (NEW.asset_id IS NOT OLD.asset_id OR NEW.album_id IS NOT OLD.album_id)
  AND EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos' AND kind = 'album' AND target_id IN (OLD.album_id, NEW.album_id)) BEGIN
  INSERT OR IGNORE INTO blocked_dirty(asset_id) VALUES (OLD.asset_id), (NEW.asset_id);
END;

CREATE TRIGGER IF NOT EXISTS blocked_dirty_event_members_update AFTER UPDATE OF asset_id, tag_id ON event_members
WHEN (NEW.asset_id IS NOT OLD.asset_id OR NEW.tag_id IS NOT OLD.tag_id)
  AND EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos' AND kind = 'event' AND target_id IN (OLD.tag_id, NEW.tag_id)) BEGIN
  INSERT OR IGNORE INTO blocked_dirty(asset_id) VALUES (OLD.asset_id), (NEW.asset_id);
END;

CREATE TRIGGER IF NOT EXISTS blocked_dirty_face_block_faces_update AFTER UPDATE OF asset_id, face_id, face_block_id ON face_block_faces
WHEN (NEW.asset_id IS NOT OLD.asset_id OR NEW.face_id IS NOT OLD.face_id OR NEW.face_block_id IS NOT OLD.face_block_id)
  AND EXISTS (SELECT 1 FROM blocks WHERE mode = 'photos' AND kind IN ('face', 'album')) BEGIN
  INSERT OR IGNORE INTO blocked_dirty(asset_id) VALUES (OLD.asset_id), (NEW.asset_id);
  INSERT OR IGNORE INTO blocked_dirty(asset_id) SELECT asset_id FROM faces WHERE id IN (OLD.face_id, NEW.face_id);
END;

CREATE TRIGGER IF NOT EXISTS blocked_rebuild_album_insert AFTER INSERT ON albums
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos' AND target_id = NEW.id) BEGIN
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

CREATE TRIGGER IF NOT EXISTS blocked_rebuild_confidence_delete AFTER DELETE ON settings
WHEN OLD.key = 'tag_confidence' BEGIN
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;

CREATE TRIGGER IF NOT EXISTS blocked_rebuild_confidence_key AFTER UPDATE OF key ON settings
WHEN NEW.key IS NOT OLD.key AND (OLD.key = 'tag_confidence' OR NEW.key = 'tag_confidence') BEGIN
  INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1);
END;
