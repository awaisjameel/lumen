-- Names are attached to the oldest asset in each automatically detected
-- event. If an earlier photo joins that event, the group gets a new anchor.
CREATE TABLE event_names (
  anchor_asset_id INTEGER PRIMARY KEY REFERENCES assets(id) ON DELETE CASCADE,
  name TEXT NOT NULL
);
