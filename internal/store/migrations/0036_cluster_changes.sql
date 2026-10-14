-- Keep recent changes, including deletions, without an unbounded journal.
-- A reader older than the retained history falls back to the full snapshot.
CREATE TABLE IF NOT EXISTS cluster_revision (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  value INTEGER NOT NULL,
  reset_value INTEGER NOT NULL
);
INSERT OR IGNORE INTO cluster_revision VALUES (1, 0, 0);
CREATE TABLE IF NOT EXISTS cluster_faces (face_id INTEGER PRIMARY KEY, revision INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS cluster_faces_by_revision ON cluster_faces(revision);
DROP TRIGGER IF EXISTS cluster_history;
CREATE TRIGGER cluster_history AFTER UPDATE OF value ON cluster_revision BEGIN
  UPDATE cluster_revision SET reset_value = max(reset_value, NEW.value - 100000);
  DELETE FROM cluster_faces WHERE revision <= NEW.value - 100000;
END;
DROP TRIGGER IF EXISTS cluster_face_added;
CREATE TRIGGER cluster_face_added AFTER INSERT ON faces BEGIN
  UPDATE cluster_revision SET value = value + 1;
  INSERT INTO cluster_faces SELECT NEW.id, value FROM cluster_revision WHERE id = 1
    ON CONFLICT(face_id) DO UPDATE SET revision = excluded.revision;
END;
-- Foreign-key actions can override OR REPLACE, so coalescing uses an upsert.
DROP TRIGGER IF EXISTS cluster_face_changed;
CREATE TRIGGER cluster_face_changed AFTER UPDATE OF id, asset_id, at_ms, person_id, assigned_by, sharpness, yaw, model, vec ON faces BEGIN
  UPDATE cluster_revision SET value = value + 1;
  INSERT INTO cluster_faces SELECT OLD.id, value FROM cluster_revision WHERE id = 1 AND OLD.id <> NEW.id
    ON CONFLICT(face_id) DO UPDATE SET revision = excluded.revision;
  INSERT INTO cluster_faces SELECT NEW.id, value FROM cluster_revision WHERE id = 1
    ON CONFLICT(face_id) DO UPDATE SET revision = excluded.revision;
END;
DROP TRIGGER IF EXISTS cluster_face_deleted;
CREATE TRIGGER cluster_face_deleted AFTER DELETE ON faces BEGIN
  UPDATE cluster_revision SET value = value + 1;
  INSERT INTO cluster_faces SELECT OLD.id, value FROM cluster_revision WHERE id = 1
    ON CONFLICT(face_id) DO UPDATE SET revision = excluded.revision;
END;
DROP TRIGGER IF EXISTS cluster_asset_visibility;
CREATE TRIGGER cluster_asset_visibility AFTER UPDATE OF hidden, trashed_at, missing_since ON assets
WHEN NEW.hidden IS NOT OLD.hidden OR NEW.trashed_at IS NOT OLD.trashed_at OR NEW.missing_since IS NOT OLD.missing_since BEGIN
  UPDATE cluster_revision SET value = value + 1;
  INSERT INTO cluster_faces
    SELECT f.id, r.value FROM faces f CROSS JOIN cluster_revision r WHERE f.asset_id = NEW.id
    ON CONFLICT(face_id) DO UPDATE SET revision = excluded.revision;
END;
