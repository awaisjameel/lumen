package people

import (
	"math"
	"reflect"
	"runtime"
	"slices"
	"testing"

	"lumen/internal/store"
)

func TestRetainedPlacedNeighbourChangesTheReferenceResult(t *testing.T) {
	p := DefaultParams
	faces := []store.ClusterFace{
		{ID: 1, AssetID: 1000, Sharpness: 50, Vec: []float32{1, 0}},
		{ID: 2, AssetID: 2000, Sharpness: 50, Vec: []float32{0.9, float32(math.Sqrt(0.19))}},
	}
	var people []store.ClusterPerson
	for i := range p.Neighbours {
		id := int64(i + 1)
		faces = append(faces, store.ClusterFace{ID: id + 2, AssetID: 1000, Person: id, Vec: []float32{1, 0}})
		people = append(people, store.ClusterPerson{ID: id, Confirmed: true})
	}
	linker := NewLinker(p)
	initialLinks, err := linker.Update(t.Context(), faces)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := Cluster(t.Context(), Input{Faces: faces, People: people, Links: initialLinks}, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Assign) != 0 || len(initial.NewPeople) != 0 || len(initial.Merges) != 0 {
		t.Fatalf("initial run changed the fixture: %+v", initial)
	}
	// A manual placement leaves a retained link to a person outside the
	// twelve nearest means. Every nearer person shares the loose face's photo.
	person := int64(p.Neighbours + 1)
	faces[1].Person, faces[1].ByUser = person, true
	people = append(people, store.ClusterPerson{ID: person, Confirmed: true})
	links, err := linker.Update(t.Context(), faces)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := Cluster(t.Context(), Input{Faces: faces, People: people, Links: links}, p)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := Cluster(t.Context(), Input{Faces: faces, People: people}, p)
	if err != nil {
		t.Fatal(err)
	}
	if retained.Assign[1] != person || len(retained.Assign) != 1 || len(fresh.Assign) != 0 {
		t.Fatalf("retained = %+v; fresh = %+v", retained, fresh)
	}
	state := newClusterState(p)
	before := slices.Clone(faces)
	before[1].Person, before[1].ByUser = 0, false
	if err := state.update(t.Context(), store.ClusterSnapshot{Full: true, Faces: before}); err != nil {
		t.Fatal(err)
	}
	incrementalLinks := NewLinker(p)
	if _, err := incrementalLinks.Update(t.Context(), before); err != nil {
		t.Fatal(err)
	}
	if err := state.update(t.Context(), store.ClusterSnapshot{Faces: []store.ClusterFace{faces[1]}}); err != nil {
		t.Fatal(err)
	}
	loose := state.looseFaces()
	linked, err := incrementalLinks.updateChanged(t.Context(), loose, []store.ClusterFace{faces[1]}, nil)
	if err != nil {
		t.Fatal(err)
	}
	incremental, err := state.cluster(t.Context(), loose, people, linked)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(incremental, retained) {
		t.Fatalf("incremental = %+v; retained reference = %+v", incremental, retained)
	}
	if reflect.DeepEqual(retained, fresh) {
		t.Fatal("the retained neighbour unexpectedly matched the fresh reference")
	}
	t.Logf("retained assignment = %v; fresh assignment = %v", retained.Assign, fresh.Assign)
}

func BenchmarkCluster120000PlacedPlus100New(b *testing.B) {
	w := newWorld(20261013, 120)
	faces := make([]store.ClusterFace, 0, 120100)
	people := make([]store.ClusterPerson, 0, 120)
	for k := range 120 {
		id := int64(k + 1)
		people = append(people, store.ClusterPerson{ID: id, Confirmed: true})
		for range 1000 {
			f := w.face(k, 0)
			f.Person = id
			faces = append(faces, f)
		}
	}
	linker := NewLinker(DefaultParams)
	if _, err := linker.Update(b.Context(), faces); err != nil {
		b.Fatal(err)
	}
	for k := range 100 {
		faces = append(faces, w.face(k, 0))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		// Forget only this batch, so every sample compares its hundred new
		// faces while keeping the placed faces' fingerprints from the prior run.
		for _, f := range faces[120000:] {
			delete(linker.near, f.ID)
		}
		b.StartTimer()
		links, err := linker.Update(b.Context(), faces)
		if err != nil {
			b.Fatal(err)
		}
		got, err := Cluster(b.Context(), Input{Faces: faces, People: people, Links: links}, DefaultParams)
		if err != nil {
			b.Fatal(err)
		}
		if len(got.Assign) != 100 || len(got.NewPeople) != 0 || len(got.Merges) != 0 {
			b.Fatalf("new batch = %+v", got)
		}
	}
}

func BenchmarkIncrementalCluster120000PlacedPlus100New(b *testing.B) {
	w := newWorld(20261013, 120)
	var faces []store.ClusterFace
	var people []store.ClusterPerson
	for k := range 120 {
		id := int64(k + 1)
		people = append(people, store.ClusterPerson{ID: id, Confirmed: true})
		for range 1000 {
			f := w.face(k, 0)
			f.Person = id
			faces = append(faces, f)
		}
	}
	state := newClusterState(DefaultParams)
	if err := state.update(b.Context(), store.ClusterSnapshot{Full: true, Faces: faces}); err != nil {
		b.Fatal(err)
	}
	linker := NewLinker(DefaultParams)
	if _, err := linker.Update(b.Context(), faces); err != nil {
		b.Fatal(err)
	}
	var batch []store.ClusterFace
	for k := range 100 {
		batch = append(batch, w.face(k, 0))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		for _, f := range batch {
			delete(linker.near, f.ID)
		}
		b.StartTimer()
		if err := state.update(b.Context(), store.ClusterSnapshot{Faces: batch}); err != nil {
			b.Fatal(err)
		}
		loose := state.looseFaces()
		links, err := linker.updateChanged(b.Context(), loose, batch, nil)
		if err != nil {
			b.Fatal(err)
		}
		got, err := state.cluster(b.Context(), loose, people, links)
		if err != nil {
			b.Fatal(err)
		}
		if len(got.Assign) != 100 || len(got.NewPeople) != 0 || len(got.Merges) != 0 {
			b.Fatalf("new batch = %+v", got)
		}
	}
}

func TestClusterStateSizeAt120000PlacedFaces(t *testing.T) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	state, linker := retainedClusterFixture(t)
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("retained state: %d bytes for %d faces and %d people", after.HeapAlloc-before.HeapAlloc, len(state.faces), len(state.people))
	runtime.KeepAlive(state)
	runtime.KeepAlive(linker)
}

func retainedClusterFixture(t *testing.T) (*clusterState, *Linker) {
	t.Helper()
	w := newWorld(20261013, 120)
	var faces []store.ClusterFace
	for k := range 120 {
		for range 1000 {
			f := w.face(k, 0)
			f.Person = int64(k + 1)
			faces = append(faces, f)
		}
	}
	state := newClusterState(DefaultParams)
	if err := state.update(t.Context(), store.ClusterSnapshot{Full: true, Faces: faces}); err != nil {
		t.Fatal(err)
	}
	linker := NewLinker(DefaultParams)
	if _, err := linker.Update(t.Context(), faces); err != nil {
		t.Fatal(err)
	}
	return state, linker
}
