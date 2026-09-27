package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"lumen/internal/core"
)

func TestMetadataPreservesCaptureDatesBeforeUnixEpoch(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	src, err := st.AddSource(ctx, `C:\Photos`)
	check(t, err)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\scan.jpg`, "old-photo")})))
	jobs, err := st.PendingJobs(ctx, core.StageMetadata, nil, 1)
	check(t, err)
	if len(jobs) != 1 {
		t.Fatal("missing fixture job")
	}
	at := time.Date(1965, time.May, 12, 14, 30, 0, 0, time.UTC).Unix()
	check(t, st.SaveMetadata(ctx, "v2", []MetadataResult{{AssetID: jobs[0].AssetID, TakenAt: at}}))
	detail, err := st.AssetDetail(ctx, jobs[0].AssetID)
	check(t, err)
	if detail.TakenAt != at || detail.DateFrom != core.DateFromEXIF {
		t.Fatalf("lost capture date: %+v", detail)
	}
}

func TestConcurrentSourceAdditionsNeverOverlap(t *testing.T) {
	st, ctx := openTest(t), context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, path := range []string{`C:\Photos`, `C:\Photos\Trip`, `c:\photos`, `C:\Photos\Trip\Day`} {
		wg.Go(func() {
			<-start
			if _, err := st.AddSource(ctx, path); err != nil && !errors.Is(err, ErrAlreadyCovered) {
				t.Errorf("add source: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()
	sources, err := st.ListSources(ctx)
	check(t, err)
	if len(sources) != 1 || !IsWithin(sources[0].Path, `C:\Photos`) || !IsWithin(`C:\Photos`, sources[0].Path) {
		t.Fatalf("overlapping sources: %+v", sources)
	}
}

func TestStaleClusteringRespectsNewRejections(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	fa, fb := facesOf(t, st, a)[0].ID, facesOf(t, st, b)[0].ID
	check(t, st.ApplyClusters(ctx, ClusterChanges{NewPeople: [][]int64{{fa, fb}}}))
	person := facesOf(t, st, a)[0].Person
	check(t, st.RejectFaces(ctx, person, []int64{fb}))
	check(t, st.ApplyClusters(ctx, ClusterChanges{Assign: map[int64]int64{fb: person}}))
	if got := facesOf(t, st, b)[0]; got.Person != 0 {
		t.Fatalf("stale assignment undid rejection: %+v", got)
	}
}

func TestStaleMergeRespectsNewRejections(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	fa, fb := facesOf(t, st, a)[0].ID, facesOf(t, st, b)[0].ID
	check(t, st.ApplyClusters(ctx, ClusterChanges{NewPeople: [][]int64{{fa}, {fb}}}))
	pa, pb := facesOf(t, st, a)[0].Person, facesOf(t, st, b)[0].Person
	check(t, st.RejectFaces(ctx, pa, []int64{fb}))
	for _, m := range []Merge{{From: pa, Into: pb}, {From: pb, Into: pa}} {
		check(t, st.ApplyClusters(ctx, ClusterChanges{Merges: []Merge{m}}))
		if facesOf(t, st, a)[0].Person != pa || facesOf(t, st, b)[0].Person != pb {
			t.Fatalf("stale merge undid rejection: %+v", m)
		}
	}
}
