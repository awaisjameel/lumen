package store

import (
	"context"
	"errors"
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
