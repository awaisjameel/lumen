-- Schema v8: jobs held back by an earlier stage are 'waiting', not
-- 'pending', so asset_jobs_pending holds only jobs a worker can run. A stage
-- whose earlier stage had finished nothing walked its whole backlog to find
-- no work (85 ms per batch at 100,000 photos). The store moves a job to
-- 'pending' when the last earlier stage it needs is saved (store.settle).
UPDATE asset_jobs SET status = 'waiting'
  WHERE status = 'pending' AND EXISTS (SELECT 1 FROM asset_jobs p
    WHERE p.asset_id = asset_jobs.asset_id AND p.status IN ('pending', 'waiting')
    AND (p.stage = 'metadata' AND asset_jobs.stage IN ('thumbnail', 'embedding', 'faces')
      OR p.stage = 'thumbnail' AND asset_jobs.stage IN ('embedding', 'faces')));

-- The v7 triggers ran an UPDATE for every job a new asset got (Ingest now
-- writes the capture time with the job) and for every save that listed
-- taken_at, changed or not.
DROP TRIGGER asset_jobs_taken_at_insert;
CREATE TRIGGER asset_jobs_taken_at_insert AFTER INSERT ON asset_jobs WHEN NEW.taken_at = 0
BEGIN
  UPDATE asset_jobs SET taken_at = (SELECT taken_at FROM assets WHERE id = NEW.asset_id)
    WHERE asset_id = NEW.asset_id AND stage = NEW.stage;
END;

DROP TRIGGER asset_jobs_taken_at_update;
CREATE TRIGGER asset_jobs_taken_at_update AFTER UPDATE OF taken_at ON assets WHEN NEW.taken_at IS NOT OLD.taken_at
BEGIN
  UPDATE asset_jobs SET taken_at = NEW.taken_at WHERE asset_id = NEW.id;
END;
