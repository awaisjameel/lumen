DROP TRIGGER event_block_face_added;

DROP TRIGGER event_block_face_changed;

DROP TRIGGER event_block_face_deleted;

DROP TRIGGER event_block_tag_added;

DROP TRIGGER event_block_tag_changed;

DROP TRIGGER event_block_tag_deleted;

CREATE TRIGGER event_block_face_added AFTER INSERT ON faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind IN ('person', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER event_block_face_changed AFTER UPDATE OF person_id ON faces
WHEN NEW.person_id IS NOT OLD.person_id AND EXISTS
  (SELECT 1 FROM blocks WHERE kind IN ('person', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER event_block_face_deleted AFTER DELETE ON faces
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind IN ('person', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER event_block_tag_added AFTER INSERT ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER event_block_tag_changed AFTER UPDATE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER event_block_tag_deleted AFTER DELETE ON asset_tags
WHEN EXISTS (SELECT 1 FROM blocks WHERE kind IN ('tag', 'album') AND mode = 'photos') BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
