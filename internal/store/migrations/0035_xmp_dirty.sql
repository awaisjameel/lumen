-- Schema v35: durable, revision-guarded work for XMP export.
-- Deleted assets retain work until their abandoned sidecars have been cleared.
CREATE TABLE IF NOT EXISTS xmp_dirty (
  asset_id INTEGER PRIMARY KEY,
  revision INTEGER NOT NULL
);

DROP TRIGGER IF EXISTS xmp_asset_changed;
CREATE TRIGGER xmp_asset_changed AFTER UPDATE OF favourite, width, height, kind, hidden, trashed_at, missing_since ON assets
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (NEW.favourite IS NOT OLD.favourite OR NEW.width IS NOT OLD.width OR NEW.height IS NOT OLD.height OR NEW.kind IS NOT OLD.kind OR NEW.hidden IS NOT OLD.hidden OR NEW.trashed_at IS NOT OLD.trashed_at OR NEW.missing_since IS NOT OLD.missing_since) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT NEW.id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_asset_added;
CREATE TRIGGER xmp_asset_added AFTER INSERT ON assets
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT NEW.id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_asset_deleted;
CREATE TRIGGER xmp_asset_deleted AFTER DELETE ON assets
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_file_added;
CREATE TRIGGER xmp_file_added AFTER INSERT ON files
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT NEW.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_file_changed;
CREATE TRIGGER xmp_file_changed AFTER UPDATE OF asset_id, path ON files
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (NEW.asset_id IS NOT OLD.asset_id OR NEW.path IS NOT OLD.path) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id UNION SELECT NEW.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_file_deleted;
CREATE TRIGGER xmp_file_deleted AFTER DELETE ON files
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_tag_applied;
CREATE TRIGGER xmp_tag_applied AFTER INSERT ON asset_tags
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT NEW.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_tag_changed;
CREATE TRIGGER xmp_tag_changed AFTER UPDATE ON asset_tags
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id UNION SELECT NEW.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_tag_removed;
CREATE TRIGGER xmp_tag_removed AFTER DELETE ON asset_tags
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_thing_changed;
CREATE TRIGGER xmp_thing_changed AFTER UPDATE OF name, hidden ON tags
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (NEW.name IS NOT OLD.name OR NEW.hidden IS NOT OLD.hidden) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT asset_id FROM asset_tags WHERE tag_id = NEW.id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_face_added;
CREATE TRIGGER xmp_face_added AFTER INSERT ON faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND NEW.person_id IS NOT NULL BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT NEW.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_face_changed;
CREATE TRIGGER xmp_face_changed AFTER UPDATE OF person_id, asset_id, id, x, y, w, h ON faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (OLD.person_id IS NOT NULL OR NEW.person_id IS NOT NULL) AND (NEW.person_id IS NOT OLD.person_id OR NEW.asset_id IS NOT OLD.asset_id OR NEW.id IS NOT OLD.id OR NEW.x IS NOT OLD.x OR NEW.y IS NOT OLD.y OR NEW.w IS NOT OLD.w OR NEW.h IS NOT OLD.h) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id UNION SELECT NEW.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_face_removed;
CREATE TRIGGER xmp_face_removed AFTER DELETE ON faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND OLD.person_id IS NOT NULL BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_person_changed;
CREATE TRIGGER xmp_person_changed AFTER UPDATE OF name ON people
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND NEW.name IS NOT OLD.name BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT asset_id FROM faces WHERE person_id = NEW.id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_person_removed;
CREATE TRIGGER xmp_person_removed BEFORE DELETE ON people
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT asset_id FROM faces WHERE person_id = OLD.id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_face_block_added;
CREATE TRIGGER xmp_face_block_added AFTER INSERT ON face_block_faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT NEW.asset_id AS asset_id UNION SELECT asset_id FROM faces WHERE id IN (NEW.face_id)) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_face_block_removed;
CREATE TRIGGER xmp_face_block_removed AFTER DELETE ON face_block_faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id UNION SELECT asset_id FROM faces WHERE id IN (OLD.face_id)) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_face_block_changed;
CREATE TRIGGER xmp_face_block_changed AFTER UPDATE ON face_block_faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id UNION SELECT NEW.asset_id AS asset_id UNION SELECT asset_id FROM faces WHERE id IN (OLD.face_id, NEW.face_id)) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

