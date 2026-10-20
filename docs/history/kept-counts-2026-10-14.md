# Kept counts and three smaller items, 14 October 2026

The [bottleneck audit](bottleneck-audit-2026-10-13.md)'s remaining P1 items
were the counts behind Things (P1-2), People (P1-3), the timeline and the
progress report (P1-4). Each was recounted over the whole library on every
read, and the screens read them every few seconds while indexing. They are
now kept in tables. This round also sizes the search index before loading
it, chooses the number of fingerprinting workers from the drive, and
refreshes SQLite's statistics when indexing goes idle (P3 and implementation
issue 4).

Figures marked "gate" are `BenchmarkLargeLibraryStore` on 100,000 photos,
i7-13700H, no race detector, five samples of three calls each, in
milliseconds. "Before" is the budget file's previous samples.

## One mechanism for four counts

Migration 0037 adds `day_counts` (visible photos per day, with the
favourites and videos among them), `tag_counts`, `person_counts` (with
`person_counts_faces`, the faces that can be shown, which also ranks each
person's cover) and `job_counts` (unfinished jobs per stage and kind, and
assets with a failed or skipped stage, each counted once). Triggers keep
them in the transaction that changes their inputs: visibility (`assets`'
three columns, inserts and deletes, and `blocked_assets`), dates and kinds,
tag matches and the tag confidence, faces, face blocks and people, and job
states. An asset's deletion is subtracted before its rows cascade away.

The queries the counts replaced stay in the store as each table's recount,
built from the same fragments as the remaining fallbacks (`visible`,
`tagAppliesTo`, `faceShown`), so a predicate is still spelled once in Go.
`count_rebuild` names a table to recount before commit; while a table is
named, its triggers do nothing. The migration creates the tables empty and
names all four, and the empty transaction `Open` already runs fills them, as
migration 0032 fills `blocked_assets`. A change to the tag confidence names
`tag_counts`.

`Things` uses the kept counts when it is asked at the stored confidence
(always, from the app) and its old query otherwise; covers are probed as
before, since 180 Things cost 2.5 to 4 ms. `DayBuckets` uses them for the
whole timeline, favourites and videos; a Thing, a person, an album, a place,
an event or the Trash keeps its query.

| Read (gate)            | Before, no blocks | After, no blocks | Before, three blocks | After, three blocks |
| ---------------------- | ----------------- | ---------------- | -------------------- | ------------------- |
| `Things`               | 312.7–360.1       | 2.67–3.17        | 388.2–411.3          | 3.86–4.42           |
| `People`               | 169.8–180.8       | 4.41–5.18        | 221.4–250.3          | 7.78–10.79          |
| `DayBuckets`           | 57.1–59.1         | 1.62–2.23        | 66.6–68.1            | 2.38–3.19           |
| `CountVisible`         | 11.0–11.4         | 0.20–0.22        | 20.4–21.5            | 0.29–0.41           |
| `JobCounts`, backlog   | 111.3–115.5       | 0.02–0.04        | 111.8–114.9          | 0.03–0.09           |
| `JobCounts`, done      | 40.0–41.2         | 0.02–0.04        | 47.3–50.0            | 0.02–0.04           |

## What it costs to write

| Write (gate, no blocks) | Before      | After       |
| ----------------------- | ----------- | ----------- |
| `Ingest` 128 photos     | 9.8–12.8    | 17.0–20.3   |
| 64 tag results          | 15.0–22.2   | 21.8–25.3   |
| 64 embeddings           | 18.9–22.3   | 28.1–30.0   |
| 64 face results         | 2.11–2.36   | 3.50–3.69   |
| 64 thumbnails           | 3.29–3.42   | 4.29–5.06   |

Most of the ingest cost is the job counts: a trigger per queued job, about
35 microseconds a photo, or about four seconds over a 100,000-photo scan.
Splitting the job trigger into one statement per case did not change it.
`Ingest128`'s ceilings were raised to 70, 80 and 200 ms by the budget file's
rule from those samples; the other writes stay inside their ceilings.

A change to the visibility of many photos at once would fire the triggers
for each. A full recount of all four tables costs about 0.7 s at 100,000
photos, and the triggers about 23 microseconds a photo, so a write about to
change at least 32,000 photos names all four tables first and they are
recounted once: a block's settlement (counting the rows it will add and
remove), marking a removed source's photos missing, and trashing or
restoring a selection or the whole Trash.

| Block and unblock a folder (own benchmark) | No kept counts | Triggers only | As built    |
| ------------------------------------------ | -------------- | ------------- | ----------- |
| 50,000 of 100,000 photos                   | 0.33–0.39 s    | 2.7–3.0 s     | 1.55–1.57 s |
| 99,900 of 100,000 photos                   | 0.54–0.64 s    | 4.8–4.9 s     | 1.5–2.1 s   |

## Proof

Each table has a randomised test that compares it with its reference query
in the same snapshot after thousands of steps over every input it reads
(every block kind in both modes, deletions, trash, missing files, tag and
face edits, job states, rollbacks), run once on the trigger path and once
with the bulk limit lowered so the recount path is taken. Others cover
readers beside a writer, deletion through cascades, unchanged inputs doing
no work, the confidence default the triggers spell as a literal, a failed
settlement rolling back, and the migration on a populated version-36
library. Running the forced-recount tests found that a face whose id changed
while its table awaited a recount kept a stale row; `person_counts_faces`
now follows the id.

## The three smaller items

**Search index.** Loading appended 300,000 vectors one at a time to slices
of about 230 MB. The store now counts a model's vectors in the snapshot it
reads them from, and the index sizes its slices and map once.

| Load (own benchmark) | Allocated before | After  | Time           |
| -------------------- | ---------------- | ------ | -------------- |
| 100,000 vectors      | 489 MB           | 165 MB | about the same |
| 300,000 vectors      | 1,496 MB         | 498 MB | about the same |

**Fingerprinting workers.** Four were used for every drive. A scan now asks
Windows whether its folder's volume has a seek penalty: a spinning disk gets
one worker, a drive without one gets eight (held to the CPU thread budget),
and a drive it cannot ask about keeps four. On this NVMe drive, a cold scan
of 204 photos (510 MB) took a median 949 ms with four workers and 667 ms
with eight; more varied without gaining. A spinning disk was not available
to measure.

**Statistics.** `PRAGMA optimize` ran only in `Close`. It now also runs when
indexing goes idle, before the WAL checkpoint, on its own connection with no
busy wait (shared with `Checkpoint`), with `analysis_limit=1000`. On a fresh
100,000-photo library the first run took 90 to 166 ms, later ones 18 to
29 ms.

## How the work was done

Four engineers each built one count in a separate checkout to one shared
design; a fifth did the three smaller items. Their changes were then made
into the one mechanism above. Not run this round: the end-to-end suites, and
a real library opened with migrations 0032 to 0037.
