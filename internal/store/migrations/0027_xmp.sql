-- Schema v27: XMP sidecars (Phase 5, unit U11).
-- Safe to run again: the upgrade tests replay migrations over a current schema.

-- The sidecars Lumen wrote: which file, and the hash of the bytes written.
-- Lumen replaces a sidecar only while the file on disk still hashes to this,
-- and skips the write when what it would write hashes the same. A sidecar with
-- no row here is someone else's and is never touched. The path is the
-- sidecar's (IMG_1.jpg.xmp), unique by its fold key like files.path, so a
-- re-spelled path is the same file; the asset is whichever photo's file the
-- sidecar sits beside now. No foreign key: when an asset is deleted its row
-- must stay, so that the names in the sidecar it leaves behind can be taken out.
CREATE TABLE IF NOT EXISTS xmp_exports (
  asset_id INTEGER NOT NULL,
  path     TEXT    NOT NULL,
  hash     BLOB    NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS xmp_exports_path
  ON xmp_exports ((CASE WHEN octet_length(path) != length(path) THEN fold(path) ELSE lower(path) END));
CREATE INDEX IF NOT EXISTS xmp_exports_asset ON xmp_exports (asset_id);

-- Sidecars Lumen left alone (someone else's, edited since, or not writable),
-- and the file as it was then: size and modification time in nanoseconds,
-- or size -1 for no file at all. A background pass does not look at the path
-- again until the file differs from this; Export now always does.
CREATE TABLE IF NOT EXISTS xmp_skips (
  path  TEXT    NOT NULL,
  size  INTEGER NOT NULL,
  mtime INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS xmp_skips_path
  ON xmp_skips ((CASE WHEN octet_length(path) != length(path) THEN fold(path) ELSE lower(path) END));

-- Counts changes to what a sidecar says. Together with event_revision's
-- visible_value (which assets may be exported at all) it tells the exporter
-- that something may be out of date. The triggers below count only while
-- the user has XMP export on, so a library without it pays nothing.
CREATE TABLE IF NOT EXISTS xmp_revision (id INTEGER PRIMARY KEY CHECK (id = 1), value INTEGER NOT NULL);
INSERT OR IGNORE INTO xmp_revision (id, value) VALUES (1, 0);

-- A photo's favourite mark and its size (the face regions are stated against it).
CREATE TRIGGER IF NOT EXISTS xmp_asset_changed AFTER UPDATE OF favourite, width, height ON assets
WHEN (NEW.favourite IS NOT OLD.favourite OR NEW.width IS NOT OLD.width OR NEW.height IS NOT OLD.height)
  AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;

-- The files a sidecar goes beside.
CREATE TRIGGER IF NOT EXISTS xmp_file_added AFTER INSERT ON files
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_file_changed AFTER UPDATE OF asset_id, path ON files
WHEN (NEW.asset_id IS NOT OLD.asset_id OR NEW.path IS NOT OLD.path)
  AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_file_deleted AFTER DELETE ON files
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;

-- Things on photos, and the Things themselves (a rename, or a hidden sink).
CREATE TRIGGER IF NOT EXISTS xmp_tag_applied AFTER INSERT ON asset_tags
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_tag_changed AFTER UPDATE ON asset_tags
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_tag_removed AFTER DELETE ON asset_tags
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_thing_changed AFTER UPDATE OF name, hidden ON tags
WHEN (NEW.name IS NOT OLD.name OR NEW.hidden IS NOT OLD.hidden)
  AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;

-- The tag confidence decides which AI tags apply.
CREATE TRIGGER IF NOT EXISTS xmp_confidence_added AFTER INSERT ON settings
WHEN NEW.key = 'tag_confidence' AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_confidence_changed AFTER UPDATE OF value ON settings
WHEN NEW.key = 'tag_confidence' AND NEW.value IS NOT OLD.value
  AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;

-- Who is in a photo: a face put with a person, moved, or taken away, and a
-- person's name. A face with no person says nothing.
CREATE TRIGGER IF NOT EXISTS xmp_face_added AFTER INSERT ON faces
WHEN NEW.person_id IS NOT NULL AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_face_changed AFTER UPDATE OF person_id, x, y, w, h ON faces
WHEN (NEW.person_id IS NOT OLD.person_id OR NEW.x IS NOT OLD.x OR NEW.y IS NOT OLD.y
    OR NEW.w IS NOT OLD.w OR NEW.h IS NOT OLD.h)
  AND (OLD.person_id IS NOT NULL OR NEW.person_id IS NOT NULL)
  AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_face_removed AFTER DELETE ON faces
WHEN OLD.person_id IS NOT NULL AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_person_changed AFTER UPDATE OF name ON people
WHEN NEW.name IS NOT OLD.name AND (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_person_removed AFTER DELETE ON people
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;

-- A blocked face's name stays out of sidecars even when its block hides only
-- the label, which visible_value does not count: what the block covers.
CREATE TRIGGER IF NOT EXISTS xmp_face_block_added AFTER INSERT ON face_block_faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS xmp_face_block_removed AFTER DELETE ON face_block_faces
WHEN (SELECT value FROM settings WHERE key = 'xmp_enabled') = '1' BEGIN
  UPDATE xmp_revision SET value = value + 1 WHERE id = 1;
END;
