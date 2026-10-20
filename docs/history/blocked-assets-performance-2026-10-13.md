# Materialised block coverage — 13 October 2026

This implements P1-1 from the bottleneck audit. The visibility rule is unchanged:
base visibility plus exclusion by photo-mode blocks. The expensive part of
that rule is evaluated when its inputs change, rather than for each asset of
each read. P1-2 to P1-5 and P2 are outside this change.

## Maintenance and privacy

Migration `0032_blocked_assets.sql` creates `blocked_assets`, keyed by asset id,
plus dirty ids and a singleton full-rebuild marker. Migration numbers come from
filenames, rather than their position in the directory; the gaps at 0024 and
0025 are preserved. No previously applied migration is changed.

`Store.tx` settles event tags first, then block coverage, then commits. Event
settling can add a newly indexed photo to a blocked event; settling coverage
afterwards hides that photo in the same commit. Readers retain their previous
WAL snapshot until the commit and cannot observe an intermediate answer. A
settlement error rolls back the inputs, membership and coverage together.

`blockMatch` remains the only definition of coverage. It generates the SQL
used for settlement and still serves the Blocked screen and coverage queries
for both modes. `blockVisible` checks once whether the table is empty, then
probes `blocked_assets` by integer primary key when necessary. There is no
read-time dirty fallback or background maintenance queue.

Common dirty-id writes extend the existing revision triggers and their guards.
Each journal row also accumulates the original event and visibility revision
increments. Settlement pays their exact totals once before commit, instead of
writing a revision counter and a dirty marker for every tag or face row.
Asset insertion and deletion keep their original direct revision update when
there is no photo block, so those writes need no coverage journal.
Separate triggers cover inputs those triggers did not name. The guards select
potentially affected assets; they do not define the answer. The original
predicate evaluates that answer before commit.

| Input | Maintenance |
| --- | --- |
| Asset insertion or deletion | That asset; deletion also cascades its materialised row |
| Place, favourite, kind or capture date | That asset when a photo block can depend on the changed input |
| Tags: insertion, removal, source, score, tag id or asset id | Old and new asset ids for a relevant tag or smart-album block |
| Faces: insertion, removal, person assignment, asset id or identity | Old and new asset ids for relevant person or smart-album blocks |
| Files: insertion, removal, path or asset id | Old and new asset ids while a folder block exists |
| Manual album items and event members | Old and new asset ids for the relevant photo-mode block |
| Face-block membership | Stored old/new asset ids and the referenced faces' owners; smart person albums also depend on label-mode face coverage |
| Any block row inserted, changed or removed | Full rebuild, including mode and target changes |
| A blocked album's rule, kind or identity; creation of a referenced album | Full rebuild |
| Relevant tag-confidence value changes, deletion or key changes | Full rebuild |

The migration marks existing libraries for a rebuild. `Open` backfills before
creating its reader pool. If the process stops between migration and backfill,
the persisted marker makes the next open retry. Visibility revision changes
alone do not request a rebuild. Thumbnail, embedding-vector and unrelated
metadata writes do not dirty coverage.

Settlement evaluates each dirty asset once, retains its answer temporarily in
the dirty table, then uses that answer for removal and insertion. When none of
the dirty ids already has a materialised row, only insertion is needed: matches
are inserted directly without temporary answers or removals. Full rebuilds
replace the dirty set with the original predicate's matching ids. Settlement
retains unchanged rows, removes obsolete matches and inserts new matches.
Actual membership changes also advance visibility and event revisions,
covering gaps in the old revision triggers, including asset kind and label-mode
face coverage used by a smart person album. Unchanged matches do not add a
settlement-driven invalidation.

The private writer facade routes standalone `ExecContext` writes through
`Store.tx`, including block removal, settings and album deletion. Schema
migrations deliberately use `runTx` without settlement. Batch admission settles
coverage before its next check, so an earlier mutation that blocks a repeated
id is respected. No wire type, service signature or binding changes.

`writeSettings`, which saves several preferences in one statement, previously
ran without settlement. It now goes through `Store.tx`, so a setting that
feeds the predicate cannot commit ahead of its coverage whichever path saves
it. `TestSettingsWrittenTogetherSettleBlockedAssets` fails without that.

## Benchmark method

The permanent `BenchmarkLargeLibraryBlocking` in `internal/store` seeds scratch
stores through the real schema and triggers. It runs only when explicitly
selected as a benchmark. It has 200,000 photos over ten years, one tenth on one
day, one file and four completed jobs per photo, 1.6 million AI tag rows across
180 Things, 120,000 faces across all 500 people, and 200 places. A seed assertion
checks that face assignment spans all 500 people.

