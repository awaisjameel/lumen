# Benchmark and performance audit — 29 September 2026

Branch `fixes/refactore` (00351e5, the [29 September audit](audit-2026-09-29.md))
measured against `main` (ffd27d5). Measurements cover four levels:

- the store (SQLite) at 100,000 assets;
- the real app in WebView2, indexing and in use;
- CPU, block and mutex profiles of the engine;
- decode micro-benchmarks for the media pipeline.

Each result says whether it was measured or found by reading code. Code-reading
findings are listed separately at the end and are not confirmed.

## Summary

- **The UI got clearly better.**
  - Accessibility errors fell from 349 elements to 16 (−95%).
  - Scrolling no longer drops frames: the slowest 5% of frames went from 34 ms to 17.5 ms.
  - Opening a photo, stepping to the next one and the first search are 26–62% faster on real photos.
  - The window and the first thumbnail appear about 30% sooner on a warm start.
- **The UI does far less work while indexing.** It makes 69–80% fewer calls to
  the engine, reloads the timeline 66–82% less often, and downloads 89% fewer
  thumbnails again. WebView2 used 20–32% less CPU.
- **Indexing takes about as long as before.**
  - 3,000 photos: 235 s against 240 s (−2%).
  - 204 real photos: slower on the branch in the one fair run (51 s against 42 s). The profiled runs without the UI took 48 s on both.
  - The engine used 6–22% more CPU on the branch. Profiles of the first minute did not show where the extra goes.
- **The job queue is much faster:** fetching the next batch went from 39.5 ms to
  0.18 ms at 100,000 assets.
- **Four store operations got slower**, all traced to a cause:
  - folder rescans (+920%);
  - recording new files (+71%);
  - fetching a batch for a stage that waits on an earlier one (+105%);
  - Thing name suggestions (+275%).

  Prototype fixes bring the first two and the Thing suggestions back to main's
  speed. The blocked-stage fetch needs a small queue redesign.

- **The biggest remaining costs are the same on both branches.**
  - A new library spends 28–52 s encoding the built-in Things' text before any photo is embedded.
  - The faces stage decodes every original a second time.
  - Thumbnails take close to half of all indexing CPU.
  - The Duplicates view is not virtualized.
  - Scrolling the timeline makes one engine call for about every four rows.

## Method

### Machine

| Component | Detail                                                                  |
| --------- | ----------------------------------------------------------------------- |
| CPU       | Intel Core i7-13700H, 20 threads                                        |
| Memory    | 31.5 GB                                                                 |
| OS        | Windows 11 Pro 10.0.26200, on mains power                               |
| AI        | SigLIP image model on DirectML (GPU); text model and face models on CPU |
| Toolchain | Go 1.27.1, llvm-mingw, Wails v3.0.0-beta.26, Node 24.21                 |

Both apps were built with `wails3 build` from their own trees, with the same
toolchain and the same `deps/runtime`. Nothing else heavy ran during timed
runs. The two trees were always run alternately, never at the same time.

### Store benchmarks

- 34 Go benchmarks in `package store`. One file compiled in both trees; a small
  shim covered the four signatures that differ (`CreateAlbum`, `KnownFiles`,
  `SaveEmbeddings`, `SaveFaces`).
- The synthetic library has 100,000 assets spread over 12 years, 4% of them videos:
  - all stages indexed;
  - 60 Things with 4 AI tags per asset;
  - 30% of assets with 1–3 faces, grouped into 400 people, 100 of them named with an accent ("Émile");
  - 5% favourites;
  - 10 manual albums of 500 items and 5 smart albums.
- A second copy, `fresh.db`, holds the same library straight after scanning, with
  every job pending. Each benchmark opens a private copy of one of the two.
- 5 alternating rounds per tree at `-benchtime 1s`, compared with `benchstat`.
  "~" means the difference is not significant (p ≥ 0.05).

### Real-app runs

- **Corpora:**
  - **office:** 204 real JPEGs, 5184×3456, 487 MB, many faces, no ICC profiles.
  - **synth:** 3,000 generated JPEGs, 2000×1500, 3.7 GB, dates in the file names across 2015–2025, no faces or EXIF.
- **Driver:** Node over the DevTools protocol (`LUMEN_DEVTOOLS_PORT`), with a scratch
  `LUMEN_DATA_DIR` and one source seeded with `store.AddSource`. AI on battery was
  allowed, but the machine was on mains.
- **Recorded:**
  - job counts per stage, read once a second from the database (read-only);
  - every engine call by method, and every thumbnail request, from the DevTools network log;
  - CPU seconds and working set of Lumen.exe and its WebView2 processes, every 2 s.
- **Index run:** start from an empty library and wait until nothing is pending and
  the status reads "Up to date". Then the UI suite runs.
- **Warm run:** start again on the indexed library. Measure window, first
  thumbnail and the point where the network goes quiet, then run the UI suite.
- **UI suite, in this order:**
  1. 20 s idle.
  2. Scroll the whole timeline, 400 px per animation frame.
  3. Jump to the middle.
  4. Open the viewer and step through 15 photos.
  5. Click each sidebar view.
  6. Two searches ("red", then "green circles").
  7. axe-core 4.13 on the viewer and on every view.
- **Runs:** 2 index runs and 2 warm runs per tree per corpus. Index runs alternated
  which tree went first. Tables show medians. With two runs, differences under
  about 10% are within noise.

### Profiles

Separate builds added `net/http/pprof` behind an environment variable (not
committed). Captures:

- 45 s of CPU from 1 s after launch (office);
- 60 s of CPU from 5 s (synth);
- block and mutex profiles over the same windows.

The UI was not driven during these runs.

## Store (100,000 assets)

Time per operation, median of 5. Significance by Mann-Whitney U; n = 5 gives
p = 0.008 at best.

