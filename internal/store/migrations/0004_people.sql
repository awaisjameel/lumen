-- Schema v4: people (Phase 3).

-- People: faces the AI grouped as one person, named or not. A new group
-- starts unnamed; naming it is how the user confirms it.
CREATE TABLE people (
  id         INTEGER PRIMARY KEY,
  name       TEXT    UNIQUE COLLATE NOCASE,   -- NULL until the user names them
  hidden     INTEGER NOT NULL DEFAULT 0,
  cover_face INTEGER REFERENCES faces (id) ON DELETE SET NULL, -- the user's pick; NULL = the clearest face
  created_at INTEGER NOT NULL
);

-- Faces found in photos. The box is in the upright photo, as fractions of
-- its width and height, so it fits every thumbnail size.
CREATE TABLE faces (
  id          INTEGER PRIMARY KEY,
  asset_id    INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  x           REAL    NOT NULL,
  y           REAL    NOT NULL,
  w           REAL    NOT NULL,
  h           REAL    NOT NULL,
  score       REAL    NOT NULL,               -- detection confidence, 0..1
  sharpness   REAL    NOT NULL,               -- detail in the aligned face (ai.Identity)
  yaw         REAL    NOT NULL,               -- head turn (ai.Face.Yaw); blurry or turned faces don't start new people
  model       TEXT    NOT NULL,               -- face models' version that made vec
  vec         BLOB    NOT NULL,               -- identity vector, dim x float32
  person_id   INTEGER REFERENCES people (id) ON DELETE SET NULL,
  assigned_by TEXT                            -- 'auto' | 'user' while person_id is set
);
CREATE INDEX faces_by_asset ON faces (asset_id);
CREATE INDEX faces_by_person ON faces (person_id);

-- "Not this person": the user took a face out of a person. Clustering never
-- puts it back there.
CREATE TABLE face_rejections (
  face_id   INTEGER NOT NULL REFERENCES faces (id) ON DELETE CASCADE,
  person_id INTEGER NOT NULL REFERENCES people (id) ON DELETE CASCADE,
  PRIMARY KEY (face_id, person_id)
) WITHOUT ROWID;

-- Every existing photo gets the new stage.
INSERT OR IGNORE INTO asset_jobs (asset_id, stage, status, updated_at)
  SELECT id, 'faces', 'pending', CAST(strftime('%s') AS INTEGER) FROM assets;
