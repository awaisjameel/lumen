package people

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"lumen/internal/store"
)

const dim = 128

func TestClusterWithCachedLinksHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	in := Input{Faces: []store.ClusterFace{{ID: 1, Vec: []float32{1}}, {ID: 2, Person: 1, Vec: []float32{1}}}, Links: map[int64][]int64{1: {2}}}
	got, err := Cluster(ctx, in, DefaultParams)
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(got, store.ClusterChanges{}) {
		t.Fatalf("cancelled clustering = %+v, %v", got, err)
	}
}

type cancelOnCheck struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *cancelOnCheck) Err() error {
	c.checks++
	if c.checks == 3 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestLinkExistingPeopleStopsDuringCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checked := &cancelOnCheck{Context: ctx, cancel: cancel}
	var faces []store.ClusterFace
	for i := range 64 {
		id := int64(i + 1)
		faces = append(faces, store.ClusterFace{ID: id, AssetID: id, Person: id, Vec: []float32{1}})
	}
	c := newClustering(faces, nil, nil)
	if err := c.link(checked, map[int64][]int64{}, DefaultParams); !errors.Is(err, context.Canceled) {
		t.Fatalf("link cancellation = %v", err)
	}
	// Cancellation at the next periodic check must leave later pairs alone.
	if _, linked := c.groups[16].links[17]; linked {
		t.Error("existing people kept linking after cancellation")
	}
}

func TestMergeStopsWhileBuildingCandidateQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checked := &cancelOnCheck{Context: ctx, cancel: cancel}
	var faces []store.ClusterFace
	for i := range 64 {
		id := int64(i + 1)
		faces = append(faces, store.ClusterFace{ID: id, AssetID: id, Person: id, Vec: []float32{1}})
	}
	c := newClustering(faces, nil, nil)
	if err := c.link(ctx, map[int64][]int64{}, DefaultParams); err != nil {
		t.Fatal(err)
	}
	if err := c.merge(checked, DefaultParams.Threshold); !errors.Is(err, context.Canceled) {
		t.Fatalf("merge cancellation = %v", err)
	}
	for _, g := range c.groups {
		if g.dead {
			t.Fatal("groups merged after cancellation while building the queue")
		}
	}
}

func TestLinkerStopsWhileComparingOneNewFace(t *testing.T) {
	l := NewLinker(DefaultParams)
	faces := make([]store.ClusterFace, 0, 2049)
	for i := range 2048 {
		faces = append(faces, store.ClusterFace{ID: int64(i + 1), Vec: []float32{1}})
	}
	if _, err := l.Update(t.Context(), faces); err != nil {
		t.Fatal(err)
	}
	fresh := store.ClusterFace{ID: 2049, Vec: []float32{1}}
	faces = append(faces, fresh)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checked := &cancelOnCheck{Context: ctx, cancel: cancel}
	if _, err := l.Update(checked, faces); !errors.Is(err, context.Canceled) {
		t.Fatalf("comparison cancellation = %v", err)
	}
	if _, remembered := l.near[fresh.ID]; remembered {
		t.Fatal("cancelled comparisons were cached as finished")
	}
	if got, err := l.Update(t.Context(), faces); err != nil || len(got[fresh.ID]) != DefaultParams.Neighbours {
		t.Fatalf("retry after cancellation = %v, %v", got[fresh.ID], err)
	}
}

// world makes identity vectors: each person has a random direction, and
// each face of them is that direction plus noise (same person ~0.7 cosine,
// different people ~0).
type world struct {
	rng    *rand.Rand
	people [][]float32
	nextID int64
}

func newWorld(seed uint64, n int) *world {
	w := &world{rng: rand.New(rand.NewPCG(seed, 1))}
	for range n {
		w.people = append(w.people, w.unit(nil, 0))
	}
	return w
}

func (w *world) unit(base []float32, noise float64) []float32 {
	v := make([]float32, dim)
	var n float64
	for i := range v {
		x := w.rng.NormFloat64()
		if base != nil {
			x = float64(base[i]) + noise*x/math.Sqrt(dim)
		}
		v[i] = float32(x)
		n += x * x
	}
	for i := range v {
		v[i] /= float32(math.Sqrt(n))
	}
	return v
}

// face is a new face of person k in photo asset (0 = a photo of its own).
func (w *world) face(k int, asset int64) store.ClusterFace {
	w.nextID++
	if asset == 0 {
		asset = 1000 + w.nextID
	}
	return store.ClusterFace{ID: w.nextID, AssetID: asset, Sharpness: 50, Vec: w.unit(w.people[k], 0.65)}
}

func (w *world) faces(k, n int) []store.ClusterFace {
	var out []store.ClusterFace
	for range n {
		out = append(out, w.face(k, 0))
	}
	return out
}

