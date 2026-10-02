-- Organise on disk: a run of moves is journaled here so that a crash part-way
-- leaves a record of what was meant, what was done and what was in doubt. A
-- move's row becomes 'moving' (committed) before the file is touched, and
-- 'moved' in the same transaction that points files.path at the new place.
-- The IF NOT EXISTS clauses let the upgrade tests replay this migration.

-- AUTOINCREMENT: previews are deleted when a newer one is made, and a run id
-- the UI still holds must never name a different run.
CREATE TABLE IF NOT EXISTS organise_runs (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  template   TEXT    NOT NULL,
  created_at INTEGER NOT NULL,
  state      TEXT    NOT NULL CHECK (state IN ('planned', 'running', 'done', 'undoing', 'undone', 'failed'))
);

-- file_id is the files row that moves; it is not a foreign key because a
-- file the library forgets later must not take its history with it. size and
-- mtime (Unix nanoseconds, as files.mtime) are the file as previewed: a
-- file that no longer matches them is skipped, and a crashed move is only
-- finished when the file at the destination still matches.
CREATE TABLE IF NOT EXISTS organise_moves (
  run_id    INTEGER NOT NULL REFERENCES organise_runs (id) ON DELETE CASCADE,
  seq       INTEGER NOT NULL,
  file_id   INTEGER,
  asset_id  INTEGER NOT NULL,
  from_path TEXT    NOT NULL,
  to_path   TEXT    NOT NULL,
  size      INTEGER NOT NULL DEFAULT 0,
  mtime     INTEGER NOT NULL DEFAULT 0,
  renamed   INTEGER NOT NULL DEFAULT 0,   -- the name took a " (2)" for a collision
  -- The photo's XMP sidecar (its name plus ".xmp") moves with it, as part of
  -- the same move: '' none, 'planned' one to move, 'moved', 'kept' where it
  -- was (the reason is in error), 'undone' put back with the photo.
  sidecar   TEXT    NOT NULL DEFAULT '' CHECK (sidecar IN ('', 'planned', 'moved', 'kept', 'undone')),
  state     TEXT    NOT NULL CHECK (state IN ('planned', 'moving', 'moved', 'undoing', 'undone', 'skipped', 'failed')),
  error     TEXT,
  PRIMARY KEY (run_id, seq)
) WITHOUT ROWID;

-- Interrupted moves are found at startup without reading every row.
CREATE INDEX IF NOT EXISTS organise_moves_open ON organise_moves (state, run_id) WHERE state IN ('moving', 'undoing');

-- Folders Lumen created for a run, journaled before they are made. Undo
-- removes these, and only these, once they are empty.
CREATE TABLE IF NOT EXISTS organise_dirs (
  run_id INTEGER NOT NULL REFERENCES organise_runs (id) ON DELETE CASCADE,
  path   TEXT    NOT NULL,
  PRIMARY KEY (run_id, path)
) WITHOUT ROWID;
