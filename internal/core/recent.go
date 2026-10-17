package core

import "strings"

// RecentSearchesMax is how many of the user's searches Lumen remembers.
const RecentSearchesMax = 8

// NormalisedRecentSearches is list as it is kept: each search trimmed, empty
// ones dropped, each once (compared folded, the newest spelling kept), newest
// first, RecentSearchesMax at most. A list never nil, so it is [] on the wire.
func NormalisedRecentSearches(list []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, q := range list {
		q = strings.TrimSpace(q)
		key := Fold(q)
		if q == "" || seen[key] {
			continue
		}
		if len(out) == RecentSearchesMax {
			break
		}
		seen[key] = true
		out = append(out, q)
	}
	return out
}