func TestFindsEachPersonOnce(t *testing.T) {
	w := newWorld(1, 4)
	var faces []store.ClusterFace
	for k := range 3 {
		faces = append(faces, w.faces(k, 6)...)
	}
	faces = append(faces, w.faces(3, 2)...) // too few to be someone yet
	got := cluster(Input{Faces: faces}, DefaultParams)
	if len(got.NewPeople) != 3 || len(got.Assign) != 0 || len(got.Merges) != 0 {
		t.Fatalf("changes = %+v, want 3 new people", got)
	}
	for i, ids := range got.NewPeople {
		want := faces[i*6 : i*6+6]
		if len(ids) != 6 || !slices.ContainsFunc(want, func(f store.ClusterFace) bool { return f.ID == ids[0] }) {
			t.Errorf("person %d = %v, want the 6 faces of one identity", i, ids)
		}
	}
	if again := cluster(Input{Faces: faces}, DefaultParams); !reflect.DeepEqual(again, got) {
		t.Error("clustering the same faces twice gave different answers")
	}
}

func TestNewFacesJoinTheirPerson(t *testing.T) {
	w := newWorld(2, 2)
	faces := w.faces(0, 5)
	for i := range faces {
		faces[i].Person = 7
	}
	newAli, stranger := w.face(0, 0), w.face(1, 0)
	faces = append(faces, newAli, stranger)
	got := cluster(Input{Faces: faces, People: []store.ClusterPerson{{ID: 7}}}, DefaultParams)
	if got.Assign[newAli.ID] != 7 || len(got.Assign) != 1 || len(got.NewPeople) != 0 {
		t.Errorf("changes = %+v, want only the new face placed with person 7", got)
	}
}

func TestTwoFacesInOnePhotoAreTwoPeople(t *testing.T) {
	w := newWorld(3, 1)
	// Near-identical twins: every photo holds both, so no two of these faces
	// may be one person; alone they are too few for anyone.
	var faces []store.ClusterFace
	for photo := range int64(4) {
		faces = append(faces, w.face(0, photo+1), w.face(0, photo+1))
	}
	got := cluster(Input{Faces: faces}, DefaultParams)
	for _, ids := range got.NewPeople {
		assets := map[int64]bool{}
		for _, id := range ids {
			a := faces[id-1].AssetID
			if assets[a] {
				t.Fatalf("person %v holds two faces of photo %d", ids, a)
			}
			assets[a] = true
		}
	}
}

func TestVideoFacesAreConstrainedPerFrame(t *testing.T) {
	w := newWorld(33, 1)
	faces := w.faces(0, 4)
	for i := range faces {
		faces[i].Person = 7
	}
	a, b := w.face(0, 500), w.face(0, 500)
	a.Frame, b.Frame = 1000, 6000
	got := cluster(Input{Faces: append(faces, a, b), People: []store.ClusterPerson{{ID: 7}}}, DefaultParams)
	if got.Assign[a.ID] != 7 || got.Assign[b.ID] != 7 {
		t.Fatalf("faces at different moments of a video should join their person: %+v", got)
	}
	b.Frame = a.Frame
	got = cluster(Input{Faces: append(faces, a, b), People: []store.ClusterPerson{{ID: 7}}}, DefaultParams)
	if len(got.Assign) != 1 {
		t.Fatalf("two faces in one frame cannot be one person: %+v", got)
	}
}

func TestRejectedFaceStaysOut(t *testing.T) {
	w := newWorld(4, 1)
	faces := w.faces(0, 5)
	for i := range faces {
		faces[i].Person = 7
	}
	f := w.face(0, 0)
	faces = append(faces, f)
	got := cluster(Input{Faces: faces, People: []store.ClusterPerson{{ID: 7}}, Rejections: []store.Rejection{{Face: f.ID, Person: 7}}}, DefaultParams)
	if _, ok := got.Assign[f.ID]; ok {
		t.Errorf("rejected face placed: %+v", got)
	}
}

