-- Schema v2: index the job queue by status first. Progress counts only the
-- unfinished jobs (status IN ('pending', 'error', 'skipped')), which with
-- status leading is a few small range scans instead of a walk over every
-- finished job; the stage workers' "stage = ? AND status = 'pending'" lookup
-- uses it just as well.
DROP INDEX asset_jobs_queue;
CREATE INDEX asset_jobs_status ON asset_jobs (status, stage);
