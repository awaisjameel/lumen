-- Bulk changes and upgrades share the Go-owned definitions at settlement.
-- The marks suppress row maintenance until those definitions have been rebuilt.
CREATE TABLE IF NOT EXISTS count_rebuild (name TEXT PRIMARY KEY);
INSERT OR IGNORE INTO count_rebuild(name) VALUES ('day_counts'), ('tag_counts'), ('person_counts'), ('job_counts');

-- Keep the timeline's three filters in the transaction that changes visibility.
-- The empty key preserves SQLite's NULL day for dates outside strftime's range,
-- without making those assets disappear from the library total.
CREATE TABLE IF NOT EXISTS day_counts (
  day TEXT PRIMARY KEY NOT NULL,
  total INTEGER NOT NULL CHECK (total >= 0),
  favourites INTEGER NOT NULL CHECK (favourites BETWEEN 0 AND total),
  videos INTEGER NOT NULL CHECK (videos BETWEEN 0 AND total)
) WITHOUT ROWID;

CREATE TRIGGER IF NOT EXISTS day_counts_asset_added AFTER INSERT ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'day_counts') AND (NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = NEW.id)) BEGIN
  INSERT INTO day_counts(day, total, favourites, videos)
    SELECT COALESCE(strftime('%Y-%m-%d', NEW.taken_at, 'unixepoch'), ''), 1, NEW.favourite = 1, NEW.kind = 'video' WHERE true
    ON CONFLICT(day) DO UPDATE SET total = total + 1,
      favourites = favourites + excluded.favourites, videos = videos + excluded.videos;
END;

-- Subtract before the blocked row cascades away. Its deletion trigger can
-- then add back only assets that still exist, so deletion never counts twice.
CREATE TRIGGER IF NOT EXISTS day_counts_asset_deleted BEFORE DELETE ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'day_counts') AND (OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = OLD.id)) BEGIN
  UPDATE day_counts SET total = total - 1,
    favourites = favourites - (OLD.favourite = 1), videos = videos - (OLD.kind = 'video')
    WHERE day = COALESCE(strftime('%Y-%m-%d', OLD.taken_at, 'unixepoch'), '');
  DELETE FROM day_counts WHERE day = COALESCE(strftime('%Y-%m-%d', OLD.taken_at, 'unixepoch'), '') AND total = 0;
END;

CREATE TRIGGER IF NOT EXISTS day_counts_asset_changed
AFTER UPDATE OF hidden, trashed_at, missing_since, taken_at, kind, favourite ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'day_counts') AND (NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = NEW.id)
  AND ((OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL) != (NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL)
    OR ((NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL) AND (
      COALESCE(strftime('%Y-%m-%d', OLD.taken_at, 'unixepoch'), '') IS NOT COALESCE(strftime('%Y-%m-%d', NEW.taken_at, 'unixepoch'), '')
      OR (OLD.favourite = 1) != (NEW.favourite = 1)
      OR (OLD.kind = 'video') != (NEW.kind = 'video'))))) BEGIN
  UPDATE day_counts SET total = total - 1,
    favourites = favourites - (OLD.favourite = 1), videos = videos - (OLD.kind = 'video')
    WHERE day = COALESCE(strftime('%Y-%m-%d', OLD.taken_at, 'unixepoch'), '') AND OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL;
  DELETE FROM day_counts WHERE day = COALESCE(strftime('%Y-%m-%d', OLD.taken_at, 'unixepoch'), '') AND total = 0;
  INSERT INTO day_counts(day, total, favourites, videos)
    SELECT COALESCE(strftime('%Y-%m-%d', NEW.taken_at, 'unixepoch'), ''), 1, NEW.favourite = 1, NEW.kind = 'video' WHERE NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL
    ON CONFLICT(day) DO UPDATE SET total = total + 1,
      favourites = favourites + excluded.favourites, videos = videos + excluded.videos;
END;

