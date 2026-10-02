package store

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lumen/internal/core"
)

// seedThumbed adds n assets with a thumbnail each, asset i added at time 10 * i.
// Their ids are 1..n, in that order.
func seedThumbed(tb testing.TB, st *Store, n int) {
	tb.Helper()
	_, err := st.w.ExecContext(context.Background(), `WITH RECURSIVE n(x) AS
		(SELECT 1 UNION ALL SELECT x + 1 FROM n WHERE x < ?)
		INSERT INTO assets(content_hash, kind, ext, taken_at, added_at, thumb_version)
		SELECT printf('%016x', x), 'photo', '.jpg', x, x * 10, 1 FROM n`, n)
	if err != nil {
		tb.Fatal(err)
	}
}

func addJob(t *testing.T, st *Store, id int64, stage core.Stage, status core.JobStatus) {
	t.Helper()
	check(t, errOf(st.w.ExecContext(t.Context(),
		`INSERT INTO asset_jobs (asset_id, stage, status, updated_at) VALUES (?, ?, ?, 0)`, id, stage, status)))
}

func candidates(t *testing.T, st *Store, limit int) []int64 {
	t.Helper()
	ids, err := st.ThumbEvictionCandidates(t.Context(), limit)
	check(t, err)
	return ids
}

func TestThumbCandidatesDropNeverViewedFirstThenLeastRecentlyViewed(t *testing.T) {
	st := openTest(t)
	seedThumbed(t, st, 6)
	check(t, st.TouchThumbs(t.Context(), map[int64]int64{2: 500, 4: 100}))
	if got, want := candidates(t, st, 10), []int64{1, 3, 5, 6, 4, 2}; !slices.Equal(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	if got, want := candidates(t, st, 5), []int64{1, 3, 5, 6, 4}; !slices.Equal(got, want) {
		t.Fatalf("limited to 5 = %v, want %v", got, want)
	}
	if got := candidates(t, st, 2); !slices.Equal(got, []int64{1, 3}) {
		t.Fatalf("limited to 2 = %v, want [1 3]", got)
	}
}

func TestThumbCandidatesSkipAssetsWhoseAIJobsAreUnfinished(t *testing.T) {
	st := openTest(t)
	seedThumbed(t, st, 8)
	check(t, st.TouchThumbs(t.Context(), map[int64]int64{7: 10, 8: 20}))
	addJob(t, st, 1, core.StageEmbedding, core.JobPending)
	addJob(t, st, 2, core.StageFaces, core.JobWaiting)
	addJob(t, st, 3, core.StageOCR, core.JobPending)
	addJob(t, st, 4, core.StageEmbedding, core.JobDone)   // finished: free to go
	addJob(t, st, 5, core.StageMetadata, core.JobPending) // not read from the cache
	addJob(t, st, 7, core.StageFaces, core.JobPending)    // viewed, but still wanted
	// Asset 6 has no thumbnail to drop.
	check(t, errOf(st.w.ExecContext(t.Context(), "UPDATE assets SET thumb_version = 0 WHERE id = 6")))
	if got, want := candidates(t, st, 10), []int64{4, 5, 8}; !slices.Equal(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	// The job finishing frees the asset.
	check(t, errOf(st.w.ExecContext(t.Context(), "UPDATE asset_jobs SET status = 'done' WHERE asset_id = 1")))
	if got := candidates(t, st, 1); !slices.Equal(got, []int64{1}) {
		t.Fatalf("after the job finished = %v, want [1]", got)
	}
}

func TestTouchThumbsWritesOneBatchAndKeepsTheLatestView(t *testing.T) {
	st := openTest(t)
	seedThumbed(t, st, 3)
	check(t, st.TouchThumbs(t.Context(), nil)) // nothing to write is not an error
	// Asset 99 was deleted since it was shown: it must not fail the others.
	check(t, st.TouchThumbs(t.Context(), map[int64]int64{1: 100, 2: 200, 99: 300}))
	check(t, st.TouchThumbs(t.Context(), map[int64]int64{1: 50, 2: 250})) // 1's view is older than the stored one
	views := map[int64]int64{}
	rows, err := st.r.QueryContext(t.Context(), "SELECT asset_id, viewed_at FROM thumb_views")
	check(t, err)
	defer rows.Close()
	for rows.Next() {
		var id, at int64
		check(t, rows.Scan(&id, &at))
		views[id] = at
	}
	check(t, rows.Err())
	if len(views) != 2 || views[1] != 100 || views[2] != 250 {
		t.Fatalf("views = %v, want asset 1 at 100 and asset 2 at 250", views)
	}
}

func TestThumbViewsGoWithTheirAsset(t *testing.T) {
	st := openTest(t)
	seedThumbed(t, st, 2)
	check(t, st.TouchThumbs(t.Context(), map[int64]int64{1: 1, 2: 2}))
	check(t, errOf(st.w.ExecContext(t.Context(), "DELETE FROM assets WHERE id = 1")))
	var n int
	check(t, st.r.QueryRowContext(t.Context(), "SELECT count(*) FROM thumb_views").Scan(&n))
	if n != 1 {
		t.Fatalf("%d view rows after deleting an asset, want 1", n)
	}
}

func TestEvictedThumbsStopBeingCandidatesUntilViewedOrRebuilt(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedThumbed(t, st, 4)
	check(t, st.TouchThumbs(ctx, map[int64]int64{3: 100, 4: 200}))
	n, err := st.MarkThumbsEvicted(ctx, []int64{1, 3}, 1000)
	check(t, err)
	if n != 2 {
		t.Fatalf("marked %d rows, want 2", n)
	}
	if got, want := candidates(t, st, 10), []int64{2, 4}; !slices.Equal(got, want) {
		t.Fatalf("after evicting 1 and 3, candidates = %v, want %v", got, want)
	}

	// Shown again (it was rebuilt for the request): back in the order, at its new place.
	check(t, st.TouchThumbs(ctx, map[int64]int64{3: 300}))
	if got, want := candidates(t, st, 10), []int64{2, 4, 3}; !slices.Equal(got, want) {
		t.Fatalf("after a view, candidates = %v, want %v", got, want)
	}

	// Rebuilt by the thumbnail stage, which bumps the version: also back.
	check(t, errOf(st.w.ExecContext(ctx, "UPDATE assets SET thumb_version = thumb_version + 1 WHERE id = 1")))
	if got, want := candidates(t, st, 10), []int64{1, 2, 4, 3}; !slices.Equal(got, want) {
		t.Fatalf("after a rebuild, candidates = %v, want %v", got, want)
	}
}

func TestMarkingEvictedKeepsAViewMadeAfterTheEvictionBegan(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	seedThumbed(t, st, 2)
	check(t, st.TouchThumbs(ctx, map[int64]int64{1: 2000, 2: 500}))
	_, err := st.MarkThumbsEvicted(ctx, []int64{1, 2}, 1000)
	check(t, err)
	// Asset 1 was shown (and so rebuilt) after the eviction began; asset 2 before it.
	if got := candidates(t, st, 10); !slices.Equal(got, []int64{1}) {
		t.Fatalf("candidates = %v, want [1]", got)
	}
}

func TestThumbCacheCapIsHeldToItsRange(t *testing.T) {
	st, ctx := openTest(t), t.Context()
	if mb, err := st.ThumbCacheMB(ctx); err != nil || mb != 2048 {
		t.Fatalf("default cap = %d MB (%v), want 2048", mb, err)
	}
	for _, tc := range []struct{ set, want int }{
		{4096, 4096},
		{1, core.ThumbCacheMinMB},
		{-5, core.ThumbCacheMinMB},
		{core.ThumbCacheMaxMB * 4, core.ThumbCacheMaxMB},
	} {
		stored, err := st.SetThumbCacheMB(ctx, tc.set)
		check(t, err)
		got, err := st.ThumbCacheMB(ctx)
		check(t, err)
		if stored != tc.want || got != tc.want {
			t.Errorf("set %d: stored %d, read %d, want %d", tc.set, stored, got, tc.want)
		}
	}
	// A value edited by hand out of range reads as the default, as Settings does.
	check(t, st.SetInt(ctx, core.SettingThumbCacheMB, 3))
	if mb, _ := st.ThumbCacheMB(ctx); mb != core.ThumbCacheDefaultMB {
		t.Errorf("out of range reads as %d, want the default", mb)
	}
}

func TestThumbCandidateQueriesUseTheirIndexes(t *testing.T) {
	st := openTest(t)
	seedThumbed(t, st, 100)
	for name, q := range map[string]struct{ sql, index string }{
		"never viewed": {neverViewedQuery, "assets_thumb_added"},
		"viewed":       {viewedQuery, "thumb_views_oldest"},
	} {
		plan := queryPlan(t, st, q.sql, 10)
		if !strings.Contains(plan, q.index) || strings.Contains(plan, "TEMP B-TREE") {
			t.Errorf("%s plan should walk %s without sorting:\n%s", name, q.index, plan)
		}
	}
}

// queryPlan is SQLite's plan for a query, one step to a line.
func queryPlan(t *testing.T, st *Store, q string, args ...any) string {
	t.Helper()
	rows, err := st.r.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+q, args...)
	check(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		check(t, rows.Scan(&id, &parent, &unused, &detail))
		plan = append(plan, detail)
	}
	check(t, rows.Err())
	return strings.Join(plan, "\n")
}

// thumbBenchStore is a library of 100,000 assets with thumbnails, the size
// the cap is designed for.
func thumbBenchStore(b *testing.B) *Store {
	b.Helper()
	st, err := Open(filepath.Join(b.TempDir(), "library.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { st.Close() })
	seedThumbed(b, st, 100_000)
	return st
}

func BenchmarkThumbEvictionCandidates(b *testing.B) {
	for _, tc := range []struct {
		name   string
		viewed int // of every 100 assets, this many have been shown
	}{{"none viewed", 0}, {"half viewed", 50}, {"all viewed", 100}} {
		b.Run(tc.name, func(b *testing.B) {
			st := thumbBenchStore(b)
			if tc.viewed > 0 {
				_, err := st.w.ExecContext(b.Context(), `INSERT INTO thumb_views (asset_id, viewed_at)
					SELECT id, id FROM assets WHERE id % 100 < ?`, tc.viewed)
				if err != nil {
					b.Fatal(err)
				}
			}
			// Evicted assets pile up at the front of the order in a real
			// library: mark the first third so the query has to walk past them.
			first := make([]int64, 33_000)
			for i := range first {
				first[i] = int64(i + 1)
			}
			if _, err := st.MarkThumbsEvicted(b.Context(), first, 1<<40); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				if ids, err := st.ThumbEvictionCandidates(b.Context(), 500); err != nil || len(ids) != 500 {
					b.Fatalf("candidates = %d, %v", len(ids), err)
				}
			}
		})
	}
}

func BenchmarkTouchThumbs10000(b *testing.B) {
	st := thumbBenchStore(b)
	views := make(map[int64]int64, 10_000)
	for i := range 10_000 {
		views[int64(i*10+1)] = int64(i)
	}
	for b.Loop() {
		if err := st.TouchThumbs(b.Context(), views); err != nil {
			b.Fatal(err)
		}
	}
}
