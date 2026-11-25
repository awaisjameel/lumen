package core

import (
	"slices"
	"strings"
	"testing"
)

func TestCleanRelationKeepsOnlyWhatTheUserCanChoose(t *testing.T) {
	cases := []struct {
		relation Relation
		label    string
		want     Relation
		wantText string
		ok       bool
	}{
		{RelationNone, "", RelationNone, "", true},
		{RelationMother, "", RelationMother, "", true},
		{RelationMother, "Mum", "", "", false}, // a label is only for a custom relationship
		{"stepmother", "", "", "", false},      // not one Lumen offers
		{RelationCustom, "  Coach  ", RelationCustom, "Coach", true},
		{RelationCustom, "My  football   coach", RelationCustom, "football coach", true},
		{RelationCustom, "Mum", RelationMother, "", true}, // a built-in word is that relationship
		{RelationCustom, "BEST FRIEND", RelationFriend, "", true},
		{RelationCustom, "parents", RelationParent, "", true}, // the word for several, said of one
		{RelationCustom, "family", "", "", false},             // covers every relative: no one person
		{RelationCustom, "me", "", "", false},                 // a search reads it as the user
		{RelationCustom, "my", "", "", false},
		{RelationCustom, "   ", "", "", false},
		{RelationCustom, strings.Repeat("é", MaxRelationLabelLen), RelationCustom, strings.Repeat("é", MaxRelationLabelLen), true},
		{RelationCustom, strings.Repeat("é", MaxRelationLabelLen+1), "", "", false},
	}
	for _, c := range cases {
		got, text, ok := CleanRelation(c.relation, c.label)
		if got != c.want || text != c.wantText || ok != c.ok {
			t.Errorf("CleanRelation(%q, %q) = %q, %q, %v; want %q, %q, %v", c.relation, c.label, got, text, ok, c.want, c.wantText, c.ok)
		}
	}
}

func TestEveryBuiltInRelationshipCanBeSearchedFor(t *testing.T) {
	for _, r := range Relations {
		targets := RelatedPerson{ID: 1, Relation: r}.Targets()
		if !slices.ContainsFunc(targets, func(t NamedTarget) bool { return !t.Plural }) {
			t.Errorf("%s has no singular phrase", r)
		}
		if got, _, ok := CleanRelation(RelationCustom, string(r)); !ok || got != r {
			t.Errorf("its own name %q as a label = %q, %v", r, got, ok)
		}
	}
}

func TestMentionsUserLooksForWholeWords(t *testing.T) {
	for query, want := range map[string]bool{
		"me at the beach":   true,
		"photos of MYSELF":  true,
		"my mum, laughing":  true,
		"(me)":              true,
		"memories of Rome":  false,
		"mythical creature": false,
		"":                  false,
	} {
		if got := MentionsUser(query); got != want {
			t.Errorf("MentionsUser(%q) = %v, want %v", query, got, want)
		}
	}
}

func TestMergeKeepIsOneOfItsChoices(t *testing.T) {
	for _, k := range []MergeKeep{MergeKeepAsk, MergeKeepInto, MergeKeepFrom} {
		if !k.Valid() {
			t.Errorf("%q is not valid", k)
		}
	}
	if MergeKeep("both").Valid() {
		t.Error(`"both" is valid`)
	}
}
