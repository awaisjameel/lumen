# The audit's P2 items and the thumbnail keep-alive, 14 October 2026

The [bottleneck audit](bottleneck-audit-2026-10-13.md) listed seven P2 items;
P2-2 was measured in the [previous round](event-lookup-latency-gate-2026-10-13.md).
This round changes P2-1, P2-4, P2-5 and P2-7, the whole `Events` regroup left
over from P1-5, and the P3 settings cache. It tried the thumbnail keep-alive
that the previous round proposed and left it out, for the reasons below.
P2-3 and P2-6 were not changed: the gate now measures both and the figures
no longer support the redesign the audit proposed.

Figures marked "gate" are `BenchmarkLargeLibraryStore` on 100,000 photos,
i7-13700H, no race detector, five samples of three calls each, quiet machine,
in milliseconds. "Before" is the budget file's previous samples. Other figures
were taken by the benchmarks named beside them while other work loaded the
machine, so compare their order of magnitude, not their digits.

## The event list follows its own journal

`Store.Events` regrouped every visible photo whenever `event_revision` moved,
which during indexing was nearly always. Migration 0033 adds
`event_list_revision` and `event_list_dirty`: triggers on assets, event
membership and `blocked_assets` record the date each change touched, in the
same transaction. `Events` keeps its list and regroups only the windows around
those dates, widened through both the cached events and the current
gap-joined runs (removing a photo can split an event beyond the run around
it; adding one can join runs beyond the old edges), then splices them in and
restores the pin order. A changed thumbnail refreshes the covers of the events
whose dates hold it (`event_list_cover_dirty`).

The whole regroup stays as the reference and the fallback: the first call, a
changed gap, and a reader that fell behind the retained 4,096 revisions.

| Call, no blocks (gate)                 | Before      | After       |
| -------------------------------------- | ----------- | ----------- |
| `Events` after one photo changed       | 92.5–93.8   | 1.29–1.61   |
| `Events` after a cover changed         | 16.0–17.0   | 0.41–0.48   |
| `Events` regrouped whole (new call)    | n/a         | 91.9–125.8  |

`events_incremental_test.go` compares the list with a whole regroup in the
same snapshot after each of 3,000 randomised steps (dates moved, photos added,
hidden, trashed, missing and restored, names, pins, event, place and folder
blocks, every gap), runs readers against a writer, and covers the pruned
history and the migration.

## Duplicates are cached until an input changes

`DuplicateGroups` read every hashed photo and rebuilt every group on each
call; the Duplicates screen asks every few seconds while thumbnails are made.
It now keeps its answer by four revisions: the event, visibility and cover
revisions already cover dates, places, blocks and thumbnail versions, and
migration 0034 adds `duplicate_value` for the hash, dimensions, the fields
that pick the best copy, and every file insert, delete or change. Callers get
a deep copy. Photos with identical hashes are paired once rather than in each
of the 28 bucket passes.

| Call (gate, no blocks)                 | Before        | After        |
| -------------------------------------- | ------------- | ------------ |
| `DuplicateGroups` after a change       | 265.7–270.8   | 292.3–312.3  |
| `DuplicateGroups` again (new call)     | (the same)    | 0.03–0.08    |
| 6,000 identical hashes (own benchmark) | 55 to 113 s   | 8 to 10      |

A rebuild is about a tenth slower, the cost of keeping and copying the answer.
The randomised test compares the new grouping with the previous algorithm,
kept as a test-only reference.

## Clustering reads only what changed

`people.Engine` read every face and its vector and rebuilt every person on
each run, at least every 30 s while faces arrived. Migration 0036 adds a
journal of changed faces (`cluster_faces`, `cluster_revision`), filled by
triggers on faces and on asset visibility. `Store.ClusterSince` returns what
changed since the engine's cursor, from one snapshot, or everything when the
cursor fell behind the retained 100,000 revisions. The engine keeps each
person as a sum and a count; a person whose faces changed is resummed in
face-ID order, so its mean is bit-for-bit the reference's.

Today's engine keeps neighbour links between runs, so after a manual
placement it can place one more face than clustering the same data from
scratch would. That behaviour is kept; the reference is today's engine, not
a fresh one, and `incremental_reference_test.go` documents the difference.

| 120,000 placed faces + 100 new (own benchmarks) | Before        | After      |
| ----------------------------------------------- | ------------- | ---------- |
| Loading                                         | 884–1,711 ms  | 0.8–2.4 ms |
| Allocated by loading                            | 255 MB        | 0.2 MB     |
| Linking and clustering                          | 249–359 ms    | 2.2–4.8 ms |

The kept state is about 99 MB at 120,000 faces. It is given back two minutes
after the last run, so a library at rest holds none of it; the run after that
loads everything once, as the first run does. The randomised test runs the
incremental engine beside today's through arrivals, rejections, manual
placements, names, merges, blocks, trash, removals, stage re-runs, model
changes, restarts and releases, and compares every proposal and the linker's
state after each run.

## XMP export writes only what changed

With export on, every tag batch moved `xmp_revision` and started a pass over
the whole library. Migration 0035 replaces those triggers with ones that also
record the asset in `xmp_dirty` with the revision of its change: tags, faces
placed or moved, people renamed or removed, favourites, dimensions, files,
`blocked_assets`, label blocks and face blocks. A change to the tag confidence
or turning export on marks every asset, and an upgrade marks every asset
once. A batch acknowledges its marks in the transaction that records what it
wrote, only where the revision is still the one it read, so a change during a
write is kept for the next pass and a crash leaves the work to retry. The
explicit Export command still checks the whole library against the disk.

