# Bottleneck and implementation audit — 13 October 2026

An end-to-end audit of Lumen at `f7bc835` (main, after Phase 12) for
performance bottlenecks and implementation issues. Nothing in the source was
changed by it.

## Verdict

Lumen is fast at the size it has been measured at (3,000 photos), and its
job queue and write path hold up at 200,000. Its **read path does not**: several screens are answered by queries that walk the whole
library, the UI asks for them again every one to three seconds while indexing
runs, and the visibility rule becomes a per-row sub-query as soon as one
photo-mode block exists. On a synthetic 200,000-photo library:

- **Things takes 1.7 s to list, and 32 s once a folder, a Thing and a person
  are blocked.** The Things screen reloads it every 3 s while the AI works.
- **People takes 1.8 s** (4.3 s with those blocks), on the same 3 s reload.
- **One folder block makes every count 3 to 24 times slower**: the visible
  count goes from 20 ms to 483 ms, the progress counts from 156 ms to 1.16 s.
- The timeline's day counts cost 190 ms per library change (866 ms with
  blocks), at up to one change a second during indexing.

None of this shows at 3,000 photos, which is why the Phase 6 performance pass
met its targets. The plan's 100,000-photo acceptance is still open
([performance record](performance-2026-10-03.md)); these findings are what it
would meet first.

The static gate is clean: `scripts/lint.sh` reported 0 issues, no reachable
vulnerability, and 594 of 594 frontend tests passing on this commit. The Go
test suite was not run for this audit.

## Method and coverage

**Measured.** A temporary test in `internal/store` seeded a library through
the real schema and triggers and timed the store's own functions (median of
three, warm cache, no race detector, on this laptop: 20 logical processors,
NVMe; single figures vary by about a fifth between runs). The harness was
deleted afterwards. The library: 200,000 photos over ten years, with a tenth of them
on one day (an import without capture dates); one file, four finished jobs
and eight AI tags each (1.6 million tag rows, 180 Things); 120,000 faces among
500 people; 200 places. Tags and people are spread evenly, which a real
library is not, so treat the figures as the size of the problem, not a
forecast for one library.

**Read in full.** `main.go`; `internal/store` (the schema and every trigger,
visibility, timeline, jobs, ingest, tags, embeddings, faces, people, events
and event tags, duplicates, thumbnail views, places, selection, text queue);
`internal/index` (indexer, stages, CPU budget, kept pixels, cache eviction);
`internal/search` (engine, index); `internal/people` (clustering, links);
`internal/scanner`; `internal/media` (cache, eviction, thumbnails);
`services` (library, search, media, video server); the frontend's library
store, grid, tile, list stores and live reload.

**Sampled** (searched for specific patterns, not read line by line):
`internal/ai`, the rest of `internal/media` (render, frames, RAW, JPEG XL),
`internal/store` (albums, XMP, text, backup, compression, organise),
`internal/index` (XMP pass, videos, politeness), `internal/fileops`,
`internal/compress`, the remaining services, and the Viewer, video player,
details panel and settings screens.

**Not reviewed.** `internal/update` and its commands, `internal/signin`,
`internal/diag`, `internal/xmp`, the installer and build scripts, the
end-to-end suites, and the Go tests themselves. Nothing was profiled in the
running app: there are no frame-time, memory or GPU measurements here.

## Measurements

Milliseconds, 200,000 photos. "Blocks" is one folder block (covering 0.1 % of
the library), one Thing block and one person block, all in photo mode.

| Call                              | What asks for it                            | Plain | One folder block | Three blocks |
| --------------------------------- | ------------------------------------------- | ----: | ---------------: | -----------: |
| `Things`                          | Things screen, every 3 s while indexing     | 1,721 |            6,293 |       31,872 |
| `People`                          | People screen, every 3 s while indexing     | 1,822 |            3,094 |        4,342 |
| `DayBuckets`, whole timeline      | Grid, on every assets change                |   190 |              656 |          866 |
| `JobCounts`                       | Progress report, about once a second        |   156 |            1,162 |          850 |
| `JobCounts`, 400,000 jobs pending | The same, mid-index                         |   336 |                — |            — |
| `CountVisible`                    | Progress report                             |    20 |              483 |          646 |
| `Events`, regrouping              | Events screen; any event view               |   310 |            1,291 |        3,443 |
| `Selection`, whole timeline       | Shift-click, Select all                     |    98 |              665 |        2,672 |
| `DuplicateGroups`                 | Duplicates screen, every 3 s while indexing |   610 |              910 |        1,527 |
| `UnavailableAssets`               | First search after a visibility change      |    20 |              343 |          722 |
| `ClusterData` (120,000 faces)     | Every clustering run                        |   336 |              312 |        1,067 |
| `Places`                          | Places screen                               |     8 |              196 |          801 |
| `DayBuckets`, one Thing           | A Thing's grid                              |    14 |               27 |          122 |
| `DayItems`, the 5,952-photo day   | Grid                                        |    10 |               23 |           55 |
| `DayItems`, three ordinary days   | Grid                                        |   0.6 |              1.1 |          3.2 |
| `AssetVisible`, per call          | Every thumbnail request                     |  0.16 |             0.16 |         0.33 |

