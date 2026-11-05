-- Numeric IDs can be reused after deletion. An editor must not write its
-- old draft to a replacement album, even if its name and rule are identical.
ALTER TABLE albums ADD COLUMN identity TEXT NOT NULL DEFAULT '';
UPDATE albums SET identity = lower(hex(randomblob(16)));

CREATE TRIGGER albums_assign_identity AFTER INSERT ON albums
WHEN NEW.identity = '' BEGIN
  UPDATE albums SET identity = lower(hex(randomblob(16))) WHERE id = NEW.id;
END;