Configurations are no blocks, one folder block covering 0.1% of photos, and
that folder plus one tag and one person block, all in photo mode. Each read is
warmed before measurement; Events explicitly invalidates its cache so every
measurement regroups. Writes use 64 results, 12 tags per embedding, and an
ingest batch of 128. Ingest cleanup is outside its timer. OCR is enabled with
real eligibility tags, so QueueOCR measures its library scan.

Before measurements use an isolated checkout of `main` at `d001c79`, with the
same benchmark file. The machine is an i7-13700H running Go 1.27.2 on Windows.
There is no race detector in benchmarks. The original audit's deleted fixture
is unavailable; these are fresh comparable measurements, rather than a claim
to reproduce its exact timings.

The first baseline was recorded before changing production code. Subsequent
fixture corrections enabled real OCR eligibility and spread faces across all
500 people, rather than inadvertently assigning only 300. The baseline was
repeated against unchanged `main` with those corrections before comparison.

```bash
source scripts/env.sh
go test -run '^$' -bench BenchmarkLargeLibraryBlocking -benchtime=3x -count=1 -timeout 900s ./internal/store
```

The write comparison alternates the unchanged-main and changed-store test
executables on the same logical CPU, reversing their order on each sample.
Both use the permanent benchmark's scratch seed and save inputs, with
`GOMAXPROCS=1`. Means exclude one warm-up and include 100 saves per cell, or 20
QueueOCR calls. Only the measurement processes have their affinity changed.
This gives a longer write sample without overlapping the race or lint gates.

## Read results

Milliseconds per call, before → after; each value is the benchmark's mean of
three warmed iterations. These runs were sequential, without other test gates.

| Call | No blocks | Folder | Folder + tag + person |
| --- | ---: | ---: | ---: |
| Things | 718.20 → 1,189.20 | 3,158.78 → 1,035.41 | 15,209.10 → 1,898.80 |
| People | 304.27 → 419.86 | 733.89 → 492.28 | 1,411.25 → 1,059.83 |
| DayBuckets | 106.22 → 132.81 | 378.80 → 157.80 | 680.76 → 203.98 |
| CountVisible | 18.42 → 15.67 | 280.28 → 36.34 | 592.27 → 52.88 |
| Events regroup | 168.54 → 199.88 | 513.14 → 247.61 | 1,150.01 → 481.13 |
| Selection | 59.66 → 63.90 | 377.78 → 118.15 | 1,063.59 → 176.54 |
| UnavailableAssets | 16.98 → 13.21 | 283.51 → 43.30 | 705.00 → 71.83 |
| Places | 19.32 → 14.09 | 382.86 → 59.31 | 1,249.34 → 126.14 |

One folder block is 1.49–7.71 times faster; three blocks are 1.33–11.20 times faster. No-block results vary in both directions; these short sequential samples do not establish identical no-block performance.

A second comparison alternated unchanged-main and changed-store calls on one logical CPU with `GOMAXPROCS=1`, reversing order on each sample. It used three timed samples after a warm-up for every cell, and applied the save inputs between configurations as the permanent benchmark does. This checks the substantial timing variability in the short sequential run without discarding its results.

| Paired read | No blocks | Folder | Folder + tag + person |
| --- | ---: | ---: | ---: |
| Things | 1,187.17 → 1,191.41 | 4,331.80 → 1,069.84 | 22,633.16 → 1,216.72 |
| People | 417.91 → 375.32 | 1,307.38 → 452.26 | 1,987.21 → 726.05 |
| DayBuckets | 163.24 → 139.64 | 574.86 → 200.23 | 861.46 → 151.68 |
| CountVisible | 24.59 → 13.12 | 349.25 → 27.19 | 711.36 → 35.37 |
| Events | 280.89 → 265.87 | 741.43 → 272.16 | 1,414.26 → 237.61 |
| Selection | 96.48 → 76.57 | 505.03 → 99.71 | 1,220.32 → 110.00 |
| UnavailableAssets | 27.70 → 13.78 | 344.70 → 26.68 | 867.55 → 39.90 |
| Places | 27.99 → 14.09 | 553.06 → 39.70 | 1,356.00 → 55.94 |

In the paired sample, one block is 2.72–13.93 times faster and three blocks
are 2.74–24.24 times faster. Things without blocks is essentially unchanged
(+0.36%); the other no-block calls improve.

## Write results

Milliseconds per call, before → after; these are the longer alternating means described above.

