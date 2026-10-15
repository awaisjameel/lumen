package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"testing"
)

func TestEmbeddingReservationAndVectorsShareAModelSnapshot(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b"), item(src.ID, `C:\Photos\c.jpg`, "c")})))
	check(t, st.SaveEmbeddings(ctx, "current", "v", []EmbedResult{{AssetID: 1, Vec: []int8{1, 2}, Scale: 0.1}, {AssetID: 2, Vec: []int8{3, 4}, Scale: 0.2}}))
	check(t, st.SaveEmbeddings(ctx, "old", "v", []EmbedResult{{AssetID: 3, Vec: []int8{5, 6}, Scale: 0.3}}))
	var prepared, visited int
	check(t, st.LoadEmbeddings(ctx, "current", func(n int) {
		prepared = n
		if visited != 0 {
			t.Fatal("vectors arrived before reservation")
		}
		check(t, st.SaveEmbeddings(ctx, "current", "v", []EmbedResult{{AssetID: 1, Vec: []int8{9, 9}, Scale: 0.9}, {AssetID: 3, Vec: []int8{7, 8}, Scale: 0.4}}))
	}, func(id int64, vec []int8, scale float32) {
		visited++
		if prepared != 2 || id == 3 {
			t.Fatalf("snapshot count=%d id=%d", prepared, id)
		}
		if id == 1 && (vec[0] != 1 || scale != 0.1) {
			t.Fatalf("vector read a newer snapshot: %v, %v", vec, scale)
		}
	}))
	if prepared != 2 || visited != 2 {
		t.Fatalf("count=%d visits=%d", prepared, visited)
	}
	visited = 0
	check(t, st.Embeddings(ctx, "current", func(_ int64, _ []int8, _ float32) { visited++ }))
	if visited != 3 {
		t.Fatalf("committed vectors=%d", visited)
	}
}

func TestEmbeddingLoadReservesAnEmptyModelAndReportsCancellation(t *testing.T) {
	st := openTest(t)
	prepared := false
	check(t, st.LoadEmbeddings(t.Context(), "absent", func(n int) {
		prepared = true
		if n != 0 {
			t.Fatal(n)
		}
	}, func(_ int64, _ []int8, _ float32) { t.Fatal("empty model returned a vector") }))
	if !prepared {
		t.Fatal("empty model did not reserve zero vectors")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := st.LoadEmbeddings(ctx, "absent", func(int) { t.Fatal("cancelled load reserved memory") }, func(_ int64, _ []int8, _ float32) { t.Fatal("cancelled load returned a vector") })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled load=%v", err)
	}
}

func TestEmbeddingLoadPreservesEverySignedValueAndScale(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{
		item(src.ID, `C:\Photos\a.jpg`, "a"),
		item(src.ID, `C:\Photos\b.jpg`, "b"),
		item(src.ID, `C:\Photos\c.jpg`, "c"),
		item(src.ID, `C:\Photos\d.jpg`, "d"),
	})))
	all := make([]int8, 768)
	for i := range all {
		all[i] = int8(i)
	}
	want := map[int64]EmbedResult{
		1: {AssetID: 1, Vec: all, Scale: 0.003125},
		2: {AssetID: 2, Vec: []int8{-128, -1, 0, 1, 127}, Scale: math.SmallestNonzeroFloat32},
		3: {AssetID: 3, Vec: []int8{}, Scale: math.MaxFloat32},
	}
	for _, r := range want {
		check(t, st.SaveEmbeddings(ctx, "current", "v", []EmbedResult{r}))
	}
	check(t, st.SaveEmbeddings(ctx, "old", "v", []EmbedResult{{AssetID: 4, Vec: []int8{99}, Scale: 0.9}}))
	seen := make(map[int64]bool)
	check(t, st.LoadEmbeddings(ctx, "current", func(n int) {
		if n != len(want) || len(seen) != 0 {
			t.Fatalf("reservation=%d visits=%d", n, len(seen))
		}
	}, func(id int64, vec []int8, scale float32) {
		r, ok := want[id]
		if !ok || seen[id] {
			t.Fatalf("unexpected or repeated id=%d", id)
		}
		if !slices.Equal(vec, r.Vec) || math.Float32bits(scale) != math.Float32bits(r.Scale) {
			t.Fatalf("id=%d vector=%v scale=%v, want %v scale=%v", id, vec, scale, r.Vec, r.Scale)
		}
		seen[id] = true
	}))
	if len(seen) != len(want) {
		t.Fatalf("visited=%d want=%d", len(seen), len(want))
	}
}

func TestEmbeddingLoadAvoidsPerRowScanConversionAllocations(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	const count = 1000
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x < ?)
			INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at)
			SELECT x, printf('%016x', x), 'photo', '.jpg', 0, 0 FROM n`, count)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO embeddings(asset_id, model, scale, vec)
			SELECT id, 'current', 0.003125, zeroblob(768) FROM assets`)
		return err
	}))
	allocs := testing.AllocsPerRun(5, func() {
		check(t, st.LoadEmbeddings(ctx, "current", func(int) {}, func(_ int64, _ []int8, _ float32) {}))
	})
	// The driver owns four allocations per row; scanning must not add numeric conversions.
	if allocs > 4*count+100 {
		t.Fatalf("load allocated %.0f times, want at most %d", allocs, 4*count+100)
	}
}

func TestEmbeddingLoadStillRejectsAnOverflowingScale(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a")})))
	check(t, st.SaveEmbeddings(ctx, "current", "v", []EmbedResult{{AssetID: 1, Vec: []int8{-1}, Scale: 0.1}}))
	check(t, st.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE embeddings SET scale = ?", math.MaxFloat64)
		return err
	}))
	err = st.LoadEmbeddings(ctx, "current", nil, func(_ int64, _ []int8, _ float32) {
		t.Fatal("an overflowing scale reached the visitor")
	})
	if err == nil {
		t.Fatal("an overflowing scale did not return a scan error")
	}
}
