# Audit report: hardening pass, 1 October 2026

**Branch:** `fixes/audit`, compared with `main`. When the pass started (`ef0013a`), the branch held 3 commits and 139 changed files.

**Source of truth:** "Local AI Photo Gallery — End-to-End Plan.md", read together with the README's "Deviations from the plan", which records decisions already accepted.

**Outcome:**
- 37 commits on top of `ef0013a`, up to `153b406`: one adds `CONVENTIONS.md`, two record the 1 October decisions, and the rest are fixes, tests and cleanups.
- 96 files changed, 3,464 lines added and 365 removed.
- No next-phase work was started.

## How the pass ran

1. **Baseline.**
   - The branch built and every check passed (see section 1).
   - `CONVENTIONS.md` records the codebase's dominant patterns, and picks one where two competed. It covers errors, context, SQLite, logging, the Go-to-TypeScript contract, Pinia and Vue, and tests. The README now links it.
2. **Seven parallel work units.** Each ran in its own git worktree, owning a disjoint set of files:
   - **A:** store
   - **B:** indexing
   - **C:** AI, search and people
   - **D:** media and scanner
   - **E:** services and app wiring
   - **F:** library, settings and search UI
   - **G:** feature views
3. **One read-only review (unit H).** It covered the seams I kept for myself as lead: core types, migrations, the bridge, shared UI code, the bindings, i18n and the docs.
4. **Review before merging.** I read every diff before it merged. For the main fixes, I ran the new tests against the old code to confirm they fail without the fix.
5. **Merging.**
   - Units merged one at a time.
   - Each merge was followed by the full lint gate, the Go race suite and `wails3 build`.
   - I made the commits, grouped logically.
6. **Lead work after the units.** I handled the cross-unit requests and cross-cutting fixes, then did a final consistency pass for styles that diverged between units.
7. **Acceptance in the real app.** I ran the production build in WebView2 against two copies of scratch libraries upgraded from schema 16.

**Two interventions:**
- Unit E left a test-output file outside its files; it removed it before finishing.
- One test from unit G failed under Vitest for a test-tool reason. I rewrote it to fake the engine with a plain function. It still fails against the old code. See section 4.

## 1. Status before and after

| Check | Before (`ef0013a`) | After (`HEAD`) |
| --- | --- | --- |
| `scripts/lint.sh`: golangci-lint (vet, staticcheck and more), deadcode, govulncheck, vue-tsc, ESLint, Prettier, Ruff, ShellCheck | pass, 0 issues | pass, 0 issues; no vulnerabilities |
| Go race suite: `go test -race -count=1 . ./internal/... ./services/...` | pass, 14 packages | pass, 13 packages (`media/vips` now has tests) |
| Go test functions | 166 | 212 |
| Vitest | 108 tests in 21 files | 144 tests in 24 files |
| `wails3 build` | pass | pass |
| Bindings regenerated, then compared | no drift | no drift (`Progress.discovered` was removed on purpose and regenerated) |
| Skipped Go tests | 3: the face tests that need `LUMEN_FACE_PHOTOS` | the same 3 |
| Known flaky tests | Vitest failed about 1 run in 6 with "window is not defined" | fixed: 0 in 20 runs |

**Acceptance in the real app.** I used the production `Lumen.exe` in WebView2, on copies of the previous round's scratch libraries.

The 5-photo library migrated from schema 16 to 17. These all pass:
- The grid keeps keyboard focus, with an active descendant.
- The details panel shows the place name.
- A label-only place block removes the place's card, and its name from event titles.
- Renaming an event moves focus correctly.
- A wrong PIN gets its coded message. The rate limit now reads "Try again in 30 seconds", with the number taken from the engine.
- Unblocking restores the card.
- Every view opens.
- Three quick clicks on the videos toggle save the last value.
- No page exceptions, and no warnings in the log.

The 2,000-photo library with one video also migrated from schema 16 to 17. These all pass:
- **Upgrade.** It re-queued 2,002 jobs and finished all of them. Photo embeddings stood untouched. Face analysis moved to version 3, and the video's two AI stages reran at `+video-frames-3`.
- **Duplicates.** The list keeps 5 to 7 of 2,000 groups mounted, at the top, at the bottom and at phone width, and previews open.
- **Albums.** Membership and counts are right, and the `album_date` code comes through.
- **Blocked names.** Searching a blocked person, Thing, place or album returns `blocked: true` and no items.
- No page exceptions, and both apps shut down cleanly.

