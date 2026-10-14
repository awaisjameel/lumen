package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkIdleOptimize measures the first analysis and a subsequent small
// change, on exit (Close's PRAGMA optimize on the writer) and at idle
// (Optimize), both bounded by connectPragmas. Every iteration starts with an
// unanalysed library through the real triggers.
func BenchmarkIdleOptimize(b *testing.B) {
	for _, mode := range []string{"close", "idle"} {
		b.Run(mode, func(b *testing.B) {
			b.StopTimer()
			for range b.N {
				st, err := Open(filepath.Join(b.TempDir(), "library.db"))
				mustBench(b, err)
				mustBench(b, st.tx(b.Context(), func(tx *sql.Tx) error {
					for _, q := range []string{
						`INSERT INTO sources(id,path,added_at) VALUES (1,'C:\Photos',0)`,
						`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<100000)
       INSERT INTO assets(id,content_hash,kind,ext,taken_at,added_at) SELECT x,printf('%016x',x),'photo','.jpg',1577836800+x*86400,0 FROM n`,
						`INSERT INTO files(asset_id,source_id,path,size,mtime) SELECT id,1,'C:\Photos\'||id||'.jpg',1000,0 FROM assets`,
						`INSERT INTO asset_jobs(asset_id,stage,status,updated_at) SELECT id,stage,'done',0 FROM assets CROSS JOIN
       (SELECT 'metadata' stage UNION ALL SELECT 'thumbnail' UNION ALL SELECT 'embedding' UNION ALL SELECT 'faces')`,
						`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<180)
       INSERT INTO tags(id,name,kind,created_at) SELECT x,'Thing '||x,'builtin',0 FROM n`,
						`WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<7)
       INSERT INTO asset_tags(asset_id,tag_id,source,score) SELECT id,1+(id*7+x)%180,'ai',0.5 FROM assets CROSS JOIN n`,
					} {
						if _, err := tx.ExecContext(b.Context(), q); err != nil {
							return err
						}
					}
					return nil
				}))
				var readerCount int
				mustBench(b, st.r.QueryRowContext(b.Context(), "SELECT count(*) FROM files").Scan(&readerCount))
				optimize := func() error {
					if mode == "close" {
						_, err := st.w.DB.ExecContext(b.Context(), "PRAGMA optimize")
						return err
					}
					completed, err := st.Optimize(b.Context())
					if err == nil && !completed {
						err = errors.New("optimisation did not complete")
					}
					return err
				}
				b.StartTimer()
				start := time.Now()
				err = optimize()
				first := time.Since(start)
				b.StopTimer()
				mustBench(b, err)
				var stats int
				mustBench(b, st.r.QueryRowContext(b.Context(), "SELECT count(*) FROM sqlite_stat1").Scan(&stats))
				mustBench(b, st.tx(b.Context(), func(tx *sql.Tx) error {
					_, err := tx.ExecContext(b.Context(), "UPDATE files SET mtime=1 WHERE asset_id <= 64")
					return err
				}))
				start = time.Now()
				mustBench(b, optimize())
				small := time.Since(start)
				b.Logf("mode=%s first_ms=%.3f small_ms=%.3f statistics_rows=%d", mode, float64(first)/float64(time.Millisecond), float64(small)/float64(time.Millisecond), stats)
				mustBench(b, st.Close())
			}
		})
	}
}
