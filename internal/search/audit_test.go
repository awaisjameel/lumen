package search

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"lumen/internal/ai"
	"lumen/internal/core"
	"lumen/internal/store"
	"lumen/internal/testenv"
)

func TestEncodeTextReleasesAFailedSessionAndRecovers(t *testing.T) {
	rt := testenv.Runtime(t)
	if err := ai.Init(rt); err != nil {
		t.Fatal(err)
	}
	m, err := ai.LoadModel(filepath.Join(rt, "models", "siglip2"))
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, m)
	t.Cleanup(e.Close)
	e.text, err = ai.OpenTextEncoder(m)
	if err != nil {
		t.Fatal(err)
	}
	e.text.Close() // Simulate a session that cannot run anymore.
	if _, err := e.EncodeText(t.Context(), "a query absent from shipped prompts"); err == nil {
		t.Fatal("failed session encoded a query")
	}
	if e.text != nil || e.textIdle != nil {
		t.Fatal("failed text session or its idle timer was retained")
	}
	vecs, err := e.EncodeText(t.Context(), "a query absent from shipped prompts")
	if err != nil || len(vecs) != 1 || len(vecs[0]) != m.Dim {
		t.Fatalf("next query did not recover: %v, %v", vecs, err)
	}
}

func TestCloseStopsLoadRetriesWithoutCancellingTheCaller(t *testing.T) {
	fixture, _, _ := searchFixture(t)
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(fixture.store, fixture.model)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	e.Start(ctx)
	if err := e.Wait(ctx); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("load of closed database = %v, want ErrNotLoaded", err)
	}
	done := make(chan struct{})
	go func() {
		e.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		cancel() // Release the old loader too, so a failure cannot leak it.
		<-done
		t.Fatal("Close waited for the caller to cancel the retry loop")
	}
	if ctx.Err() != nil {
		t.Fatal("Close cancelled the caller's context")
	}
	if err := e.Wait(ctx); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait after Close = %v, want ErrClosed", err)
	}
}

func TestStartAfterCloseCannotRestartTheLoader(t *testing.T) {
	e := NewEngine(nil, ai.Model{Dim: 2})
	e.Close()
	e.Start(t.Context())
	if e.started.Load() {
		t.Fatal("closed engine started a loader")
	}
	if err := e.Wait(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("Wait after Close = %v, want ErrClosed", err)
	}
}

func TestConcurrentStartsShareOneLoader(t *testing.T) {
	fixture, _, _ := searchFixture(t)
	if err := fixture.store.Close(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(fixture.store, fixture.model)
	t.Cleanup(e.Close)
	var starts sync.WaitGroup
	for range 4 {
		starts.Go(func() { e.Start(t.Context()) })
	}
	starts.Wait()
	if err := e.Wait(t.Context()); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("load of closed database = %v, want ErrNotLoaded", err)
	}
}

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
	e.index.Put(photo, []int8{50, 87}, 0.01)
	e.index.Put(video, []int8{100, 0}, 0.01)
	e.video.replace(video, []store.FrameEmbedding{{At: 1000, Vec: []int8{100, 0}, Scale: 0.01}})
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
	e.index.Put(video, []int8{0, 100}, 0.01) // mean doesn't resemble this moment
	e.video.replace(video, []store.FrameEmbedding{{At: 1000, Vec: []int8{100, 0}, Scale: 0.01}})
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
	e.index.Put(video, []int8{100, 0}, 0.01)
	e.video.replace(video, []store.FrameEmbedding{{At: 1000, Vec: []int8{0, 100}, Scale: 0.01}})
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

// A batch tagged before a Thing was created is tagged again as it is
// committed, so the Thing's re-scoring of the index can't have missed it.
func TestCommitRetagsABatchTaggedUnderOlderThings(t *testing.T) {
	e, photo, _ := searchFixture(t)
	gen := e.LabelsGeneration()
	batch := []store.EmbedResult{{AssetID: photo, Vec: []int8{127, 0}, Scale: 1.0 / 127}} // tagged: nothing
	e.labelsMu.Lock()
	e.labels = append(e.labels, label{id: 42, vecs: [][]float32{{1, 0}}}) // as CreateLabel does
	e.labelsGen.Add(1)
	e.labelsMu.Unlock()
	var saved []store.EmbedResult
	if err := e.Commit(gen, batch, func(rs []store.EmbedResult) error { saved = rs; return nil }); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || len(saved[0].Tags) != 1 || saved[0].Tags[0].TagID != 42 {
		t.Fatalf("saved tags = %+v, want the new Thing", saved)
	}
	if e.index.Len() != 1 {
		t.Fatalf("committed batch not searchable: %d", e.index.Len())
	}
}

// Prompt vectors must stay stale until scoring commits, so a restart finds
// photos indexed before an interrupted Thing creation.
func TestInterruptedCreateLabelRecoversOnRestart(t *testing.T) {
	ctx := t.Context()
	db := filepath.Join(t.TempDir(), "library.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	src, err := st.AddSource(ctx, `C:\Photos`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Ingest(ctx, []store.IngestItem{{SourceID: src.ID, Path: `C:\Photos\a.jpg`,
		Hash: []byte("a"), Kind: core.KindPhoto, TakenAt: 1}}); err != nil {
		t.Fatal(err)
	}
	days, err := st.DayItems(ctx, "1970-01-01", "1970-01-01", core.View{}, 0)
	if err != nil || len(days) != 1 || len(days[0].Items) != 1 {
		t.Fatalf("fixture: %+v, %v", days, err)
	}
	photo := days[0].Items[0].ID
	model := ai.Model{Dim: 2, Version: "test", LogitScale: 20, LogitBias: -10}
	if err := st.SaveEmbeddings(ctx, model.Version, "test", []store.EmbedResult{
		{AssetID: photo, Vec: []int8{100, 0}, Scale: 0.01},
	}); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, model)
	close(e.loaded)
	e.queries["beach"], e.queries["a photo of beach"] = []float32{1, 0}, []float32{1, 0}
	// Stop at the first index access during scoring, bypassing error cleanup
	// as a terminated process would. The restarted engine has a normal index.
	e.index = nil
	func() {
		defer func() {
			if recover() == nil {
				t.Error("creation did not stop before scoring committed")
			}
		}()
		_, _ = e.CreateLabel(ctx, "beach")
	}()
	tags, err := st.AutoTags(ctx)
	if err != nil || len(tags) != 1 {
		t.Fatalf("interrupted Thing: %+v, %v", tags, err)
	}
	tag := tags[0].ID
	if tags[0].Model != "" || len(tags[0].Vecs) != 0 {
		t.Error("interrupted Thing's vectors were marked current before scoring committed")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewEngine(st, model)
	restarted.queries["beach"], restarted.queries["a photo of beach"] = []float32{1, 0}, []float32{1, 0}
	if err := st.Embeddings(ctx, model.Version, restarted.index.Put); err != nil {
		t.Fatal(err)
	}
	if err := restarted.loadLabels(ctx, false); err != nil {
		t.Fatal(err)
	}
	got, err := st.AssetTags(ctx, photo, core.TagConfidenceDefault)
	if err != nil || len(got) != 1 || got[0].ID != tag {
		t.Fatalf("existing photo missed after restart: %+v, %v", got, err)
	}
	tags, err = st.AutoTags(ctx)
	if err != nil || len(tags) != 1 || tags[0].Model != model.Version || len(tags[0].Vecs) != 4 {
		t.Fatalf("completed Thing's vectors: %+v, %v", tags, err)
	}
}
