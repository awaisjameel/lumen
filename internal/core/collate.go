package core

import (
	"strings"
	"sync"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// CompareNames is the order of every list of names the user reads (albums,
// people, Things, places): the Unicode Collation Algorithm's root order from
// CLDR, which is English's (the UI's language, see LOCALE in shared/i18n.ts).
// A letter sorts with its accented forms, so "Émile" comes before "Zoë"
// rather than after every unaccented name, and accents and case only decide
// between names that are otherwise equal. Runs of digits compare by value,
// as Explorer sorts them, so "Trip 2" comes before "Trip 10". Names the
// collation calls equal fall back to their bytes, so the order is total.
//
// The store registers it as the SQL collation "name" (store.byName), so a
// list sorted in SQL and one sorted in Go agree.
func CompareNames(a, b string) int {
	c := collators.Get().(*collate.Collator) // only collators are put in the pool
	defer collators.Put(c)
	if order := c.CompareString(a, b); order != 0 {
		return order
	}
	return strings.Compare(a, b)
}

// A Collator keeps buffers between calls, so each caller takes its own.
var collators = sync.Pool{New: func() any { return collate.New(language.Und, collate.Numeric) }}