| Operation                                        | main     | branch   | Change         | p     |
| ------------------------------------------------ | -------- | -------- | -------------- | ----- |
| Fetch next metadata batch (fresh library)        | 39.5 ms  | 0.176 ms | **−99.6%**     | 0.008 |
| Fetch next batch inside a focused date range     | 11.9 ms  | 0.193 ms | **−98.4%**     | 0.008 |
| Metadata batch cycle (fetch 64, save 64)         | 69.7 ms  | 21.8 ms  | **−68.7%**     | 0.008 |
| Things list with covers                          | 515 ms   | 270 ms   | **−47.6%**     | 0.008 |
| Fetch thumbnail batch while metadata is pending  | 41.6 ms  | 85.3 ms  | **+105%**      | 0.008 |
| Ingest 500 new files                             | 33.8 ms  | 57.7 ms  | **+71%**       | 0.008 |
| Known files of one folder (watcher rescan)       | 1.01 ms  | 10.3 ms  | **+920%**      | 0.008 |
| Search suggestions ("thi" Things + "émi" people) | 0.111 ms | 2.48 ms  | +2139% (below) | 0.008 |
| Known files of a whole source                    | 109 ms   | 131 ms   | ~              | 0.095 |
| Job counts (progress)                            | 44.1 ms  | 51.7 ms  | ~              | 0.548 |
| Count pending, all stages                        | 103 ms   | 102 ms   | ~              | 0.548 |
| Requeue outdated (nothing to do)                 | 39.7 ms  | 34.9 ms  | ~              | 0.310 |
| Timeline buckets, everything                     | 83.0 ms  | 86.5 ms  | ~              | 1.000 |
| Timeline buckets, favourites                     | 15.2 ms  | 14.7 ms  | ~              | 1.000 |
| Timeline buckets, one Thing                      | 9.73 ms  | 9.66 ms  | ~              | 0.841 |
| Timeline buckets, one person                     | 0.322 ms | 0.318 ms | ~              | 0.841 |
| Timeline buckets, smart album                    | 5.11 ms  | 4.85 ms  | ~              | 0.421 |
| Day items, one month                             | 1.40 ms  | 1.39 ms  | ~              | 0.421 |
| Grid items, 200 ids                              | 0.345 ms | 0.327 ms | ~              | 0.841 |
| Details panel (asset, tags, faces, albums)       | 0.129 ms | 0.111 ms | ~              | 0.690 |
| Asset file lookup                                | 21 µs    | 46 µs    | ~              | 0.151 |
| People list                                      | 303 ms   | 274 ms   | ~              | 0.421 |
| One person                                       | 0.360 ms | 0.400 ms | ~              | 0.421 |
| One person's faces                               | 0.808 ms | 0.910 ms | ~              | 0.841 |
| Albums list                                      | 51.1 ms  | 58.6 ms  | ~              | 0.841 |
| Duplicate groups                                 | 230 ms   | 245 ms   | ~              | 1.000 |
| Group duplicates (existing benchmark)            | 240 ms   | 256 ms   | ~              | 0.310 |
| Scan all embeddings                              | 211 ms   | 245 ms   | ~              | 0.841 |
| Unavailable assets                               | 6.27 ms  | 7.56 ms  | ~              | 0.421 |
| Cluster data (all faces with vectors)            | 187 ms   | 214 ms   | ~              | 0.690 |
| Set favourite                                    | 20.7 µs  | 23.1 µs  | ~              | 0.310 |
| Tag and untag                                    | 181 µs   | 230 µs   | ~              | 0.095 |
| Read a float setting                             | 8.5 µs   | 7.5 µs   | ~              | 0.548 |
| Open the database                                | 1.88 ms  | 1.60 ms  | ~              | 0.421 |
| **Geometric mean**                               |          |          | **−8.6%**      |       |

Allocations per operation grew in:

- Suggestions: 97 → 2,884, one per row passed to `fold()`.
- Tag and untag: 39 → 92.
- Things: 1,285 → 2,781.
- Metadata batch cycle: 2,054 → 2,957.
- Asset file lookup: 37 → 76. It now returns every copy of the file.
- People: 11.3k → 14.6k.

Across all benchmarks, allocations are +22% (geometric mean).

Building the seed libraries took 29 s on main and 45 s on the branch. The two
ran at the same time, so this is only a hint that writes got slower. The
ingest and trigger results below are the measured version.

### What the regressions come from

| Regression                       | Cause                                                                                                                                                                                                                                        |
| -------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Folder rescan +920%              | `KnownFiles` gained `ORDER BY id`. SQLite then uses `files_source` and reads every file of the source, instead of the path range on the unique path index. Checked with `EXPLAIN QUERY PLAN`.                                                |
| Ingest +71%                      | The migration 0007 insert trigger runs an `UPDATE` for every new `asset_jobs` row: 4 per asset, and each one moves an entry in the partial index. The update trigger also fires whenever `taken_at` is in a `SET` list, even when unchanged. |
| Blocked-stage fetch +105%        | `PendingJobs` is pinned to `asset_jobs_pending`. When nothing is ready (thumbnails while metadata is still pending), it walks every pending row of the stage and checks each one against earlier stages.                                     |
| Thing suggestions 48 µs → 180 µs | Names now match with `instr(fold(name), ?)`, which calls Go once per row through cgo.                                                                                                                                                        |
| People suggestions               | Mostly correctness, not speed. Main's `LIKE` could not match "émi" to "Émile" and returned nothing in 0.1 ms. The branch finds the 8 matches and looks up a cover for each, about 0.2 ms apiece.                                             |

Suggestions split out (3 runs each):

| Query                     | main                  | branch       | prototype fix |
| ------------------------- | --------------------- | ------------ | ------------- |
| Things, "thi" (ASCII)     | 43–50 µs              | 168–223 µs   | 53–57 µs      |
| People, "per" (ASCII)     | 1.21–1.38 ms          | 1.62–1.87 ms | 1.43–1.56 ms  |
| People, "émi" (non-ASCII) | 76–125 µs (0 results) | 1.60–1.64 ms | 1.73–3.31 ms  |

People suggestions are dominated by the per-person cover query on every branch.

### Prototype fixes (measured, not applied)

The prototype was made in a scratch worktree of the branch. `go test ./internal/store/` passes with it, including the queue plan test. The diff is `D:\tmp\lumen-bench\prototype-store-fixes.diff`:

1. `KnownFiles` with a folder uses `WHERE +source_id = ?`, so SQLite keeps the
   path index and sorts the few rows it finds.
2. `Ingest` writes `taken_at` in the job `INSERT`. The insert trigger gains
   `WHEN NEW.taken_at = 0`, the update trigger `WHEN NEW.taken_at IS NOT OLD.taken_at`.