The user's own library was not touched.

## 2. Findings and what was fixed

Every finding below was checked in the code before it was fixed, and each fix has a regression test. The main ones were also run against the code before the fix, where they fail.

### P1: privacy

| Finding | Fix |
| --- | --- |
| A search answered before a visibility change put its hits on the grid, then pruned them. A newly blocked photo could flash into the results (`library/store.ts`). | Hits are checked against the visibility change before they reach the grid, and checked again if another change arrives meanwhile (`4c49cb0`). |
| A vocabulary update that retired a blocked built-in Thing deleted the Thing's block and matches, which revealed the photos it covered (`store/tags.go`). | A blocked built-in keeps its prompts and matches until it is unblocked (`c623785`). |
| After a place block, the Places list reloaded but kept the old cards. If the reload failed, the blocked place stayed on screen. | The list is cleared first, then read again (`e6448b2`). |

### P2: real bugs

**Privacy:**
- The progress counts included jobs of blocked, hidden and trashed assets (`44acef3`).
- `PeopleAssets` returned photos for label-blocked people (`44acef3`).
- A label block saved between a timeline filter's first check and its query was ignored. The block is now checked again inside the query, and this also covers album and place filters (`44acef3`).
- The scan status counted every file scanned, including blocked folders and photos. The plan's §9 hides blocked items from "every count and the indexing status". The count is gone from the wire type, and the status shows "Looking for photos…" (`95e9d86`).

**Video:**
- Frame times were wrong for videos whose container doesn't start at zero, MPEG-TS for example. FFmpeg already offsets its timestamps, and Lumen subtracted the start time a second time, giving 0, 0.4 and 2.4 s instead of 0, 2 and 4 s. So search jumped to the wrong moment, and faces were tagged with the wrong frame time.
  - `VideoFramesVersion` and `VideoThumbnailVersion` are bumped, so video AI reruns once.
  - Photos are unaffected. Without the next fix, the thumbnail bump would have rebuilt every photo thumbnail (`4b2d1ff`).
- A video-only thumbnail version bump rebuilt every photo thumbnail and re-queued its embedding. Each kind now compares only its own version (`df50c5e`).

**Cancellation and shutdown:**
- A worker waiting for a model could load it after cancellation. Queued batches, face runs and video sampling now stop at their next boundary (`9bca534`).
- `Stop` didn't wait for a filesystem flush that was already running, and continuous changes could postpone the flush past its 30-second maximum (`62ca874`).
- The video server's `Close` returned before its conversions ended. Closing it before `Run` leaked the listener. A failed response write left FFmpeg running (`e41bfd2`).
- The video server and the frame-cache sweep could outlive shutdown (`2fdd9a0`). Shutdown can now interrupt the sweep (`a741c4b`).
- Search: `Close` waited forever for load retries, and a failed text session was reused until it went idle (`0fa32d8`).
- The scanner's error path, the frame sampler and timestamp waits all ignored cancellation (`e4e3018`, `4b2d1ff`).
- An overlong FFmpeg log line blocked frame reading until the timeout (`4b2d1ff`).

**Correctness:**
- A folder named `..Private` inside a source escaped exclusion in incremental scans (`e4e3018`).
- A cache path containing brackets broke the frame-cache sweep (`fb18020`).
- Infinite detector confidence was clamped into a confident face (`98efea1`).
- Asset details, and the inputs to clustering, each read from several database snapshots instead of one (`44acef3`).

**Frontend:**
- A failed timeline read dropped a newer refresh that was queued (`4c49cb0`).
- Favourite, pause, videos and battery toggles could roll back a newer choice when an older save failed (`4c49cb0`, `da8fc6a`).
- The library store never released its event subscriptions (`4c49cb0`).
- An open Duplicates preview kept stale paths after a reload (`a87d123`).
- People:
  - A failed reload after a face correction was treated as "this person is gone".
  - Two renames could overlap.

  Both fixed in `3227eaa`.
- An event's rename draft came back after the event disappeared and returned (`53a9a63`).

