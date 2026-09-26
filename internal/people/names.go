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
// if only fillers remain. Longer names win ("Ali Khan" before "Ali"), and a
// name only counts as whole words.
func FindNames(query string, named []core.Person) (ids []int64, names []string, rest string) {
	named = slices.Clone(named)
	slices.SortStableFunc(named, func(a, b core.Person) int { return cmp.Compare(len(b.Name), len(a.Name)) })
	rest = query
	for _, p := range named {
		if p.Name == "" {
			continue
		}
		if at := wordIndex(rest, p.Name); at >= 0 {
			ids = append(ids, p.ID)
			names = append(names, rest[at:at+len(p.Name)])
			rest = rest[:at] + " " + rest[at+len(p.Name):]
		}
	}
	words := strings.Fields(rest)
	if !slices.ContainsFunc(words, func(w string) bool { return !fillers[strings.ToLower(w)] }) {
		words = nil
	}
	return ids, names, strings.Join(words, " ")
}

// wordIndex finds name in s as whole words, ignoring case; -1 if absent.
func wordIndex(s, name string) int {
	ls, ln := strings.ToLower(s), strings.ToLower(name)
	if len(ls) != len(s) {
		return -1 // lower-casing changed byte lengths: offsets wouldn't map back
	}
	for from := 0; ; {
		i := strings.Index(ls[from:], ln)
		if i < 0 {
			return -1
		}
		i += from
		end := i + len(ln)
		before, _ := utf8.DecodeLastRuneInString(ls[:i])
		after, _ := utf8.DecodeRuneInString(ls[end:])
		if (i == 0 || !isWord(before)) && (end == len(ls) || !isWord(after)) {
			return i
		}
		from = i + 1
	}
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