3. Name matching uses `LIKE` when the typed text is ASCII (SQLite's `LIKE`
   folds A–Z) and `fold()` otherwise.

The diff edits migration 0007 in place, which is only acceptable because 0007
has not reached main. The `LIKE` path still needs `%` and `_` in typed text
escaped. `core.Fold` folds a few non-ASCII letters to ASCII (the Kelvin sign,
long s), which `LIKE` won't match.

| Operation (branch → fix)       | branch  | fix     | Change | p     |
| ------------------------------ | ------- | ------- | ------ | ----- |
| Ingest 500 new files           | 50.3 ms | 35.7 ms | −28.9% | 0.032 |
| Known files of one folder      | 9.55 ms | 0.90 ms | −90.6% | 0.008 |
| Fetch thumbnail batch, blocked | 64.2 ms | 69.7 ms | ~      | 0.841 |
| Metadata batch cycle           | 22.5 ms | 19.8 ms | ~      | 0.421 |
| Suggestions (mixed, as above)  | 1.84 ms | 1.85 ms | ~      | 1.000 |

Main's figures were 33.8 ms and 1.01 ms, so the first two regressions are
fully recovered. The combined suggestion benchmark shows no change because
its accented people query takes the `fold()` path; the split table above shows
the gain for Things.

## Real app: indexing

### 3,000 synthetic photos (medians of 2)

| Measure                     | main    | branch  | Change   |
| --------------------------- | ------- | ------- | -------- |
| Window shown                | 1.73 s  | 1.39 s  | −19%     |
| First thumbnail in the grid | 9.73 s  | 4.72 s  | **−51%** |
| Metadata finished           | 6.95 s  | 6.39 s  | −8%      |
| Thumbnails finished         | 98.4 s  | 107.9 s | **+10%** |
| Faces finished              | 154.8 s | 159.8 s | +3%      |
| Image embeddings finished   | 239.4 s | 234.4 s | −2%      |
| Up to date                  | 239.9 s | 235.4 s | −2%      |
| Engine calls from the UI    | 501     | 157     | **−69%** |
| — Timeline reloads          | 149     | 51      | −66%     |
| — Days loads                | 202     | 51      | −75%     |
| Thumbnails requested again  | 117     | 13      | **−89%** |
| CPU, whole process tree     | 1,489 s | 1,556 s | +5%      |
| — Lumen.exe                 | 1,405 s | 1,490 s | +6%      |
| — WebView2                  | 83 s    | 67 s    | −20%     |
| Peak working set, tree      | 3.61 GB | 3.59 GB | −1%      |
| — Lumen.exe                 | 2.14 GB | 2.10 GB | −2%      |
| — WebView2                  | 1.82 GB | 1.81 GB | 0%       |

Throughput was about the same on both:

- Thumbnails: about 30 photos a second.
- Faces: about 20 a second.
- Image embedding: about 18 a second. It started only at about 55–60 s, once the Things' text vectors were ready (see bottleneck 1).

The process tree averaged about 6.2 of 20 hardware threads (1,489 CPU-s over 240 s).

### 204 real photos, per run

The first run of main hit a cold file cache. Its scan trickled files in over
40 s, so its scan-dependent numbers are not comparable. Run 2 had a warm cache
on both trees and is the fair comparison.

| Measure             | main run 1 (cold) | main run 2 | branch run 1 | branch run 2 |
| ------------------- | ----------------- | ---------- | ------------ | ------------ |
| Window shown        | 2.48 s            | 0.76 s     | 0.94 s       | 1.22 s       |
| First thumbnail     | 4.74 s            | 2.82 s     | 4.03 s       | 3.76 s       |
| Thumbnails finished | 3.5 s             | 6.9 s      | 10.3 s       | 8.9 s        |
| Faces finished      | 8.1 s             | 30.6 s     | 45.4 s       | 46.0 s       |
| Up to date          | 87.4 s            | 42.0 s     | 54.6 s       | 51.1 s       |
| Engine calls        | 106               | 57         | 14           | 19           |
| CPU, process tree   | 414 s             | 368 s      | 473 s        | 466 s        |
| Peak working set    | 3.94 GB           | 4.02 GB    | 4.02 GB      | 4.13 GB      |

- **Faces:** under the UI harness the branch's faces stage took about 37 s
  against 24 s, in both of its runs. Its 32-photo batches alternated between
  about 4 s and 8 s, where main's were a steady 4 s. The log also shows the Things'
  text vectors taking longer: "search ready" at 35.6 s against 27.6 s. That work
  runs on the CPU at the same time as faces.
- **The profiled runs disagree:** without the UI, both trees indexed the same
  photos in 48 s, and their CPU profiles are close to identical (below).
- **Verdict:** a possible regression under load, not confirmed. The next step is
  to profile a whole index run rather than its first 45 s.

## Real app: in use

### Warm start

| Measure                     | office main | office branch | synth main | synth branch |
| --------------------------- | ----------- | ------------- | ---------- | ------------ |
| Window shown                | 1,104 ms    | 750 ms        | 686 ms     | 526 ms       |
| First thumbnail             | 1,182 ms    | 830 ms        | 721 ms     | 552 ms       |
| Network quiet               | 2,214 ms    | 1,970 ms      | 1,911 ms   | 1,941 ms     |
| Engine calls during startup | 7           | 9.5           | 7          | 6            |
| CPU, whole run (UI suite)   | 13.5 s      | 16.9 s        | 35.1 s     | 39.6 s       |
| Peak working set            | 2.28 GB     | 2.29 GB       | 2.89 GB    | 2.88 GB      |

Startup calls on the branch are one Timeline, one Days and one Focus call (plus
their repeats for the grid). Main made Timeline three times.

### The UI suite (medians of the warm runs)

