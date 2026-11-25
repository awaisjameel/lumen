package people

import (
	"reflect"
	"slices"
	"testing"

	"lumen/internal/core"
)

func TestParseQueryFindsNames(t *testing.T) {
	var named []core.NamedTarget
	for _, p := range []core.Person{{ID: 1, Name: "Ali"}, {ID: 2, Name: "Ali Khan"}, {ID: 3, Name: "Sara"}, {ID: 4, Name: "Zoë"}, {ID: 5, Name: "Émile"}, {ID: 6, Name: "İbrahim"}} {
		named = append(named, core.NamedTarget{ID: p.ID, Name: p.Name, Kind: core.BlockPerson})
	}
	cases := []struct {
		query string
		ids   []int64
		rest  string
	}{
		{"Ali at the beach", []int64{1}, "at the beach"},
		{"ali khan birthday cake", []int64{2}, "birthday cake"}, // the longer name wins
		{"Ali and Sara", []int64{1, 3}, ""},                     // only fillers left
		{"Ali Khan and Ali Khan", []int64{2}, ""},
		{"photos of Sara", []int64{3}, ""},
		{"Alice in the snow", nil, "Alice in the snow"}, // whole words only
		{"Zoë's dog", []int64{4}, "'s dog"},
		{"a dog", nil, "a dog"},
		{"photos", nil, "photos"},                        // no name: fillers are the search
		{"ÉMILE at the lake", []int64{5}, "at the lake"}, // any case, any letters
		{"İbrahim ibrahim", []int64{6}, "ibrahim"},       // İ is its own letter; no panic
	}
	for _, c := range cases {
		q := ParseQuery(c.query, named)
		var ids []int64
		for _, p := range q.People {
			ids = append(ids, p.People...)
		}
		slices.Sort(ids)
		if !slices.Equal(ids, c.ids) || q.Rest != c.rest {
			t.Errorf("%q: people %v, rest %q; want %v, %q", c.query, ids, q.Rest, c.ids, c.rest)
		}
	}
}

// related returns the search targets of people as the store reads them.
func related(people ...core.RelatedPerson) []core.NamedTarget {
	var targets []core.NamedTarget
	for _, p := range people {
		targets = append(targets, p.Targets()...)
	}
	return targets
}

func TestParseQueryFindsTheUserAndTheirRelationships(t *testing.T) {
	named := slices.Concat([]core.NamedTarget{{ID: 9, Name: "Jane", Kind: core.BlockPerson}}, related(
		core.RelatedPerson{ID: 1, Me: true},
		core.RelatedPerson{ID: 2, Relation: core.RelationMother},
		core.RelatedPerson{ID: 3, Relation: core.RelationFriend},
		core.RelatedPerson{ID: 4, Relation: core.RelationFriend},
		core.RelatedPerson{ID: 5, Relation: core.RelationCustom, Label: "Coach"},
		core.RelatedPerson{ID: 6, Relation: core.RelationFather},
	))
	cases := []struct {
		query string
		want  []core.SearchPerson
		rest  string
	}{
		{"me at the beach", []core.SearchPerson{{Text: "me", People: []int64{1}}}, "at the beach"},
		{"photos of myself", []core.SearchPerson{{Text: "myself", People: []int64{1}}}, ""},
		{"my mum at the beach", []core.SearchPerson{{Text: "my mum", People: []int64{2}}}, "at the beach"},
		{"My Mom", []core.SearchPerson{{Text: "My Mom", People: []int64{2}}}, ""}, // any word for her, in any case
		{"me and my mum", []core.SearchPerson{{Text: "me", People: []int64{1}}, {Text: "my mum", People: []int64{2}}}, ""},
		{"my friends at a party", []core.SearchPerson{{Text: "my friends", People: []int64{3, 4}}}, "at a party"},
		// One friend is expected: two are a choice for the user to make.
		{"my friend", []core.SearchPerson{{Text: "my friend", People: []int64{3, 4}, Ambiguous: true}}, ""},
		{"my parents", []core.SearchPerson{{Text: "my parents", People: []int64{2, 6}}}, ""},
		{"my coach running", []core.SearchPerson{{Text: "my coach", People: []int64{5}}}, "running"},
		{"my coaches", []core.SearchPerson{{Text: "my coaches", People: []int64{5}}}, ""},
		{"mum at the beach", nil, "mum at the beach"}, // without "my", a description
		{"Jane and my mum", []core.SearchPerson{{Text: "Jane", People: []int64{9}}, {Text: "my mum", People: []int64{2}}}, ""},
	}
	for _, c := range cases {
		q := ParseQuery(c.query, named)
		if c.want == nil {
			c.want = []core.SearchPerson{}
		}
		if !reflect.DeepEqual(q.People, c.want) || q.Rest != c.rest || q.Blocked || len(q.Unassigned) != 0 {
			t.Errorf("%q: %+v, rest %q, unassigned %q; want %+v, %q", c.query, q.People, q.Rest, q.Unassigned, c.want, c.rest)
		}
	}
}

