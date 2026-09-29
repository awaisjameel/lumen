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
	named = slices.Clone(named)
	slices.SortStableFunc(named, func(a, b core.Person) int {
		return cmp.Compare(utf8.RuneCountInString(b.Name), utf8.RuneCountInString(a.Name))
	})
	rest = query
	for _, p := range named {
		if p.Name == "" {
			continue
		}
		if start, end := wordIndex(rest, p.Name); start >= 0 {
			ids = append(ids, p.ID)
			names = append(names, rest[start:end])
			rest = rest[:start] + " " + rest[end:]
		}
	}
	words := strings.Fields(rest)
	if len(ids) > 0 && !slices.ContainsFunc(words, func(w string) bool { return !fillers[core.Fold(w)] }) {
		words = nil
	}
	return ids, names, strings.Join(words, " ")
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