**Contract:**
- A malformed stored PIN reached the UI without an error code (`85e0b5a`).
- `search.ErrClosed` had no code; it now maps to `no_ai` (`5b73eb1`).
- Name inputs used JavaScript's `trim`. It strips U+FEFF, which Go keeps, and keeps U+0085, which Go strips, so "﻿Ali" was sent as "Ali" and could resolve to a different person. A shared `trimName` now matches Go everywhere (`f31a434`).

### P3: consistency, duplication, typing, tests, dead code

- **Layering.**
  - `shared/block.ts` imported five feature stores. Stores now register with it when they are created, one registry per Pinia instance.
  - An ESLint rule keeps `shared/` from importing features (`1b422f2`).
- **One source of truth.** The PIN guidance hard-coded 6–12 digits and 30 seconds. It now uses the mirrored engine values, and a new `PIN_COOLDOWN_SECONDS` mirror is guarded by `contract_test.go` (`4ded94b`).
- **Duplicated state.** `SearchInfo` re-declared generated fields; it is now derived with `Omit<SearchResults, 'items'>` (`4c49cb0`).
- **Context.**
  - `Faces.Embed` (`b276b73`) and `scanner.Fingerprint` (`fd24082`) take a context.
  - `SweepFrames` takes one too (`a741c4b`).
- **Error context.**
  - Native decode errors now carry the file path.
  - Model and stage errors are wrapped, and visibility-read failures are logged instead of discarded.
- **Native buffers.** Size checks in libvips can no longer overflow, and RGB copies are bounded (`6dbd125`).
- **Dead code.** `store.BlockedNamedPeople` is removed. `deadcode` can't see unused exported methods here, because Wails' reflection makes them all look reachable. I scanned for them by hand: none remain in Go, and the TypeScript exports are all used.
- **Tests.**
  - Every album validation code is covered.
  - PIN timing tests use an injectable clock instead of rewriting private fields.
  - The Vitest timer flake is fixed (`0a20b85`).
- **Style.**
  - Comments the units added are back to the codebase's lower-case trailing style.
  - The README's bindings command now matches the build (`fd24082`, `e13e524`).

### Checklist coverage

| Item | Status |
| --- | --- |
| One source of truth, Go and TS | Satisfied. Constants, codes and events are mirrored and guarded by `contract_test.go`. Name trimming and PIN guidance are now shared. Unit H matched all 24 wire structs, 146 fields and 11 enums between Go and the bindings. |
| Bindings are the contract | Satisfied. There are no hand-written shadow types; the bridge's `NonNullLists` normalises the 6 nullable lists. Bindings were regenerated after the `core.Progress` change, with no drift. |
| Events defined centrally and typed | Satisfied. Each is a `services.Event*` constant with `RegisterEvent[T]`, and the bridge checks `satisfies keyof Events.CustomEvents`. |
| Errors propagate to the UI | Satisfied. The two codeless errors found are fixed, and coded failures were seen in the real app. |
| No dead code, debug output or TODOs | Satisfied (see the dead code item above). |
| Go: boundaries, interfaces, global state | Satisfied, with stated exceptions. The new interfaces sit with their consumers and exist for test fakes. Two package-level states remain, and both are justified: `ai`'s GPU lock, which takes turns between models and is process-wide by nature and the lazily loaded place index (immutable once loaded). |
| Go: errors, context, goroutines, races | Satisfied. All goroutines started on the branch are tracked. The one limit: a native ONNX call can't be interrupted mid-run (see section 4). |
| SQLite | Satisfied. Migrations are versioned and transactional. Foreign keys, WAL and `busy_timeout` are set explicitly. There is one writer connection and a read pool. Unit A checked 42 queue query plans. File rows are unique by their fold key (0018), and the ingest lookup uses that index. Migration 0018 is safe to run again. |
| AI integration | Satisfied. The models are local and behind narrow types. Failures back off per stage, and output is validated (NaN, Inf, output shape). There are no secrets, and the prompts live in `internal/search`. |
| TypeScript, Vue and Pinia | Satisfied. Strict mode is on, with no `any`, casts or suppressions. Every component is `<script setup lang="ts">`. All stores are setup stores; destructuring uses `useList`'s own refs. Listeners are disposed. Large lists are virtualized. |

## 3. Deviations from the plan

