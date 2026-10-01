-- event_revision.value counts changes to event grouping, which also follows
-- dates, places and names. visible_value counts only changes to which assets
-- can be browsed (store.visible), so search's exclusion set and the UI's
-- "assets changed" signal no longer follow every metadata or thumbnail batch.
ALTER TABLE event_revision ADD COLUMN visible_value INTEGER NOT NULL DEFAULT 0;

CREATE TRIGGER visible_asset_added AFTER INSERT ON assets BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER visible_asset_deleted AFTER DELETE ON assets BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER visible_asset_changed AFTER UPDATE OF hidden, trashed_at, missing_since ON assets
WHEN NEW.hidden IS NOT OLD.hidden OR NEW.trashed_at IS NOT OLD.trashed_at
  OR NEW.missing_since IS NOT OLD.missing_since BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;
-- A place block covers by place_id, a smart album's dates by taken_at (the
-- event triggers already count both for grouping).
CREATE TRIGGER visible_block_place_changed AFTER UPDATE OF place_id ON assets
WHEN NEW.place_id IS NOT OLD.place_id AND EXISTS
  (SELECT 1 FROM blocks WHERE kind = 'place' AND mode = 'photos' AND target_id IN (OLD.place_id, NEW.place_id)) BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER visible_block_date_changed AFTER UPDATE OF taken_at ON assets
WHEN NEW.taken_at IS NOT OLD.taken_at AND EXISTS (SELECT 1 FROM blocks b JOIN albums a ON a.id = b.target_id
  WHERE b.kind = 'album' AND b.mode = 'photos' AND a.kind = 'smart'
    AND (COALESCE(json_extract(a.rule_json, '$.from'), '') != '' OR COALESCE(json_extract(a.rule_json, '$.to'), '') != '')) BEGIN
  UPDATE event_revision SET visible_value = visible_value + 1 WHERE id = 1;
END;

-- Every trigger below changes what a block covers: grouping and visibility
-- both follow. They replace 0014's and 0015's, which also fired for changes
-- no block can see (a new face nobody was put with yet, a tag no block
-- names), so indexing a library with any block regrouped events and rebuilt
-- search's exclusions after every batch.
DROP TRIGGER event_block_added;
DROP TRIGGER event_block_changed;
DROP TRIGGER event_block_deleted;
DROP TRIGGER event_block_face_added;
DROP TRIGGER event_block_face_changed;
DROP TRIGGER event_block_face_deleted;
DROP TRIGGER event_block_tag_added;
DROP TRIGGER event_block_tag_changed;
DROP TRIGGER event_block_tag_deleted;
DROP TRIGGER event_block_album_added;
DROP TRIGGER event_block_album_deleted;
DROP TRIGGER event_block_album_rule_changed;
DROP TRIGGER event_block_favourite_changed;
DROP TRIGGER event_block_confidence_changed;
DROP TRIGGER event_block_confidence_added;
DROP TRIGGER event_block_file_added;
DROP TRIGGER event_block_file_changed;
DROP TRIGGER event_block_file_deleted;

CREATE TRIGGER event_block_added AFTER INSERT ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_changed AFTER UPDATE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_deleted AFTER DELETE ON blocks BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;

-- A face counts once it is someone's: a photo block on that person, or on a
-- smart album whose rule names them.
CREATE TRIGGER event_block_face_added AFTER INSERT ON faces
WHEN NEW.person_id IS NOT NULL AND EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'person' AND b.target_id = NEW.person_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.person') = NEW.person_id)))) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_face_changed AFTER UPDATE OF person_id ON faces
WHEN NEW.person_id IS NOT OLD.person_id AND EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'person' AND b.target_id IN (OLD.person_id, NEW.person_id))
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.person') IN (OLD.person_id, NEW.person_id))))) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_face_deleted AFTER DELETE ON faces
WHEN OLD.person_id IS NOT NULL AND EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'person' AND b.target_id = OLD.person_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.person') = OLD.person_id)))) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;

-- A tag counts when a photo block names it, or a blocked smart album's rule does.
CREATE TRIGGER event_block_tag_added AFTER INSERT ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'tag' AND b.target_id = NEW.tag_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.tag') = NEW.tag_id)))) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_tag_changed AFTER UPDATE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'tag' AND b.target_id IN (OLD.tag_id, NEW.tag_id))
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.tag') IN (OLD.tag_id, NEW.tag_id))))) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_tag_deleted AFTER DELETE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks b WHERE b.mode = 'photos' AND (
  (b.kind = 'tag' AND b.target_id = OLD.tag_id)
  OR (b.kind = 'album' AND EXISTS (SELECT 1 FROM albums a WHERE a.id = b.target_id AND a.kind = 'smart'
    AND json_extract(a.rule_json, '$.tag') = OLD.tag_id)))) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;

CREATE TRIGGER event_block_album_added AFTER INSERT ON album_items
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos' AND target_id = NEW.album_id) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_album_deleted AFTER DELETE ON album_items
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'album' AND mode = 'photos' AND target_id = OLD.album_id) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_album_rule_changed AFTER UPDATE OF rule_json ON albums
WHEN NEW.rule_json IS NOT OLD.rule_json AND EXISTS
  (SELECT 1 FROM blocks WHERE kind = 'album' AND target_id = NEW.id AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_favourite_changed AFTER UPDATE OF favourite ON assets
WHEN NEW.favourite IS NOT OLD.favourite AND EXISTS (SELECT 1 FROM blocks b JOIN albums a ON a.id = b.target_id
  WHERE b.kind = 'album' AND b.mode = 'photos' AND a.kind = 'smart'
    AND COALESCE(json_extract(a.rule_json, '$.favourites'), 0) != 0) BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_confidence_changed AFTER UPDATE OF value ON settings
WHEN NEW.key = 'tag_confidence' AND NEW.value IS NOT OLD.value AND EXISTS
  (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_confidence_added AFTER INSERT ON settings
WHEN NEW.key = 'tag_confidence' AND EXISTS
  (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_file_added AFTER INSERT ON files
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
-- A file whose content changed moves to another asset under the same path.
CREATE TRIGGER event_block_file_changed AFTER UPDATE OF path, asset_id ON files
WHEN (NEW.path IS NOT OLD.path OR NEW.asset_id IS NOT OLD.asset_id)
  AND EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_block_file_deleted AFTER DELETE ON files
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind = 'folder') BEGIN
  UPDATE event_revision SET value = value + 1, visible_value = visible_value + 1 WHERE id = 1;
END;
