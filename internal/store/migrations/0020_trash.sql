-- The Trash view reads exactly the assets the app has trashed that are still
-- on disk and not hidden (store.trashed spells the clause the one way SQLite
-- must see it to use this index). Newest trashed first is the order the count
-- and the restore-all update walk; the grid itself orders by capture time.
CREATE INDEX IF NOT EXISTS assets_trash ON assets (trashed_at DESC, id DESC)
  WHERE trashed_at IS NOT NULL AND missing_since IS NULL AND hidden = 0;
