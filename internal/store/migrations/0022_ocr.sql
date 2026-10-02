-- Schema v22: the text read from screenshots and documents (Phase 5, OCR).
-- Safe to run again: the upgrade tests replay migrations over a current schema.

-- One row per photo whose text was read and is not empty. model is the OCR
-- stage's version that read it.
CREATE TABLE IF NOT EXISTS ocr_text (
  asset_id INTEGER PRIMARY KEY REFERENCES assets (id) ON DELETE CASCADE,
  model    TEXT NOT NULL,
  text     TEXT NOT NULL
);

-- The search index over it: FTS4 (compiled into the SQLite driver; FTS5
-- would need a build tag in every build and test command) in the same
-- external-content form SQLite documents, so the text is stored once. Its
-- docid is the asset's id. unicode61 folds case and drops diacritics, so
-- "cafe" finds "Café".
CREATE VIRTUAL TABLE IF NOT EXISTS ocr_fts USING fts4 (content="ocr_text", text, tokenize=unicode61 "remove_diacritics=2");

-- The documented triggers that keep an external-content table in step: the
-- old entry goes before its row changes (the index reads the row to know
-- what to remove), the new one after. A cascade from a deleted asset fires
-- the delete one too.
CREATE TRIGGER IF NOT EXISTS ocr_text_before_update BEFORE UPDATE ON ocr_text BEGIN
  DELETE FROM ocr_fts WHERE docid = OLD.rowid;
END;
CREATE TRIGGER IF NOT EXISTS ocr_text_before_delete BEFORE DELETE ON ocr_text BEGIN
  DELETE FROM ocr_fts WHERE docid = OLD.rowid;
END;
CREATE TRIGGER IF NOT EXISTS ocr_text_after_update AFTER UPDATE ON ocr_text BEGIN
  INSERT INTO ocr_fts (docid, text) VALUES (NEW.rowid, NEW.text);
END;
CREATE TRIGGER IF NOT EXISTS ocr_text_after_insert AFTER INSERT ON ocr_text BEGIN
  INSERT INTO ocr_fts (docid, text) VALUES (NEW.rowid, NEW.text);
END;
