-- When each asset's thumbnail was last shown, so the size cap can drop the
-- least recently viewed first. Views are batched in memory and written every
-- half minute, so a row can lag the screen by that long.
--
-- viewed_at = 0 marks a thumbnail the cap evicted: it is no longer on disk,
-- so it must not come up as a candidate again. The next view replaces the
-- zero, and so does a rebuild by the thumbnail stage (the trigger below).
-- Every statement is IF NOT EXISTS, since tests re-run migrations over a
-- database that has them.
CREATE TABLE IF NOT EXISTS thumb_views (
  asset_id  INTEGER PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
  viewed_at INTEGER NOT NULL
) WITHOUT ROWID;

-- Candidates seen at least once, oldest view first.
CREATE INDEX IF NOT EXISTS thumb_views_oldest ON thumb_views (viewed_at, asset_id) WHERE viewed_at > 0;

-- Candidates never seen, oldest first. The partial index holds only assets
-- that have a thumbnail, and a new one is added once, so it costs nothing to keep.
CREATE INDEX IF NOT EXISTS assets_thumb_added ON assets (added_at, id) WHERE thumb_version > 0;

CREATE TRIGGER IF NOT EXISTS thumb_views_rebuilt AFTER UPDATE OF thumb_version ON assets
WHEN NEW.thumb_version IS NOT OLD.thumb_version BEGIN
  DELETE FROM thumb_views WHERE asset_id = NEW.id AND viewed_at = 0;
END;
