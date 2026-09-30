-- Offline GeoNames city matches. IDs are GeoNames IDs, so locations remain
-- stable across indexing and source moves. Unmatched GPS stays unlabelled.
CREATE TABLE places (
  id      INTEGER PRIMARY KEY,
  name    TEXT NOT NULL,
  region  TEXT NOT NULL,
  country TEXT NOT NULL,
  lat     REAL NOT NULL,
  lon     REAL NOT NULL
);
ALTER TABLE assets ADD COLUMN place_checked INTEGER NOT NULL DEFAULT 0;
CREATE INDEX assets_by_place ON assets(place_id, taken_at DESC, id DESC)
  WHERE hidden = 0 AND trashed_at IS NULL AND missing_since IS NULL AND place_id IS NOT NULL;