CREATE TRIGGER IF NOT EXISTS day_counts_block_added AFTER INSERT ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'day_counts') AND (EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL)) BEGIN
  UPDATE day_counts SET total = total - 1,
    favourites = favourites - (SELECT favourite = 1 FROM assets WHERE id = NEW.asset_id),
    videos = videos - (SELECT kind = 'video' FROM assets WHERE id = NEW.asset_id)
    WHERE day = (SELECT COALESCE(strftime('%Y-%m-%d', a.taken_at, 'unixepoch'), '') FROM assets a WHERE a.id = NEW.asset_id);
  DELETE FROM day_counts WHERE total = 0
    AND day = (SELECT COALESCE(strftime('%Y-%m-%d', a.taken_at, 'unixepoch'), '') FROM assets a WHERE a.id = NEW.asset_id);
END;

CREATE TRIGGER IF NOT EXISTS day_counts_block_deleted AFTER DELETE ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'day_counts') AND (EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL)) BEGIN
  INSERT INTO day_counts(day, total, favourites, videos)
    SELECT COALESCE(strftime('%Y-%m-%d', a.taken_at, 'unixepoch'), ''), 1, a.favourite = 1, a.kind = 'video' FROM assets a WHERE a.id = OLD.asset_id
    ON CONFLICT(day) DO UPDATE SET total = total + 1,
      favourites = favourites + excluded.favourites, videos = videos + excluded.videos;
END;

-- These predicates mirror tagAppliesTo, tagConfidenceSQL and visible. Counts
-- include hidden labels; label visibility and pin order are read-time choices.
CREATE TABLE IF NOT EXISTS tag_counts (
  tag_id INTEGER PRIMARY KEY REFERENCES tags(id) ON DELETE CASCADE,
  n INTEGER NOT NULL CHECK (n >= 0)
);

CREATE TRIGGER IF NOT EXISTS tag_counts_tag_added AFTER INSERT ON tags
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') BEGIN
  INSERT INTO tag_counts(tag_id, n) VALUES (NEW.id, 0);
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_match_insert AFTER INSERT ON asset_tags
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND ((NEW.source = 'user' OR (NEW.source = 'ai' AND NEW.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))) AND EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))) BEGIN
  UPDATE tag_counts SET n = n + 1 WHERE tag_id = NEW.tag_id;
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_match_delete AFTER DELETE ON asset_tags
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND ((OLD.source = 'user' OR (OLD.source = 'ai' AND OLD.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))) AND EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))) BEGIN
  UPDATE tag_counts SET n = n - 1 WHERE tag_id = OLD.tag_id;
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_match_removed AFTER UPDATE OF source, score, tag_id, asset_id ON asset_tags
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND (COALESCE((OLD.source = 'user' OR (OLD.source = 'ai' AND OLD.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))), 0) AND (OLD.tag_id IS NOT NEW.tag_id OR OLD.asset_id IS NOT NEW.asset_id OR NOT COALESCE((NEW.source = 'user' OR (NEW.source = 'ai' AND NEW.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))), 0))) BEGIN
  UPDATE tag_counts SET n = n - 1 WHERE tag_id = OLD.tag_id AND EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id));
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_match_applied AFTER UPDATE OF source, score, tag_id, asset_id ON asset_tags
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND (COALESCE((NEW.source = 'user' OR (NEW.source = 'ai' AND NEW.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))), 0) AND (OLD.tag_id IS NOT NEW.tag_id OR OLD.asset_id IS NOT NEW.asset_id OR NOT COALESCE((OLD.source = 'user' OR (OLD.source = 'ai' AND OLD.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))), 0))) BEGIN
  UPDATE tag_counts SET n = n + 1 WHERE tag_id = NEW.tag_id AND EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id));
END;

-- Subtract before cascades remove the asset. Child triggers then see no
-- parent and cannot subtract its contribution a second time.

