-- Phase 5: user-owned collections. Smart album rules are validated by the
-- store and evaluated against current metadata whenever the album is opened.
CREATE TABLE albums (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
  kind       TEXT NOT NULL CHECK (kind IN ('manual', 'smart')),
  rule_json  TEXT,
  created_at INTEGER NOT NULL
);

CREATE TABLE album_items (
  album_id INTEGER NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  asset_id INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  added_at INTEGER NOT NULL,
  PRIMARY KEY (album_id, asset_id)
) WITHOUT ROWID;
CREATE INDEX album_items_asset ON album_items(asset_id);
