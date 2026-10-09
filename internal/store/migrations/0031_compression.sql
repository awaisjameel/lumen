-- Candidates and publication are separate: a reviewed file survives a restart.
CREATE TABLE IF NOT EXISTS compression_runs (
  id INTEGER PRIMARY KEY,
  created_at INTEGER NOT NULL,
  options TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'planned'
);
CREATE TABLE IF NOT EXISTS compression_jobs (
  id INTEGER PRIMARY KEY,
  run_id INTEGER NOT NULL REFERENCES compression_runs(id) ON DELETE CASCADE,
  asset_id INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  from_path TEXT NOT NULL,
  to_path TEXT NOT NULL,
  copies INTEGER NOT NULL,
  size INTEGER NOT NULL,
  mtime INTEGER NOT NULL,
  digest TEXT NOT NULL DEFAULT '',
  output_size INTEGER NOT NULL DEFAULT 0,
  output_digest TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'planned',
  reason TEXT NOT NULL DEFAULT '',
  verification TEXT NOT NULL DEFAULT '',
  tool TEXT NOT NULL DEFAULT '',
  temp TEXT NOT NULL DEFAULT '',
  publish_temp TEXT NOT NULL DEFAULT '',
  sidecar_digest TEXT NOT NULL DEFAULT '',
  sidecar_size INTEGER NOT NULL DEFAULT 0,
  restore_digest TEXT NOT NULL DEFAULT '',
  restore_size INTEGER NOT NULL DEFAULT 0,
  output_asset_id INTEGER REFERENCES assets(id) ON DELETE SET NULL,
  UNIQUE(run_id, from_path)
);
CREATE INDEX IF NOT EXISTS compression_jobs_run ON compression_jobs(run_id, id);