CREATE TRIGGER IF NOT EXISTS tag_counts_asset_deleted BEFORE DELETE ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND (OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = OLD.id)) BEGIN
  UPDATE tag_counts SET n = n - 1
  WHERE tag_id IN (SELECT x.tag_id FROM asset_tags x WHERE x.asset_id = OLD.id AND (x.source = 'user' OR (x.source = 'ai' AND x.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))));
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_asset_visibility AFTER UPDATE OF hidden, trashed_at, missing_since ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND ((OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL) IS NOT (NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL) AND NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = NEW.id)) BEGIN
  UPDATE tag_counts SET n = n + CASE WHEN NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL THEN 1 ELSE -1 END
  WHERE tag_id IN (SELECT x.tag_id FROM asset_tags x WHERE x.asset_id = NEW.id AND (x.source = 'user' OR (x.source = 'ai' AND x.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))));
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_block_insert AFTER INSERT ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND (EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL)) BEGIN
  UPDATE tag_counts SET n = n - 1
  WHERE tag_id IN (SELECT x.tag_id FROM asset_tags x WHERE x.asset_id = NEW.asset_id AND (x.source = 'user' OR (x.source = 'ai' AND x.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))));
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_block_delete AFTER DELETE ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND (EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL)) BEGIN
  UPDATE tag_counts SET n = n + 1
  WHERE tag_id IN (SELECT x.tag_id FROM asset_tags x WHERE x.asset_id = OLD.asset_id AND (x.source = 'user' OR (x.source = 'ai' AND x.score >= COALESCE((SELECT CAST(value AS REAL) FROM settings WHERE key = 'tag_confidence'), 0.02))));
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_confidence_added AFTER INSERT ON settings
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND (NEW.key = 'tag_confidence' AND COALESCE(CAST(NEW.value AS REAL), 0.02) IS NOT 0.02) BEGIN
  INSERT OR IGNORE INTO count_rebuild(name) VALUES ('tag_counts');
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_confidence_deleted AFTER DELETE ON settings
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND (OLD.key = 'tag_confidence' AND COALESCE(CAST(OLD.value AS REAL), 0.02) IS NOT 0.02) BEGIN
  INSERT OR IGNORE INTO count_rebuild(name) VALUES ('tag_counts');
END;

CREATE TRIGGER IF NOT EXISTS tag_counts_confidence_changed AFTER UPDATE OF key, value ON settings
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'tag_counts') AND ((OLD.key = 'tag_confidence' OR NEW.key = 'tag_confidence') AND
  (CASE WHEN OLD.key = 'tag_confidence' THEN COALESCE(CAST(OLD.value AS REAL), 0.02) ELSE 0.02 END) IS NOT
  (CASE WHEN NEW.key = 'tag_confidence' THEN COALESCE(CAST(NEW.value AS REAL), 0.02) ELSE 0.02 END)) BEGIN
  INSERT OR IGNORE INTO count_rebuild(name) VALUES ('tag_counts');
END;

