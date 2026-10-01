-- Media kinds never change for content-keyed assets. Keeping the kind on each
-- job lets an empty photo or video fetch skip the other kind's entire backlog.
ALTER TABLE asset_jobs ADD COLUMN kind TEXT NOT NULL DEFAULT '';
UPDATE asset_jobs SET kind = COALESCE((SELECT kind FROM assets WHERE id = asset_jobs.asset_id), '');

CREATE INDEX asset_jobs_pending_kind ON asset_jobs (stage, kind, taken_at DESC, asset_id DESC)
  WHERE status = 'pending';

-- Keep asset_jobs_pending too: reads across both kinds need capture-time order
-- without sorting the backlog. The extra pending-index write avoids that sort
-- on every metadata and thumbnail batch.
