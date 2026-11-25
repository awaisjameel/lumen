package core

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Relation is what a person in the photos is to the user: their mum, a
// friend. It is always relative to the user, whether or not one of the
// people is marked as them (Person.Me), and it is only ever what the user
// chose: Lumen never guesses one from the photos.
type Relation string

// The relationships a person can have. RelationCustom carries the user's own
// word in Person.RelationLabel. The others are built in: the UI names them
// in its own words, and a search finds them by several (MatchRelations).
const (
	RelationNone        Relation = ""
	RelationPartner     Relation = "partner"
	RelationMother      Relation = "mother"
	RelationFather      Relation = "father"
	RelationParent      Relation = "parent"
	RelationDaughter    Relation = "daughter"
	RelationSon         Relation = "son"
	RelationChild       Relation = "child"
	RelationSister      Relation = "sister"
	RelationBrother     Relation = "brother"
	RelationSibling     Relation = "sibling"
	RelationGrandmother Relation = "grandmother"
	RelationGrandfather Relation = "grandfather"
	RelationGrandparent Relation = "grandparent"
	RelationGrandchild  Relation = "grandchild"
	RelationAunt        Relation = "aunt"
	RelationUncle       Relation = "uncle"
	RelationCousin      Relation = "cousin"
	RelationNiece       Relation = "niece"
	RelationNephew      Relation = "nephew"
	RelationRelative    Relation = "relative"
	RelationFriend      Relation = "friend"
	RelationColleague   Relation = "colleague"
	RelationNeighbour   Relation = "neighbour"
	RelationCustom      Relation = "custom"
)

// Relations lists the built-in relationships in the order the UI offers them.
var Relations = []Relation{
	RelationPartner, RelationMother, RelationFather, RelationParent,
	RelationDaughter, RelationSon, RelationChild,
	RelationSister, RelationBrother, RelationSibling,
	RelationGrandmother, RelationGrandfather, RelationGrandparent, RelationGrandchild,
	RelationAunt, RelationUncle, RelationCousin, RelationNiece, RelationNephew, RelationRelative,
	RelationFriend, RelationColleague, RelationNeighbour,
}

// MaxRelationLabelLen is the longest relationship label (in characters) the
// user may write. frontend/src/bridge mirrors it.
const MaxRelationLabelLen = 40

// MergeKeep says which relationship a merged person keeps when the two
// people merged had different ones, or one was the user and the other had a
// relationship. Whoever is the user stays so, without a relationship.
type MergeKeep string

// The choices of MergeKeep.
const (
	MergeKeepAsk  MergeKeep = ""     // they must agree; else the merge is refused, so the user is asked
	MergeKeepInto MergeKeep = "into" // the relationship of the person merged into
	MergeKeepFrom MergeKeep = "from" // the relationship of the person merged away
)

// Valid reports whether k is one of the choices.
func (k MergeKeep) Valid() bool {
	return k == MergeKeepAsk || k == MergeKeepInto || k == MergeKeepFrom
}

// relationWord is a word or phrase for relationships, as a search says it
// after "my" ("my mum", "my parents").
type relationWord struct {
	Text string
	// Means is every relationship the word covers: "parent" covers mothers
	// and fathers too, so "my parents" finds whichever the user chose.
	Means []Relation
	// Is is the one relationship a label of exactly this word is (a custom
	// "Mum" is RelationMother); "" for words that cover several on purpose
	// ("family"), which can't be a label.
	Is Relation
	// Plural words expect several people; a singular word that finds
	// several asks the user which one they meant.
	Plural bool
}

var (
	parents      = []Relation{RelationMother, RelationFather, RelationParent}
	children     = []Relation{RelationDaughter, RelationSon, RelationChild}
	siblings     = []Relation{RelationSister, RelationBrother, RelationSibling}
	grandparents = []Relation{RelationGrandmother, RelationGrandfather, RelationGrandparent}
	family       = slices.Concat([]Relation{RelationPartner}, parents, children, siblings, grandparents, []Relation{
		RelationGrandchild, RelationAunt, RelationUncle, RelationCousin, RelationNiece, RelationNephew, RelationRelative,
	})
)

