-- One cheap revision check lets event timelines reuse their grouping between
-- writes. The triggers cover only fields that affect event membership, name
-- or cover; favourites and AI tags do not force a regroup.
CREATE TABLE event_revision (id INTEGER PRIMARY KEY CHECK (id = 1), value INTEGER NOT NULL);
INSERT INTO event_revision(id, value) VALUES (1, 0);
CREATE TRIGGER event_asset_added AFTER INSERT ON assets BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_asset_changed AFTER UPDATE OF taken_at, lat, lon, place_id, hidden, trashed_at, missing_since, thumb_version ON assets BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_asset_deleted AFTER DELETE ON assets BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_name_added AFTER INSERT ON event_names BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_name_changed AFTER UPDATE ON event_names BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_name_deleted AFTER DELETE ON event_names BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