| Measure                            | office main | office branch | synth main | synth branch |
| ---------------------------------- | ----------- | ------------- | ---------- | ------------ |
| Idle 20 s: engine calls            | 0           | 0             | 0          | 0            |
| Scroll: average frame              | 16.6 ms     | 15.1 ms       | 16.6 ms    | 16.6 ms      |
| Scroll: slowest 5% of frames       | 34.2 ms     | 17.5 ms       | 17.5 ms    | 17.5 ms      |
| Scroll: frames over 50 ms          | 0.5         | 0             | 0          | 0            |
| Scroll: engine calls (Days)        | 1           | 1             | 786        | 786          |
| Scroll: thumbnails loaded          | 150         | 150           | 2,988      | 2,988        |
| Scroll: page elements              | 193         | 196           | 252        | 255          |
| Jump to middle, grid filled        | 12.9 ms     | 11.3 ms       | 30.8 ms    | 28.9 ms      |
| Viewer open: preview shown         | 15.7 ms     | 15.9 ms       | 17.6 ms    | 17.8 ms      |
| Viewer open: full photo shown      | 119 ms      | 46 ms         | 112 ms     | 46 ms        |
| Next photo: preview (median of 15) | 144 ms      | 55 ms         | 17.1 ms    | 15.4 ms      |
| Next photo: full photo             | 188 ms      | 99 ms         | 39.6 ms    | 43.8 ms      |
| Engine calls per viewer step       | 1           | 1             | 1          | 1            |
| Switch to People                   | 180 ms      | 153 ms        | 37 ms      | 44 ms        |
| Switch to Things                   | 42 ms       | 44 ms         | 32 ms      | 33 ms        |
| Switch to Albums                   | 51 ms       | 48 ms         | 37 ms      | 46 ms        |
| Switch to Duplicates               | 61 ms       | 55 ms         | 79 ms      | 41 ms        |
| Switch to Favourites               | 41 ms       | 43 ms         | 178 ms     | 186 ms       |
| Switch to Settings                 | 51 ms       | 42 ms         | 38 ms      | 36 ms        |
| Back to Library                    | 332 ms      | 329 ms        | 178 ms     | 33 ms        |
| First search (text model loads)    | 3,369 ms    | 2,489 ms      | 1,734 ms   | 1,301 ms     |
| Second search                      | 201 ms      | 146 ms        | 202 ms     | 192 ms       |
| JS heap at the end                 | 15.9 MB     | 18.0 MB       | 197 MB     | 199 MB       |
| Console errors                     | 2           | 1             | 2          | 1            |

Notes on reading the table:

- **Switch time** is from the click until the page has rendered and the network
  has been quiet for 300 ms. It includes thumbnail downloads.
- **Viewer on office photos:** these are 18 MP originals, so the full photo
  dominates. The branch is 47–62% faster at every step. On the smaller synthetic
  photos both trees are within noise.
- **Favourites after Duplicates (synth, about 180 ms):** this is not the Favourites
  query. Its Timeline call took about 100 ms because the page was still tearing
  down the Duplicates view's 18,000 elements. See bottleneck 6.
- **The 197 MB heap (synth):** it builds up after the Duplicates view and the
  axe runs over it. The heap was 4.8 MB after scrolling the whole timeline and
  using the viewer.
- **Console errors:** both trees request Wails' optional `/wails/custom.js`
  (404). Main also requests `favicon.ico` (404); the branch fixed that.

Engine calls per view switch were the same on both trees:

| View       | Calls                                           |
| ---------- | ----------------------------------------------- |
| People     | People                                          |
| Things     | Things                                          |
| Albums     | Albums + Things + People                        |
| Duplicates | DuplicateGroups                                 |
| Favourites | Timeline                                        |
| Settings   | none                                            |
| Library    | Timeline + Days (main made Days twice on synth) |

### Rendering work while indexing 204 photos

These counts come from the DevTools performance counters, over the indexing
run plus the UI suite.

| Counter       | main   | branch | Change |
| ------------- | ------ | ------ | ------ |
| Script time   | 0.18 s | 0.08 s | −57%   |
| Layouts       | 530    | 480    | −9%    |
| Style recalcs | 804    | 586    | −27%   |
| WebView2 CPU  | 16.3 s | 11.1 s | −32%   |

## Accessibility (axe-core 4.13)

Elements failing each rule, summed over the viewer, the library and every view
(office library, warm run):

| Rule                                     | Impact   | main | branch |
| ---------------------------------------- | -------- | ---- | ------ |
| `button-name` (grid cells, icon buttons) | critical | 162  | 0      |
| `aria-required-children`                 | critical | 4    | 0      |
| `aria-dialog-name` (viewer)              | serious  | 1    | 0      |
| `link-name` (People and Things cards)    | serious  | 36   | 0      |
| `color-contrast`                         | serious  | 49   | 16     |
| `region` (content outside landmarks)     | moderate | 90   | 0      |
| `landmark-one-main`                      | moderate | 7    | 0      |
| **Total**                                |          | 349  | 16     |
| Serious and critical                     |          | 252  | 16     |

On the synthetic library the totals were 137 and 16. It has no people and
fewer Things cards.

The 16 left are two elements, found on every view:

| Element                                     | Colours                | Ratio  | Needed |
| ------------------------------------------- | ---------------------- | ------ | ------ |
| Active sidebar link (`.router-link-active`) | `#3b7ddd` on `#dbe4f3` | 3.17:1 | 4.5:1  |
| Status line under the progress bar (`.sub`) | `#6c6c76` on `#e9e9ea` | 4.27:1 | 4.5:1  |

## Where the engine's CPU goes

The two trees' profiles are within a few percent of each other.

**Office, 45 s from launch.** Main used 242 CPU-s (5.4 cores), the branch 233 CPU-s.

| Work                                   | main  | branch | Share  |
| -------------------------------------- | ----- | ------ | ------ |
| Faces stage, total                     | 113 s | 118 s  | ~50%   |
| — decoding originals at 2048 px        | 55 s  | 55 s   | 23%    |
| — recognition (SFace, CPU)             | 50 s  | 49 s   | 21%    |
| — detection (YuNet, CPU)               | 13 s  | 14 s   | 6%     |
| Thumbnails (libvips)                   | 39 s  | 30 s   | 13–16% |
| Things' text vectors (text model, CPU) | 25 s  | 26 s   | 10–11% |
| Image embedding (SigLIP, DirectML)     | 10 s  | 9 s    | 4%     |

**Synthetic, 60 s from 5 s after launch.** Both trees used 666–667 CPU-s (11 cores).

| Work                            | main  | branch | Share  |
| ------------------------------- | ----- | ------ | ------ |
| Thumbnails (libvips)            | 321 s | 298 s  | 45–48% |
| Faces stage, total              | 213 s | 196 s  | 29–32% |
| — decoding originals at 2048 px | 97 s  | 88 s   | 13–15% |
| — detection                     | 116 s | 108 s  | 16–17% |
| Things' text vectors            | 34 s  | 41 s   | 5–6%   |
| Image embedding                 | 21 s  | —      | 3%     |

