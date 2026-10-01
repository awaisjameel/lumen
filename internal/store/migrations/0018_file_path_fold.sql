-- A file is one file in any case, as Windows treats "ÉTÉ.jpg" and "été.jpg"
-- as one name, but files.path's UNIQUE COLLATE NOCASE folds only A-Z. A file
-- renamed in non-ASCII letters while its content changed got a second row,
-- and the next scan dropped the newer one, so the file switched between two
-- assets on every scan. Rows are now unique by their fold key, the
-- expression store.foldPath builds (store.foldKey("path")).

-- Of the rows naming one file, keep the newest: it has the current spelling
-- and the content last seen. The other rows' assets are kept, marked missing
-- if no file is left for them (as store.markOrphans does), so their names,
-- tags and albums survive should the content come back. Safe to run again.
DROP TABLE IF EXISTS temp.file_fold_displaced;
CREATE TEMP TABLE file_fold_displaced AS
SELECT id, asset_id FROM files
WHERE id NOT IN (
  SELECT max(id) FROM files
  GROUP BY (CASE WHEN octet_length(path) != length(path) THEN fold(path) ELSE lower(path) END)
);

DELETE FROM files WHERE id IN (SELECT id FROM file_fold_displaced);

UPDATE assets SET missing_since = CAST(strftime('%s', 'now') AS INTEGER)
WHERE id IN (SELECT asset_id FROM file_fold_displaced) AND missing_since IS NULL
  AND NOT EXISTS (SELECT 1 FROM files WHERE asset_id = assets.id);

DROP TABLE temp.file_fold_displaced;

CREATE UNIQUE INDEX IF NOT EXISTS files_path_fold
  ON files ((CASE WHEN octet_length(path) != length(path) THEN fold(path) ELSE lower(path) END));
