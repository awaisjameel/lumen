package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"lumen/internal/core"
)

// relatedFixture names the faces of peopleFixture: Ali (a's left face and
// b's), Sara (a's right face) and Omar (c's).
func relatedFixture(t *testing.T) (st *Store, ali, sara, omar int64) {
	t.Helper()
	st, ctx, a, b, c := peopleFixture(t)
	fa, fb, fc := facesOf(t, st, a), facesOf(t, st, b), facesOf(t, st, c)
	var err error
	ali, err = st.NameFaces(ctx, []int64{fa[0].ID, fb[0].ID}, "Ali")
	check(t, err)
	sara, err = st.NameFaces(ctx, []int64{fa[1].ID}, "Sara")
	check(t, err)
	omar, err = st.NameFaces(ctx, []int64{fc[0].ID}, "Omar")
	check(t, err)
	return st, ali, sara, omar
}

// person reads one person, failing the test if they can't be read.
func person(t *testing.T, st *Store, id int64) core.Person {
	t.Helper()
	p, err := st.Person(t.Context(), id)
	check(t, err)
	return p
}

func TestOnlyOnePersonIsMeAndReplacingThemIsConfirmed(t *testing.T) {
	st, ali, sara, _ := relatedFixture(t)
	ctx := t.Context()
	check(t, st.SetMe(ctx, ali, false))
	if !person(t, st, ali).Me {
		t.Fatal("Ali is not Me")
	}
	if err := st.SetMe(ctx, sara, false); !errors.Is(err, ErrRelationConflict) {
		t.Fatalf("replacing Me unasked = %v, want ErrRelationConflict", err)
	}
	if person(t, st, sara).Me || !person(t, st, ali).Me {
		t.Fatal("a refused change changed Me")
	}
	check(t, st.SetMe(ctx, sara, true))
	if !person(t, st, sara).Me || person(t, st, ali).Me {
		t.Fatal("Me did not move to Sara")
	}
	check(t, st.SetMe(ctx, sara, false)) // already Me: nothing to confirm
	if err := st.SetMe(ctx, 9999, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("Me of no one = %v, want ErrNotFound", err)
	}
	// The user comes first in People.
	people, err := st.People(ctx)
	check(t, err)
	if len(people) == 0 || people[0].ID != sara {
		t.Errorf("people = %+v, want Sara (Me) first", people)
	}
}

func TestMeHasNoRelationship(t *testing.T) {
	st, ali, _, _ := relatedFixture(t)
	ctx := t.Context()
	_, _, err := st.SetRelation(ctx, ali, core.RelationBrother, "")
	check(t, err)
	if err := st.SetMe(ctx, ali, false); !errors.Is(err, ErrRelationConflict) {
		t.Fatalf("Me with a relationship unasked = %v, want ErrRelationConflict", err)
	}
	check(t, st.SetMe(ctx, ali, true))
	if p := person(t, st, ali); !p.Me || p.Relation != core.RelationNone {
		t.Fatalf("Ali = %+v, want Me without a relationship", p)
	}
	if _, _, err := st.SetRelation(ctx, ali, core.RelationFriend, ""); !errors.Is(err, ErrRelationConflict) {
		t.Errorf("a relationship for Me = %v, want ErrRelationConflict", err)
	}
	if _, _, err := st.SetRelation(ctx, ali, core.RelationNone, ""); err != nil {
		t.Errorf("clearing Me's (absent) relationship = %v", err)
	}
}

func TestRemovingMeKeepsEveryRelationship(t *testing.T) {
	st, ali, sara, omar := relatedFixture(t)
	ctx := t.Context()
	check(t, st.SetMe(ctx, ali, false))
	_, _, err := st.SetRelation(ctx, sara, core.RelationSister, "")
	check(t, err)
	check(t, st.RemoveMe(ctx, omar)) // not Me: nothing to do
	if !person(t, st, ali).Me {
		t.Fatal("removing Me from someone else changed Me")
	}
	check(t, st.RemoveMe(ctx, ali))
	if person(t, st, ali).Me || person(t, st, sara).Relation != core.RelationSister {
		t.Fatal("removing Me lost it or the relationships")
	}
}