// relationWords is English, as search's own vocabulary is: each built-in
// relationship's words, singular then plural, and the words that cover a
// group of them. Neutral words cover their gendered forms ("my sibling"
// finds a sister), so a search never misses whoever the user chose.
var relationWords = slices.Concat(
	words(RelationPartner, []string{"partner", "spouse", "husband", "wife", "boyfriend", "girlfriend", "fiance", "fiancé", "fiancee", "fiancée"}, []string{"partners"}),
	words(RelationMother, []string{"mother", "mum", "mom", "mummy", "mommy", "mam", "mama"}, nil),
	words(RelationFather, []string{"father", "dad", "daddy", "papa"}, nil),
	words(RelationDaughter, []string{"daughter"}, []string{"daughters"}),
	words(RelationSon, []string{"son"}, []string{"sons"}),
	words(RelationSister, []string{"sister"}, []string{"sisters"}),
	words(RelationBrother, []string{"brother"}, []string{"brothers"}),
	words(RelationGrandmother, []string{"grandmother", "grandma", "granny", "gran", "nan", "nana", "nanna"}, nil),
	words(RelationGrandfather, []string{"grandfather", "grandpa", "grandad", "granddad"}, nil),
	words(RelationGrandchild, []string{"grandchild", "grandkid", "grandson", "granddaughter"},
		[]string{"grandchildren", "grandkids", "grandsons", "granddaughters"}),
	words(RelationAunt, []string{"aunt", "auntie", "aunty"}, []string{"aunts", "aunties"}),
	words(RelationUncle, []string{"uncle"}, []string{"uncles"}),
	words(RelationCousin, []string{"cousin"}, []string{"cousins"}),
	words(RelationNiece, []string{"niece"}, []string{"nieces"}),
	words(RelationNephew, []string{"nephew"}, []string{"nephews"}),
	words(RelationRelative, []string{"relative", "family member"}, []string{"relatives", "family members"}),
	words(RelationFriend, []string{"friend", "best friend"}, []string{"friends", "best friends"}),
	words(RelationColleague, []string{"colleague", "coworker", "co-worker"}, []string{"colleagues", "coworkers", "co-workers"}),
	words(RelationNeighbour, []string{"neighbour", "neighbor"}, []string{"neighbours", "neighbors"}), //nolint:misspell // American spellings are search words, as "mom" is, not prose
	[]relationWord{
		{Text: "parent", Means: parents, Is: RelationParent},
		{Text: "parents", Means: parents, Is: RelationParent, Plural: true},
		{Text: "child", Means: children, Is: RelationChild},
		{Text: "kid", Means: children, Is: RelationChild},
		{Text: "children", Means: children, Is: RelationChild, Plural: true},
		{Text: "kids", Means: children, Is: RelationChild, Plural: true},
		{Text: "sibling", Means: siblings, Is: RelationSibling},
		{Text: "siblings", Means: siblings, Is: RelationSibling, Plural: true},
		{Text: "grandparent", Means: grandparents, Is: RelationGrandparent},
		{Text: "grandparents", Means: grandparents, Is: RelationGrandparent, Plural: true},
		{Text: "family", Means: family, Plural: true},
	},
)

func words(r Relation, singular, plural []string) []relationWord {
	out := make([]relationWord, 0, len(singular)+len(plural))
	for _, w := range singular {
		out = append(out, relationWord{Text: w, Means: []Relation{r}, Is: r})
	}
	for _, w := range plural {
		out = append(out, relationWord{Text: w, Means: []Relation{r}, Is: r, Plural: true})
	}
	return out
}

// MePhrases are what a search calls the user's own person ("photos of me").
var MePhrases = []string{"me", "myself"}

// MyPrefix starts every relationship phrase in a search: "my mum", never
// "mum" alone, which may be a description or someone's name.
const MyPrefix = "my "