-- Eligible faces retain their membership until removed, so cascades never
-- need to reconstruct visibility from a parent row that has already gone.
CREATE TABLE IF NOT EXISTS person_counts (
  person_id INTEGER PRIMARY KEY REFERENCES people(id) ON DELETE CASCADE,
  count INTEGER NOT NULL CHECK (count >= 0)
);
CREATE TABLE IF NOT EXISTS person_counts_faces (
  face_id INTEGER PRIMARY KEY REFERENCES faces(id) ON DELETE CASCADE ON UPDATE CASCADE,
  person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  asset_id INTEGER NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  cover_weight REAL,
  cover_ready INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS person_counts_faces_pair ON person_counts_faces(person_id, asset_id, face_id);
CREATE INDEX IF NOT EXISTS person_counts_faces_asset ON person_counts_faces(asset_id);
CREATE INDEX IF NOT EXISTS person_counts_faces_cover ON person_counts_faces(person_id, cover_weight DESC, face_id)
  WHERE cover_ready = 1;
CREATE TRIGGER IF NOT EXISTS person_counts_pair_added AFTER INSERT ON person_counts_faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND (NOT EXISTS (SELECT 1 FROM person_counts_faces WHERE person_id = NEW.person_id AND asset_id = NEW.asset_id AND face_id <> NEW.face_id)) BEGIN
  UPDATE person_counts SET count = count + 1 WHERE person_id = NEW.person_id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_pair_deleted AFTER DELETE ON person_counts_faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND (NOT EXISTS (SELECT 1 FROM person_counts_faces WHERE person_id = OLD.person_id AND asset_id = OLD.asset_id)) BEGIN
  UPDATE person_counts SET count = count - 1 WHERE person_id = OLD.person_id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_person_added AFTER INSERT ON people
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') BEGIN
  INSERT INTO person_counts VALUES (NEW.id, 0);
END;

CREATE TRIGGER IF NOT EXISTS person_counts_face_added AFTER INSERT ON faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND (NEW.person_id IS NOT NULL) BEGIN
  INSERT INTO person_counts_faces(face_id, person_id, asset_id, cover_weight, cover_ready)
  SELECT f.id, f.person_id, f.asset_id,
    f.w * a.width * f.h * a.height * f.score, a.thumb_version > 0
  FROM faces f JOIN assets a ON a.id = f.asset_id
  WHERE f.person_id IS NOT NULL AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id)
    AND NOT EXISTS (SELECT 1 FROM face_block_faces b WHERE b.face_id = f.id) AND f.id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_face_moved AFTER UPDATE OF id, person_id, asset_id ON faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND (NEW.id IS NOT OLD.id OR NEW.person_id IS NOT OLD.person_id OR NEW.asset_id IS NOT OLD.asset_id) BEGIN
  DELETE FROM person_counts_faces WHERE face_id = OLD.id;
  INSERT INTO person_counts_faces(face_id, person_id, asset_id, cover_weight, cover_ready)
  SELECT f.id, f.person_id, f.asset_id,
    f.w * a.width * f.h * a.height * f.score, a.thumb_version > 0
  FROM faces f JOIN assets a ON a.id = f.asset_id
  WHERE f.person_id IS NOT NULL AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id)
    AND NOT EXISTS (SELECT 1 FROM face_block_faces b WHERE b.face_id = f.id) AND f.id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_face_cover AFTER UPDATE OF w, h, score ON faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND (NEW.w IS NOT OLD.w OR NEW.h IS NOT OLD.h OR NEW.score IS NOT OLD.score) BEGIN
  UPDATE person_counts_faces SET cover_weight = (SELECT
    NEW.w * a.width * NEW.h * a.height * NEW.score
    FROM assets a WHERE a.id = NEW.asset_id) WHERE face_id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_asset_deleted BEFORE DELETE ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') BEGIN
  DELETE FROM person_counts_faces WHERE asset_id = OLD.id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_asset_visibility AFTER UPDATE OF hidden, trashed_at, missing_since ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND ((NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL)
  <> (OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL)) BEGIN
  DELETE FROM person_counts_faces WHERE asset_id = NEW.id;
  INSERT INTO person_counts_faces(face_id, person_id, asset_id, cover_weight, cover_ready)
  SELECT f.id, f.person_id, f.asset_id,
    f.w * a.width * f.h * a.height * f.score, a.thumb_version > 0
  FROM faces f JOIN assets a ON a.id = f.asset_id
  WHERE f.person_id IS NOT NULL AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id)
    AND NOT EXISTS (SELECT 1 FROM face_block_faces b WHERE b.face_id = f.id) AND a.id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_asset_cover AFTER UPDATE OF thumb_version, width, height ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND ((NEW.thumb_version > 0) <> (OLD.thumb_version > 0) OR NEW.width IS NOT OLD.width OR NEW.height IS NOT OLD.height) BEGIN
  UPDATE person_counts_faces SET cover_weight = (SELECT
    f.w * NEW.width * f.h * NEW.height * f.score
    FROM faces f WHERE f.id = face_id), cover_ready = NEW.thumb_version > 0 WHERE asset_id = NEW.id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_blocked_added AFTER INSERT ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') BEGIN
  DELETE FROM person_counts_faces WHERE asset_id = NEW.asset_id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_blocked_deleted AFTER DELETE ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') BEGIN
  INSERT INTO person_counts_faces(face_id, person_id, asset_id, cover_weight, cover_ready)
  SELECT f.id, f.person_id, f.asset_id,
    f.w * a.width * f.h * a.height * f.score, a.thumb_version > 0
  FROM faces f JOIN assets a ON a.id = f.asset_id
  WHERE f.person_id IS NOT NULL AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id)
    AND NOT EXISTS (SELECT 1 FROM face_block_faces b WHERE b.face_id = f.id) AND a.id = OLD.asset_id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_face_block_added AFTER INSERT ON face_block_faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') BEGIN
  DELETE FROM person_counts_faces WHERE face_id = NEW.face_id;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_face_block_deleted AFTER DELETE ON face_block_faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') BEGIN
  INSERT INTO person_counts_faces(face_id, person_id, asset_id, cover_weight, cover_ready)
  SELECT f.id, f.person_id, f.asset_id,
    f.w * a.width * f.h * a.height * f.score, a.thumb_version > 0
  FROM faces f JOIN assets a ON a.id = f.asset_id
  WHERE f.person_id IS NOT NULL AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id)
    AND NOT EXISTS (SELECT 1 FROM face_block_faces b WHERE b.face_id = f.id) AND f.id = OLD.face_id
  ON CONFLICT(face_id) DO NOTHING;