The branch's synthetic profile also showed 35 CPU-s of file opening while
hashing, and its initial scan took 18.3 s there, against 1.5 s in both of the
branch's benchmark runs. An A/B test of the branch's `FILE_SHARE_DELETE` open
against `os.Open`, over the same 3,000 files, measured 1.40–1.52 s either way.
This was an environmental one-off (most likely an antivirus scan), not the
code. The branch's larger mutex profile (33 s against 4 s of runtime lock
contention) falls in the same window.

Block profiles show nothing waiting on the store's locks. Goroutines wait on
channels (stages woken for work) and on `parallel.For` batches.

## Media micro-benchmarks

`vips.DecodeFile` and `vips.ThumbnailFile`, 20 iterations × 3:

| Case                                                         | Time per photo                           |
| ------------------------------------------------------------ | ---------------------------------------- |
| 12 MP (4032×3024): thumbnails + separate 2048 decode (today) | 141–146 ms                               |
| 12 MP: one 2048 decode, thumbnails made from it              | 86–90 ms (−38%)                          |
| 18 MP (5184×3456): thumbnails + separate 2048 decode         | 174–192 ms                               |
| 18 MP: one 2048 decode, thumbnails made from it              | 113–116 ms (−35%)                        |
| 18 MP: thumbnails only                                       | 65 ms                                    |
| 12 MP decode at 2048 against 2016                            | 74–78 ms against 74–84 ms: no difference |
| 18 MP decode at 2048 against 1728                            | 101–128 ms against 93–98 ms              |

Lowering `faceSide` to let libvips shrink more while decoding gains little.
Sharing one decode between thumbnails and faces gains about a third.

## Bottlenecks, ranked

These affect both branches unless stated.

### 1. Every new library encodes the built-in Things before embedding any photo

The 182 Things' prompts go through the text model on the CPU:

- 25–41 CPU-s;
- 28–36 s of wall time on the office photos, 40–52 s on the synthetic library ("search ready" in the log).

Image embedding waits for it (`search.Engine.Wait`), so at 3,000 photos it
starts about a quarter of the way into indexing. On the 204 office photos it
is most of the total time. The same model is also why the first search after
start takes 1.3–3.4 s.

**Fix:** the model and the vocabulary are pinned, so ship the vectors with the
app (a small file keyed by model version and vocabulary version) and encode
only user-made labels. Expected: 28–52 s off every first index on this machine, and the
first search no longer waits for the whole vocabulary.

### 2. The faces stage decodes every original again

The faces stage reads each photo again at 2048 px: 23% of all engine CPU on
real photos. The thumbnail stage has just decoded the same file.

**Fix:** decode once at 2048 and make the 1024, 256 and hash images from those
pixels (`vips.ThumbnailPixels` already exists). Hand the pixels to face
detection in the same worker, or through a small cache of recent decodes.
Measured: 35–38% less decode CPU per photo. It needs the stage split changed,
because faces currently wait for the thumbnail job to be saved.

### 3. The thumbnail stage takes close to half of indexing CPU, on 6 workers

The stage uses `min(NumCPU-1, 6)` workers, and libvips concurrency is 1 per
operation. It ran at about 30 photos a second while the process averaged 6 of
20 threads. JPEGs are saved with `optimize_coding` (a second Huffman pass).

**Fix:** size the pool from the CPU count while the AI stages are idle or
waiting. Try without `optimize_coding` for the 256 px image. Keep workers fed
across batches instead of waiting for the slowest file of each batch; this last
idea comes from reading `runStage` and is unmeasured.

### 4. Face models run on the CPU

YuNet and SFace are opened on the CPU (`internal/ai/faces.go:89, 93`). Only
SigLIP uses DirectML. Together they are 27% of CPU on real photos, and faces
per photo are embedded in small batches.

**Fix to try:** DirectML for detection, and recognition batched across photos.
Unmeasured.

### 5. The timeline and lists rescan the library on every reload

At 100,000 assets:

| Query            | Time       |
| ---------------- | ---------- |
| Timeline buckets | 83 ms      |
| Things           | 270 ms     |
| People           | 274–303 ms |
| Albums           | 51–59 ms   |
| Duplicates       | 230–245 ms |
| Job counts       | 44–52 ms   |
| Count pending    | 101–103 ms |

The branch already reloads far less during indexing (51 timeline reloads per
3,000 photos, from 149). These queries still run in full each time.

**Fix:** cache each result behind a write-generation counter in the store, and
let saves that change only thumbnails skip the bucket reload. Keep indexing
counts in memory in the indexer, which knows what it saves.

### 6. The Duplicates view is not virtualized

At 3,000 photos with many near-copies it renders about 18,000 elements, and
leaving it blocks the page for about 100 ms.

**Fix:** virtualize the group list, as the grid is.

### 7. Scrolling the timeline makes one call for about every four rows

A full scroll of 3,000 photos made 786 `Days` calls and 2,988 thumbnail
requests. Frames stayed smooth, but the engine does a call for every small
span of days.

**Fix:** load days in wider windows (the visible span plus a screen or two on
each side, or a month at a time) and prefetch in the scroll direction.

### 8. Returning to the Library costs about 330 ms on real photos

The grid is remounted and its thumbnails are fetched again (office: 332 ms and
329 ms). The branch's synthetic warm runs were faster (33 ms).

**Fix:** keep the Library grid alive (`<KeepAlive>`) or keep loaded days
between visits.

### 9. Fetching a batch for a stage that is still blocked (branch)

This takes 85 ms at 100,000 assets whenever the earlier stage has nothing
finished.

**Fix:** give jobs whose earlier stages are unfinished their own status
("waiting"), and move them to pending when the earlier stage saves. The
partial index then holds only runnable jobs, and every fetch reads just
`limit` rows.

### 10. Upgrading an existing library re-runs most of the indexing (branch)

This comes from the branch's own audit (Compatibility) and from code reading:

- the metadata, thumbnail and faces versions change, so every photo re-runs those stages once;
- new thumbnail URLs empty the WebView's image cache;
- migration 0007's backfill `UPDATE` runs before the window opens.

