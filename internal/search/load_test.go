package search

import (
	"fmt"
	"testing"

	"lumen/internal/core"
	"lumen/internal/store"
)

func TestReservationSizesEverySliceAndPreservesExistingVectors(t *testing.T) {
	x := NewIndex(768)
	x.Put(7, make([]int8, 768), 0.1)
	x.reserve(10000)
	if x.Len() != 1 || cap(x.ids) < 10000 || cap(x.scales) < 10000 || cap(x.vecs) < 10000*768 {
		t.Fatal("reservation did not size every slice while retaining its rows")
	}
	original := &x.vecs[0]
	q := make([]int8, 768)
	for id := int64(8); id < 10007; id++ {
		x.Put(id, q, 0.2)
	}
	if &x.vecs[0] != original || x.Len() != 10000 || x.pos[7] != 0 || x.scales[0] != 0.1 {
		t.Fatal("reserved vectors grew or lost an existing vector")
	}
	x.reserve(1)
	if x.Len() != 10000 {
		t.Fatal("smaller reservation lost vectors")
	}
}

func TestEngineSizesItsStoredVectorsBeforeLoading(t *testing.T) {
	e, _, _ := searchFixture(t)
	const n = 4096
	src, err := e.store.AddSource(t.Context(), `C:\Load`)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]store.IngestItem, n)
	embeddings := make([]store.EmbedResult, n)
	for i := range items {
		items[i] = store.IngestItem{SourceID: src.ID, Path: fmt.Sprintf(`C:\Load\%d.jpg`, i), Hash: []byte(fmt.Sprintf("load-%d", i)), Kind: core.KindPhoto, TakenAt: 1}
		embeddings[i] = store.EmbedResult{AssetID: int64(i + 3), Vec: []int8{1, 2}, Scale: 0.1}
	}
	if _, err := e.store.Ingest(t.Context(), items); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SaveEmbeddings(t.Context(), e.model.Version, "v", embeddings); err != nil {
		t.Fatal(err)
	}
	if err := e.store.SetInt(t.Context(), core.SettingVocabulary, VocabularyVersion); err != nil {
		t.Fatal(err)
	}
	if err := e.load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e.Indexed() != n || cap(e.index.ids) > n+16 || cap(e.index.vecs) > n*e.model.Dim+16 || cap(e.index.scales) > n+16 {
		t.Fatalf("load did not reserve its exact size: len=%d capacities=%d/%d/%d", e.Indexed(), cap(e.index.ids), cap(e.index.vecs), cap(e.index.scales))
	}
}
