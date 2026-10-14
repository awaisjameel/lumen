# SQLite tuning, 15 October 2026

A measured review of the store's SQLite use against the usual tuning list
(WAL, synchronous, busy timeout, cache, temporary storage, indexes, batching,
`BEGIN IMMEDIATE`, `PRAGMA optimize`, row ids, pagination, expression
indexes, WAL growth). Most of the list was already in place; five changes
were worth making, and this round makes them.

Figures marked "gate" are `BenchmarkLargeLibraryStore` on 100,000 photos,
i7-13700H, no race detector. The machine ran 20–40% slower on this day than
when the budget file's samples were taken, on calls this round does not
touch, so comparisons with the previous settings were run in the same session,
interleaved. The other figures come from a temporary probe against the same
seed; Go's clock on this machine resolves to about 0.5 ms, so small figures
there are averages over hundreds or thousands of calls.

## What changed

**Temporary storage in memory.** Every connection now runs
`PRAGMA temp_store = MEMORY` (`connectPragmas`, set in the driver's connect
hook, since go-sqlite3 has no DSN option for it). A statement journal over
64 KiB spilled to a real temporary file, and a batch of tag or embedding rows,
whose triggers touch many more, passes that easily. Saving tags for 64 photos
went from 23.4 to 12.1 ms and saving their embeddings from 26.5 to 17.4 ms
(probe); in the gate, tag saves are 24–44% faster than the previous settings
in the same session. The AI stage holds the writer about half as long. Reads
were unchanged over three interleaved rounds.

**A statement cache on the readers.** go-sqlite3 1.14.52 keeps compiled
statements per connection when `_stmt_cache_size` is set; it was off, so
every query was compiled on every call, and compiling a small read cost about
as much as running it. Each reader now keeps its 8 most recently used
statements. In the gate, the visibility check behind every thumbnail
(`AssetVisible`) is 26–60% faster, opening an event 33–42%, and an event's
days in the timeline 15–44%.

The size and the writer's exclusion are measured choices. Over cache sizes
of 0, 8, 16, 32 and 64, the small reads gained all they could by 8, while the
largest statements slowed as the cache grew: rebuilding a folder block took
129 ms without a cache and 178 ms at 64, and the duplicates rebuild 260 and
288 ms. The likeliest cause is that kept statements hold the connection's
small-allocation memory (lookaside), which go-sqlite3 gives no way to
enlarge, so a large statement falls back to `malloc`. The writer's own
transactions settle more than 8 statements, so only a cache of 32 kept them
(renaming an event 0.95 → 0.40 ms), at the cost of a third on the block
rebuild, and its indexing batches gained nothing at any size. The writer
therefore has no cache. With the readers' cache, the large calls stayed
within the run-to-run noise (about ±10%) of the previous settings in
interleaved runs. Only a single statement run directly is cached;
statements from `PrepareContext` are already reused by their callers.

**Bounded statistics on exit.** `Close` ran `PRAGMA optimize` without the
`analysis_limit` `Optimize` sets; an unbounded `ANALYZE` took 306 ms on
100,000 photos, against 140 ms bounded, and grows with the library. The limit
of 1000 rows is now part of `connectPragmas`, so every connection, the
writer `Close` uses and the one `Optimize` opens included, analyses within it.

**Queueing text reading from the tags.** `QueueOCR`, at every indexer start,
tested every asset for a Screenshots or Documents tag. It now starts from
those two tags' entries in `asset_tags_by_tag` (`ocrEligibleAll`); the
eligible set is the same, and the call went from 62–88 ms to 7–10 ms (gate).
Calls for a few photos by id keep the per-asset test (`ocrEligible`), which
is cheaper for them. Skipping photos that already have a job saved a further
0.7 ms, which did not justify another clause.

**An index of unavailable assets.** Search reads the assets it must leave
out after every change of visibility, and that read tested every asset.
Migration 0038 adds `assets_unavailable`, a partial index of the assets that
are hidden, trashed or missing, with those three columns so the query reads
the index alone; blocked assets come from `blocked_assets`. The call went
from 11–24 ms to 0.03–1.4 ms (gate; the most is with a folder, a Thing and
a person blocked, where `blocked_assets` holds more rows).

## Already in place

WAL mode, `synchronous=NORMAL`, a 10-second busy timeout, foreign keys, a
32 MB page cache, one writer that takes its lock when a transaction begins
(`_txlock=immediate`), batched and prepared writes, short read transactions,
statistics refreshed at idle, and the WAL truncated at idle. Indexes are
partial and covering where it matters, and none is a redundant prefix of
another. The link tables are `WITHOUT ROWID`, and `AUTOINCREMENT` is on two
small tables only, where ids must never be reused. The only `OFFSET` pages a
compression run's jobs, a table too small for keyset paging to matter.

## Measured and not done

- **A covering timeline index.** Selecting the whole view was no faster, and
  regrouping events gained 17%, for a large index every `assets` write would
  maintain.
- **Rewrites of `CountPending`.** Both were slower. It costs about 43 ms, and
  only while a backlog exists.
- **Reordering the day buckets query.** No gain.
- **`_mutex=no`.** Scans were 5–20% faster, but a statement finalised from
  Go's finalizer goroutine could then race inside SQLite.
- **Memory-mapped I/O** stays off, for the Windows reasons recorded earlier.
- **Unindexed foreign keys** on export and compression jobs cost nothing,
  because assets are never deleted.
- **A larger page size** would need a `VACUUM` of every library for little
  gain.

## Left for later

- Large reads are bound by Go, not SQLite: the duplicates rebuild takes 53 ms
  in SQLite and 253 ms with its rows converted, and loading every embedding
  88 and 157 ms. Fewer, packed columns would help the Duplicates screen.
- The 13 triggers on `asset_tags` are half the time of a tag save, with no
  single one dominating. Bumping the selection revision once at settlement,
  as blocked assets are settled, would recover part of it.
- Ingest prepares its statements per batch (about 0.9 ms of a 128-photo
  batch). Keeping them prepared on the writer would save that, but kept
  statements may slow its large ones, as the cache did; a larger lookaside,
  which only a build option (`SQLITE_DEFAULT_LOOKASIDE`) can give, would
  have to come first, and would let the writer keep a cache too.