| Call | No blocks | Folder | Folder + tag + person |
| --- | ---: | ---: | ---: |
| SaveEmbeddings64 | 18.757 → 19.968 (+6.5%) | 20.842 → 20.793 (-0.2%) | 26.954 → 27.719 (+2.8%) |
| SaveTags64 | 19.964 → 18.781 (-5.9%) | 18.563 → 18.133 (-2.3%) | 25.674 → 26.502 (+3.2%) |
| SaveFaces64 | 3.036 → 3.311 (+9.1%) | 2.124 → 2.201 (+3.6%) | 2.327 → 2.281 (-2.0%) |
| SaveThumbnails64 | 2.657 → 2.648 (-0.3%) | 2.686 → 2.802 (+4.3%) | 3.197 → 3.388 (+6.0%) |
| QueueOCR | 178.711 → 186.412 (+4.3%) | 246.931 → 225.125 (-8.8%) | 230.355 → 229.145 (-0.5%) |
| Ingest128 | 7.363 → 7.830 (+6.3%) | 16.763 → 18.769 (+12.0%) | 14.360 → 17.481 (+21.7%) |

The original audit quoted 25 ms for 64 embeddings, 3.3 ms for thumbnails and 17 ms for QueueOCR. This fixture scans OCR eligibility across the full seeded library, and its unchanged-main OCR baseline is much higher. Those historical absolute figures are not a like-for-like write comparison.

Ingest changes by +0.47 ms per 128 photos without blocks, +2.01 ms with one block, and +3.12 ms with three (+6.3%, +12.0%, +21.7%). These remaining relative increases must not be described as identical write performance. The coverage writes and transactional privacy guarantees remain synchronous.

### Full rebuild

A block being added, changed or removed, a blocked album's rule changing and a
tag-confidence change each re-evaluate every photo while the transaction holds
the writer. `RebuildBlocks` measures that whole cost, as the mean of five
iterations on the 200,000-photo fixture.

| Call | No blocks | Folder | Folder + tag + person |
| --- | ---: | ---: | ---: |
| RebuildBlocks | 0.10 | 534.31 | 914.89 |

The first open after this migration pays the same cost once. Indexing writes
wait behind a rebuild for that long; reads continue on their snapshot.

## Verification

- `go test -race -count=1 -timeout 900s ./internal/store` passed; the final recorded store gate took 124.395s.
- `go test -race -count=1 -timeout 900s -p 1 . ./internal/... ./services/...` passed. The slowest package, lumen/services, took 167.797 s. Package concurrency was reduced for the final rerun; no test or assertion was disabled.
- `scripts/lint.sh` passed with zero Go issues, 594 frontend tests in 58 files, and four installer regression tests. The vulnerability check found no reachable vulnerability; it reported four in required modules that the code does not call.
- The oracle checked 2,772 mutations across three deterministic seeds and 77 input operations. It compared both materialised coverage and visible-photo sets with the original predicate before and after event settlement within the transaction, and after commit or rollback.
- A revision regression checks exact original counter totals, including old/new asset owners and a full rebuild in the same transaction.
- Additional regressions cover backfill, rollback on settlement failure, local maintenance, WAL snapshot isolation, revision invalidation and repeated batch admission.
- Restoring only the original `blockVisible` in a copied-aside `store.go` made the query-plan regression fail with the original correlated block probes. The file was restored afterwards; no stash was used.
- `blocks_test.go`, `visibility_test.go`, `eventblocks_test.go`, `faceblocks_test.go`, `coverage_test.go` and `block_pin_test.go` remain unchanged.

Two preceding full runs failed `TestWatchReportsChangesAndReleasesHandle` after its 30-second wait. Its focused race rerun passed in 3.037 s. The unchanged watcher starts its read goroutine asynchronously; its first notification can race the fixture write. This separate scanner behaviour was not changed.

The full suite skipped four face-corpus tests because `LUMEN_FACE_PHOTOS` was
unset, one camera-corpus test because `LUMEN_RAW_SAMPLES` was unset, and the
symlink fixture because the process lacks the required Windows privilege.
The crash and CPU-spin child-only helper tests skip in the parent process as
designed. WIC and testenv have no standalone tests. The store gate had no skips.
No installed-app, real-library or clean-machine acceptance is claimed here.

The existing downgrade fixture now restores pre-0032 trigger definitions from
a scratch schema before removing the new tables. The metadata snapshot test
injects its concurrent mutation through a metadata view, since the optimised
visibility query no longer calls `fold`. Their assertions are preserved.

## Existing predicate discrepancy preserved

An unreadable stored tag confidence, such as `broken`, becomes zero through
SQL `CAST(value AS REAL)`, while `Store.Float` falls back to the default 0.02.
Consequently the original blocking predicate covers an AI tag scored 0.001 in
that case even though the setting reader reports the default. This discrepancy
is preserved explicitly by `TestMaterialisedBlocksPreserveTheOriginalUnreadableConfidenceRule`;
the privacy rule is not silently corrected in this performance change.