func TestParseQueryNeverChoosesBetweenANameAndARelationship(t *testing.T) {
	// Someone named "My Mum" and someone else set as Mum: both are offered.
	named := slices.Concat([]core.NamedTarget{{ID: 7, Name: "My Mum", Kind: core.BlockPerson}}, related(core.RelatedPerson{ID: 2, Relation: core.RelationMother}))
	q := ParseQuery("my mum", named)
	if want := []core.SearchPerson{{Text: "my mum", People: []int64{2, 7}, Ambiguous: true}}; !reflect.DeepEqual(q.People, want) {
		t.Errorf("people = %+v, want %+v", q.People, want)
	}
	// The same person by both is no choice at all.
	named = slices.Concat([]core.NamedTarget{{ID: 2, Name: "My Mum", Kind: core.BlockPerson}}, related(core.RelatedPerson{ID: 2, Relation: core.RelationMother}))
	if q := ParseQuery("my mum", named); len(q.People) != 1 || q.People[0].Ambiguous {
		t.Errorf("people = %+v, want her alone", q.People)
	}
}

func TestParseQuerySaysWhichPhrasesNameNoOneYet(t *testing.T) {
	q := ParseQuery("me and my mum at the beach", related(core.RelatedPerson{ID: 3, Relation: core.RelationFriend}))
	if !slices.Equal(q.Unassigned, []string{"me", "my mum"}) || len(q.People) != 0 || q.Rest != "me and my mum at the beach" {
		t.Errorf("unassigned %q, people %+v, rest %q; want both, nobody and the whole query searched", q.Unassigned, q.People, q.Rest)
	}
	if q := ParseQuery("a sign saying my way", nil); len(q.Unassigned) != 0 {
		t.Errorf("unassigned = %q for a phrase that is no relationship", q.Unassigned)
	}
}

func TestParseQueryKeepsBlockedPeopleOut(t *testing.T) {
	named := related(
		core.RelatedPerson{ID: 1, Me: true, Blocked: true},
		core.RelatedPerson{ID: 2, Relation: core.RelationFriend, Blocked: true},
		core.RelatedPerson{ID: 3, Relation: core.RelationFriend},
		core.RelatedPerson{ID: 4, Relation: core.RelationMother, Blocked: true},
	)
	for _, query := range []string{"me", "photos of my mum", "my mum and my friends"} {
		if q := ParseQuery(query, named); !q.Blocked || len(q.People) != 0 || len(q.Unassigned) != 0 {
			t.Errorf("%q = %+v, want blocked and naming no one", query, q)
		}
	}
	// A blocked friend is left out of "my friends"; the other is searched for,
	// and nothing says there was a second one.
	q := ParseQuery("my friend", named)
	if want := []core.SearchPerson{{Text: "my friend", People: []int64{3}}}; q.Blocked || !reflect.DeepEqual(q.People, want) {
		t.Errorf("my friend = %+v, want only the shown friend, unambiguous", q)
	}
	// A blocked name blocks the search even beside a relationship, or inside one.
	named = append(named, core.NamedTarget{ID: 8, Name: "Sam", Kind: core.BlockPerson, Blocked: true},
		core.NamedTarget{ID: 9, Name: "Family", Kind: core.BlockTag, Blocked: true})
	for _, query := range []string{"Sam and my friend", "my family at the beach"} {
		if q := ParseQuery(query, named); !q.Blocked {
			t.Errorf("%q: a blocked name did not block the search", query)
		}
	}
}

func TestOddOnesFirst(t *testing.T) {
	faces := []core.Face{{ID: 1}, {ID: 2}, {ID: 3, ByUser: true}, {ID: 4}, {ID: 5}}
	vecs := [][]float32{{1, 0}, {1, 0}, {0, 1}, {0, 1}, {1, 0}} // 4 is unlike most; 3 too, but settled
	got := OddOnesFirst(faces, vecs)
	if got[0].ID != 4 || got[4].ID != 3 {
		t.Errorf("order = %v", got)
	}
}
