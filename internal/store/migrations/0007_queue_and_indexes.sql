-- Schema v7: a job queue that reads in order, retries, and indexes for
-- foreign keys that had none.

-- Deleting a face or a person looked up these columns with a full scan
-- inside the writer's transaction.
CREATE INDEX people_by_cover ON people (cover_face);
CREATE INDEX face_rejections_by_person ON face_rejections (person_id);

-- Each job carries its asset's capture time, so the next batch (newest
-- first) is read in index order instead of sorting every pending job.
-- Triggers keep the copy equal to assets.taken_at.
ALTER TABLE asset_jobs ADD COLUMN taken_at INTEGER NOT NULL DEFAULT 0;
UPDATE asset_jobs SET taken_at = (SELECT taken_at FROM assets WHERE id = asset_jobs.asset_id);

CREATE TRIGGER asset_jobs_taken_at_insert AFTER INSERT ON asset_jobs
BEGIN
  UPDATE asset_jobs SET taken_at = (SELECT taken_at FROM assets WHERE id = NEW.asset_id)
    WHERE asset_id = NEW.asset_id AND stage = NEW.stage;
END;

CREATE TRIGGER asset_jobs_taken_at_update AFTER UPDATE OF taken_at ON assets
BEGIN
  UPDATE asset_jobs SET taken_at = NEW.taken_at WHERE asset_id = NEW.id;
END;

CREATE INDEX asset_jobs_pending ON asset_jobs (stage, taken_at DESC, asset_id DESC)
  WHERE status = 'pending';

-- How many times a job failed in a row. A failure may be passing (a drive
-- unplugged, a file locked), so failed jobs are retried a few times when
-- their files are seen again.
ALTER TABLE asset_jobs ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
