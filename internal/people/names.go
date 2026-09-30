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

// FindNames takes the names of named people out of a search query ("Ali at
// the beach"). It returns who was named, with their names as the query
// spelled them, and the rest of the query ("at the beach"), which is empty
// if only fillers remain beside the names. Longer names win ("Ali Khan"
// before "Ali"), a name only counts as whole words, and case is ignored as
// everywhere else (core.Fold).
func FindNames(query string, named []core.Person) (ids []int64, names []string, rest string) {
	targets := make([]core.NamedTarget, len(named))
	for i, p := range named {
		targets[i] = core.NamedTarget{ID: p.ID, Name: p.Name, Kind: core.BlockPerson}
	}
	matches := MatchNames(query, targets)
	seen := map[int64]bool{}
	for _, match := range matches {
		if !seen[match.Target.ID] {
			seen[match.Target.ID] = true
			ids = append(ids, match.Target.ID)
			names = append(names, query[match.Start:match.End])
		}
	}
	return ids, names, RemainingQuery(query, matches)
}

// NameMatch locates a whole-word entity name using byte offsets in the query.
type NameMatch struct {
	Target     core.NamedTarget
	Start, End int
}

// MatchNames resolves all occurrences, preferring longer names and blocked exact ties.
func MatchNames(query string, named []core.NamedTarget) []NameMatch {
	named = slices.Clone(named)
	slices.SortStableFunc(named, func(a, b core.NamedTarget) int {
		if length := cmp.Compare(utf8.RuneCountInString(b.Name), utf8.RuneCountInString(a.Name)); length != 0 {
			return length
		}
		if a.Blocked != b.Blocked {
			if a.Blocked {
				return -1
			}
			return 1
		}
		return 0
	})
	remaining := []byte(query)
	var matches []NameMatch
	for _, p := range named {
		if p.Name == "" {
			continue
		}
		for {
			start, end := wordIndex(string(remaining), p.Name)
			if start < 0 {
				break
			}
			matches = append(matches, NameMatch{Target: p, Start: start, End: end})
			for index := start; index < end; index++ {
				remaining[index] = ' '
			}
		}
	}
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
