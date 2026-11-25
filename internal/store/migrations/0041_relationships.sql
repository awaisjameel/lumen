-- Who the user is in their photos and what other people are to them
-- (core.Person.Me and Relation). Both are the user's own choices and live on
-- the person, so they follow merges and go with the person; a relationship is
-- relative to the user, whichever person is marked as them.
ALTER TABLE people ADD COLUMN is_me INTEGER NOT NULL DEFAULT 0 CHECK(is_me IN (0, 1));
ALTER TABLE people ADD COLUMN relation TEXT;       -- core.Relation; NULL = none
ALTER TABLE people ADD COLUMN relation_label TEXT; -- the user's own word for core.RelationCustom, else NULL

-- At most one person is the user.
CREATE UNIQUE INDEX people_me ON people (is_me) WHERE is_me = 1;

-- A search looks up the few people with either, without reading everyone.
CREATE INDEX people_related ON people (relation) WHERE is_me = 1 OR relation IS NOT NULL;
