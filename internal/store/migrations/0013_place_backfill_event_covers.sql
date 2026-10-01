CREATE INDEX assets_pending_place ON assets (id)
WHERE
    place_checked = 0
    AND lat IS NOT NULL
    AND lon IS NOT NULL;

CREATE INDEX assets_event_cover ON assets (taken_at DESC, id DESC)
WHERE
    hidden = 0
    AND trashed_at IS NULL
    AND missing_since IS NULL
    AND thumb_version > 0;

ALTER TABLE event_revision
ADD COLUMN cover_value INTEGER NOT NULL DEFAULT 0;

DROP TRIGGER event_asset_changed;

CREATE TRIGGER event_asset_changed AFTER UPDATE OF taken_at, lat, lon, place_id, hidden, trashed_at, missing_since ON assets
WHEN NEW.taken_at IS NOT OLD.taken_at OR NEW.lat IS NOT OLD.lat OR NEW.lon IS NOT OLD.lon
  OR NEW.place_id IS NOT OLD.place_id OR NEW.hidden IS NOT OLD.hidden
  OR NEW.trashed_at IS NOT OLD.trashed_at OR NEW.missing_since IS NOT OLD.missing_since BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER event_cover_changed AFTER UPDATE OF thumb_version ON assets
WHEN NEW.thumb_version IS NOT OLD.thumb_version BEGIN
  UPDATE event_revision SET cover_value = cover_value + 1 WHERE id = 1;
END;
