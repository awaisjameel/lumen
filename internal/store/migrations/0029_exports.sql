CREATE TABLE export_batches (id INTEGER PRIMARY KEY AUTOINCREMENT);
CREATE TABLE export_jobs (
    id INTEGER PRIMARY KEY,
    batch INTEGER NOT NULL REFERENCES export_batches(id),
    asset_id INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    from_path TEXT NOT NULL,
    to_path TEXT NOT NULL,
    size INTEGER NOT NULL,
    mtime INTEGER NOT NULL,
    options TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'planned',
    error TEXT NOT NULL DEFAULT '',
    temp TEXT NOT NULL DEFAULT '',
    digest TEXT NOT NULL DEFAULT ''
);
CREATE INDEX export_jobs_batch ON export_jobs(batch, id);