END;

CREATE TRIGGER IF NOT EXISTS person_counts_face_block_changed AFTER UPDATE OF face_id ON face_block_faces
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'person_counts') AND (NEW.face_id IS NOT OLD.face_id) BEGIN
  DELETE FROM person_counts_faces WHERE face_id = NEW.face_id;
  INSERT INTO person_counts_faces(face_id, person_id, asset_id, cover_weight, cover_ready)
  SELECT f.id, f.person_id, f.asset_id,
    f.w * a.width * f.h * a.height * f.score, a.thumb_version > 0
  FROM faces f JOIN assets a ON a.id = f.asset_id
  WHERE f.person_id IS NOT NULL AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id)
    AND NOT EXISTS (SELECT 1 FROM face_block_faces b WHERE b.face_id = f.id) AND f.id = OLD.face_id
  ON CONFLICT(face_id) DO NOTHING;
END;

-- Pending and waiting share one contribution; failed and skipped assets each
-- contribute once regardless of how many stages have that outcome.
CREATE TABLE IF NOT EXISTS job_counts (
  stage TEXT NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'error', 'skipped')),
  n INTEGER NOT NULL CHECK (n >= 0),
  PRIMARY KEY (stage, kind, status)
) WITHOUT ROWID;

-- Without statistics SQLite can prefer the global status index for an outcome
-- check. Explicitly selecting the WITHOUT ROWID primary key keeps the lookup
-- in the asset's small range, even when the library has many failed jobs.
CREATE TRIGGER IF NOT EXISTS job_counts_job_added AFTER INSERT ON asset_jobs
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (NEW.status IN ('pending', 'waiting')
  AND EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id
    AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))) BEGIN
  INSERT INTO job_counts(stage, kind, status, n) VALUES (NEW.stage, NEW.kind, 'pending', 1)
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + 1;
END;

-- An outcome counts its asset once, so only the asset's first job with it adds.
CREATE TRIGGER IF NOT EXISTS job_counts_outcome_added AFTER INSERT ON asset_jobs
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (NEW.status IN ('error', 'skipped')
  AND EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id
    AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))
  AND NOT EXISTS (SELECT 1 FROM asset_jobs j INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE j.asset_id = NEW.asset_id
    AND j.status = NEW.status AND j.stage != NEW.stage)) BEGIN
  INSERT INTO job_counts(stage, kind, status, n) VALUES ('', '', NEW.status, 1)
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + 1;
END;

CREATE TRIGGER IF NOT EXISTS job_counts_job_deleted AFTER DELETE ON asset_jobs
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (OLD.status IN ('pending', 'waiting', 'error', 'skipped')
  AND EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id
    AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))) BEGIN
  UPDATE job_counts SET n = n - 1
  WHERE stage = OLD.stage AND kind = OLD.kind AND status = 'pending'
    AND OLD.status IN ('pending', 'waiting');
  UPDATE job_counts SET n = n - 1
  WHERE stage = '' AND kind = '' AND status = OLD.status AND OLD.status IN ('error', 'skipped')
    AND NOT EXISTS (SELECT 1 FROM asset_jobs j INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE j.asset_id = OLD.asset_id AND j.status = OLD.status);
