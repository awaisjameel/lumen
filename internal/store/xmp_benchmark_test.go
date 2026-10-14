package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// BenchmarkXMPTagBatch50 measures the stage write and the export's database
// work, with the same tag and face density as the large-library gate. Disk
// writes and packet hashing are excluded to isolate the full-library scan.
func BenchmarkXMPTagBatch50(b *testing.B) {
	ctx := b.Context()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	mustBench(b, err)
	b.Cleanup(func() { mustBench(b, st.Close()) })
	mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`INSERT INTO sources(id, path, added_at) VALUES (1, 'C:\Photos', 0)`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<100000)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, width, height)
			SELECT x, printf('%016x', x), 'photo', '.jpg', 1577836800+x*3600, 0, 1920, 1080 FROM n`,
			`INSERT INTO files(asset_id, source_id, path, size, mtime)
			SELECT id, 1, 'C:\Photos\'||id||'.jpg', 1000, 0 FROM assets`,
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
	mustBench(b, st.SetXMP(ctx, true))
	sets := make([]AssetTagSet, 50)
	for i := range sets {
		sets[i] = AssetTagSet{AssetID: int64(i + 1)}
		for j := range 8 {
			sets[i].Tags = append(sets[i].Tags, TagScore{TagID: int64(j + 1), Score: 0.5})
		}
	}
	legacy, err := migrationFS.ReadFile("migrations/0027_xmp.sql")
	mustBench(b, err)
	current, err := migrationFS.ReadFile("migrations/0035_xmp_dirty.sql")
	mustBench(b, err)
	for _, mode := range []string{"before", "after"} {
		if mode == "before" {
			mustBench(b, st.tx(ctx, func(tx *sql.Tx) error {
				names, err := queryAll(ctx, tx, func(r *sql.Rows) (string, error) {
					var name string
					err := r.Scan(&name)
					return name, err
				}, "SELECT name FROM sqlite_master WHERE type = 'trigger' AND name GLOB 'xmp_*'")
				if err != nil {
					return err
				}
				for _, name := range names {
					if _, err := tx.ExecContext(ctx, "DROP TRIGGER "+name); err != nil {
						return err
					}
				}
				_, err = tx.ExecContext(ctx, string(legacy))
				return err
			}))
		} else {
			mustBench(b, errOf(st.w.ExecContext(ctx, string(current))))
		}
		mustBench(b, errOf(st.w.ExecContext(ctx, "DELETE FROM xmp_dirty")))
		b.Run(mode, func(b *testing.B) {
			for b.Loop() {
				mustBench(b, st.ReplaceAITags(ctx, sets))
				xmpBenchRead(ctx, b, st, mode == "after")
			}
		})
	}
}

func xmpBenchRead(ctx context.Context, b *testing.B, st *Store, dirty bool) {
	b.Helper()
	read := st.XMPAssets
	if dirty {
		read = st.XMPDirtyAssets
	}
	var count int
	for after := int64(0); ; {
		batch, err := read(ctx, after, 100)
		mustBench(b, err)
		count += len(batch.Assets)
		if dirty {
			if len(batch.Pending) == 0 {
				break
			}
			after = batch.Pending[len(batch.Pending)-1].AssetID
			mustBench(b, st.SaveXMP(ctx, XMPChanges{Completed: batch.Pending}))
		} else {
			if len(batch.Assets) == 0 {
				break
			}
			after = batch.Assets[len(batch.Assets)-1].ID
		}
	}
	want := 100000
	if dirty {
		want = 50
	}
	if count != want {
		b.Fatalf("read %d assets; want %d", count, want)
	}
}