func TestRelationshipsAreKeptCleanAndApartFromNames(t *testing.T) {
	st, ali, sara, _ := relatedFixture(t)
	ctx := t.Context()
	got, label, err := st.SetRelation(ctx, ali, core.RelationCustom, " my Coach ")
	check(t, err)
	if got != core.RelationCustom || label != "Coach" {
		t.Fatalf("custom = %q %q", got, label)
	}
	if p := person(t, st, ali); p.Name != "Ali" || p.Relation != core.RelationCustom || p.RelationLabel != "Coach" {
		t.Fatalf("Ali = %+v", p)
	}
	// A label that is a built-in word is that relationship.
	if got, label, err := st.SetRelation(ctx, sara, core.RelationCustom, "mum"); err != nil || got != core.RelationMother || label != "" {
		t.Fatalf("mum = %q %q %v, want RelationMother", got, label, err)
	}
	// Two people may share a relationship.
	_, _, err = st.SetRelation(ctx, ali, core.RelationMother, "")
	check(t, err)
	for _, bad := range []struct {
		relation core.Relation
		label    string
	}{{"aunt-in-law", ""}, {core.RelationCustom, ""}, {core.RelationCustom, "family"}, {core.RelationFriend, "best"}} {
		if _, _, err := st.SetRelation(ctx, ali, bad.relation, bad.label); !errors.Is(err, ErrInvalidRelation) {
			t.Errorf("%q %q = %v, want ErrInvalidRelation", bad.relation, bad.label, err)
		}
	}
	// Renaming or unnaming leaves the relationship; so does removing it the name.
	check(t, st.NamePerson(ctx, ali, "Alia"))
	check(t, st.NamePerson(ctx, ali, ""))
	if p := person(t, st, ali); p.Name != "" || p.Relation != core.RelationMother {
		t.Fatalf("after unnaming, Ali = %+v", p)
	}
	_, _, err = st.SetRelation(ctx, ali, core.RelationNone, "")
	check(t, err)
	if p := person(t, st, ali); p.Relation != core.RelationNone || p.RelationLabel != "" {
		t.Fatalf("after removing, Ali = %+v", p)
	}
}

func TestBlockedPeopleCantBeMeOrRelated(t *testing.T) {
	st, ali, sara, _ := relatedFixture(t)
	ctx := t.Context()
	check(t, st.SetMe(ctx, sara, false))
	_, err := st.SetBlock(ctx, core.BlockPerson, ali, "", core.BlockLabel)
	check(t, err)
	if err := st.SetMe(ctx, ali, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("a blocked Me = %v, want ErrNotFound", err)
	}
	if _, _, err := st.SetRelation(ctx, ali, core.RelationFriend, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("a blocked person's relationship = %v, want ErrNotFound", err)
	}
	if err := st.RemoveMe(ctx, ali); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing a blocked person's Me = %v, want ErrNotFound", err)
	}
	// A hidden Me is replaced without asking: asking would name them.
	_, err = st.SetBlock(ctx, core.BlockPerson, sara, "", core.BlockLabel)
	check(t, err)
	_, _, omar := relatedFixtureIDs(t, st)
	check(t, st.SetMe(ctx, omar, false))
}

// relatedFixtureIDs finds relatedFixture's people by name, blocked or not.
func relatedFixtureIDs(t *testing.T, st *Store) (ali, sara, omar int64) {
	t.Helper()
	for _, name := range []string{"Ali", "Sara", "Omar"} {
		var id int64
		check(t, st.r.QueryRowContext(t.Context(), "SELECT id FROM people WHERE name = ?", name).Scan(&id))
		switch name {
		case "Ali":
			ali = id
		case "Sara":
			sara = id
		default:
			omar = id
		}
	}
	return ali, sara, omar
}