At the throughput measured above, 100,000 photos is about 1 hour of thumbnails
and 1.5 hours of faces.

**Fix:** redo thumbnails only for photos that have an embedded colour profile,
and run the backfill in batches or after the window is up. The upgrade itself
was not measured.

## From code reading, not measured

A read-only review of the branch's diff raised these. They are unconfirmed and
listed for triage.

- **Name lookups** (`named()`, `TagByName`, `PersonByName`) now use
  `fold(name) = ?`, which can't use the unique index. They scan the whole table
  inside the writer's transaction. The ASCII fast path from the prototype
  applies here too.
- **`ApplyClusters`' new check** that two faces in one photo stay two people may scan a person's faces for
  every assignment. An index on `faces(asset_id, at_ms, person_id)` would bound it.
- **Albums' cover query** now runs even for empty albums, where main skipped it.
  With `thumb_version > 0` it can walk the whole timeline index for an album
  whose newest members have no thumbnail.
- **Grid cell labels:** `cellLabel()` builds a date label for every cell on every render.
  Scrolling was smooth in the tests; memoising per day would make it free.
- **HEIC colour conversion** in WIC runs at full resolution before scaling. It
  was not measured: there was no HEIC corpus.
- **Videos** are probed by `ffprobe` once per stage, and sampled three times at
  different sizes. Not measured: videos were off.
- **The details panel** makes four calls per selection, with no debounce. The
  viewer measured one call per step, because it uses its own `Asset` call.

## Not covered

- **HEIC, videos and wide-gamut colour:** the corpora had no HEIC and no ICC
  profiles, and videos were off.
- **Upgrading a main library to the branch.**
- **Hardware:** one laptop only, with a DirectML GPU. Libraries larger than
  3,000 real files were covered only by the store benchmarks.
- **The branch's extra engine CPU while indexing** (+6% synthetic, +12–26%
  office) is unexplained. Profiles of the first 45–60 s were equivalent, so the
  difference lies later in the run or in contention with the UI.
- **Run count:** two real-app runs per case; treat differences under 10% as noise.

## Fixes applied (same day, commit 03f8031)

Measured against HEAD (the branch above). The real-app runs used two runs per
build, alternating builds; the store benchmarks used 5–6 runs, interleaved.

### What changed

