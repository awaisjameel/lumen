# Name order, 14 October 2026

The [bottleneck audit](bottleneck-audit-2026-10-13.md)'s implementation
issue 3 found albums sorted with SQLite's `NOCASE`, which folds only A-Z and
otherwise compares bytes, so "Émile" came after "Zoë". The other lists of
names were no better: People, Things, places, a photo's tags and the
suggestions while typing broke their ties by bytes, which also puts "Zoë"
before "émile" and "Album 10" before "Album 2".

## One order

`core.CompareNames` is the order of every list of names the user reads. It
is the Unicode Collation Algorithm's root order from CLDR (`golang.org/x/text/collate`),
which is English's, the UI's language: a letter sorts with its accented
forms, accents and case decide only between names otherwise equal, and runs
of digits compare by value, as Explorer sorts them. Names the collation calls
equal ("Trip 7" and "Trip 07") fall back to their bytes, so the order is
total and the same on every run.

The store registers it on every connection as the collation `name_order`,
and `byName(column)` is the one way a query asks for it: albums, People,
Things (kept and counted), places, a photo's tags and the suggestions for
people and Things. Only queries name the collation, never the schema, so a
tool that opens the library still needs only `fold()` (as the end-to-end
scripts register it in Python).

Left alone, on purpose:

- **The ordering of names for matching** (`NamedPeople`, longest first) and
  of keywords written to XMP: neither is a list the user reads.
- **The `UNIQUE COLLATE NOCASE` constraints** on `tags.name`, `people.name`
  and `albums.name`. Two names `NOCASE` calls equal are equal under
  `core.Fold` too, so the constraints never refuse a name the `fold()` check
  (`store.named`) allows. Dropping them would mean rebuilding three tables
  that most others reference, for no change in behaviour.

## Cost

Comparing two names takes about 0.17 µs in Go and about 0.3 µs from SQLite,
through the driver's callback, and only ties on the earlier sort terms reach
it. The gate's seeded library is close to the worst case: its 500 people
and its 200 places each have almost the same count, so whole lists sort by
name. On 100,000 photos (`BenchmarkLargeLibraryStore`,
i7-13700H, five samples of twenty calls, in milliseconds):

| Call   | Bytes   | Name order | Budget |
| ------ | ------- | ---------- | ------ |
| Things | 2.6-2.9 | 2.9-3.4    | 10     |
| People | 4.1-4.3 | 5.5-6.0    | 20     |
| Places | 7.3-8.4 | 8.2-9.2    | 30     |

No budget changed. Sort keys computed once per row were the alternative.
They cost a call for every row, whether or not anything ties, so they would
only pay where whole lists tie, as here.

## Tests

`TestCompareNamesSortsAccentedLettersWithTheirBaseLetters` and
`TestCompareNamesIsATotalOrder` (core), and
`TestNameListsSortAsTheUserReads` (store), which checks every list above and
fails on each of them with byte order. The people counts' reference query
(`person_counts_test.go`) sorts with `byName` too.