| 50-asset tag batch, 100,000 assets, export on (own benchmark) | Before | After |
| ------------------------------------------------------------- | ------ | ----- |
| Batch and reconciliation, median of five                      | 11.1 s | 53 ms |
| Assets read                                                   | 100,000 | 50   |

A randomised test compares the sidecars with a full pass after 360 changes;
others cover each input, a change during a write, and an interruption between
writing and acknowledging.

## SQLite gives its memory back at rest

Each connection keeps a 32 MB cache, and `store.Open` kept `max(4, NumCPU/2)`
readers open forever: eleven caches on a 20-thread PC. Measured (100,000
photos, ten goroutines reading every screen, private bytes after GC):

| Configuration                          | Warm      | 17 s idle |
| -------------------------------------- | --------- | --------- |
| Before                                 | 426–434   | 431       |
| 16 MB cache                            | 250–253   | n/a       |
| Four readers                           | 207–209   | n/a       |
| Kept: at most four idle readers, closed after 15 s | 345–433 | 72 |

A smaller cache made `JobCounts` 36% slower; four readers made concurrent
`CountVisible` calls wait 0.6 to 0.8 s for a connection. Memory mapping was
left out: on Windows a mapped file cannot be truncated and an I/O error on a
mapped page ends the process. So the pool keeps its size while busy and closes
idle readers after 15 seconds, before the indexer's 30-second polls.

The write-ahead log is checkpointed with truncation when the indexer becomes
idle, on a separate connection with no busy wait, so a reader in the log makes
it a no-op rather than a wait. A setting never set is now cached as absent
(P3), so the tag confidence is no longer queried on every timeline read.

`RestoreBackup` opened its source with a plain path, from which the driver
drops `mode=ro`, so a missing backup was created empty and restored. It now
refuses a backup that is not there (the start-up recovery path already
checked).

## Write cost

Four journals add triggers to the write path. Gate, no blocks:

| Write                | Before      | After       |
| -------------------- | ----------- | ----------- |
| `Ingest` 128 photos  | 9.8–12.8    | 12.2–13.4   |
| 64 thumbnails saved  | 3.29–3.42   | 3.88–4.37   |
| 64 face results      | 2.11–2.36   | 3.20–3.41   |
| 64 tag results       | 15.0–22.2   | 17.4–23.8   |

All stay well inside their ceilings.

## The thumbnail keep-alive: tried, left out

The previous round found that every thumbnail coming back into view is a full
request. A keep-alive was built: up to 256 decoded images, 32 MiB, keyed by
asset, version and size, reattached when the same tile returns, emptied
before any block change and on every library change, with the Blocked screen
opted out. `e2e/perf/thumbs.mjs` judged it on a quiet machine (3,600 photos,
same fixtures):

| Scenario                     | Without (3 runs)    | With (3 runs)     |
| ---------------------------- | ------------------- | ----------------- |
| Back up ten screens          | 96–120 requests     | 0 requests        |
| Fifty-screen fling, requests | 1,351–1,569         | 927–1,261         |
| Fling, frames over 50 ms     | 2, 42, 53 of ~144   | 20, 53 of ~144    |
| WebView private bytes at rest after the fling | 428–527 MiB | 290–641 MiB |

On a quiet machine a request takes a median of about 10 ms and the frames are
as smooth either way; the fling's long frames and the memory vary as much
between runs of one build as between the builds. While indexing, when
requests are slowest, the keep is emptied by every library change, about once
a second, so it does not help there either. A visible gain did not show, and
the keep would add memory and a second place a blocked photo could be painted
from, so the product code is unchanged.

The script keeps what this work added: WebView memory before and after the
fling and at rest, a probe that scrolls the target out before the block and
fails if the grid paints it again, and tests of both helpers. The slow frames
the previous round reported (85 of 173) were the loaded machine, not the
requests.

## Not changed: P2-3 and P2-6

Both are in the gate now, after the earlier rounds:

| Call (gate)                      | No blocks   | Folder, Thing and person blocks |
| -------------------------------- | ----------- | ------------------------------- |
| `Selection` of the whole view    | 38.0–43.6   | 49.5–52.9                       |
| `DayItems` of the 10,000-photo day | 16.4–17.4 | 18.7–27.0                       |

The audit measured `Selection` at 98 ms and 2.7 s with blocks; it is now
about 50 ms in every configuration, once per shift-click range, so asking for
a range instead of the view would not be felt. A 10,000-photo day costs 16 to
27 ms in the engine; what is left is the grid fetching such a day again about
once a second while thumbnails are made, which a real library with one large
undated import would show. Neither was changed; measure a real library before
paging inside a day.

## The gate

`EventsRegroup` (another gap, so the whole regroup the journal avoids) and
`DuplicateGroupsRepeated` were added to the benchmark. `Events` and
`EventsAfterCover` were lowered from 300–400 ms and 60 ms to 5–6 ms by the
file's rule; every other ceiling is unchanged. One full run sampled
`CountVisible` with a folder block at 61 ms against its 60 ms ceiling; run on
its own five times it took 17.9 to 18.8 ms, so the ceiling stands.

## How the work was done

Five changes were made in separate checkouts and combined:
`events_incremental_test.go`, `duplicates_*_test.go`, `faces_incremental_test.go`,
`incremental_*_test.go` in `internal/people`, `xmp_dirty_test.go` in the store
and the indexer, and `sqlite_resources_test.go` and `store_test.go` hold the
regression tests; each was checked to fail without its change. The
benchmarks beside them (`*_benchmark_test.go`, `BenchmarkEventListSmallChanges`,
`BenchmarkIncrementalCluster*`) are not in the gate and run only when named.