| Bottleneck                | Change                                                                                                                                                                                                              |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Things' text vectors   | The built-in prompts' vectors ship in the binary (`internal/search/prompts.bin`, 595 KB, keyed by model version). `TestShippedPromptVectors -update-prompts` rebuilds it; a test fails when the Vocabulary changes. |
| 1. First search           | The search box asks the engine to load the text model when it gets focus (`PrepareSearch`).                                                                                                                         |
| 2. Faces decode again     | The thumbnail stage decodes photos whose faces are still to find at 2048 px, makes the thumbnails from those pixels and keeps up to 16 for the faces stage, which takes them first.                                 |
| 3. Thumbnail workers      | Half the CPU's threads, at least 6 (10 on this machine).                                                                                                                                                            |
| 5. Reloads while indexing | Thumbnail saves send `thumbs`, not `assets`: the timeline reloads the days on screen, not its buckets.                                                                                                              |
| 6. Duplicates             | Groups render 40 at a time as the list scrolls.                                                                                                                                                                     |
| 7. Days calls             | When the rows within a screen need a day not loaded, days within three screens load in one call.                                                                                                                    |
| 9. Blocked-stage fetch    | Jobs held back by an earlier stage are `waiting` (migration 0008) and move to `pending` when it saves, so `asset_jobs_pending` holds only runnable jobs.                                                            |
| Regressions               | `KnownFiles` keeps the path index (`+source_id`); `Ingest` writes `taken_at` with the job and the 0007 triggers gain `WHEN` clauses (in 0008); ASCII names use `lower()` instead of `fold()` in SQL.                |
| Accessibility             | `--accent-on-soft` (#2560b8, 4.9:1) for accent text on the soft accent; the indexing status panel's notes use `--text-2`.                                                                                           |
| Albums                    | An empty album skips its cover query.                                                                                                                                                                               |

### Store (100,000 assets)

| Operation                      | Before  | After    | Change | p     |
| ------------------------------ | ------- | -------- | ------ | ----- |
| Fetch thumbnail batch, blocked | 67.2 ms | 0.043 ms | −99.9% | 0.002 |
| Known files of one folder      | 9.57 ms | 0.87 ms  | −90.9% | 0.002 |
| Ingest 500 new files           | 48.8 ms | 34.8 ms  | −28.7% | 0.008 |
| Things suggestions ("thi")     | 152 µs  | 58 µs    | −62.2% | 0.002 |
| People suggestions             | 1.55 ms | 1.58 ms  | ~      | 0.485 |
| People list                    | 264 ms  | 255 ms   | ~      | 0.818 |

Upgrading a main (v6) library of 100,000 assets to v8 takes 0.64 s fully
indexed, 2.4 s with every job still to do.

### Real app

| Metric                            | office before | office after | synthetic before | synthetic after |
| --------------------------------- | ------------- | ------------ | ---------------- | --------------- |
| "Search ready" on a new library   | 76 s          | 62 ms        | 56 s             | 64 ms           |
| First thumbnail                   | 13.7 s        | 4.3 s        | 14.1 s           | 5.6 s           |
| Thumbnails done                   | 16.8 s        | 12.6 s       | 125 s            | 100 s           |
| Fully indexed ("Up to date")      | 73.5 s        | 48.3 s       | 265 s            | 219 s           |
| Engine CPU, whole run             | 488 s         | 284 s        | 1,647 s          | 1,491 s         |
| Peak engine working set           | 2,699 MB      | 2,578 MB     | 2,119 MB         | 1,730 MB        |
| Timeline reloads while indexing   | 11            | 1            | 58.5             | 9               |
| Calls during a full scroll        | –             | –            | 786              | 83              |
| Opening Duplicates while indexing | 46 ms         | 77 ms        | 279 ms           | 54 ms           |
| Accessibility violations (nodes)  | 16            | 0            | 16               | 0               |

A later single office run of the final build was fully indexed in 31.7 s with
169 CPU-s. Scrolling, the viewer and warm starts are unchanged within noise.

### Findings that did not hold

- **8. Returning to the Library.** The ~330 ms included the grid's focus call
  to the engine, sent 300 ms after the view settles. Timed to the grid being
  filled, the switch takes 40–45 ms on both builds, and no thumbnail is
  fetched again (they come from the WebView's memory cache). Keeping the grid
  alive was tried and reverted: it saved nothing measurable.
- **`ApplyClusters`' check.** Its plans use `faces_by_asset`, with or
  without statistics.
- **People list.** Half is counting photos per person, half the 400 cover
  lookups; evaluating the user's pick once changed nothing.

### Measured, not changed

- **Face models on DirectML:** one worker matched four CPU workers
  (48 against 45–52 ms per photo on 48 office photos) with far less CPU.
  Moving needs vector parity with the CPU (clustering mixes both), a CPU
  fallback, and sharing the GPU with SigLIP.
- **The details panel's four calls** and **grid cell labels**: cheap enough
  (0.1 ms per call in the store) not to change.

## Follow-up (2026-09-30, uncommitted)

This round took on the four items left open above: the upgrade cost (#10),
the formats the benchmark missed, the face models on the GPU, and the
branch's extra engine CPU. Builds compared:

- **HEAD:** commit 03f8031, the fixes above.
- **Upgrade fix:** HEAD plus the upgrade change.
- **Final:** the upgrade fix plus the face models on the GPU.

Real-app runs used two runs per build, alternating builds. Upgrades ran on
copies of libraries main had built.

### 10. Upgrading a main library

**What changed.** A finished job that an upgrade puts back in the queue now
remembers the version that made its output: migration 0009 adds
`asset_jobs.prior_version`. Each stage checks whether the new version would
make the same output, and if so records the new version without redoing the
work:

- **Thumbnails** stand for:
  - videos, whose part of the version did not change;
  - photos that decode as they did, meaning no colour profile or an sRGB
    one. libvips reads only the header to check. HEIC, whose WIC decode now
    converts colour, and BMP, which now keeps straight alpha, are always
    remade.
- **Faces** stand for:
  - files where none were found, whose pixels are unchanged (for photos) or
    whose frames are sampled the same way (for videos).

  Files with faces re-run: yaw and sharpness are now worked out from the
  landmarks, which are not stored. The faces stage loads its models only
  for files that re-run.

- **Metadata** still re-runs everywhere. It reads only headers, and only
  reading them tells whether a date changed.

The rules live in `internal/index/upgrade.go`. A job whose prior version no
rule knows runs in full, so a version change the rules don't account for is
always safe. A test caught one case the first draft got wrong: metadata that
re-runs and finds a new orientation now clears the prior version of the jobs
already requeued, so a sideways thumbnail is never kept.

The sRGB check was tested with Windows' sRGB profile on a 12 MP photo.
Converting from the profile changed pixels by at most 1 level, and the
perceptual hash was identical.

| Library (copy of a main build)          | HEAD wall   | Upgrade fix wall | HEAD CPU  | Upgrade fix CPU | Thumbnails remade |
| --------------------------------------- | ----------- | ---------------- | --------- | --------------- | ----------------- |
| 3,000 synthetic (no faces, no profiles) | 107–113 s   | **3.6–5.6 s**    | 931–980 s | 21–27 s         | 3,000 → 0         |
| 77 mixed (HEIC, videos, P3, sRGB, BMP)  | 15.3–16.5 s | **8.4–10.0 s**   | 62–63 s   | 33–34 s         | 77 → 19           |
| 204 office photos (185 with faces)      | 28–46 s     | 30–35 s          | 171–265 s | 145–166 s       | 204 → 0           |

The office library barely changes because 185 of its 204 photos have faces,
which must re-run. The 19 thumbnails remade in the mixed library are exactly:

- the 11 P3 photos;
- the 7 HEICs;
- the BMP.

At 100,000 assets:

- The migration from v6 to v9 takes 0.68 s.
- Requeueing 300,000 jobs (metadata, thumbnails and faces) takes 1.9 s,
  once at startup.

The hour of thumbnails becomes a header read per photo, plus remaking the
HEICs and the photos with a colour profile other than sRGB. Faces re-run
only for files that have faces.

### Formats the benchmark missed

The mixed corpus (`mkmixed.py`) is 77 files:

- **Office photos:** 20 plain, 10 tagged Display P3, 10 tagged with
  Windows' sRGB profile.
- **Synthetic JPEGs:** 20.
- **HEICs:** 7 (one an iPhone Display P3 file).
- **Videos:** 7. They include:
  - an AVI (MPEG-4);
  - a 45 s 1080p AV1;
  - a video rotated 90°;
  - a video starting at 7 s;
  - a slideshow of office photos, so a video has faces.
- **Other:** a BMP, `red-p3.jpg`, and a small JPEG.

Videos were switched on.

- **Every file indexed on both builds, with no errors or skips.** Both read
  the rotated video as 720×1280 and the AV1 codec, and both found 110 faces
  in the slideshow video.
- **Fresh index:** main took 51–57 s and 300–360 CPU-s. The final build took
  22.6–24.5 s and 76–98 CPU-s.
- **Per format:** each subset indexed alone (7 HEICs, 7 videos, 11 P3
  photos). Main's totals are dominated by its 30 s of Things text vectors
  at start, so the stages compare better:
  - thumbnails finished within 3.3 s on both builds for every subset;
  - faces finished in 7.9–10.1 s for the videos on main, 5.7–6.7 s on the
    final build.
- **Viewer:** all 77 items opened on both builds (`viewcheck.mjs`, two runs
  each).

  | Item                       | main       | final      |
  | -------------------------- | ---------- | ---------- |
  | Video, first frame playing | 45–185 ms  | 44–151 ms  |
  | — 45 s 1080p AV1 (17 MB)   | 167–185 ms | 45–60 ms   |
  | — AVI (transcoded)         | 151–180 ms | 121–151 ms |
  | HEIC preview               | 13–31 ms   | 13–31 ms   |

  The AV1 file is faster because the branch streams videos from its own
  loopback server; main buffered each file whole.

- **Colour:** measured as the mean difference in the average colour of
  16 px blocks between each thumbnail and the photo as a colour-managed
  viewer shows it (0–255 scale). WebView2 colour-manages the original, so
  this is the jump when the original replaces the preview.

  | Photos                    | main | final |
  | ------------------------- | ---- | ----- |
  | Plain JPEG                | 0.4  | 0.4   |
  | JPEG with an sRGB profile | 0.4  | 0.4   |
  | JPEG in Display P3        | 4.4  | 0.4   |
  | iPhone HEIC (Display P3)  | 1.2  | 0.6   |
  | HEIC without a profile    | 0.5  | 1.2   |

  The last row is resampling, not colour. HEICs between 1,024 and 2,048 px
  now get their thumbnails from the pixels decoded for faces, and libvips
  resizes those rather than WIC's scaler. The difference is under 0.5% of
  the range.

### Face models on the GPU

**Agreement.** Both models run entirely on DirectML: the sessions open with
the CPU fallback disabled. On all 204 office photos they found the same
1,000 faces as the CPU:

- scores and landmarks within 10⁻⁵;
- identity vectors with cosines of 0.99999 or more.

Libraries indexed with the faces on the GPU and on the CPU hold the same
1,000 faces: boxes within 0.0004, worst cosine 0.999999. So no re-run and no
version change is needed, and faces found on either device cluster
together.

**Sharing the GPU.** The first version put the faces on the GPU, taking
turns with the image model:

- office: wall time unchanged, 35% less CPU;
- synthetic: 13–21% less CPU, but 25–40% longer. That corpus is GPU-bound on
  the image model, and face runs slowed it.

The final version gives the image model the GPU first. Face runs use it only
when it is free and the image model is not waiting, and otherwise run on
CPU sessions, which stay loaded. The image model waits for at most one face
run, a few milliseconds. A GPU that fails is dropped, and the CPU runs
everything. The code is in `internal/ai/runtime.go` (`gpu`, `tryRun`) and
`internal/ai/faces.go`.

Fresh index, faces on the CPU against the final build:

| Corpus            | CPU faces wall | Final wall      | CPU faces CPU | Final CPU     |
| ----------------- | -------------- | --------------- | ------------- | ------------- |
| 204 office photos | 39.3–41.6 s    | **28.8–32.4 s** | 250–262 s     | **157–177 s** |
| 3,000 synthetic   | 203–231 s      | 192–230 s       | 1,456–1,627 s | 1,324–1,554 s |
| 77 mixed          | 23.3–23.4 s    | 22.6–24.5 s     | 93–99 s       | 76–98 s       |

The number of people found on the office photos varied between 27 and 28
across runs of the same build. Clustering is incremental: faces placed in an
earlier run stay placed, and each run sees whichever faces exist by then.
The result therefore depends on when faces arrive. This happens on main too
and is not tied to the device; in the paired comparison, both builds found 28.

### The branch's extra engine CPU

Profiles covered a later window than before: 60–180 s on the synthetic
corpus, 25–55 s on the office photos. They compared main with the branch as
it was before the fixes (`prof2.mjs`). In that window the branch spent less
CPU per item than main in every stage:

| Stage (synthetic, 60–180 s) | main           | branch         |
| --------------------------- | -------------- | -------------- |
| Faces                       | 0.22 s / photo | 0.19 s / photo |
| Thumbnails                  | 0.22 s / photo | 0.19 s / photo |
| Image embedding             | 0.063 s / item | 0.054 s / item |

Over the whole run, Lumen.exe used:

- office: 561 CPU-s on the branch, 550 on main (+2%);
- synthetic: 1,683 on the branch, 1,746 on main (−4%).

Identical runs of one build varied more than the original gap. Office runs
of the same build today ranged from 171 to 265 CPU-s, and synthetic ones
from 1,378 to 1,627. This laptop's CPU-seconds swing with its clock speed
and temperature. The +6–22% was that variation, not the code, and nothing
was changed for it.

### Other findings

- **A second instance exits silently.** Launching Lumen while the previous
  instance is still closing exits at once with an empty log, because of the
  single-instance lock. The harness now fails fast when that happens.
- **The README** now says the face models use the GPU.

## Reproducing

Everything is in `D:\tmp\lumen-bench`:

- **Store benchmarks:**
  - Copy `store_bench_test.go` and the matching `shim_main_test.go` or
    `shim_branch_test.go` into `internal/store/`, as `zz_*` files.
  - Run `go test -run '^$' -bench . -benchmem ./internal/store/`.
  - `LUMEN_BENCH_N` sets the library size (default 100,000). The seed libraries
    are cached under `%TEMP%\lumen-bench-<tree>-<n>`.
- **App runs:**
  - Build each tree and put `Lumen.exe` into `app-<tree>/` beside a copy of `deps/runtime`.
  - Build the seeder: `seedapp/main.go`, compiled inside each module.
  - Run `bash run_app.sh <name> <folder> <reps>`, then `node analyze.mjs` for the tables.
  - `appbench.mjs` is the driver. It needs `node/` (axe-core).
- **Profiles:** add `zz_pprof.go` to the module root, build, then run
  `node prof.mjs <tree> <folder> <label> <seconds> <delay>`. `prof2.mjs`
  takes the same arguments and also records the jobs finished and the
  process CPU during the window.
- **Follow-up runs (2026-09-30):**
  - `upgrade.mjs` times either an upgrade of a copied library or a fresh
    index, without the UI driver. Its drivers are `run_upgrade.sh` (#10),
    `run_gpu.sh` and `run_gpu2.sh` (faces on the GPU), and `run_formats.sh`
    (per format).
  - `mkmixed.py` builds the mixed corpus in `mixed/`.
  - `viewcheck.mjs` steps through the viewer.
  - `facecmp.mjs` compares the faces of two libraries.
  - Results are in `results5/`.
- **Raw data:**

  | Path                                   | Contents                 |
  | -------------------------------------- | ------------------------ |
  | `results/*.json`, `results/*.proc.csv` | one file of each per run |
  | `benchstat2.txt`                       | store comparison         |
  | `fixcmp-*.txt`                         | prototype comparison     |
  | `prof/`                                | profiles                 |
  | `report-app.md`                        | the app tables           |
  | `prototype-store-fixes.diff`           | the store fixes above    |

- **Second tree:** for the main side, use `git worktree add` and copy `deps/`
  into it. Do not link it with a junction: removing the worktree deletes
  through the link.