func TestSplitPeopleMergeUnlessBothConfirmed(t *testing.T) {
	w := newWorld(5, 1)
	as, bs := w.faces(0, 5), w.faces(0, 5) // one identity, found as two people
	for i := range as {
		as[i].Person, bs[i].Person = 3, 8
	}
	faces := slices.Concat(as, bs)

	got := cluster(Input{Faces: faces, People: []store.ClusterPerson{{ID: 3}, {ID: 8}}}, DefaultParams)
	if len(got.Merges) != 1 || got.Merges[0] != (store.Merge{From: 8, Into: 3}) {
		t.Errorf("unnamed split: merges = %+v, want 8 into 3 (the older)", got.Merges)
	}
	got = cluster(Input{Faces: faces, People: []store.ClusterPerson{{ID: 3}, {ID: 8, Confirmed: true}}}, DefaultParams)
	if len(got.Merges) != 1 || got.Merges[0] != (store.Merge{From: 3, Into: 8}) {
		t.Errorf("one named: merges = %+v, want 3 into the named 8", got.Merges)
	}
	got = cluster(Input{Faces: faces, People: []store.ClusterPerson{{ID: 3, Confirmed: true}, {ID: 8, Confirmed: true}}}, DefaultParams)
	if len(got.Merges) != 0 {
		t.Errorf("both named: merges = %+v, want none", got.Merges)
	}
	bs[0].ByUser = true // the user placed a face with 8: confirmed too
	got = cluster(Input{Faces: append(as, bs...), People: []store.ClusterPerson{{ID: 3, Confirmed: true}, {ID: 8}}}, DefaultParams)
	if len(got.Merges) != 0 {
		t.Errorf("named and user-confirmed: merges = %+v, want none", got.Merges)
	}
}

func BenchmarkCluster5000(b *testing.B) {
	w := newWorld(6, 200)
	var faces []store.ClusterFace
	for k := range 200 {
		faces = append(faces, w.faces(k, 25)...)
	}
	b.ResetTimer()
	for range b.N {
		cluster(Input{Faces: faces}, DefaultParams)
	}
}

// Linking in steps, as faces arrive, must give the same links as all at once.
func TestLinkerIsIncremental(t *testing.T) {
	w := newWorld(7, 20)
	var faces []store.ClusterFace
	for k := range 20 {
		faces = append(faces, w.faces(k, 6)...)
	}
	all, _ := NewLinker(DefaultParams).Update(t.Context(), faces)
	l := NewLinker(DefaultParams)
	_, _ = l.Update(t.Context(), faces[:40])
	_, _ = l.Update(t.Context(), faces[:90])
	if got, _ := l.Update(t.Context(), faces); !reflect.DeepEqual(got, all) {
		t.Error("incremental links differ from linking all at once")
	}
	// Placing faces drops them from the links.
	faces[0].Person = 1
	if got, _ := l.Update(t.Context(), faces); got[faces[0].ID] != nil || len(got) != len(all)-1 {
		t.Errorf("placed face still linked (%d links)", len(got))
	}
	// Taken out of that person again: linked as before, listed once.
	faces[0].Person = 0
	if got, _ := l.Update(t.Context(), faces); !reflect.DeepEqual(got, all) {
		t.Error("a face placed and taken out again changed the links")
	}
	// Its photo hidden: it leaves every list, as if it had never been seen.
	want, _ := NewLinker(DefaultParams).Update(t.Context(), faces[1:])
	if got, _ := l.Update(t.Context(), faces[1:]); !reflect.DeepEqual(got, want) {
		t.Error("links to a hidden face remain")
	}
	// A face looked at again gets a new vector in place: its links, and
	// those pointing at it, follow the new one.
	faces = faces[1:]
	faces[0].Vec = faces[len(faces)-1].Vec // now it looks like someone else
	want, _ = NewLinker(DefaultParams).Update(t.Context(), faces)
	if got, _ := l.Update(t.Context(), faces); !reflect.DeepEqual(got, want) {
		t.Error("links kept a face's old vector")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewLinker(DefaultParams).Update(ctx, faces); err == nil {
		t.Error("a cancelled update succeeded")
	}
	if _, err := Cluster(ctx, Input{Faces: faces}, DefaultParams); err == nil {
		t.Error("a cancelled clustering succeeded")
	}
}

// Blurry faces look alike whoever they are: they may join a person but
// never start one.
func TestBlurryFacesDontStartPeople(t *testing.T) {
	w := newWorld(8, 1)
	faces := w.faces(0, 5)
	for i := range faces {
		faces[i].Sharpness = 5
	}
	if got := cluster(Input{Faces: faces}, DefaultParams); len(got.NewPeople) != 0 {
		t.Errorf("blurry faces started a person: %+v", got)
	}
	faces[0].Sharpness, faces[1].Sharpness, faces[2].Sharpness = 40, 40, 40
	if got := cluster(Input{Faces: faces}, DefaultParams); len(got.NewPeople) != 1 || len(got.NewPeople[0]) != 5 {
		t.Errorf("three clear faces and two blurry ones: %+v, want one person of 5", got)
	}
	faces[2].Yaw = 0.8 // in profile: no longer clear
	if got := cluster(Input{Faces: faces}, DefaultParams); len(got.NewPeople) != 0 {
		t.Errorf("two clear faces and a profile started a person: %+v", got)
	}
}

// cluster is Cluster, never cancelled.
func cluster(in Input, p Params) store.ClusterChanges {
	c, err := Cluster(context.Background(), in, p)
	if err != nil {
		panic(err)
	}
	return c
}
