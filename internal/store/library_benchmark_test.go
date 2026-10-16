package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lumen/internal/core"
)

const largeLibrarySize = 100000
const largeLibraryBurstSize = largeLibrarySize / 3333

// BenchmarkLargeLibraryStore keeps the audit's scale out of ordinary tests.
// All configurations share the same schema, distribution and warm read pool.
func BenchmarkLargeLibraryStore(b *testing.B) {
	ctx := b.Context()
	seedStart := time.Now()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	mustBench(b, err)
	b.Cleanup(func() { mustBench(b, st.Close()) })
	mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
		// Short bursts and quiet weeks exercise thousands of events at the default gap.
		// Files without a capture date share one import day, as in a real scan.
		// Varied perceptual hashes exercise every bucketing pass and include near copies.
		for _, q := range []string{
			`INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0)`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<200)
			INSERT INTO places(id, name, region, country, lat, lon) SELECT x, 'Place '||x, '', '', 0, 0 FROM n`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, place_id, width, height, thumb_version, date_from, lat, lon, phash)
			SELECT x, printf('%016x', x), 'photo', '.jpg',
			1577836800 + CASE WHEN x%10=0 THEN 0 ELSE
			(1 + (x-1)/? + ((x-1)/?)/60*7)*86400 + 36000 + ((x-1)%?)*7200/? END,
			0, 1+x%200, 1920, 1080, 1,
			CASE WHEN x%10=0 THEN 'file' ELSE 'exif' END,
			CASE WHEN x%4=0 THEN 0 END, CASE WHEN x%4=0 THEN 0 END,
			(((CASE WHEN x%1000=1 THEN x-1 ELSE x END)*2654435761 & 4294967295)<<32) |
			((CASE WHEN x%1000=1 THEN x-1 ELSE x END)*2246822519 & 4294967295) FROM n`,
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
			`INSERT INTO embeddings(asset_id, model, scale, vec)
			SELECT id, 'bench', 0.003125, zeroblob(768) FROM assets`,
			`INSERT INTO settings(key, value) VALUES ('ocr_enabled', '1')`,
			`PRAGMA optimize = 0x10002`,
		} {
			var args []any
			if strings.Contains(q, "x<?") {
				args = []any{largeLibrarySize, largeLibraryBurstSize, largeLibraryBurstSize, largeLibraryBurstSize, largeLibraryBurstSize}
			}
			if _, err := tx.ExecContext(ctx, q, args...); err != nil {
				return err
			}
		}
		return nil
	}))
	assertLargeLibrarySeed(ctx, b, st)
	b.Logf("seed: %d photos in %s", largeLibrarySize, time.Since(seedStart).Round(time.Millisecond))
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
	// Pick an ordinary event whose anchor survives all three block configurations.
	var eventID, takenAt int64
	mustBench(b, st.r.QueryRowContext(ctx, "SELECT id, taken_at FROM assets WHERE id > 1000 AND id % 10 != 0 AND "+visible+" ORDER BY id LIMIT 1").Scan(&eventID, &takenAt))
	ordinaryDay := time.Unix(takenAt, 0).UTC().Format(time.DateOnly)
	nextDays := time.Unix(takenAt, 0).UTC().AddDate(0, 0, 2).Format(time.DateOnly)
	coverHash := uint64(0x73e458891af346bc)
	changeCover := func() error {
		event, err := st.Event(ctx, eventID, core.EventGapDefault)
		if err != nil {
			return err
		}
		coverHash ^= 1
		return st.SaveThumbnails(ctx, "bench", []ThumbResult{{AssetID: event.Cover, PHash: &coverHash}})
	}
	named, pinned := false, false
	// Another gap is a grouping no journal describes, so it is the whole
	// regroup that the first call and an overrun history also pay.
	gap := core.EventGapDefault
	otherGap := func() error {
		gap = 2*core.EventGapDefault + 1 - gap
		return nil
	}
	invalidate := func() error {
		// A visibility revision must exercise the read after the library changes,
		// rather than allow a warm revision cache to hide a slow recount.
		return st.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id = ?", eventID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "UPDATE assets SET hidden = 0 WHERE id = ?", eventID)
			return err
		})
	}
	pending := false
	preparePending := func() error {
		if pending {
			return nil
		}
		if err := st.tx(ctx, func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "UPDATE asset_jobs SET status = 'pending' WHERE stage IN ('embedding', 'faces')")
			return err
		}); err != nil {
			return err
		}
		counts, err := st.JobCounts(ctx)
		if err != nil {
			return err
		}
		if counts.Pending[core.StageEmbedding] < largeLibrarySize*9/10 || counts.Pending[core.StageFaces] < largeLibrarySize*9/10 {
			return fmt.Errorf("pending fixture must hold two library-sized backlogs")
		}
		pending = true
		return nil
	}
	batch := make([]IngestItem, 128)
	for i := range batch {
		name := fmt.Sprintf("bench-new-%d", i)
		batch[i] = item(1, `C:\Photos\Private\`+name+`.jpg`, name)
	}
	for _, call := range []struct {
		name    string
		run     func() error
		prepare func() error
		cleanup func() error
	}{
		{"Things", func() error { return errOf(st.Things(ctx, core.TagConfidenceDefault)) }, invalidate, nil},
		{"People", func() error { return errOf(st.People(ctx)) }, invalidate, nil},
		{"DayBuckets", func() error { return errOf(st.DayBuckets(ctx, core.View{}, core.TagConfidenceDefault)) }, invalidate, nil},
		{"CountVisible", func() error { return errOf(st.CountVisible(ctx)) }, invalidate, nil},
		{"Counts", func() error { return errOf(st.Counts(ctx)) }, invalidate, nil},
		{"DayBucketsPhotos", func() error {
			return errOf(st.DayBuckets(ctx, core.View{Filter: core.FilterPhotos}, core.TagConfidenceDefault))
		}, invalidate, nil},
		{"DayItemsPhotos", func() error {
			return errOf(st.DayItems(ctx, ordinaryDay, ordinaryDay, core.View{Filter: core.FilterPhotos}, core.TagConfidenceDefault))
		}, nil, nil},
		{"Events", func() error { return errOf(st.Events(ctx, core.EventGapDefault)) }, invalidate, nil},
		{"EventsRegroup", func() error { return errOf(st.Events(ctx, gap)) }, otherGap, nil},
		{"Event", func() error { return errOf(st.Event(ctx, eventID, core.EventGapDefault)) }, invalidate, nil},
		{"EventRepeated", func() error { return errOf(st.Event(ctx, eventID, core.EventGapDefault)) }, nil, nil},
		{"SetEventName", func() error {
			named = !named
			name := "Benchmark morning"
			if !named {
				name = "Benchmark evening"
			}
			return st.SetEventName(ctx, eventID, name, core.EventGapDefault)
		}, invalidate, nil},
		{"SetEventPinned", func() error {
			pinned = !pinned
			return st.SetEventPinned(ctx, eventID, pinned, core.EventGapDefault)
		}, invalidate, nil},
		{"EventsAfterCover", func() error { return errOf(st.Events(ctx, core.EventGapDefault)) }, changeCover, nil},
		{"DayItemsOrdinary", func() error {
			return errOf(st.DayItems(ctx, ordinaryDay, ordinaryDay, core.View{}, core.TagConfidenceDefault))
		}, nil, nil},
		{"DayItemsThreeDays", func() error {
			return errOf(st.DayItems(ctx, ordinaryDay, nextDays, core.View{}, core.TagConfidenceDefault))
		}, nil, nil},
		{"DayItemsBigDay", func() error {
			return errOf(st.DayItems(ctx, "2020-01-01", "2020-01-01", core.View{}, core.TagConfidenceDefault))
		}, nil, nil},
		{"TimelineEvent", func() error { return errOf(st.DayBuckets(ctx, core.View{Event: eventID}, core.TagConfidenceDefault)) }, invalidate, nil},
		{"DayItemsEvent", func() error {
			return errOf(st.DayItems(ctx, ordinaryDay, ordinaryDay, core.View{Event: eventID}, core.TagConfidenceDefault))
		}, invalidate, nil},
		{"DayBucketsThing", func() error { return errOf(st.DayBuckets(ctx, core.View{Tag: 2}, core.TagConfidenceDefault)) }, invalidate, nil},
		{"DuplicateGroups", func() error { return errOf(st.DuplicateGroups(ctx)) }, changeCover, nil},
		{"DuplicateGroupsRepeated", func() error { return errOf(st.DuplicateGroups(ctx)) }, nil, nil},
		{"LoadEmbeddings", func() error {
			return st.LoadEmbeddings(ctx, "bench", func(int) {}, func(_ int64, _ []int8, _ float32) {})
		}, nil, nil},
		{"ClusterData", func() error { _, _, _, err := st.ClusterData(ctx, "bench"); return err }, nil, nil},
		{"JobCountsDone", func() error { return errOf(st.JobCounts(ctx)) }, nil, nil},
		{"AssetVisible", func() error { return errOf(st.AssetVisible(ctx, eventID, false)) }, nil, nil},
		{"Selection", func() error { return errOf(st.Selection(ctx, core.View{}, nil, false)) }, invalidate, nil},
		{"UnavailableAssets", func() error { return errOf(st.UnavailableAssets(ctx)) }, invalidate, nil},
		{"Places", func() error { return errOf(st.Places(ctx)) }, invalidate, nil},
		{"SaveEmbeddings64", func() error { return st.SaveEmbeddings(ctx, "bench", "bench", embeddings) }, nil, nil},
		{"SaveTags64", func() error { return st.ReplaceAITags(ctx, tags) }, nil, nil},
		{"SaveFaces64", func() error { return st.SaveFaces(ctx, "bench", "bench", faces) }, nil, nil},
		{"SaveThumbnails64", func() error { return st.SaveThumbnails(ctx, "bench", thumbs) }, nil, nil},
		{"QueueOCR", func() error { return errOf(st.QueueOCR(ctx)) }, nil, nil},
		// A block or tag-confidence change re-evaluates every photo while it
		// holds the writer: this is that whole cost.
		{"RebuildBlocks", func() error {
			return st.tx(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO blocked_rebuild(id) VALUES (1)")
				return err
			})
		}, nil, nil},
		{"JobCountsPending", func() error { return errOf(st.JobCounts(ctx)) }, preparePending, nil},
		{"CountPending", func() error { return errOf(st.CountPending(ctx, core.StageEmbedding, core.StageFaces)) }, preparePending, nil},
		{"PendingJobsKind64", func() error { return errOf(st.PendingJobsKind(ctx, core.StageEmbedding, core.KindPhoto, nil, 64)) }, preparePending, nil},
		{"Ingest128", func() error { return errOf(st.Ingest(ctx, batch)) }, nil, func() error {
			return st.tx(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, "DELETE FROM assets WHERE id > ?", largeLibrarySize)
				return err
			})
		}},
	} {
		b.Run(call.name, func(b *testing.B) {
			// The first call fills caches and gives replacement saves existing rows.
			if call.prepare != nil {
				mustBench(b, call.prepare())
			}
			mustBench(b, call.run())
			if call.cleanup != nil {
				mustBench(b, call.cleanup())
			}
			for b.Loop() {
				if call.prepare != nil {
					b.StopTimer()
					mustBench(b, call.prepare())
					b.StartTimer()
				}
				mustBench(b, call.run())
				if call.cleanup != nil {
					b.StopTimer()
					mustBench(b, call.cleanup())
					b.StartTimer()
				}
			}
		})
	}
	mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE asset_jobs SET status = 'done'")
		return err
	}))
}

// assertLargeLibrarySeed prevents a cheaper accidental fixture from passing the gate.
func assertLargeLibrarySeed(ctx context.Context, b *testing.B, st *Store) {
	b.Helper()
	for _, check := range []struct {
		name, query string
		want        int
	}{
		{"assets", "SELECT count(*) FROM assets", largeLibrarySize},
		{"files", "SELECT count(*) FROM files", largeLibrarySize},
		{"jobs", "SELECT count(*) FROM asset_jobs WHERE status = 'done'", 4 * largeLibrarySize},
		{"tags", "SELECT count(*) FROM asset_tags", 8 * largeLibrarySize},
		{"tag coverage", "SELECT count(DISTINCT tag_id) FROM asset_tags", 180},
		{"embeddings", "SELECT count(*) FROM embeddings WHERE model = 'bench' AND length(vec) = 768", largeLibrarySize},
		{"faces", "SELECT count(*) FROM faces", 3 * largeLibrarySize / 5},
		{"people coverage", "SELECT count(DISTINCT person_id) FROM faces", 500},
		{"places", "SELECT count(DISTINCT place_id) FROM assets", 200},
		{"big day", "SELECT count(*) FROM assets WHERE taken_at = 1577836800 AND date_from = 'file'", largeLibrarySize / 10},
		{"located", "SELECT count(*) FROM assets WHERE lat IS NOT NULL AND lon IS NOT NULL", largeLibrarySize / 4},
		{"hashes", "SELECT count(*) FROM assets WHERE phash IS NOT NULL", largeLibrarySize},
	} {
		var got int
		mustBench(b, st.r.QueryRowContext(ctx, check.query).Scan(&got))
		if got != check.want {
			b.Fatalf("%s: got %d, want %d", check.name, got, check.want)
		}
	}
	events, err := st.Events(ctx, core.EventGapDefault)
	mustBench(b, err)
	if len(events) < 3000 {
		b.Fatalf("seed has %d events, want at least 3000", len(events))
	}
	b.Logf("seed: %d events; %d photos on the import day", len(events), largeLibrarySize/10)
}
