package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

// BenchmarkLargeLibraryBlocking keeps the audit's scale out of ordinary tests.
// All configurations share the same schema, distribution and warm read pool.
func BenchmarkLargeLibraryBlocking(b *testing.B) {
	ctx := context.Background()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	mustBench(b, err)
	b.Cleanup(func() { mustBench(b, st.Close()) })
	mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0)`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<200)
			INSERT INTO places(id, name, region, country, lat, lon) SELECT x, 'Place '||x, '', '', 0, 0 FROM n`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<200000)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, place_id, width, height, thumb_version)
			SELECT x, printf('%016x', x), 'photo', '.jpg',
			1577836800 + CASE WHEN x%10=0 THEN 0 ELSE (x%3650)*86400 + x%86400 END,
			0, 1+x%200, 1920, 1080, 1 FROM n`,
			`INSERT INTO files(asset_id, source_id, path, size, mtime)
			SELECT id, 1, 'C:\Photos\' || CASE WHEN id%1000=0 THEN 'Private\' ELSE 'Public\' END || id || '.jpg', 1000, 0 FROM assets`,
			`INSERT INTO asset_jobs(asset_id, stage, status, updated_at)
			SELECT id, stage, 'done', 0 FROM assets CROSS JOIN
			(SELECT 'metadata' stage UNION ALL SELECT 'thumbnail' UNION ALL SELECT 'embedding' UNION ALL SELECT 'faces')`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<180)
			INSERT INTO tags(id, name, kind, created_at) SELECT x, 'Thing '||x, 'builtin', 0 FROM n`,
			`UPDATE tags SET vocab_name = CASE id WHEN 1 THEN 'Screenshots' WHEN 2 THEN 'Documents' END WHERE id IN (1, 2)`,
			`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<7)
			INSERT INTO asset_tags(asset_id, tag_id, source, score)
			SELECT id, 1+(id*7+x)%180, 'ai', 0.5 FROM assets CROSS JOIN n`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<500)
			INSERT INTO people(id, name, created_at) SELECT x, 'Person '||x, 0 FROM n`,
			`INSERT INTO faces(asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by)
			SELECT id, 0.1, 0.1, 0.2, 0.2, 0.9, 20, 0, 'bench', zeroblob(512), 1+(id/5)%500, 'auto'
			FROM assets WHERE id%5<3`,
			`INSERT INTO settings(key, value) VALUES ('ocr_enabled', '1')`,
			`PRAGMA optimize = 0x10002`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	// Face selection and person assignment must not share a modulus: that
	// would leave some of the seeded people without any faces.
	var people int
	mustBench(b, st.r.QueryRowContext(ctx, "SELECT count(DISTINCT person_id) FROM faces").Scan(&people))
	if people != 500 {
		b.Fatalf("faces span %d people, want 500", people)
	}
	for _, configuration := range []string{"none", "folder", "folder_tag_person"} {
		if configuration == "folder" {
			_, err := st.SetBlock(ctx, core.BlockFolder, 0, `C:\Photos\Private`, core.BlockPhotos)
			mustBench(b, err)
		}
		if configuration == "folder_tag_person" {
			_, err := st.SetBlock(ctx, core.BlockTag, 1, "", core.BlockPhotos)
			mustBench(b, err)
			_, err = st.SetBlock(ctx, core.BlockPerson, 1, "", core.BlockPhotos)
			mustBench(b, err)
		}
		b.Run(configuration, func(b *testing.B) {
			largeLibraryCalls(ctx, b, st)
		})
	}
}

func largeLibraryCalls(ctx context.Context, b *testing.B, st *Store) {
	b.Helper()
	embeddings := make([]EmbedResult, 64)
	tags := make([]AssetTagSet, 64)
	faces := make([]FaceResult, 64)
	thumbs := make([]ThumbResult, 64)
	for i := range embeddings {
		id := int64(i + 1)
		embeddings[i] = EmbedResult{AssetID: id, Vec: make([]int8, 768), Scale: 1}
		for j := range 12 {
			embeddings[i].Tags = append(embeddings[i].Tags, TagScore{TagID: int64(j + 1), Score: 0.5})
		}
		tags[i] = AssetTagSet{AssetID: id, Tags: embeddings[i].Tags}
		faces[i] = FaceResult{AssetID: id, Faces: []DetectedFace{{X: 0.1, Y: 0.1, W: 0.2, H: 0.2, Score: 0.9, Sharpness: 20, Vec: make([]float32, 128)}}}
		thumbs[i] = ThumbResult{AssetID: id}
	}
	for _, call := range []struct {
		name string
		run  func() error
	}{
		{"Things", func() error { return errOf(st.Things(ctx, core.TagConfidenceDefault)) }},
		{"People", func() error { return errOf(st.People(ctx)) }},
		{"DayBuckets", func() error { return errOf(st.DayBuckets(ctx, core.View{}, core.TagConfidenceDefault)) }},
		{"CountVisible", func() error { return errOf(st.CountVisible(ctx)) }},
		{"Events", func() error {
			st.eventsCache.Lock()
			st.eventsCache.loaded = false
			st.eventsCache.Unlock()
			return errOf(st.Events(ctx, core.EventGapDefault))
		}},
		{"Selection", func() error { return errOf(st.Selection(ctx, core.View{}, nil, false)) }},
		{"UnavailableAssets", func() error { return errOf(st.UnavailableAssets(ctx)) }},
		{"Places", func() error { return errOf(st.Places(ctx)) }},
		{"SaveEmbeddings64", func() error { return st.SaveEmbeddings(ctx, "bench", "bench", embeddings) }},
		{"SaveTags64", func() error { return st.ReplaceAITags(ctx, tags) }},
		{"SaveFaces64", func() error { return st.SaveFaces(ctx, "bench", "bench", faces) }},
		{"SaveThumbnails64", func() error { return st.SaveThumbnails(ctx, "bench", thumbs) }},
		{"QueueOCR", func() error { return errOf(st.QueueOCR(ctx)) }},
		// A block or tag-confidence change re-evaluates every photo while it
		// holds the writer: this is that whole cost.
		{"RebuildBlocks", func() error {
			return st.tx(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1)")
				return err
			})
		}},
	} {
		b.Run(call.name, func(b *testing.B) {
			// The first call fills caches and gives replacement saves existing rows.
			mustBench(b, call.run())
			for b.Loop() {
				mustBench(b, call.run())
			}
		})
	}
	b.Run("Ingest128", func(b *testing.B) {
		batch := make([]IngestItem, 128)
		for i := range batch {
			name := fmt.Sprintf("bench-new-%d", i)
			batch[i] = item(1, `C:\Photos\Private\`+name+`.jpg`, name)
		}
		for b.Loop() {
			mustBench(b, errOf(st.Ingest(ctx, batch)))
			b.StopTimer()
			mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "DELETE FROM assets WHERE id > 200000")
				return err
			}))
			b.StartTimer()
		}
	})
}
