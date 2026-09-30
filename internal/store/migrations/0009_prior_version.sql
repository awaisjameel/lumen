-- Schema v9: a finished job an upgrade puts back in the queue remembers the
-- version its output was made by, so the stage can keep that output where
-- the new version would make the same (a thumbnail of a photo without a
-- colour profile, faces of a photo that has none) instead of decoding the
-- photo again. NULL: the stage runs in full. Closing the job clears it.
ALTER TABLE asset_jobs ADD COLUMN prior_version TEXT;