func TestMergingPeopleKeepsMeAndAsksAboutDifferentRelationships(t *testing.T) {
	st, ali, sara, omar := relatedFixture(t)
	ctx := t.Context()
	_, _, err := st.SetRelation(ctx, ali, core.RelationBrother, "")
	check(t, err)
	_, _, err = st.SetRelation(ctx, sara, core.RelationFriend, "")
	check(t, err)
	if err := st.MergePeople(ctx, sara, ali, core.MergeKeepAsk); !errors.Is(err, ErrRelationConflict) {
		t.Fatalf("merging Brother and Friend unasked = %v, want ErrRelationConflict", err)
	}
	if _, err := st.Person(ctx, sara); err != nil {
		t.Fatal("a refused merge merged")
	}
	if err := st.MergePeople(ctx, sara, ali, "both"); !errors.Is(err, ErrInvalidRelation) {
		t.Errorf("an unknown choice = %v", err)
	}
	check(t, st.MergePeople(ctx, sara, ali, core.MergeKeepFrom))
	if p := person(t, st, ali); p.Relation != core.RelationFriend {
		t.Fatalf("after keeping Sara's, Ali = %+v", p)
	}
	// Me moves with a merge, and Me's side wins over a relationship.
	check(t, st.SetMe(ctx, omar, false))
	if err := st.MergePeople(ctx, omar, ali, core.MergeKeepAsk); !errors.Is(err, ErrRelationConflict) {
		t.Fatalf("merging Me into a friend unasked = %v", err)
	}
	check(t, st.MergePeople(ctx, omar, ali, core.MergeKeepFrom))
	if p := person(t, st, ali); !p.Me || p.Relation != core.RelationNone {
		t.Fatalf("after merging Me in, Ali = %+v", p)
	}
}

func TestMergingAgreeingPeopleNeedsNoQuestion(t *testing.T) {
	st, ali, sara, omar := relatedFixture(t)
	ctx := t.Context()
	_, _, err := st.SetRelation(ctx, sara, core.RelationCustom, "Coach")
	check(t, err)
	_, _, err = st.SetRelation(ctx, omar, core.RelationCustom, "coach")
	check(t, err)
	check(t, st.MergePeople(ctx, omar, sara, core.MergeKeepAsk)) // the same label, whatever its case
	check(t, st.MergePeople(ctx, sara, ali, core.MergeKeepAsk))  // Ali had none: he takes Sara's
	if p := person(t, st, ali); p.Relation != core.RelationCustom || p.RelationLabel != "Coach" {
		t.Fatalf("Ali = %+v, want Sara's label", p)
	}
}

func TestClusteringNeverMergesAwayARelatedPerson(t *testing.T) {
	st, ctx, a, b, _ := peopleFixture(t)
	fa, fb := facesOf(t, st, a), facesOf(t, st, b)
	check(t, st.ApplyClusters(ctx, ClusterChanges{NewPeople: [][]int64{{fa[0].ID}, {fb[0].ID}}}))
	people, err := st.People(ctx)
	check(t, err)
	if len(people) != 2 {
		t.Fatalf("people = %+v", people)
	}
	mum, other := people[0].ID, people[1].ID
	_, _, err = st.SetRelation(ctx, mum, core.RelationMother, "")
	check(t, err)
	check(t, st.SetMe(ctx, other, false))
	// Both are unnamed, but the user said who they are: neither is merged away.
	check(t, st.ApplyClusters(ctx, ClusterChanges{Merges: []Merge{{From: mum, Into: other}, {From: other, Into: mum}}}))
	if person(t, st, mum).Relation != core.RelationMother || !person(t, st, other).Me {
		t.Fatal("clustering merged away a person the user had labelled")
	}
	_, cluster, _, err := st.ClusterData(ctx, "f1")
	check(t, err)
	for _, p := range cluster {
		if (p.ID == mum || p.ID == other) && !p.Confirmed {
			t.Errorf("person %d is not confirmed to clustering", p.ID)
		}
	}
	// A related person outlives their faces, as a named one does.
	check(t, st.RejectFaces(ctx, mum, []int64{fa[0].ID}))
	_, err = st.w.ExecContext(ctx, "DELETE FROM face_rejections")
	check(t, err)
	check(t, st.RemoveMe(ctx, other)) // which drops the empty people
	if p := person(t, st, mum); p.Relation != core.RelationMother {
		t.Errorf("a faceless Mum = %+v", p)
	}
}

