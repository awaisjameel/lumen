package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"lumen/internal/core"
)

// BenchmarkKeptCountsBulkChanges measures actual coverage deltas, which a
// rebuild retaining every blocked row cannot show. Preparation is untimed.
func BenchmarkKeptCountsBulkChanges(b *testing.B) {
	ctx := b.Context()
	st := seedKeptCountsBenchmark(b)
	var err error
	for _, coverage := range []struct {
		name, path string
		count      int
	}{{"50000", `C:\Photos\Almost\Half`, 50000}, {"99900", `C:\Photos\Almost`, 99900}} {
		b.Run(coverage.name, func(b *testing.B) {
			for _, operation := range []string{"Block", "Unblock"} {
				b.Run(operation, func(b *testing.B) {
					for b.Loop() {
						b.StopTimer()
						var id int64
						if operation == "Unblock" {
							id, err = st.SetBlock(ctx, core.BlockFolder, 0, coverage.path, core.BlockPhotos)
							mustBench(b, err)
						}
						b.StartTimer()
						if operation == "Block" {
							id, err = st.SetBlock(ctx, core.BlockFolder, 0, coverage.path, core.BlockPhotos)
							mustBench(b, err)
						} else {
							mustBench(b, st.RemoveBlock(ctx, id))
						}
						b.StopTimer()
						var covered int
						mustBench(b, st.r.QueryRowContext(ctx, "SELECT count(*) FROM blocked_assets").Scan(&covered))
						want := 0
						if operation == "Block" {
							want = coverage.count
						}
						if covered != want {
							b.Fatalf("coverage %d, want %d", covered, want)
						}
						if operation == "Block" {
							mustBench(b, st.RemoveBlock(ctx, id))
						}
						b.StartTimer()
					}
				})
			}
		})
	}
	b.Run("RemoveSource50000", func(b *testing.B) {
		for b.Loop() {
			mustBench(b, st.RemoveSource(ctx, 1))
			b.StopTimer()
			var missing int
			mustBench(b, st.r.QueryRowContext(ctx, "SELECT count(*) FROM assets WHERE missing_since IS NOT NULL").Scan(&missing))
			if missing != 50000 {
				b.Fatalf("missing %d assets, want 50000", missing)
			}
			mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
				if err := markKeptCountsStale(ctx, tx); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, `INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0);
				INSERT INTO files(asset_id, source_id, path, size, mtime)
				SELECT id, 1, 'C:\Photos\Almost\Half\'||id||'.jpg', 1000, 0 FROM assets WHERE id<=50000;
				UPDATE assets SET missing_since = NULL WHERE id<=50000`)
				return err
			}))
			b.StartTimer()
		}
	})
}

func seedKeptCountsBenchmark(b *testing.B) *Store {
	b.Helper()
	ctx := b.Context()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	mustBench(b, err)
	b.Cleanup(func() { mustBench(b, st.Close()) })
	mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
		if err := markKeptCountsStale(ctx, tx); err != nil {
			return err
		}
		for _, q := range []string{
			`INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0), (2, 'C:\Other', 0)`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<100000)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, width, height, thumb_version, favourite)
			SELECT x, printf('%016x', x), 'photo', '.jpg', 1577836800+(x/30)*86400, 0, 1920, 1080, 1, x%2 FROM n`,
			`INSERT INTO files(asset_id, source_id, path, size, mtime)
			SELECT id, CASE WHEN id<=50000 THEN 1 ELSE 2 END,
			'C:\Photos\'||CASE WHEN id<=99900 THEN 'Almost\' ELSE 'Open\' END||
			CASE WHEN id<=50000 THEN 'Half\' ELSE 'Rest\' END||id||'.jpg', 1000, 0 FROM assets`,
			`INSERT INTO asset_jobs(asset_id, stage, status, kind, updated_at)
			SELECT id, stage, CASE WHEN stage='embedding' AND id%7=0 THEN 'error'
			WHEN stage='faces' AND id%11=0 THEN 'skipped' ELSE 'pending' END, 'photo', 0
			FROM assets CROSS JOIN (SELECT 'metadata' stage UNION ALL SELECT 'thumbnail'
			UNION ALL SELECT 'embedding' UNION ALL SELECT 'faces')`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<180)
			INSERT INTO tags(id, name, kind, created_at) SELECT x, 'Thing '||x, 'builtin', 0 FROM n`,
			`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<7)
			INSERT INTO asset_tags(asset_id, tag_id, source, score)
			SELECT id, 1+(id*7+x)%180, 'ai', 0.5 FROM assets CROSS JOIN n`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<500)
			INSERT INTO people(id, name, created_at) SELECT x, 'Person '||x, 0 FROM n`,
			`INSERT INTO faces(asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by)
			SELECT id, 0.1, 0.1, 0.2, 0.2, 0.9, 20, 0, 'bench', zeroblob(512), 1+(id/5)%500, 'auto'
			FROM assets WHERE id%5<3`,
			`PRAGMA optimize = 0x10002`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}))
	return st
}

// BenchmarkKeptCountsMaintenance separates the four-table recount from the
// marginal row cost. Identical visibility writes roll back outside the timer.
func BenchmarkKeptCountsMaintenance(b *testing.B) {
	st, ctx := seedKeptCountsBenchmark(b), b.Context()
	b.Run("Recount100000", func(b *testing.B) {
		for b.Loop() {
			mustBench(b, st.tx(ctx, func(tx *sql.Tx) error { return markKeptCountsStale(ctx, tx) }))
		}
	})
	for _, size := range []int{1000, 5000, 10000} {
		for _, guarded := range []bool{false, true} {
			b.Run(fmt.Sprintf("Rows%d/guarded=%t", size, guarded), func(b *testing.B) {
				for b.Loop() {
					b.StopTimer()
					tx, err := st.w.BeginTx(ctx, nil)
					mustBench(b, err)
					if guarded {
						mustBench(b, markKeptCountsStale(ctx, tx))
					}
					b.StartTimer()
					_, err = tx.ExecContext(ctx, "UPDATE assets SET hidden = 1 WHERE id <= ?", size)
					mustBench(b, err)
					b.StopTimer()
					mustBench(b, tx.Rollback())
					b.StartTimer()
				}
			})
		}
	}
}
