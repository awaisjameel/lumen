package store

import (
	"context"
	"errors"
	"testing"

	"lumen/internal/core"
)

// peopleFixture is a library of three photos with faces: a has two, b and c
// one each.
func peopleFixture(t *testing.T) (st *Store, ctx context.Context, a, b, c int64) {
	t.Helper()
	st, ctx = openTest(t), context.Background()
	src, _ := st.AddSource(ctx, `C:\Photos`)
	check(t, errOf(st.Ingest(ctx, []IngestItem{item(src.ID, `C:\Photos\a.jpg`, "a"), item(src.ID, `C:\Photos\b.jpg`, "b"), item(src.ID, `C:\Photos\c.jpg`, "c")})))
	jobs, _ := st.PendingJobs(ctx, core.StageMetadata, nil, 10)
	a, b, c = jobs[0].AssetID, jobs[1].AssetID, jobs[2].AssetID
	err := st.SaveFaces(ctx, "f1", "f1", []FaceResult{
		{AssetID: a, Faces: []DetectedFace{face(0.1, 1), face(0.6, 2)}},
		{AssetID: b, Faces: []DetectedFace{face(0.3, 1)}},
		{AssetID: c, Faces: []DetectedFace{face(0.3, 3)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Covers need thumbnails.
	if err := st.SaveThumbnails(ctx, "t1", []ThumbResult{{AssetID: a}, {AssetID: b}, {AssetID: c}}); err != nil {
		t.Fatal(err)
	}
	return st, ctx, a, b, c
}

// face is a 0.2-wide face at x whose vector points along axis k.
func face(x float32, k int) DetectedFace {
	vec := make([]float32, 4)
	vec[k] = 1
	return DetectedFace{X: x, Y: 0.2, W: 0.2, H: 0.3, Score: 0.9, Sharpness: 50, Vec: vec}
}

func facesOf(t *testing.T, st *Store, asset int64) []core.Face {
	t.Helper()
	fs, err := st.AssetFaces(context.Background(), asset)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

func TestPersonDetailIncludesChosenCover(t *testing.T) {
	st, ctx, a, _, _ := peopleFixture(t)
	f := facesOf(t, st, a)[0].ID
	id, err := st.NameFaces(ctx, []int64{f}, "Ali")
	check(t, err)
	check(t, st.SetPersonCover(ctx, id, f))
	p, err := st.Person(ctx, id)
	check(t, err)
	if p.Cover.ID != f {
		t.Fatalf("person cover = %d, want %d", p.Cover.ID, f)
	}
}

func TestRejectingLastFaceRetainsIdentityAndRejection(t *testing.T) {
	st, ctx, a, _, _ := peopleFixture(t)
	f := facesOf(t, st, a)[0].ID
	id, err := st.NameFaces(ctx, []int64{f}, "")
	check(t, err)
	check(t, st.RejectFaces(ctx, id, []int64{f}))
	check(t, st.ApplyClusters(ctx, ClusterChanges{Assign: map[int64]int64{f: id}}))
	_, people, rejections, err := st.ClusterData(ctx, "f1")
	check(t, err)
	if len(people) != 1 || len(rejections) != 1 || rejections[0].Person != id {
		t.Fatalf("lost rejected identity: people=%+v rejections=%+v", people, rejections)
	}
	if got := facesOf(t, st, a)[0].Person; got != 0 {
		t.Fatalf("rejected face reassigned to %d", got)
	}
}

func TestFacesKeepTheirPersonWhenSeenAgain(t *testing.T) {
	st, ctx, a, _, _ := peopleFixture(t)
	before := facesOf(t, st, a)
	if len(before) != 2 || before[0].X != 0.1 {
		t.Fatalf("faces = %+v", before)
	}
	ali, err := st.NameFaces(ctx, []int64{before[0].ID}, "Ali")
	if err != nil {
		t.Fatal(err)
	}
	// A new model finds the left face slightly moved and loses the right one.
	moved := face(0.12, 1)
	if err := st.SaveFaces(ctx, "f2", "f2", []FaceResult{{AssetID: a, Faces: []DetectedFace{moved}}}); err != nil {
		t.Fatal(err)
	}
	after := facesOf(t, st, a)
	if len(after) != 1 || after[0].ID != before[0].ID || after[0].Person != ali || !after[0].ByUser || after[0].X != 0.12 {
		t.Fatalf("after re-run: %+v", after)
	}
	faces, _, _, _ := st.ClusterData(ctx, "f1")
	for _, f := range faces {
		if f.ID == after[0].ID {
			t.Error("a face from the new model was offered as the old model's")
		}
	}
}

func TestClusteringNeverOverridesTheUser(t *testing.T) {
	st, ctx, a, b, c := peopleFixture(t)
	fa, fb, fc := facesOf(t, st, a), facesOf(t, st, b), facesOf(t, st, c)

	// The AI groups a's left face with b's into a new person.
	if err := st.ApplyClusters(ctx, ClusterChanges{NewPeople: [][]int64{{fa[0].ID, fb[0].ID}}}); err != nil {
		t.Fatal(err)
	}
	people, err := st.People(ctx)
	if err != nil || len(people) != 1 || people[0].Count != 2 || people[0].Name != "" || people[0].Cover.ID == 0 {
		t.Fatalf("people = %+v (%v)", people, err)
	}
	p := people[0].ID

	// Meanwhile the user said b's face is someone else; the stale run's
	// assignment of it must not stick, and named people are never merged away.
	if err := st.RejectFaces(ctx, p, []int64{fb[0].ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.NamePerson(ctx, p, "Sara"); err != nil {
		t.Fatal(err)
	}
	other, err := st.NameFaces(ctx, []int64{fc[0].ID}, "") // someone new
	if err != nil || other == p {
		t.Fatalf("new person = %d (%v)", other, err)
	}
	err = st.ApplyClusters(ctx, ClusterChanges{
		Assign: map[int64]int64{fb[0].ID: other, fa[0].ID: other}, // fa is already Sara's
		Merges: []Merge{{From: p, Into: other}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := facesOf(t, st, a)[0]; got.Person != p || got.Name != "Sara" {
		t.Errorf("Sara's face moved: %+v", got)
	}
	if got := facesOf(t, st, b)[0]; got.Person != other || got.ByUser {
		t.Errorf("unassigned face should join by the AI: %+v", got)
	}
	_, _, rejections, _ := st.ClusterData(ctx, "f1")
	if len(rejections) != 1 || rejections[0] != (Rejection{Face: fb[0].ID, Person: p}) {
		t.Errorf("rejections = %+v", rejections)
	}
}

func TestPeopleEdits(t *testing.T) {
	st, ctx, a, b, c := peopleFixture(t)
	fa, fb, fc := facesOf(t, st, a), facesOf(t, st, b), facesOf(t, st, c)
	check(t, st.ApplyClusters(ctx, ClusterChanges{NewPeople: [][]int64{{fa[0].ID}, {fb[0].ID}, {fa[1].ID, fc[0].ID}}}))
	people, _ := st.People(ctx)
	if len(people) != 3 {
		t.Fatalf("people = %+v", people)
	}
	ids := map[int64]int64{} // face -> person
	for _, f := range append(append(facesOf(t, st, a), facesOf(t, st, b)...), facesOf(t, st, c)...) {
		ids[f.ID] = f.Person
	}
	pa, pb, pc := ids[fa[0].ID], ids[fb[0].ID], ids[fc[0].ID]

	// Names are unique, whatever the case.
	if err := st.NamePerson(ctx, pa, "Ali"); err != nil {
		t.Fatal(err)
	}
	if err := st.NamePerson(ctx, pb, "ali"); !errors.Is(err, ErrPersonExists) {
		t.Fatalf("duplicate name: %v", err)
	}
	// Merging an unnamed person into Ali: Ali keeps the name and gains a
	// photo, and the picture chosen for the other (Ali has none).
	if err := st.SetPersonCover(ctx, pb, fb[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := st.MergePeople(ctx, pb, pa); err != nil {
		t.Fatal(err)
	}
	ali, err := st.Person(ctx, pa)
	if err != nil || ali.Name != "Ali" || ali.Count != 2 || ali.Cover.ID != fb[0].ID {
		t.Fatalf("ali = %+v (%v)", ali, err)
	}
	if _, err := st.Person(ctx, pb); !errors.Is(err, ErrNotFound) {
		t.Errorf("merged person still there: %v", err)
	}

	// The grid shows a person's photos; search can ask for photos with both.
	days, _ := st.DayItems(ctx, "1970-01-01", "2100-01-01", core.View{Person: pa}, 0.02)
	if len(days) != 1 || len(days[0].Items) != 2 {
		t.Errorf("Ali's grid = %+v", days)
	}
	both, _ := st.PeopleAssets(ctx, []int64{pa, pc})
	if len(both) != 1 || both[0] != a {
		t.Errorf("photos with Ali and the other = %v, want only a", both)
	}
	named, _ := st.NamedPeople(ctx)
	if len(named) != 1 || named[0].Name != "Ali" {
		t.Errorf("named = %+v", named)
	}
	if found, _ := st.SearchPeople(ctx, "LI", 5); len(found) != 1 || found[0].ID != pa || found[0].Cover.ID == 0 {
		t.Errorf("search people = %+v", found)
	}

	// Moving the other person's faces to Ali (by name, in any case) empties
	// and removes them.
	if id, err := st.NameFaces(ctx, []int64{fa[1].ID, fc[0].ID}, " ALI "); err != nil || id != pa {
		t.Fatalf("named faces went to %d (%v), want Ali (%d)", id, err, pa)
	}
	if _, err := st.Person(ctx, pc); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty unnamed person kept: %v", err)
	}
	// A named person with no faces left stays.
	faces, _, _ := st.PersonFaces(ctx, pa)
	var all []int64
	for _, f := range faces {
		all = append(all, f.ID)
	}
	if err := st.RejectFaces(ctx, pa, all); err != nil {
		t.Fatal(err)
	}
	if p, err := st.Person(ctx, pa); err != nil || p.Count != 0 {
		t.Errorf("named person after rejecting every face = %+v (%v)", p, err)
	}
	if _, err := st.SetBlock(ctx, core.BlockPerson, 9999, "", core.BlockPhotos); !errors.Is(err, ErrNotFound) {
		t.Errorf("blocking nobody: %v", err)
	}
}