func TestPeopleAssetsTakesAnyoneOfAGroup(t *testing.T) {
	st, ali, sara, omar := relatedFixture(t)
	ctx := t.Context()
	all := func(groups ...[]int64) []int64 {
		t.Helper()
		ids, err := st.PeopleAssets(ctx, groups)
		check(t, err)
		slices.Sort(ids)
		return ids
	}
	// a shows Ali and Sara, b Ali, c Omar.
	if got := all([]int64{sara, omar}); len(got) != 2 {
		t.Errorf("Sara or Omar = %v, want a and c", got)
	}
	if got := all([]int64{ali}, []int64{sara, omar}); len(got) != 1 {
		t.Errorf("Ali with Sara or Omar = %v, want only a", got)
	}
	if got := all([]int64{omar}, []int64{sara}); len(got) != 0 {
		t.Errorf("Omar with Sara = %v, want none", got)
	}
}

func TestSearchNamesFindsTheUserAndRelationshipsOnlyWhenAsked(t *testing.T) {
	st, ali, sara, omar := relatedFixture(t)
	ctx := t.Context()
	check(t, st.SetMe(ctx, ali, false))
	_, _, err := st.SetRelation(ctx, sara, core.RelationMother, "")
	check(t, err)
	_, _, err = st.SetRelation(ctx, omar, core.RelationFriend, "")
	check(t, err)
	_, err = st.SetBlock(ctx, core.BlockPerson, omar, "", core.BlockLabel)
	check(t, err)
	related := func(query string) map[string]core.NamedTarget {
		t.Helper()
		targets, err := st.SearchNames(ctx, query)
		check(t, err)
		out := map[string]core.NamedTarget{}
		for _, target := range targets {
			if target.Related {
				out[target.Name] = target
			}
		}
		return out
	}
	if got := related("Sara at the beach"); len(got) != 0 {
		t.Errorf("a query without me or my looked up %v", got)
	}
	got := related("me and my mum")
	if got["me"].ID != ali || got["my mum"].ID != sara || got["my mom"].ID != sara {
		t.Errorf("targets = %+v", got)
	}
	if friend := got["my friend"]; friend.ID != omar || !friend.Blocked {
		t.Errorf("the blocked friend = %+v, want kept, as blocked", friend)
	}
}

func TestRelationshipMigrationKeepsEveryPersonAndCorrection(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "library.db")
	db, err := sql.Open(driverName, path+"?_foreign_keys=1")
	check(t, err)
	t.Cleanup(func() { _ = db.Close() })
	names, err := migrationFS.ReadDir("migrations")
	check(t, err)
	for _, name := range names {
		if name.Name() >= "0041" {
			break
		}
		body, err := migrationFS.ReadFile("migrations/" + name.Name())
		check(t, err)
		check(t, errOf(db.ExecContext(ctx, string(body))))
	}
	check(t, errOf(db.ExecContext(ctx, `INSERT INTO people(id, name, created_at) VALUES (7, 'Ali', 1), (8, NULL, 1);
 INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at) VALUES (23, 'existing-asset', 'photo', '.jpg', 1, 1);
 INSERT INTO faces(id, asset_id, x, y, w, h, score, sharpness, yaw, model, vec, person_id, assigned_by)
   VALUES (5, 23, 0.1, 0.1, 0.2, 0.2, 0.9, 50, 0, 'f', x'0000803f', 7, 'user');
 INSERT INTO face_rejections(face_id, person_id) VALUES (5, 8);
 PRAGMA user_version = 40`)))
	check(t, db.Close())
	st, err := Open(path)
	check(t, err)
	t.Cleanup(func() { check(t, st.Close()) })
	if p := person(t, st, 7); p.Name != "Ali" || p.Me || p.Relation != core.RelationNone {
		t.Fatalf("migrated Ali = %+v", p)
	}
	var assigned string
	var rejected int
	check(t, st.r.QueryRowContext(ctx, `SELECT assigned_by, (SELECT count(*) FROM face_rejections WHERE person_id = 8)
		FROM faces WHERE id = 5`).Scan(&assigned, &rejected))
	if assigned != "user" || rejected != 1 {
		t.Fatalf("corrections after the migration: %q, %d rejections", assigned, rejected)
	}
	// The new columns work at once, and their constraints hold.
	check(t, st.SetMe(ctx, 7, false))
	if _, err := st.w.ExecContext(ctx, "UPDATE people SET is_me = 1 WHERE id = 8"); err == nil {
		t.Error("a second Me was accepted")
	}
}