What is not a bottleneck, on the same library:

| Call                                                                 |   Time |
| -------------------------------------------------------------------- | -----: |
| `PendingJobsKind`, a batch of 64 from a 200,000-job backlog          | 1.1 ms |
| `SaveEmbeddings`, 64 photos with 12 tags each (every trigger firing) |  25 ms |
| `SaveThumbnails`, 64 photos                                          | 3.3 ms |
| `QueueOCR`, whole library                                            |  17 ms |
| `CountPending`                                                       |  80 ms |

With one Thing block, the count query alone accounts for all of `Things`
(8.5 s of 8.5 s); for `People` it is about half (1.3 s of 2.5 s), the rest
being the cover picked per person. Reading the 1.6 million tag rows on their
own takes 32 ms, so the cost is the join into `assets` and the visibility test
on each row, not the tags.

## Bottlenecks

Ordered by what a user with a large library meets first. P1 is felt at the
plan's 100,000-photo target; P2 at that size in some circumstances; P3 is
headroom.

### P1-1. One photo-mode block turns visibility into a sub-query per row

`visible` ([store.go](../internal/store/store.go), `blockVisible` and
`blockMatch`) is `NOT EXISTS (SELECT 1 FROM blocks … )` with up to twelve
correlated probes inside (faces, tags, files, album rules, event members). With
no block the scan of `blocks` ends at once. With one, SQLite runs the probes
for every asset row of every query that says `visible`: the plan shows
`SCAN assets` with `CORRELATED SCALAR SUBQUERY` thirteen deep. A folder block
is the costliest per row, since it folds and compares every file path of
every asset.

The rule is right and has one owner; its evaluation is the problem. Every
aggregate inherits it, which is the "one folder block" column above.

**Fix.** Keep the rule, materialise its answer: a `blocked_assets (asset_id)`
table, rebuilt when `visible_value` moves (the triggers that bump it already
name every input) and maintained inside the same transaction, so `visible`
becomes the three indexed columns plus one primary-key probe. The existing
predicate stays as the definition the table is built from and as the test's
oracle. This one change removes most of the two right-hand columns.

### P1-2. `Things` joins every tag row to its asset to count them

[`Store.Things`](../internal/store/tags.go) counts with
`tags LEFT JOIN asset_tags LEFT JOIN assets … GROUP BY t.id`: 1.6 million
primary-key lookups into `assets`, each followed by the visibility test. It
is asked for by the Things screen on mount and then every 3 s while tags,
assets or thumbnails change
([ThingsView.vue](../frontend/src/things/ThingsView.vue),
[liveReload.ts](../frontend/src/shared/liveReload.ts)), and by any grid
that needs a Thing's name. At 1.7 s per answer the screen spends over half
its time waiting during indexing; at 32 s the reads overlap the next change
and a reader connection is held throughout.

**Fix.** Count from a maintained table (`tag_counts`, or per-tag counters
updated where `asset_tags` is written) and recount only the tags a batch
touched; or cache the list in the store by revision, as `Events` is, and
recompute at most once per revision. With P1-1 in place a plain recount is
about 1.7 s, still too slow to repeat every 3 s.

### P1-3. `People` counts and picks covers over every face

[`Store.People`](../internal/store/people.go) joins every placed face to its
asset (`count(DISTINCT …)`, a temporary B-tree), then `addCovers` runs one
query per person that sorts all of that person's faces by `clearest` to take
one. The People screen reloads it every 3 s on people, assets or thumbnail
changes ([PeopleView.vue](../frontend/src/people/PeopleView.vue)).

**Fix.** As P1-2: counts kept per person, or cached by revision. Store each
person's cover choice (the user's pick or the computed clearest) when faces
are saved or clustered, rather than sorting on every read.

### P1-4. The timeline's day counts are recomputed from scratch on every change