**Fixed:**
- The scan status showed a raw file count that included blocked items (§9, "every count and the indexing status").
- **File identity.** The README says paths compare with Unicode case folding, but file rows were matched with SQLite `NOCASE`, which folds only A–Z. Renaming "Été.jpg" to "été.jpg" while its content changed left two rows, and every later scan swapped the file between two assets. Files are now looked up by their fold key, and migration 0018 makes that key unique (decision 8, below).

**Decided on 1 October:** each deviation below is now recorded in the README, either as an accepted deviation or as remaining scope.

1. **Politeness (§6).** Pausing on battery and a manual pause exist. Pausing for gaming or full screen, pausing when the CPU is busy, and "index only when idle" are **deferred to Phase 6**.
2. **Pipeline shape (§6).** The code uses durable SQLite stage queues with bounded batches instead of channels. **Accepted.**
3. **Vocabulary size (§4).** There are 182 built-in Things, against the plan's 300 to 1,000. **Accepted:** more are added only with measurement.
4. **Clustering order (§4).** Named and unnamed people cluster together, instead of matching named people first. **Accepted.**
5. **Model location (§9, "Data location").** The models ship beside `Lumen.exe`. **Accepted.**
6. **Things and Events (§2).** Renaming and pinning Things, and pinning Events, **move into the Phase 5 remainder.**
7. **Duplicates (§9).** Pre-selecting the best copy is **Phase 5 scope, built together with Trash.**

## 4. Deferred items and recommendations

- **Native inference can't be cancelled mid-call.** ONNX Runtime calls can't be interrupted, so cancellation now happens between batches, faces and frames. That bounds a shutdown to one model run.
- **A Vitest quirk.** On one Places test path, a rejected promise returned by `vi.fn` failed the test even though the view handles it. A plain function behaves as expected. The test uses a plain function and says why. Worth a look when Vitest is next upgraded.
- **Timing-sensitive Go test.** `TestWatchReportsChangesAndReleasesHandle` failed once with its 5-second wait while seven work units loaded the machine. It passed 5 times out of 5 alone, and in every later suite run. Consider a longer wait if it recurs on slow CI.
- **Gaps in acceptance.**
  - Real HEIC through WIC, with a Display P3 sample, still needs a sample file.
  - The three face-corpus tests need `LUMEN_FACE_PHOTOS`.
- **Dependencies.** None were upgraded; there was no defect that called for it.

## 5. Decisions (1 October 2026)

The product owner took the recommended option on all nine questions.

| # | Question | Decision | Where it's recorded |
| --- | --- | --- | --- |
| 1 | Politeness controls | Defer to Phase 6's performance pass | README, Phase 5 status |
| 2 | Pipeline shape | Accept the SQLite queues | README deviations |
| 3 | Vocabulary size | Keep the measured set, and grow only with measurement | README deviations |
| 4 | Clustering order | Keep joint clustering | README deviations |
| 5 | Model location | Accept "beside the exe" | README deviations |
| 6 | Things rename and pin, Events pin | Phase 5 scope | README, still to finish |
| 7 | Best copy in Duplicates | Phase 5 scope, together with Trash | README, still to finish |
| 8 | Unicode path identity | Keep the newest row and mark the others missing | Fixed: migration 0018 |
| 9 | Scan status | No count while scanning | Already in place (`95e9d86`) |

## 6. Readiness for the next phase

**The branch is ready for the next phase. Nothing blocks it.**
- Every check passes with zero errors and zero warnings.
- `wails3 build` succeeds, and the bindings have no drift.
- The production app passed both acceptance runs on upgraded libraries, with clean shutdowns.

Phase 5's remaining scope now also covers renaming and pinning Things, pinning Events, and best-copy pre-selection in Duplicates (with Trash).

### Upgrade notes

- **Migration 0018 runs once.** It keeps one row per file in any case, which is the newest. An asset left with no file is marked missing: its names, tags and albums are kept, should the content come back. It then adds the unique index `files_path_fold`. It is safe to run again. Libraries at schema 16 also apply 0017 first.
- **Video AI reruns once.** `VideoFramesVersion` is now `+video-frames-3` and `VideoThumbnailVersion` is now `ffmpeg-9.0+vips-8.18-2`, so every video's poster, sprite and AI stages run again. Photos are unaffected.
- **Old frame folders are removed in the background** after start. Shutdown can now interrupt that cleanup.
