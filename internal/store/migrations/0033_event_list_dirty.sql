-- The list has its own clock: event_revision also counts deferred block work,
-- whose changed coverage is known exactly only after settlement. Recording
-- blocked_assets changes here keeps list invalidation in that same transaction.
CREATE TABLE IF NOT EXISTS event_list_revision (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  value INTEGER NOT NULL,
  full_value INTEGER NOT NULL,
  floor INTEGER NOT NULL
);
INSERT OR IGNORE INTO event_list_revision VALUES (1, 0, 0, 0);
CREATE TABLE IF NOT EXISTS event_list_dirty (at INTEGER PRIMARY KEY, revision INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS event_list_dirty_revision ON event_list_dirty(revision);

-- Covers keep their existing separate revision, so a thumbnail does not
-- invalidate grouping. Its date limits the cover refresh to affected events.
CREATE TABLE IF NOT EXISTS event_list_cover_dirty (at INTEGER PRIMARY KEY, revision INTEGER NOT NULL);
CREATE INDEX IF NOT EXISTS event_list_cover_dirty_revision ON event_list_cover_dirty(revision);
DROP TRIGGER IF EXISTS event_cover_changed;
CREATE TRIGGER IF NOT EXISTS event_cover_changed AFTER UPDATE OF thumb_version ON assets
WHEN NEW.thumb_version IS NOT OLD.thumb_version BEGIN
  UPDATE event_revision SET cover_value = cover_value + 1 WHERE id = 1;
  INSERT INTO event_list_cover_dirty(at, revision)
    SELECT NEW.taken_at, cover_value FROM event_revision WHERE id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

-- Prune once per batch to avoid extra writes for every changed photo.
-- Keeping 4033 revisions
-- leaves room for the next 63, so history never exceeds 4096 revisions.
-- A reader that falls behind the retained history must regroup from scratch.
CREATE TRIGGER IF NOT EXISTS event_list_prune AFTER UPDATE OF value ON event_list_revision
WHEN NEW.value % 64 = 0 BEGIN
  DELETE FROM event_list_dirty WHERE revision <= NEW.value - 4033;
  UPDATE event_list_revision SET floor = MAX(floor, NEW.value - 4033) WHERE id = 1;
END;
CREATE TRIGGER IF NOT EXISTS event_list_cover_prune AFTER UPDATE OF cover_value ON event_revision
WHEN NEW.cover_value % 64 = 0 BEGIN
  DELETE FROM event_list_cover_dirty WHERE revision <= NEW.cover_value - 4033;
END;

CREATE TRIGGER IF NOT EXISTS event_list_asset_added AFTER INSERT ON assets BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT NEW.taken_at, value FROM event_list_revision WHERE id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_asset_deleted AFTER DELETE ON assets BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT OLD.taken_at, value FROM event_list_revision WHERE id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_asset_changed AFTER UPDATE OF id, taken_at, lat, lon, place_id, hidden, trashed_at, missing_since ON assets
WHEN NEW.id IS NOT OLD.id OR NEW.taken_at IS NOT OLD.taken_at OR NEW.lat IS NOT OLD.lat OR NEW.lon IS NOT OLD.lon
  OR NEW.place_id IS NOT OLD.place_id OR NEW.hidden IS NOT OLD.hidden
  OR NEW.trashed_at IS NOT OLD.trashed_at OR NEW.missing_since IS NOT OLD.missing_since BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT OLD.taken_at, value FROM event_list_revision WHERE id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
  INSERT INTO event_list_dirty(at, revision)
    SELECT NEW.taken_at, value FROM event_list_revision WHERE id = 1 AND NEW.taken_at IS NOT OLD.taken_at
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_member_added AFTER INSERT ON event_members BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = NEW.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_member_deleted AFTER DELETE ON event_members BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = OLD.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_member_changed AFTER UPDATE OF asset_id, tag_id ON event_members
WHEN NEW.asset_id IS NOT OLD.asset_id OR NEW.tag_id IS NOT OLD.tag_id BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = OLD.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = NEW.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_coverage_added AFTER INSERT ON blocked_assets BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = NEW.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_coverage_deleted AFTER DELETE ON blocked_assets BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = OLD.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

CREATE TRIGGER IF NOT EXISTS event_list_coverage_changed AFTER UPDATE OF asset_id ON blocked_assets
WHEN NEW.asset_id IS NOT OLD.asset_id BEGIN
  UPDATE event_list_revision SET value = value + 1 WHERE id = 1;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = OLD.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
  INSERT INTO event_list_dirty(at, revision)
    SELECT a.taken_at, r.value FROM assets a CROSS JOIN event_list_revision r
    WHERE a.id = NEW.asset_id AND r.id = 1
    ON CONFLICT(at) DO UPDATE SET revision = excluded.revision;
END;

-- These inputs can change labels or visibility across the whole timeline.

CREATE TRIGGER IF NOT EXISTS event_list_block_added AFTER INSERT ON blocks BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_block_changed AFTER UPDATE ON blocks BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_block_deleted AFTER DELETE ON blocks BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_tag_changed AFTER UPDATE OF kind, name, created_at ON event_tags BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_place_added AFTER INSERT ON places BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_place_changed AFTER UPDATE ON places BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_place_deleted AFTER DELETE ON places BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_gap_added AFTER INSERT ON settings WHEN NEW.key = 'event_gap_hours' BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_gap_changed AFTER UPDATE ON settings WHEN OLD.key = 'event_gap_hours' OR NEW.key = 'event_gap_hours' BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;

CREATE TRIGGER IF NOT EXISTS event_list_gap_deleted AFTER DELETE ON settings WHEN OLD.key = 'event_gap_hours' BEGIN
  UPDATE event_list_revision SET value = value + 1, full_value = value + 1 WHERE id = 1;
END;
