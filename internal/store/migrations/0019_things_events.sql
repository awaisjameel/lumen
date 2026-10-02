-- Things can be renamed and pinned, and events can be pinned (and, later,
-- blocked): both keep their identity as photos come and go.

-- A built-in Thing's name in the vocabulary. The vocabulary update finds a
-- built-in by it, so one the user renamed keeps its prompts and matches. It
-- is NULL for the user's own Things.
ALTER TABLE tags ADD COLUMN vocab_name TEXT;
UPDATE tags SET vocab_name = name WHERE kind = 'builtin';
CREATE UNIQUE INDEX tags_vocab_name ON tags (vocab_name) WHERE vocab_name IS NOT NULL;

-- When the user pinned the Thing; NULL when it is not pinned.
ALTER TABLE tags ADD COLUMN pinned_at INTEGER;

-- An event has no row of its own: it is a run of photos, regrouped as the
-- library changes. What the user says about one (its name, a pin, later a
-- block) is an event tag, and the photos of the event it was set on are the
-- tag's members. A run carries a tag when it holds any of its members;
-- settling (store/eventtags.go) keeps the members equal to the run.
CREATE TABLE event_tags (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('name', 'pin', 'block')),
  name       TEXT,                    -- kind = 'name' only
  created_at INTEGER NOT NULL,        -- a pin's is also its order: later pins sort first
  CHECK ((kind = 'name') = (name IS NOT NULL))
);

CREATE TABLE event_members (
  tag_id   INTEGER NOT NULL REFERENCES event_tags (id) ON DELETE CASCADE,
  asset_id INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  moved    INTEGER NOT NULL DEFAULT 0,   -- its date or place changed since the last settle
  PRIMARY KEY (tag_id, asset_id)
) WITHOUT ROWID;
CREATE INDEX event_members_asset ON event_members (asset_id);

-- The times at which the timeline changed since the last settle: the old and
-- new date of each photo that came, went, moved or changed visibility. Only
-- filled while an event tag exists, and emptied by the transaction that
-- filled it (Store.tx settles before it commits). The time is the key, so a
-- batch of photos from one afternoon leaves one row.
CREATE TABLE event_dirty (at INTEGER PRIMARY KEY);

-- Every name the user gave an event becomes a name tag (one per distinct
-- name), held by the photos that carried the name. The tag ids follow the
-- date of each name's earliest photo, so where a run holds two names the
-- earlier one still shows, as it did when the earliest named photo won.
INSERT INTO event_tags (kind, name, created_at)
  SELECT 'name', n.name, CAST(strftime('%s') AS INTEGER)
  FROM event_names n JOIN assets a ON a.id = n.anchor_asset_id
  GROUP BY n.name
  ORDER BY min(a.taken_at), min(a.id);
INSERT INTO event_members (tag_id, asset_id)
  SELECT t.id, n.anchor_asset_id
  FROM event_names n JOIN event_tags t ON t.kind = 'name' AND t.name = n.name;
DROP TABLE event_names; -- its triggers go with it

-- Membership and tags change what the events views show.
CREATE TRIGGER event_tag_added AFTER INSERT ON event_tags BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_tag_changed AFTER UPDATE ON event_tags BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_tag_deleted AFTER DELETE ON event_tags BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_member_added AFTER INSERT ON event_members BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;
CREATE TRIGGER event_member_deleted AFTER DELETE ON event_members BEGIN
  UPDATE event_revision SET value = value + 1 WHERE id = 1;
END;

-- What settling must look at again. Each trigger checks first that an event
-- tag exists, so a library without one pays a lookup per changed photo and
-- nothing more.
CREATE TRIGGER event_dirty_asset_added AFTER INSERT ON assets
WHEN EXISTS (SELECT 1 FROM event_tags) BEGIN
  INSERT OR IGNORE INTO event_dirty (at) VALUES (NEW.taken_at);
END;
CREATE TRIGGER event_dirty_asset_deleted AFTER DELETE ON assets
WHEN EXISTS (SELECT 1 FROM event_tags) BEGIN
  INSERT OR IGNORE INTO event_dirty (at) VALUES (OLD.taken_at);
END;
CREATE TRIGGER event_dirty_asset_changed AFTER UPDATE OF taken_at, lat, lon, hidden, trashed_at, missing_since ON assets
WHEN (NEW.taken_at IS NOT OLD.taken_at OR NEW.lat IS NOT OLD.lat OR NEW.lon IS NOT OLD.lon
    OR NEW.hidden IS NOT OLD.hidden OR NEW.trashed_at IS NOT OLD.trashed_at OR NEW.missing_since IS NOT OLD.missing_since)
  AND EXISTS (SELECT 1 FROM event_tags) BEGIN
  INSERT OR IGNORE INTO event_dirty (at) VALUES (OLD.taken_at), (NEW.taken_at);
END;

-- A member whose date or place changed may have left its event: settling
-- keeps it only where the event it joined holds an unmoved member too.
CREATE TRIGGER event_member_moved AFTER UPDATE OF taken_at, lat, lon ON assets
WHEN (NEW.taken_at IS NOT OLD.taken_at OR NEW.lat IS NOT OLD.lat OR NEW.lon IS NOT OLD.lon)
  AND EXISTS (SELECT 1 FROM event_tags) BEGIN
  UPDATE event_members SET moved = 1 WHERE asset_id = NEW.id AND moved = 0;
END;
