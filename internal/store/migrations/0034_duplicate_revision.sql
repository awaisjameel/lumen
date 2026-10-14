-- Event grouping, visibility and covers already track dates, GPS, membership,
-- blocks and thumbnail versions. Only the remaining duplicate inputs need a
-- separate revision, including file changes when there are no folder blocks.
ALTER TABLE event_revision ADD COLUMN duplicate_value INTEGER NOT NULL DEFAULT 0;

CREATE TRIGGER duplicate_asset_changed
AFTER UPDATE OF phash, width, height, kind, ext, camera, date_from, added_at ON assets
WHEN NEW.phash IS NOT OLD.phash OR NEW.width IS NOT OLD.width OR NEW.height IS NOT OLD.height
  OR NEW.kind IS NOT OLD.kind OR NEW.ext IS NOT OLD.ext OR NEW.camera IS NOT OLD.camera
  OR NEW.date_from IS NOT OLD.date_from OR NEW.added_at IS NOT OLD.added_at BEGIN
  UPDATE event_revision SET duplicate_value = duplicate_value + 1 WHERE id = 1;
END;

CREATE TRIGGER duplicate_file_added AFTER INSERT ON files BEGIN
  UPDATE event_revision SET duplicate_value = duplicate_value + 1 WHERE id = 1;
END;
CREATE TRIGGER duplicate_file_deleted AFTER DELETE ON files BEGIN
  UPDATE event_revision SET duplicate_value = duplicate_value + 1 WHERE id = 1;
END;
CREATE TRIGGER duplicate_file_changed AFTER UPDATE OF asset_id, path, size, mtime ON files
WHEN NEW.asset_id IS NOT OLD.asset_id OR NEW.path IS NOT OLD.path
  OR NEW.size IS NOT OLD.size OR NEW.mtime IS NOT OLD.mtime BEGIN
  UPDATE event_revision SET duplicate_value = duplicate_value + 1 WHERE id = 1;
END;
