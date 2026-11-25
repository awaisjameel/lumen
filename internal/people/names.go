package people

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"lumen/internal/core"
)

// fillers are words that, left alone once names are taken out of a search,
// say nothing about the photo ("Ali and Sara", "photos of Ali").
var fillers = map[string]bool{
	"and": true, "with": true, "of": true, "together": true, "&": true, "+": true, ",": true,
	"photo": true, "photos": true, "picture": true, "pictures": true, "pic": true, "pics": true,
}

// Query is what a search query names: the people it narrows to, and what
// is left to search for.
type Query struct {
	// Blocked: the query names something blocked, so it finds nothing (and
	// a recent search that does is not listed).
	Blocked bool
	// People are its parts that name people, each a name, "me" or "my …",
	// in the order the query first names them.
	People []core.SearchPerson
	// Unassigned are its "me" and "my …" phrases no one is yet, as spelled.
	// They stay in Rest: they are searched for as a description, and the
	// UI says why they did not name anyone.
	Unassigned []string
	// Rest is the query without the people's parts; empty if only fillers
	// remain beside them.
	Rest string
}

// ParseQuery finds who a search names among named, as the store's
// SearchNames finds them: names ("Ali at the beach"), "me", and
// relationships ("my mum", "my friends"). Each part a person, a name or a
// singular relationship, should name one person; one that names several
// ("my mum" with two mums, or a person named "My Mum" beside someone set as
// Mum) is Ambiguous, and its photos are those of any of them, so the user
// can choose without the search choosing for them. A blocked name blocks
// the whole query; a relationship only when everyone it names is blocked,
// the others being searched for alone.
func ParseQuery(query string, named []core.NamedTarget) Query {
	blocked := Query{Blocked: true, People: []core.SearchPerson{}, Unassigned: []string{}}
	// A blocked name blocks the query wherever it is, even inside a longer
	// relationship phrase ("my family" beside a blocked Thing "Family").
	names := slices.DeleteFunc(slices.Clone(named), func(t core.NamedTarget) bool { return t.Related })
	if slices.ContainsFunc(MatchNames(query, names), NameMatch.Blocked) {
		return blocked
	}
	if core.MentionsUser(query) {
		named = slices.Concat(named, core.UnassignedTargets())
	}
	var q Query
	var people []NameMatch // the matches that name someone
	for _, match := range MatchNames(query, named) {
		if match.Blocked() {
			return blocked
		}
		ids, plural := match.People()
		switch {
		case len(ids) > 0:
			people = append(people, match)
			text := query[match.Start:match.End]
			if i := slices.IndexFunc(q.People, func(p core.SearchPerson) bool { return slices.Equal(p.People, ids) }); i < 0 {
				q.People = append(q.People, core.SearchPerson{Text: text, People: ids, Ambiguous: len(ids) > 1 && !plural})
			}
		case slices.ContainsFunc(match.Targets, func(t core.NamedTarget) bool { return t.Related }):
			q.Unassigned = append(q.Unassigned, query[match.Start:match.End])
		}
	}
	if q.People == nil {
		q.People = []core.SearchPerson{}
	}
	if q.Unassigned == nil {
		q.Unassigned = []string{}
	}
	q.Rest = RemainingQuery(query, people)
	return q
}

// Groups returns the people of each part of q, for store.PeopleAssets.
func (q Query) Groups() [][]int64 {
	groups := make([][]int64, len(q.People))
	for i, p := range q.People {
		groups[i] = p.People
	}
	return groups
}

// NameMatch locates a whole-word entity name using byte offsets in the
// query. Targets are everything of that name, whatever its case: a person
// and a place may share a name, several people a relationship.
type NameMatch struct {
	Targets    []core.NamedTarget
	Start, End int
}

// Blocked reports whether the match names something blocked. A name
// blocks it outright; a relationship phrase only when every person it names
// is blocked, as the others can be searched for without revealing them.
func (m NameMatch) Blocked() bool {
	everyone := true
	for _, t := range m.Targets {
		switch {
		case t.Blocked && !t.Related:
			return true
		case t.ID != 0 && !t.Blocked:
			everyone = false
		}
	}
	return everyone && slices.ContainsFunc(m.Targets, func(t core.NamedTarget) bool { return t.Blocked })
}

// People returns the shown people the match names, in ascending order, and
// whether its words expect several (a plural relationship phrase).
func (m NameMatch) People() (ids []int64, plural bool) {
	plural = len(m.Targets) > 0
	for _, t := range m.Targets {
		if t.Kind != core.BlockPerson {
			continue
		}
		plural = plural && t.Plural
		if t.ID != 0 && !t.Blocked && !slices.Contains(ids, t.ID) {
			ids = append(ids, t.ID)
		}
	}
	slices.Sort(ids)
	return ids, plural
}

// MatchNames resolves all occurrences, preferring longer names. Targets of
// the same name, whatever its case, match together.
func MatchNames(query string, named []core.NamedTarget) []NameMatch {
	byName := map[string][]core.NamedTarget{}
	var names []string // one spelling of each, longest first
	for _, t := range named {
		if t.Name == "" {
			continue
		}
		key := core.Fold(t.Name)
		if _, ok := byName[key]; !ok {
			names = append(names, t.Name)
		}
		byName[key] = append(byName[key], t)
	}
	slices.SortStableFunc(names, func(a, b string) int {
		return cmp.Compare(utf8.RuneCountInString(b), utf8.RuneCountInString(a))
	})
	remaining := []byte(query)
	var matches []NameMatch
	for _, name := range names {
		for {
			start, end := wordIndex(string(remaining), name)
			if start < 0 {
				break
			}
			matches = append(matches, NameMatch{Targets: byName[core.Fold(name)], Start: start, End: end})
			for index := start; index < end; index++ {
				remaining[index] = ' '
			}
		}
	}
	slices.SortFunc(matches, func(a, b NameMatch) int { return cmp.Compare(a.Start, b.Start) })
	return matches
}

// RemainingQuery removes matched names and ignores fillers when names were present.
func RemainingQuery(query string, matches []NameMatch) string {
	remaining := []byte(query)
	for _, match := range matches {
		for index := match.Start; index < match.End; index++ {
			remaining[index] = ' '
		}
	}
	words := strings.Fields(string(remaining))
	if len(matches) > 0 && !slices.ContainsFunc(words, func(w string) bool { return !fillers[core.Fold(w)] }) {
		words = nil
	}
	return strings.Join(words, " ")
}

// wordIndex finds name in s as whole words, whatever the case, and returns
// where it starts and ends in s; -1, -1 if it is not there. Letters are
// compared one by one, so offsets stay right even when a letter's other
// case has another length in bytes ("İ" and "i").
func wordIndex(s, name string) (start, end int) {
	for i := 0; i < len(s); {
		if j, ok := matchAt(s, i, name); ok {
			before, _ := utf8.DecodeLastRuneInString(s[:i])
			after, _ := utf8.DecodeRuneInString(s[j:])
			if (i == 0 || !isWord(before)) && (j == len(s) || !isWord(after)) {
				return i, j
			}
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return -1, -1
}

// matchAt reports whether name is at s[i:], ignoring case, and where it ends.
func matchAt(s string, i int, name string) (int, bool) {
	for _, want := range name {
		if i >= len(s) {
			return 0, false
		}
		got, size := utf8.DecodeRuneInString(s[i:])
		if core.FoldRune(got) != core.FoldRune(want) {
			return 0, false
		}
		i += size
	}
	return i, true
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
