package search

import (
	"context"
	"path/filepath"
	"testing"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/store"
)

func searchFixture(t *testing.T) (engine *Engine, photo, video int64) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{
		{SourceID: src.ID, Path: `C:\Photos\a.jpg`, Hash: []byte("a"), Kind: core.KindPhoto, TakenAt: 1},
		{SourceID: src.ID, Path: `C:\Photos\b.mp4`, Hash: []byte("b"), Kind: core.KindVideo, TakenAt: 2},
	}); err != nil {
		t.Fatal(err)
	}
	days, err := st.DayItems(ctx, "1970-01-01", "1970-01-01", core.View{}, 0.02)
	if err != nil || len(days) != 1 || len(days[0].Items) != 2 {
		t.Fatalf("fixture: %+v, %v", days, err)
	}
	e := NewEngine(st, ai.Model{Dim: 2, Version: "test", LogitScale: 20, LogitBias: -10})
	close(e.loaded)
	return e, days[0].Items[1].ID, days[0].Items[0].ID
}

func TestRemovedAssetsDoNotSuppressVisibleSearchResults(t *testing.T) {
	e, photo, video := searchFixture(t)
	ctx := context.Background()
	e.queries["beach"] = []float32{1, 0}
	e.Add(photo, []int8{50, 87}, 0.01)
	e.Add(video, []int8{100, 0}, 0.01)
	e.AddVideo(video, []store.FrameEmbedding{{At: 1000, Vec: []int8{100, 0}, Scale: 0.01}})
	sources, err := e.store.ListSources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files, err := e.store.KnownFiles(ctx, sources[0].ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.RemoveFiles(ctx, []int64{files[`c:\photos\b.mp4`].ID}); err != nil {
		t.Fatal(err)
	}
	hits, complete, err := e.Search(ctx, "beach", nil)
	if err != nil || !complete || len(hits) != 1 || hits[0].ID != photo {
		t.Fatalf("removed best hit suppressed visible photo: %+v, complete=%v, err=%v", hits, complete, err)
	}
}

func TestVideoTagsUseMomentsWhenRescoringAndRetagging(t *testing.T) {
	e, _, video := searchFixture(t)
	ctx := context.Background()
	tag, err := e.store.CreateTag(ctx, "beach", core.TagLabel, []string{"beach"})
	if err != nil {
		t.Fatal(err)
	}
	l := label{id: tag, vecs: [][]float32{{1, 0}}}
	e.labels = []label{l}
	e.Add(video, []int8{0, 100}, 0.01) // mean doesn't resemble this moment
	e.AddVideo(video, []store.FrameEmbedding{{At: 1000, Vec: []int8{100, 0}, Scale: 0.01}})
	for _, run := range []struct {
		name string
		fn   func() error
	}{
		{"new label", func() error { return e.rescore(ctx, l) }},
		{"vocabulary update", func() error { return e.retag(ctx) }},
	} {
		t.Run(run.name, func(t *testing.T) {
			if err := run.fn(); err != nil {
				t.Fatal(err)
			}
			tags, err := e.store.AssetTags(ctx, video, core.TagConfidenceDefault)
			if err != nil || len(tags) != 1 || tags[0].ID != tag {
				t.Fatalf("moment's tag lost: %+v, %v", tags, err)
			}
		})
	}
	// A mean vector may resemble a label that none of the sampled moments do.
	// Creating that label must agree with indexing and vocabulary updates.
	e.Add(video, []int8{100, 0}, 0.01)
	e.AddVideo(video, []store.FrameEmbedding{{At: 1000, Vec: []int8{0, 100}, Scale: 0.01}})
	if err := e.rescore(ctx, l); err != nil {
		t.Fatal(err)
	}
	tags, err := e.store.AssetTags(ctx, video, core.TagConfidenceDefault)
	if err != nil || len(tags) != 0 {
		t.Fatalf("mean introduced a tag absent from every moment: %+v, %v", tags, err)
	}
}

func TestBestTagsKeepsStrongestScoresWithStableTies(t *testing.T) {
	var tags []store.TagScore
	for id := int64(20); id > 0; id-- {
		tags = append(tags, store.TagScore{TagID: id, Score: 0.5})
	}
	got := BestTags(tags, []store.TagScore{{TagID: 20, Score: 0.9}})
	if len(got) != maxTagsPerItem || got[0].TagID != 20 || got[1].TagID != 1 {
		t.Fatalf("unstable or incorrect tags: %+v", got)
	}
}

func TestVocabularyUpdateRemainsPendingUntilLoadCompletes(t *testing.T) {
	e, _, _ := searchFixture(t)
	ctx := context.Background()
	// Simulate stopping after the vocabulary is written but before re-tagging.
	for range 2 {
		changed, err := e.syncVocabulary(ctx)
		if err != nil || !changed {
			t.Fatalf("interrupted update must retry: changed=%v, err=%v", changed, err)
		}
	}
	// Cached prompt vectors let the restarted load finish without an AI session.
	tags, err := e.store.AutoTags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		vecs := make([]float32, len(tag.Prompts)*e.model.Dim)
		for i := 0; i < len(vecs); i += e.model.Dim {
			vecs[i] = 1
		}
		if err := e.store.SetTagVectors(ctx, tag.ID, e.model.Version, vecs); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.load(ctx); err != nil {
		t.Fatal(err)
	}
	changed, err := e.syncVocabulary(ctx)
	if err != nil || changed {
		t.Fatalf("completed update should not repeat: changed=%v, err=%v", changed, err)
	}
}