END;

-- Status-only writes share one visibility probe; a changed owner or bucket
-- uses the general trigger because its old and new visibility can differ.
CREATE TRIGGER IF NOT EXISTS job_counts_job_status_changed AFTER UPDATE OF status ON asset_jobs
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts')
  AND NEW.asset_id IS OLD.asset_id AND NEW.stage IS OLD.stage AND NEW.kind IS OLD.kind
  AND (CASE WHEN NEW.status IN ('pending', 'waiting') THEN 'pending' ELSE NEW.status END) IS NOT
    (CASE WHEN OLD.status IN ('pending', 'waiting') THEN 'pending' ELSE OLD.status END)
  AND (OLD.status IN ('pending', 'waiting', 'error', 'skipped') OR NEW.status IN ('pending', 'waiting', 'error', 'skipped'))
  AND EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id
    AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
    AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id)) BEGIN
  UPDATE job_counts SET n = n - 1
    WHERE stage = OLD.stage AND kind = OLD.kind AND status = 'pending' AND OLD.status IN ('pending', 'waiting');
  UPDATE job_counts SET n = n - 1
    WHERE stage = '' AND kind = '' AND status = OLD.status AND OLD.status IN ('error', 'skipped')
    AND NOT EXISTS (SELECT 1 FROM asset_jobs j INDEXED BY sqlite_autoindex_asset_jobs_1
      WHERE j.asset_id = OLD.asset_id AND j.status = OLD.status);
  INSERT INTO job_counts(stage, kind, status, n)
    SELECT NEW.stage, NEW.kind, 'pending', 1 WHERE NEW.status IN ('pending', 'waiting')
    ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
  INSERT INTO job_counts(stage, kind, status, n)
    SELECT '', '', NEW.status, 1 WHERE NEW.status IN ('error', 'skipped')
    AND NOT EXISTS (SELECT 1 FROM asset_jobs j INDEXED BY sqlite_autoindex_asset_jobs_1
      WHERE j.asset_id = NEW.asset_id AND j.status = NEW.status AND j.stage != NEW.stage)
    ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
END;

CREATE TRIGGER IF NOT EXISTS job_counts_job_changed AFTER UPDATE OF status, stage, kind, asset_id ON asset_jobs
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND ((NEW.asset_id IS NOT OLD.asset_id OR NEW.stage IS NOT OLD.stage OR NEW.kind IS NOT OLD.kind)
  AND (OLD.status IN ('pending', 'waiting', 'error', 'skipped') OR NEW.status IN ('pending', 'waiting', 'error', 'skipped'))) BEGIN
  UPDATE job_counts SET n = n - 1
  WHERE stage = OLD.stage AND kind = OLD.kind AND status = 'pending'
    AND OLD.status IN ('pending', 'waiting')
    AND EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id
      AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id));
  UPDATE job_counts SET n = n - 1
  WHERE stage = '' AND kind = '' AND status = OLD.status AND OLD.status IN ('error', 'skipped')
    AND EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id
      AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))
    AND NOT EXISTS (SELECT 1 FROM asset_jobs j INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE j.asset_id = OLD.asset_id AND j.status = OLD.status);
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT NEW.stage, NEW.kind, 'pending', 1 WHERE NEW.status IN ('pending', 'waiting')
    AND EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id
      AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT '', '', NEW.status, 1 WHERE NEW.status IN ('error', 'skipped')
    AND (OLD.asset_id IS NOT NEW.asset_id OR OLD.status IS NOT NEW.status)
    AND EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id
      AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL
      AND NOT EXISTS (SELECT 1 FROM blocked_assets b WHERE b.asset_id = a.id))
    AND NOT EXISTS (SELECT 1 FROM asset_jobs j INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE j.asset_id = NEW.asset_id
      AND j.status = NEW.status AND j.stage != NEW.stage)
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
END;

