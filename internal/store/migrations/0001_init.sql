-- Schema v1: the fast library browser (Phase 1). Later phases add their own
-- tables (people, faces, tags, albums, events, vectors) in new migrations.

-- Folders the user asked Lumen to look in.
CREATE TABLE sources (
  id       INTEGER PRIMARY KEY,
  path     TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  enabled  INTEGER NOT NULL DEFAULT 1,
  added_at INTEGER NOT NULL
);

-- One row per unique piece of content. The content hash is the identity, so
-- moving or renaming a file keeps everything attached to its asset.
CREATE TABLE assets (
  id            INTEGER PRIMARY KEY,
  content_hash  BLOB    NOT NULL UNIQUE,
  kind          TEXT    NOT NULL,               -- 'photo' | 'video'
  ext           TEXT    NOT NULL,               -- '.jpg', '.heic', ...
  taken_at      INTEGER NOT NULL,               -- wall-clock seconds (core.WallClock)
  date_from     TEXT    NOT NULL DEFAULT 'file',-- 'exif' | 'filename' | 'file'
  width         INTEGER,
  height        INTEGER,                        -- as displayed (orientation applied)
  duration_ms   INTEGER,
  orientation   INTEGER NOT NULL DEFAULT 1,     -- EXIF orientation 1..8
  lat           REAL,
  lon           REAL,
  place_id      INTEGER,
  phash         INTEGER,                        -- 64-bit perceptual hash
  camera        TEXT,
  favourite     INTEGER NOT NULL DEFAULT 0,
  hidden        INTEGER NOT NULL DEFAULT 0,
  trashed_at    INTEGER,                        -- app-level trash; the file is untouched
  missing_since INTEGER,                        -- no file on disk points here any more
  quality       REAL,
  thumb_version INTEGER NOT NULL DEFAULT 0,     -- 0 = no thumbnail yet; bumps bust caches
  added_at      INTEGER NOT NULL
);

-- The grid's only access path: visible assets, newest first. Queries must repeat
-- this WHERE clause exactly for SQLite to use the partial index.
CREATE INDEX assets_timeline ON assets (taken_at DESC, id DESC)
  WHERE hidden = 0 AND trashed_at IS NULL AND missing_since IS NULL;

-- Physical files. Several files can share one asset (exact duplicates).
CREATE TABLE files (
  id        INTEGER PRIMARY KEY,
  asset_id  INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  source_id INTEGER NOT NULL REFERENCES sources (id) ON DELETE CASCADE,
  path      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  size      INTEGER NOT NULL,
  mtime     INTEGER NOT NULL                    -- unix nanoseconds
);
CREATE INDEX files_asset ON files (asset_id);
CREATE INDEX files_source ON files (source_id);

-- Per-stage processing state: the durable job queue.
CREATE TABLE asset_jobs (
  asset_id      INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  stage         TEXT    NOT NULL,
  status        TEXT    NOT NULL,               -- 'pending' | 'done' | 'error' | 'skipped'
  model_version TEXT,
  error         TEXT,
  updated_at    INTEGER NOT NULL,
  PRIMARY KEY (asset_id, stage)
) WITHOUT ROWID;
CREATE INDEX asset_jobs_queue ON asset_jobs (stage, status);

CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
) WITHOUT ROWID;
