package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

func identicalDuplicatePhotos(n int) []duplicateCandidate {
	photos := make([]duplicateCandidate, n)
	for i := range photos {
		photos[i] = duplicateCandidate{DuplicateItem: core.DuplicateItem{ID: int64(i + 1), Width: 1920, Height: 1080, Ext: ".jpg"}, hash: 7, hashed: true}
	}
	return photos
}

func BenchmarkDuplicateIdenticalHashes(b *testing.B) {
	photos := identicalDuplicatePhotos(6000)
	b.ReportAllocs()
	for b.Loop() {
		groups, err := groupDuplicates(b.Context(), photos)
		mustBench(b, err)
		if len(groups) != 1 || len(groups[0].Items) != len(photos) {
			b.Fatalf("got %d groups, want one with %d photos", len(groups), len(photos))
		}
	}
}

func BenchmarkDuplicateOrdinaryHashes(b *testing.B) {
	photos := randomPhotos(100000)
	b.ReportAllocs()
	for b.Loop() {
		mustBench(b, errOf(groupDuplicates(b.Context(), photos)))
	}
}

func seedDuplicateBenchmark(ctx context.Context, b *testing.B, st *Store) {
	b.Helper()
	mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0)`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<100000)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, width, height, thumb_version, phash)
			SELECT x, printf('%016x', x), 'photo', '.jpg', x, 0, 1920, 1080, 1,
			(((CASE WHEN x%1000=1 THEN x-1 ELSE x END)*2654435761 & 4294967295)<<32) |
			((CASE WHEN x%1000=1 THEN x-1 ELSE x END)*2246822519 & 4294967295) FROM n`,
			`INSERT INTO files(asset_id, source_id, path, size, mtime)
			SELECT id, 1, 'C:\Photos\' || id || '.jpg', 1000, 0 FROM assets`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
}

func BenchmarkDuplicateUnchangedLibrary(b *testing.B) {
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	mustBench(b, err)
	b.Cleanup(func() { mustBench(b, st.Close()) })
	seedDuplicateBenchmark(b.Context(), b, st)
	mustBench(b, errOf(st.DuplicateGroups(b.Context())))
	b.ReportAllocs()
	for b.Loop() {
		mustBench(b, errOf(st.DuplicateGroups(b.Context())))
	}
}