CREATE TRIGGER IF NOT EXISTS job_counts_asset_added AFTER INSERT ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = NEW.id)
  AND EXISTS (SELECT 1 FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE asset_id = NEW.id)) BEGIN
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT stage, kind, 'pending', 1 FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1
  WHERE asset_id = NEW.id AND status IN ('pending', 'waiting')
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT '', '', status, 1 FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1
  WHERE asset_id = NEW.id AND status IN ('error', 'skipped') GROUP BY status
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
END;

-- Each asset has at most one job per stage, so each pending bucket moves by
-- one. Subtract before cascades; child deletions see no parent and do no work.

CREATE TRIGGER IF NOT EXISTS job_counts_asset_deleted BEFORE DELETE ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL AND NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = OLD.id)) BEGIN
  UPDATE job_counts SET n = n - 1
  WHERE status = 'pending' AND (stage, kind) IN
    (SELECT stage, kind FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE asset_id = OLD.id AND status IN ('pending', 'waiting'));
  UPDATE job_counts SET n = n - 1 WHERE stage = '' AND kind = ''
    AND status IN (SELECT status FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE asset_id = OLD.id AND status IN ('error', 'skipped'));
END;

CREATE TRIGGER IF NOT EXISTS job_counts_asset_hidden AFTER UPDATE OF hidden, trashed_at, missing_since ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND ((OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL) AND NOT (NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL)
  AND NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = NEW.id)) BEGIN
  UPDATE job_counts SET n = n - 1
  WHERE status = 'pending' AND (stage, kind) IN
    (SELECT stage, kind FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE asset_id = NEW.id AND status IN ('pending', 'waiting'));
  UPDATE job_counts SET n = n - 1 WHERE stage = '' AND kind = ''
    AND status IN (SELECT status FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE asset_id = NEW.id AND status IN ('error', 'skipped'));
END;

CREATE TRIGGER IF NOT EXISTS job_counts_asset_restored AFTER UPDATE OF hidden, trashed_at, missing_since ON assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (NOT (OLD.hidden = 0 AND OLD.trashed_at IS NULL AND OLD.missing_since IS NULL) AND (NEW.hidden = 0 AND NEW.trashed_at IS NULL AND NEW.missing_since IS NULL)
  AND NOT EXISTS (SELECT 1 FROM blocked_assets WHERE asset_id = NEW.id)) BEGIN
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT stage, kind, 'pending', 1 FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1
  WHERE asset_id = NEW.id AND status IN ('pending', 'waiting')
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT '', '', status, 1 FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1
  WHERE asset_id = NEW.id AND status IN ('error', 'skipped') GROUP BY status
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
END;

CREATE TRIGGER IF NOT EXISTS job_counts_blocked AFTER INSERT ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (EXISTS (SELECT 1 FROM assets a WHERE a.id = NEW.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL)) BEGIN
  UPDATE job_counts SET n = n - 1
  WHERE status = 'pending' AND (stage, kind) IN
    (SELECT stage, kind FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE asset_id = NEW.asset_id AND status IN ('pending', 'waiting'));
  UPDATE job_counts SET n = n - 1 WHERE stage = '' AND kind = ''
    AND status IN (SELECT status FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1 WHERE asset_id = NEW.asset_id AND status IN ('error', 'skipped'));
END;

CREATE TRIGGER IF NOT EXISTS job_counts_unblocked AFTER DELETE ON blocked_assets
WHEN NOT EXISTS (SELECT 1 FROM count_rebuild WHERE name = 'job_counts') AND (EXISTS (SELECT 1 FROM assets a WHERE a.id = OLD.asset_id AND a.hidden = 0 AND a.trashed_at IS NULL AND a.missing_since IS NULL)) BEGIN
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT stage, kind, 'pending', 1 FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1
  WHERE asset_id = OLD.asset_id AND status IN ('pending', 'waiting')
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
  INSERT INTO job_counts(stage, kind, status, n)
  SELECT '', '', status, 1 FROM asset_jobs INDEXED BY sqlite_autoindex_asset_jobs_1
  WHERE asset_id = OLD.asset_id AND status IN ('error', 'skipped') GROUP BY status
  ON CONFLICT(stage, kind, status) DO UPDATE SET n = n + excluded.n;
END;
