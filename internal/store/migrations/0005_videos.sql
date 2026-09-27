-- Schema v5: video codecs and frame-level results (Phase 4).
ALTER TABLE assets ADD COLUMN video_codec TEXT;
ALTER TABLE assets ADD COLUMN audio_codec TEXT;
ALTER TABLE faces ADD COLUMN at_ms INTEGER NOT NULL DEFAULT 0;

-- A video has several searchable frames. Each vector belongs to its moment.
CREATE TABLE video_embeddings (
  asset_id INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  at_ms    INTEGER NOT NULL,
  model    TEXT    NOT NULL,
  scale    REAL    NOT NULL,
  vec      BLOB    NOT NULL,
  PRIMARY KEY (asset_id, at_ms)
) WITHOUT ROWID;
