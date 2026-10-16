-- Effective fields remain the single input to visibility, day counts, events,
-- search and smart albums. Originals are retained only while corrected and
-- refreshed by metadata reads; clearing a correction restores the latest read.
ALTER TABLE assets ADD COLUMN original_taken_at INTEGER;
ALTER TABLE assets ADD COLUMN original_date_from TEXT;
ALTER TABLE assets ADD COLUMN location_corrected INTEGER NOT NULL DEFAULT 0 CHECK(location_corrected IN (0, 1));
ALTER TABLE assets ADD COLUMN original_lat REAL;
ALTER TABLE assets ADD COLUMN original_lon REAL;
ALTER TABLE assets ADD COLUMN original_place_id INTEGER;

-- Duplicate keeper ranking still knows whether the file supplied an EXIF
-- date while its effective date is corrected. Metadata reruns can change it.
CREATE TRIGGER duplicate_original_date_changed AFTER UPDATE OF original_date_from ON assets
WHEN NEW.original_date_from IS NOT OLD.original_date_from BEGIN
  UPDATE event_revision SET duplicate_value = duplicate_value + 1 WHERE id = 1;
END;

-- Unlike timeline days, Trash ignores blocks. Keep its total independently so
-- sidebar reads do not walk a large Trash, using exactly the Trash predicate.
CREATE TABLE trash_count (id INTEGER PRIMARY KEY CHECK(id = 1), n INTEGER NOT NULL);
INSERT INTO trash_count SELECT 1, count(*) FROM assets
WHERE trashed_at IS NOT NULL AND missing_since IS NULL AND hidden = 0;

CREATE TRIGGER trash_count_added AFTER INSERT ON assets
WHEN NEW.trashed_at IS NOT NULL AND NEW.missing_since IS NULL AND NEW.hidden = 0 BEGIN
  UPDATE trash_count SET n = n + 1 WHERE id = 1;
END;
CREATE TRIGGER trash_count_deleted BEFORE DELETE ON assets
WHEN OLD.trashed_at IS NOT NULL AND OLD.missing_since IS NULL AND OLD.hidden = 0 BEGIN
  UPDATE trash_count SET n = n - 1 WHERE id = 1;
END;
CREATE TRIGGER trash_count_changed AFTER UPDATE OF trashed_at, missing_since, hidden ON assets
WHEN (OLD.trashed_at IS NOT NULL AND OLD.missing_since IS NULL AND OLD.hidden = 0)
  IS NOT (NEW.trashed_at IS NOT NULL AND NEW.missing_since IS NULL AND NEW.hidden = 0) BEGIN
  UPDATE trash_count SET n = n + (NEW.trashed_at IS NOT NULL AND NEW.missing_since IS NULL AND NEW.hidden = 0)
    - (OLD.trashed_at IS NOT NULL AND OLD.missing_since IS NULL AND OLD.hidden = 0) WHERE id = 1;
END;