[`Store.DayBuckets`](../internal/store/library.go) is
`GROUP BY strftime('%Y-%m-%d', taken_at, 'unixepoch')` over all visible
assets, with a temporary B-tree for the grouping: 190 ms plain. The library
store calls it for every `library:changed` event that says assets changed
([store.ts](../frontend/src/library/store.ts), `refresh` → `load`), and the
engine sends that up to once a second throughout scanning and the metadata
stage. Each answer is also a few thousand buckets serialised to the WebView
and a full rebuild of the grid layout.

Beside it, the engine's own report loop
([indexer.go](../internal/index/indexer.go), `reportLoop`) runs `JobCounts`
and `CountVisible` each time something changed. It limits itself to a fifth
of the time, so at 336 ms mid-index the progress figures are over a second
old; with blocks they are five or more.

**Fix.** Keep `day_counts (day, n)` in step with `assets` (the same places
that bump `visible_value`), or cache the buckets in the store by visibility
revision so repeated asks cost nothing. Derive the progress counts from
counters updated as jobs change state instead of counting 800,000 job rows.

### P1-5. Event grouping is a whole-library pass behind one mutex

[`Store.Events`](../internal/store/events.go) regroups every visible asset
whenever `event_revision` moved, which every metadata batch does (dates,
coordinates, places). It holds `eventsCache`'s mutex throughout, so every
caller queues behind one regroup: the Events screen, and any grid of an event,
whose `Timeline` and `Days` calls go through `Store.Event`. 310 ms plain,
3.4 s with blocks.

Two more costs sit beside it:

- `refreshEventCovers` runs one query per event whenever a thumbnail was
  saved (`cover_value`), under the same mutex. A ten-year library has
  thousands of events and the thumbnail stage saves a batch every second or
  two.
- `SetEventName` and `SetEventPinned` call `s.Event` **inside** the write
  transaction. The single writer connection is then held for the length of a
  regroup, and every indexer save waits behind it. The read also comes from
  the reader pool's snapshot, not the transaction's.

**Fix.** Resolve the event before opening the write transaction and pass its
bounds in. Regroup incrementally around the changed dates (the settler in
`eventtags.go` already finds the affected window) or debounce regrouping
while indexing. Refresh covers only for events whose date range holds a
changed thumbnail.

### P2-1. Clustering reloads and rebuilds every face on each run

[`people.Engine.recluster`](../internal/people/engine.go) reads every face
with its vector (`ClusterData`: 336 ms and about 60 MB for 120,000 faces),
then [`newClustering`](../internal/people/cluster.go) allocates a group with
three maps per face and compares every pair of people's means. It runs after
each quiet period and at least every 30 s while faces arrive, so indexing a
large library repeats work proportional to everything already indexed. The
`Linker` already avoids the quadratic face-to-face step; the load and the
rebuild are what remain.

**Fix.** Keep placed people as their sum vector and count between runs (a
person only changes when a face joins or leaves), and load vectors only for
unplaced faces and faces changed since the last run.

### P2-2. Every thumbnail request revalidates through Go and SQLite

[`serveOpened`](../services/media.go) answers with
`Cache-Control: private, no-cache`, though the URL already carries the
thumbnail's version (`/media/thumb/{id}/256?v=…`). A tile the WebView cannot
answer from memory is therefore a request through Wails' asset handler, a
visibility query (0.16 ms plain, 0.33 ms with blocks), a file open and a
conditional answer, where a cacheable response would cost nothing. The
service's own comment calls the thumbnails "browser-cached", which this
header prevents. How many tiles revalidate while scrolling was not measured
in the running app (the WebView may reuse images it still holds in memory);
count them in the DevTools network panel before changing anything.

The header exists for a reason: a blocked photo must not be shown from the
WebView's cache. That can be kept without revalidating everything.

**Fix.** Put the visibility revision in the URL beside the version and serve
`private, max-age=31536000, immutable`. A block changes the revision, so
every URL the UI builds afterwards is new, and the admission check still runs
for each new URL. Decide separately whether a cached blocked image left on
disk in the WebView's cache folder is acceptable; if not, clear the WebView's
HTTP cache when a block is added.

### P2-3. A day is the grid's paging unit, however large

The grid loads and reloads whole days. A day holding thousands of photos (an
import whose files carry no capture date all lands on one) is one array:
`DayItems` returned 5,952 items in 10 ms here, but the whole answer is
serialised to the WebView and replaced each time the day goes stale. While
thumbnails are made, `refresh` marks every loaded day stale once a second and
the grid asks again for what it shows, so that day is fetched whole each
second. `itemAt` and `indexOf` in the store search the day linearly on each
selection step, and `days` keeps every day ever scrolled past.

