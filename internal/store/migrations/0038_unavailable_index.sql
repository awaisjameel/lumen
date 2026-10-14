-- Search reads the assets the user does not browse (store.UnavailableAssets)
-- again after every change of visibility. They are few in most libraries, so
-- this index holds only them rather than search testing every asset. Its
-- clause is the negation of assets_timeline's, which store.UnavailableAssets
-- repeats exactly for SQLite to use it, and it carries the clause's columns so
-- the query reads the index alone; blocked assets come from their own table.
CREATE INDEX IF NOT EXISTS assets_unavailable ON assets (hidden, trashed_at, missing_since)
  WHERE NOT (hidden = 0 AND trashed_at IS NULL AND missing_since IS NULL);