-- Observe canonical coverage changes instead of duplicating block rules.
DROP TRIGGER IF EXISTS xmp_coverage_added;
CREATE TRIGGER xmp_coverage_added AFTER INSERT ON blocked_assets
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT NEW.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_coverage_removed;
CREATE TRIGGER xmp_coverage_removed AFTER DELETE ON blocked_assets
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT OLD.asset_id AS asset_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_label_block_added;
CREATE TRIGGER xmp_label_block_added AFTER INSERT ON blocks
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (NEW.kind IN ('tag', 'person')) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT asset_id FROM asset_tags WHERE NEW.kind = 'tag' AND tag_id = NEW.target_id UNION SELECT asset_id FROM faces WHERE NEW.kind = 'person' AND person_id = NEW.target_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_label_block_changed;
CREATE TRIGGER xmp_label_block_changed AFTER UPDATE ON blocks
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (OLD.kind IN ('tag', 'person') OR NEW.kind IN ('tag', 'person')) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT asset_id FROM asset_tags WHERE OLD.kind = 'tag' AND tag_id = OLD.target_id UNION SELECT asset_id FROM faces WHERE OLD.kind = 'person' AND person_id = OLD.target_id UNION SELECT asset_id FROM asset_tags WHERE NEW.kind = 'tag' AND tag_id = NEW.target_id UNION SELECT asset_id FROM faces WHERE NEW.kind = 'person' AND person_id = NEW.target_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_label_block_removed;
CREATE TRIGGER xmp_label_block_removed AFTER DELETE ON blocks
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (OLD.kind IN ('tag', 'person')) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT asset_id FROM asset_tags WHERE OLD.kind = 'tag' AND tag_id = OLD.target_id UNION SELECT asset_id FROM faces WHERE OLD.kind = 'person' AND person_id = OLD.target_id) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

-- These settings affect the whole library, including abandoned sidecars.
DROP TRIGGER IF EXISTS xmp_confidence_added;
CREATE TRIGGER xmp_confidence_added AFTER INSERT ON settings
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND NEW.key = 'tag_confidence' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT id AS asset_id FROM assets UNION SELECT asset_id FROM xmp_exports) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_confidence_changed;
CREATE TRIGGER xmp_confidence_changed AFTER UPDATE OF value, key ON settings
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND (NEW.key = 'tag_confidence' OR OLD.key = 'tag_confidence') AND (NEW.value IS NOT OLD.value OR NEW.key IS NOT OLD.key) BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT id AS asset_id FROM assets UNION SELECT asset_id FROM xmp_exports) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_confidence_removed;
CREATE TRIGGER xmp_confidence_removed AFTER DELETE ON settings
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' AND OLD.key = 'tag_confidence' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT id AS asset_id FROM assets UNION SELECT asset_id FROM xmp_exports) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_enabled_added;
CREATE TRIGGER xmp_enabled_added AFTER INSERT ON settings
WHEN NEW.key = 'xmp_enabled' AND NEW.value = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT id AS asset_id FROM assets UNION SELECT asset_id FROM xmp_exports) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

DROP TRIGGER IF EXISTS xmp_enabled_changed;
CREATE TRIGGER xmp_enabled_changed AFTER UPDATE OF value ON settings
WHEN NEW.key = 'xmp_enabled' AND NEW.value = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT id AS asset_id FROM assets UNION SELECT asset_id FROM xmp_exports) WHERE 1
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
END;

-- An upgrade must reconcile sidecars left by the previous full-pass exporter.
UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
INSERT INTO xmp_dirty(asset_id, revision) SELECT asset_id, (SELECT value FROM xmp_revision WHERE id = 1) FROM (SELECT id AS asset_id FROM assets UNION SELECT asset_id FROM xmp_exports) WHERE (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1'
    ON CONFLICT(asset_id) DO UPDATE SET revision = excluded.revision;