**Fix.** Page inside a day (offset and limit on `DayItems`, keyed by the row
range the virtualiser wants); on a thumbnail-only change send the ids and
versions that changed rather than invalidating days; drop days far from the
viewport.

### P2-4. SQLite's caches can exceed the idle memory target on their own

[`store.Open`](../internal/store/store.go) gives every connection
`_cache_size=-32000` (32 MB) and opens `max(4, NumCPU/2)` readers kept idle
forever, plus the writer. On a 20-thread PC that is eleven private caches,
up to 352 MB once the whole-library queries above have filled them, against a
500 MB idle target for the whole app (454 MB was measured at 3,000 photos).
The caches are not shared between connections, so the same pages are held
several times.

Related: a read that runs for seconds pins the WAL, which cannot be reset
while any reader is inside it. With `Things` at 6 to 32 s on a 3 s reload
there is nearly always one, so the WAL grows for as long as indexing writes.

**Fix.** A smaller per-connection cache with `mmap_size` set so the readers
share the operating system's pages; cap the readers nearer four to six;
`PRAGMA wal_checkpoint(TRUNCATE)` when indexing goes idle. Fixing P1-1 to
P1-5 removes the long readers.

### P2-5. Duplicates are regrouped from the whole library every 3 s

[`DuplicateGroups`](../internal/store/duplicates.go) reads every hashed
photo, makes 28 bucketing passes and rebuilds every group: 610 ms. The
Duplicates screen reloads it every 3 s while thumbnails are made. Its worst
case is quadratic: photos with identical hashes (thousands of black frames or
one app's screenshots) share a bucket in all 28 passes and every pair in it
is compared each time.

**Fix.** Cache by a revision of `phash` writes; cap the pairs compared within
one bucket, or collapse identical hashes to one representative before
pairing.

### P2-6. Shift-click and Select all send every id in the view

[`Store.Selection`](../internal/store/selection.go) returns the id of every
asset in the view (98 ms and 200,000 integers plain, 2.7 s with blocks), and
the store then runs `indexOf` over it twice and builds a `Set` of it. It is
kept between steps and only revalidated, which is the right idea; the first
shift-click still pays in full.

**Fix.** Ask the engine for the range between the two anchors, not the whole
view, and keep "all" as a predicate (view plus revision) until an action
needs the ids.

### P2-7. With XMP export on, every tag batch starts a whole-library pass

The XMP triggers bump `xmp_revision` for each row written to `asset_tags`
and each face placed with a person, and [`runXMP`](../internal/index/xmp.go) answers a moved
revision with a pass over every asset
([`XMPAssets`](../internal/store/xmp.go), which tests `visible` twice per
row). During indexing that is one pass per cooldown for the whole of the AI
stages. Off by default, so only users who turn it on meet it.

**Fix.** Record which assets changed (a dirty table filled by the same
triggers) and export those.

### P3. Headroom and smaller costs

- **Search scan.** [`Index.cos`](../internal/search/index.go) is a scalar
  loop per vector, and `allow` does one or two map lookups per row before it
  (`unavailable`, then the video set, which `video.assets()` rebuilds for
  every query). The 17 ms at 300,000 that the Phase 0 spike measured is fine; the map lookups are
  the first thing to go if it stops being so (a bitmap by index position).
- **Index growth.** `Index.Put` appends to one slice; loading 300,000 vectors
  row by row doubles a 230 MB slice several times. Size it from a count
  first.
- **Scanning.** `hashWorkers` is fixed at 4: slow for NVMe, and four
  concurrent readers thrash a spinning disk. Every file up to 64 MB is read
  whole to fingerprint it (the performance record's cold scan took 27 s for
  3.7 GB). Choose the worker count from the drive type.
- **Kept pixels.** [`decoded`](../internal/index/decoded.go) keeps up to 16
  photos decoded at 2048 px for the faces stage: about 150 MB (9 MB each)
  while both stages run, on top of the models. Intended, and bounded; worth
  knowing when reading a memory figure taken mid-index.
- **Exports and viewing renders** run one at a time (`Cache.rendering` has
  one slot, shared with `Inspect`). Right for the viewer; a batch export of
  hundreds of photos is single-threaded. Not measured.
- **Settings cache.** [`Store.setting`](../internal/store/store.go) caches
  only settings that exist. One never set (the tag confidence at its default)
  is queried again on every `Timeline` and `Days` call, though the comment
  says it is cached because it is "read on every scroll". Cache the absence
  too.
- **Bundle.** The main script is 473 KB and the text catalogue 314 KB, loaded
  from the embedded files at start. Cold start is already about a second, so
  this matters only if that target tightens.
- **`notifyDays`** copies the whole `days` map on each change. Small beside
  the reloads above.

## Implementation issues

Not performance; found on the way.

1. **Reads inside write transactions.** `SetEventName` and `SetEventPinned`
   (see P1-5) read through the reader pool while holding the writer. Beyond
   the stall, the read and the write see different snapshots, which is not
   what `AUDIT_CONVENTIONS.md` means by one snapshot per piece of work.
2. **Writes outside `Store.tx`.** `RemoveBlock`, `SetTagVectors`,
   `MakeLabel`, `SetTagPinned`, `SetPersonCover` and `setSetting` call
   `s.w.ExecContext` directly. None of them touches `assets`, so no event tag
   is left unsettled today, but the invariant "every write settles before it
   commits" then depends on each author knowing which tables the dirty
   triggers watch. Route them through `Store.tx`.
3. **`NOCASE` remains in three places** the conventions say it must not:
   the `UNIQUE COLLATE NOCASE` constraints on `sources.path`, `tags.name` and
   `people.name` (migrations 0001, 0003, 0004) and
   `ORDER BY name COLLATE NOCASE` in [`Store.Albums`](../internal/store/albums.go).
   The code compares with `fold()` first, so the constraints are a second,
   weaker rule rather than a bug; the album order sorts "Émile" after "Zoë".
4. **Statistics are refreshed only on a clean exit.** `PRAGMA optimize` runs
   in `Store.Close` alone. A library built in one long session, or one that
   always ends in a crash or a forced shutdown, plans with no statistics; the
   code already pins indexes with `INDEXED BY` in several places for this reason.
   Run it when indexing goes idle as well.
5. **No continuous integration.** There is no workflow in the repository:
   the gate (`scripts/lint.sh`, the race-enabled Go tests, the end-to-end
   suites) runs only when someone runs it. Windows-only cgo dependencies make
   hosted runners awkward, but lint, `vue-tsc` and Vitest need none of them.
6. **No performance regression test.** The benchmark harness of 29 September
   was deleted and the performance scripts in `e2e/perf` drive a 3,000-photo
   library. Every finding above is invisible at that size. A store-level
   benchmark on a seeded 100,000-photo database (the harness used here took
   97 s to seed 200,000) would have caught P1-1 to P1-4 when blocks, Things
   and People were added.
7. **Stale comment.** `MediaService`'s header says thumbnails are
   "browser-cached"; they are served `no-cache` (P2-2).

## What holds up

Worth saying, since it bounds where the work is:

- The job queue: the partial indexes of migrations 0007, 0008 and 0017 make a
  batch fetch 1 ms against a 200,000-job backlog.
- The write path: batched, prepared, one transaction per stage batch. The
  row triggers cost little (25 ms for an embedding batch that fires several
  of them per row).
- Grid reads of ordinary days (0.6 ms) and the layout built from counts
  alone, so the scroll height is right before any item loads.
- Thumbnail cache eviction walks two partial indexes, not the library.
- Politeness, the CPU budget and the GPU gate are deliberate and documented;
  indexing throughput is a chosen trade, not an accident.
- Layering and the Go-to-TypeScript contract are intact: nine `//nolint`
  lines, no `eslint-disable`, two `@ts-expect-error`, no stray `console`
  calls, lint at zero.

## Recommended order

1. **Materialise blocked assets (P1-1).** One schema migration and a change
   to three SQL fragments; every other figure improves with it. It needs the
   existing blocking tests as its oracle and a property test that the table
   always equals the predicate.
2. **Counts by revision for Things, People and the timeline (P1-2 to P1-4).**
   A store-level cache keyed by the revisions that already exist is the small
   version; maintained count tables are the lasting one.
3. **Take `s.Event` out of the write transactions (P1-5, issue 1).** A small,
   contained change.
4. **A seeded-library benchmark in the gate (issue 6)**, so the above stays
   fixed and the 100,000-photo acceptance has something to run.
5. Then P2 in the order listed, starting with thumbnail caching (P2-2), which
   is the one that touches scrolling.

Each of these changes behaviour under load and needs the regression test the
conventions ask for; none changes a wire type.
