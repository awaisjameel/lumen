-- Schema v3: semantic search and Things (Phase 2).

-- SigLIP image embeddings. The search index keeps a copy in memory; this is
-- the source of truth. int8, one scale per vector: vector = vec * scale.
CREATE TABLE embeddings (
  asset_id INTEGER PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
  model    TEXT    NOT NULL,                  -- model version that made it
  scale    REAL    NOT NULL,
  vec      BLOB    NOT NULL                   -- dim x int8
);

-- Things: labels the AI looks for, and tags people add by hand.
CREATE TABLE tags (
  id         INTEGER PRIMARY KEY,
  name       TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  kind       TEXT    NOT NULL,                -- 'builtin' | 'label' (user-made, AI-applied) | 'manual'
  prompts    TEXT,                            -- JSON array of captions the AI scores ('builtin', 'label')
  exclusive  INTEGER NOT NULL DEFAULT 0,      -- a broad kind of photo: each photo gets its best-fitting kind
  hidden     INTEGER NOT NULL DEFAULT 0,
  model      TEXT,                            -- model version of vecs
  vecs       BLOB,                            -- the prompts' text embeddings, float32
  created_at INTEGER NOT NULL
);

-- Which assets carry which tags. User edits always win: the AI only ever
-- writes 'ai' rows and never touches 'user' or 'rejected' ones.
CREATE TABLE asset_tags (
  asset_id INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  tag_id   INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
  source   TEXT    NOT NULL,                  -- 'ai' | 'user' | 'rejected' (the user removed an AI tag)
  score    REAL,                              -- AI probability, 0..1
  PRIMARY KEY (asset_id, tag_id)
) WITHOUT ROWID;
CREATE INDEX asset_tags_by_tag ON asset_tags (tag_id, source, score);

-- Every existing photo gets the new stage.
INSERT OR IGNORE INTO asset_jobs (asset_id, stage, status, updated_at)
  SELECT id, 'embedding', 'pending', CAST(strftime('%s') AS INTEGER) FROM assets;
