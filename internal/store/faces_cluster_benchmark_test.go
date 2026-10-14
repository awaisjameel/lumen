package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func clusterBenchmarkStore(b *testing.B) *Store {
	b.Helper()
	ctx := b.Context()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := st.Close(); err != nil {
			b.Error(err)
		}
	})
	err = st.tx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<120)
			INSERT INTO people(id, name, created_at) SELECT x, 'Person '||x, 0 FROM n`,
			`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<120100)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at)
			SELECT x, printf('%016x', x), 'photo', '.jpg', 1700000000, 0 FROM n`,
			`INSERT INTO faces(id, asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by)
			SELECT id AS face_id, id AS asset_id, 0.1, 0.1, 0.2, 0.2, 0.9, 50, 0, 'cluster-bench', zeroblob(512),
			CASE WHEN id<=120000 THEN 1+(id-1)/1000 END,
			CASE WHEN id<=120000 THEN 'auto' END FROM assets`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return st
}

func BenchmarkClusterData120000PlacedPlus100New(b *testing.B) {
	st := clusterBenchmarkStore(b)
	ctx := b.Context()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		faces, people, rejections, err := st.ClusterData(ctx, "cluster-bench")
		if err != nil {
			b.Fatal(err)
		}
		if len(faces) != 120100 || len(people) != 120 || len(rejections) != 0 {
			b.Fatalf("snapshot = %d faces, %d people, %d rejections", len(faces), len(people), len(rejections))
		}
	}
}

func BenchmarkIncrementalClusterData120000PlacedPlus100New(b *testing.B) {
	st := clusterBenchmarkStore(b)
	snapshot, err := st.ClusterSince(b.Context(), "cluster-bench", ClusterCursor{})
	if err != nil {
		b.Fatal(err)
	}
	cursor := snapshot.Cursor
	// The final hundred inserts are the arrivals since the retained snapshot.
	cursor.Revision -= 100
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		got, err := st.ClusterSince(b.Context(), "cluster-bench", cursor)
		if err != nil {
			b.Fatal(err)
		}
		if got.Full || len(got.Faces) != 100 || len(got.People) != 120 || len(got.Rejections) != 0 || len(got.Removed) != 0 {
			b.Fatalf("delta = %+v", got)
		}
	}
}