// reservedLabels can't be custom labels: a search reads them as the user.
var reservedLabels = map[string]bool{"me": true, "myself": true, "my": true, "i": true}

// CleanRelation checks a relationship the user chose and returns it as it
// is kept. A label is only for RelationCustom: it is trimmed, loses a
// leading "my " ("My coach" is a coach), and must then be 1 to
// MaxRelationLabelLen characters. A label that is a built-in word becomes
// that relationship ("Mum" is RelationMother), so a search finds it by
// every word for it. ok is false for an unknown relationship, a label on a
// built-in one, or a label that is empty, too long, or a word for the user
// or for several relationships ("family").
func CleanRelation(r Relation, label string) (Relation, string, bool) {
	switch {
	case (r == RelationNone || slices.Contains(Relations, r)) && label == "":
		return r, "", true
	case r != RelationCustom:
		return "", "", false
	}
	label = strings.Join(strings.Fields(label), " ")
	if len(label) >= len(MyPrefix) && Fold(label[:len(MyPrefix)]) == MyPrefix {
		label = strings.TrimSpace(label[len(MyPrefix):])
	}
	folded := Fold(label)
	if label == "" || utf8.RuneCountInString(label) > MaxRelationLabelLen || reservedLabels[folded] {
		return "", "", false
	}
	for _, w := range relationWords {
		if Fold(w.Text) == folded {
			return w.Is, "", w.Is != RelationNone
		}
	}
	return RelationCustom, label, true
}

// SameRelation reports whether two kept relationships are the same, custom
// labels compared whatever their case.
func SameRelation(a Relation, aLabel string, b Relation, bLabel string) bool {
	return a == b && Fold(aLabel) == Fold(bLabel)
}

// RelatedPerson is a person the user marked as themself or gave a
// relationship, as a search looks them up.
type RelatedPerson struct {
	ID       int64
	Me       bool
	Relation Relation
	Label    string // the custom label of RelationCustom
	Blocked  bool   // blocked, or every face of theirs is: a search names them only to say so
}

// Targets returns the phrases a search names p by: "me" and "myself" for
// the user, else "my" with every word for their relationship ("my mum",
// "my parents"), or with their custom label and its plurals.
func (p RelatedPerson) Targets() []NamedTarget {
	target := func(text string, plural bool) NamedTarget {
		return NamedTarget{ID: p.ID, Name: text, Kind: BlockPerson, Blocked: p.Blocked, Related: true, Plural: plural}
	}
	var out []NamedTarget
	switch {
	case p.Me:
		for _, text := range MePhrases {
			out = append(out, target(text, false))
		}
	case p.Relation == RelationCustom:
		out = append(out, target(MyPrefix+p.Label, false), target(MyPrefix+p.Label+"s", true), target(MyPrefix+p.Label+"es", true))
	case p.Relation != RelationNone:
		for _, w := range relationWords {
			if slices.Contains(w.Means, p.Relation) {
				out = append(out, target(MyPrefix+w.Text, w.Plural))
			}
		}
	}
	return out
}

// UnassignedTargets returns every built-in phrase for the user or a
// relationship as targets of no one (ID 0): a search that uses one no one
// is yet can say so instead of quietly reading it as a description.
func UnassignedTargets() []NamedTarget {
	out := make([]NamedTarget, 0, len(MePhrases)+len(relationWords))
	for _, text := range MePhrases {
		out = append(out, NamedTarget{Name: text, Kind: BlockPerson, Related: true})
	}
	for _, w := range relationWords {
		out = append(out, NamedTarget{Name: MyPrefix + w.Text, Kind: BlockPerson, Related: true, Plural: w.Plural})
	}
	return out
}

// MentionsUser reports whether query has a word ("me", "myself", "my") that
// may begin a phrase naming the user or a relationship: only then does a
// search look for them.
func MentionsUser(query string) bool {
	return slices.ContainsFunc(strings.FieldsFunc(Fold(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), func(word string) bool {
		return word == "my" || slices.Contains(MePhrases, word)
	})
}
