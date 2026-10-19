# Phase 5 remainder: design — 1 October 2026

This note says how each remaining Phase 5 item is built: its data model and
migrations, its store, service and bridge surface, where the user meets it,
and the risks and how each is tested. It binds the builders of each unit, as
`CONVENTIONS.md` does. The scope is the README's "Still to finish in
Phase 5" paragraph; the plan's §2 and §9 are the source of truth for
behaviour. `AUDIT_REPORT.md` §5 decisions are not reopened.

Product questions this design could not settle were put to the product owner
in one batch; their answers are recorded in [Decisions](#decisions) and
override anything below that they contradict.

## Shared rules

**One owner for the user's files.** A new package, `internal/fileops`, makes
every change to files in the user's folders: recycling, moves, the move
journal's execution, and sidecar creation. Nothing else writes there.
- Deletion is only ever `fileops.Recycle`. It uses the shell's
  `IFileOperation` with `FOFX_RECYCLEONDELETE`, and a progress sink whose
  `PreDeleteItem` aborts any item the shell would not put in the Recycle Bin
  (`TSF_DELETE_RECYCLE_IF_POSSIBLE` unset: a network share, a drive without a
  bin, a file too big for it). So a file is recycled or left alone, never
  destroyed. `os.Remove` is never called on a user file; the only `os.Remove`
  in the package removes an empty folder that the journal says Lumen created.
- Moves are `MoveFileExW` without `MOVEFILE_REPLACE_EXISTING`: a move never
  overwrites, whatever raced it.
- New files (sidecars) are created with `O_EXCL` through a temporary name and
  a rename that does not replace.
- The Windows code is in `_windows.go` files with an `_other.go` stub that
  returns `errors.ErrUnsupported`, as `internal/scanner` does.
- `fileops` holds no SQL: it persists through `store` methods.

**Visibility.** Trash, event blocks and face blocks feed the existing
`visible` / `labelBlocked` / `blockVisible` predicates; nothing builds its own.
- Trash is already in `baseVisibleOf` (`trashed_at IS NULL`), the
  `assets_timeline` partial index's own clause, and migration 0016's
  `visible_asset_changed` already bumps `visible_value` for it.
- Event and face blocks are two new branches of `blockVisible`, each an
  indexed lookup in a materialised membership table (below), so the predicate
  stays a cheap per-row `NOT EXISTS` and the partial index stays usable.
  Inserts and deletes in those tables bump `value` and `visible_value` only
  when a photo-mode block names them.
- `blockVisible` is split into `blockMatch(asset, block)` plus the existing
  `NOT EXISTS` wrapper, so the covered-media browser asks "does this block
  cover this asset" with the same SQL as visibility (one source of truth).
- Gate: the 100k-asset blocked timeline count (`<scratch>\lumen-bench`,
  ~26 ms/call on `main`) must not regress materially; it is re-measured
  after Waves 1 and 2.

**Serving media outside the browse view.** Three places show assets that
`visible` excludes: Trash (trashed assets), the Blocked screen's browser
(covered assets, only while unlocked) and the Viewer opened from either. The
store gets one predicate, `servable`:

```
visible
OR (trashed_at IS NOT NULL AND missing_since IS NULL AND hidden = 0)        -- Trash
OR (:unlocked AND trashed_at IS NULL AND missing_since IS NULL AND hidden = 0) -- Blocked, unlocked
```

`AssetVisible`, `AssetFile` and `AssetDetail` use it. The unlock state lives
in one `services.BlockGate` (today's `LibraryService.blocked` struct, moved),
shared by `LibraryService`, `MediaService` and the `Streamer`. Leaving the
Blocked screen locks it, so its media stops being served at once.

**Event identity (shared by names, pins and blocks).** Today a rename writes
the name on every member photo (`event_names`), and a group shows the name of
its earliest named member. That rule generalises to "event tags":

```sql
CREATE TABLE event_tags (
  id         INTEGER PRIMARY KEY,
  kind       TEXT NOT NULL CHECK (kind IN ('name', 'pin', 'block')),
  name       TEXT,                    -- kind = 'name' only
  created_at INTEGER NOT NULL,
  CHECK ((kind = 'name') = (name IS NOT NULL))
);
CREATE TABLE event_members (
  tag_id   INTEGER NOT NULL REFERENCES event_tags (id) ON DELETE CASCADE,
  asset_id INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  moved    INTEGER NOT NULL DEFAULT 0,   -- its date or place changed since the last settle
  PRIMARY KEY (tag_id, asset_id)
) WITHOUT ROWID;
CREATE INDEX event_members_asset ON event_members (asset_id);
```

- A tag's members are the photos of the event it was set on. A group carries
  a tag when it holds any of the tag's members.
- **Settling** keeps members equal to the event as it regroups. It groups the
  *base* timeline (present, unhidden, untrashed assets, ignoring blocks, so a
  block can never change the grouping that defines another block), with the
  same gap and 100 km rules as `groupEvents` (one shared splitter). For each
  tag and each group:
  - a group with an unmoved member carries the tag, and every photo in it
    becomes a member (photos that join the event take its name, pin or
    block);
  - a group holding only moved members carries it only if the tag has no
    unmoved member anywhere (the whole event moved, for example a time-zone
    fix); otherwise those members are dropped: the photos left the event,
    so they leave its name, pin and block.
  - Splits keep the tag on both halves (both hold original members), as
    renames do today.
- **When.** Triggers record the times that changed (old and new `taken_at`
  of an inserted, deleted or re-dated asset, or one whose `lat`, `lon`,
  `hidden`, `trashed_at` or `missing_since` changed) in `event_dirty(at)`,
  only while any event tag exists. `Store.tx` settles before it commits
  whenever `event_dirty` has rows, so no transaction ever commits a photo that
  joined a blocked event without hiding it. Settling only re-groups the
  windows around the dirty times, bounded by gaps longer than the event gap
  (a hard boundary every rule respects); a gap-setting change settles
  everything.
- `event_names` is migrated to name tags (one per distinct name) and dropped.
- **Behaviour change (renames):** a photo whose date or location is corrected
  into a different event no longer carries its old event's name there. This is
  required by the plan's "photos that leave it come back" for blocks, and the
  rule is shared.

**Migrations** (numbers reserved by the lead; never edit an applied one):

| # | File | Unit |
| --- | --- | --- |
| 0019 | `things_events.sql`: `tags.vocab_name`, `tags.pinned_at`, event tags | U1 |
| 0020 | `trash.sql`: Trash index | U2 |
| 0021 | `thumb_views.sql` | U3 |
| 0022 | `ocr.sql` | U4 |
| 0023 | `block_kinds.sql`: `blocks` rebuilt with `event` and `face`, face-block tables, their triggers | lead |
| 0024 | reserved: event blocks | U6 |
| 0025 | reserved: face blocks | U7 |
| 0026 | `organise.sql` | U10 |
| 0027 | `xmp.sql` | U11 |

`blocks.kind` has a `CHECK` constraint, so adding kinds rebuilds the table
(create, copy, drop, rename, recreate its indexes and triggers). It is done
once, for both new kinds, in 0023.

**Contract.** The lead writes, before each wave: the `core` types and
constants, the service method signatures (as stubs returning
`errors.ErrUnsupported`), the error codes and events with their
`contract_test.go` lines, the bridge functions, and regenerated bindings.
Builders implement behind them and return any further change to a lead-owned
file as an exact patch.

## U1 — Things: rename and pin; Events: pin (Wave 1)

**Data.** 0019:
- `tags.vocab_name TEXT` (the vocabulary's name for a built-in; NULL for the
  user's own), backfilled from `name`, with a unique partial index.
  `SyncVocabulary` matches built-ins by `vocab_name`, so a renamed built-in
  keeps its identity, prompts and matches.
- `tags.pinned_at INTEGER` (NULL = not pinned).
- A built-in the vocabulary retires is kept while it is blocked (today),
  renamed or pinned, with its prompts and matches.
- A new vocabulary built-in whose name is taken by a renamed Thing is added
  under its own name with the user's Thing untouched; if the name is taken,
  it is inserted with `name` = `vocab_name` + " (built-in)" (tested).
- Event tags (shared rules). `event_names` migrates to name tags.

**Store / service / bridge.**
- `store.RenameTag(ctx, id, name)`: `cleanName`, `ErrTagExists`,
  `ErrBlockedName` against blocked names. An empty name on a built-in
  restores `vocab_name`.
- `store.SetTagPinned(ctx, id, pinned)`.
- `store.SetEventPinned(ctx, id, pinned, gap)`. `SetEventName` is rewritten
  over event tags; the settle machinery goes in `store/eventtags.go`.
- `Things` sorts pinned first (newest pin first), then by count; `Events`
  sorts pinned first, then newest. `core.Tag` gains `pinned` and `original`
  (the vocabulary name when renamed); `core.Event` gains `pinned`.
- Services: `SearchService.RenameThing`, `SearchService.SetThingPinned`,
  `LibraryService.SetEventPinned`.

**UI.** ThingsView and EventsView cards get Pin/Unpin and (Things) Rename
with the shared `useRename`. Pinned cards show a pin badge.

**Risks and tests.**
- Vocabulary sync with renamed, pinned, renamed-and-retired built-ins, and a
  name clash (store tests, like `c623785`'s).
- Pins survive restarts (store reopened in the test).
- Event tags: split keeps tags on both halves; a joiner takes the tag; a
  re-dated photo leaves; a whole-event re-date keeps it; gap change; trashed
  member kept while trashed; settle runs inside the writing transaction.
- Settling cost at 100k assets: an ingest batch of 128 new photos with event
  tags present, against the same batch without (benchmark).
- Migration of `event_names` on a copy of a v18 scratch library.

## U2 — Trash and `internal/fileops` recycling (Wave 1)

**Data.** 0020: `assets_trash ON assets (trashed_at DESC, id DESC) WHERE
trashed_at IS NOT NULL AND missing_since IS NULL AND hidden = 0`.
`trashed_at` already exists and already feeds visibility.

**Store / service / bridge.**
- `store.TrashAssets(ctx, ids)` sets `trashed_at` on present assets;
  `store.RestoreAssets(ctx, ids)` clears it.
- `core.FilterTrash`: the timeline reads it with the Trash predicate (blocks
  ignored: blocked photos show in Trash like any other), so the grid,
  scrubber and Viewer work unchanged.
- `servable` (shared rules) for media, details and Explorer.
- `fileops.Recycle(ctx, paths) ([]fileops.Outcome, error)` (shared rules);
  then `store.RemoveFiles` for the recycled paths, so the asset becomes
  missing and keeps its names, tags and albums should it come back. It keeps
  `trashed_at`, so a file restored from the Recycle Bin returns to Trash.
- Services: `LibraryService.TrashAssets(ids)`, `RestoreAssets(ids)`,
  `DeleteFromDisk(ids)` (the last only for trashed assets; per the Trash
  decision, also extra exact copies from Duplicates), `TrashCount()`.
- Errors: `not_recyclable` (the shell would not recycle a file: nothing
  happened to it), `delete_failed`.

**UI.** Sidebar "Trash" (with count) → `/trash` (LibraryView, Trash filter).
Viewer and Details panel: "Move to Trash" (no confirmation needed: it's
reversible; a Notice offers Undo). In Trash: "Restore", "Delete from disk…"
(confirm dialog naming the file count and "to the Recycle Bin"), "Restore
all", "Delete all from disk…".

**Risks and tests.**
- Recycling a temp file under a scratch folder lands in the bin (checked with
  `SHQueryRecycleBin`); the sink refuses an item without the recycle flag
  (unit-tested decision function); a refused item is reported and untouched.
- Trashed assets vanish from timeline, search, counts, Things, People,
  Places, Events, albums and Duplicates (one visibility test per reader, like
  `visibility_test.go`); restore brings them back.
- A trashed blocked photo appears in Trash; its media is served there.
- Nothing in tests touches a real photo folder: fixtures under `t.TempDir()`
  or a scratch folder.

## U3 — Thumbnail cache cap (Wave 1)

**Data.** 0021: `thumb_views (asset_id PRIMARY KEY REFERENCES assets ON
DELETE CASCADE, viewed_at INTEGER NOT NULL) WITHOUT ROWID`. Setting
`thumb_cache_mb` (default 2048, range 256–102400), `core.Settings.thumbCacheMB`.

**Engine.**
- `media.Cache` keeps a running byte total: a background walk at start, then
  `Write` adds and eviction subtracts (an approximate total that the next
  walk corrects is fine).
- `MediaService` records each served thumbnail or sprite in an in-memory set
  (mutex); a flusher writes the set in one batched upsert every 30 s and at
  shutdown. No database write per request.
- An eviction loop (in `index`, held by pause like the stages; not an AI
  stage, so not held on battery) wakes when the total passes the cap and
  removes least-recently-viewed assets' thumbnail pair and sprite, oldest
  first (never-viewed by `added_at`), down to 90% of the cap. It skips
  assets whose embedding, faces or OCR job is unfinished: the image model
  reads the 1024 px thumbnail from the cache.
- `thumb_version` is unchanged: the media handler already rebuilds a missing
  thumbnail on demand (`Cache.Ensure`).

**Surface.** `LibraryService.SetThumbCacheMB(mb)`; Settings shows the
cap and the current size (`LibraryService.ThumbCacheSize()`).

**Risks and tests.**
- Eviction order and the skip rule (store + media tests with a tiny cap).
- A thumbnail evicted while the grid shows it is rebuilt on its next request
  (services test).
- View batching: N requests make one write (counting test).
- Benchmark: eviction pass and `thumb_views` upsert at 100k assets.

## U4 — OCR (Wave 1)

**Model.** PP-OCRv5 mobile detector plus the recogniser chosen in the
decisions, fetched by `scripts/deps.sh` from pinned Hugging Face revisions,
SHA-256-checked, with their licence, into `deps/runtime/models/ocr`. Never
downloaded at run time. `paths.OCR()` locates it.

**Engine.**
- `internal/ai/ocr.go`: detection, line crops and CTC recognition ported from
  `spikes/cmd/ocr`, on the CPU, spinning off, lines batched 4 by width. It
  validates outputs like the other models (NaN, shapes).
- `core.StageOCR = "ocr"` with `ai.OCRVersion`. It is not in `core.Stages`:
  only eligible photos get a job. `waitsFor[ocr]` = metadata, thumbnail,
  embedding. `Stage.AI()` includes it (held on battery).
- Eligible = a photo whose AI kind is Screenshots or Documents at the current
  tag confidence. With OCR on: turning it on queues every eligible photo; the
  embedding save queues newly eligible ones in the same transaction. Turning
  it off removes unfinished OCR jobs; stored text stays searchable.
- `index`: an OCR stage with its own model holder (loaded only with work),
  pause and battery like the others. Progress gains `ocr` (visible photos
  waiting).

**Data.** 0022: `ocr_text (asset_id PRIMARY KEY REFERENCES assets ON DELETE
CASCADE, model TEXT NOT NULL, text TEXT NOT NULL)` and an external-content
FTS4 table `ocr_fts` (unicode61, diacritics removed) kept in step by
triggers. FTS4 is compiled into go-sqlite3 by default; FTS5 would need a
build tag in every build and test command.

**Search.** `store.TextMatches(ctx, query, limit)` returns visible assets
whose text contains every query word (prefix match), joined through
`visible`, so blocked or trashed text never reaches search. The search
service merges them with the semantic hits as decided below.

**Surface.** Settings: "Find text in screenshots and documents" toggle
(`LibraryService.SetOCR(on)`), off by default.

**Risks and tests.**
- Real-model test on a rendered screenshot fixture (text drawn by the test)
  through `testenv.Runtime`: the words come back.
- Queueing on enable and on new embeddings; dequeue on disable; blocked and
  trashed text never returned (store tests).
- Benchmark: OCR-queue fetch at 100k assets with 10k eligible.

## U5 — Forgotten-PIN reset through Windows sign-in (Wave 1)

**Engine.** `internal/signin` (`signin_windows.go`, `signin_other.go`):
`Verify(ctx, owner uintptr, reason string) error` returns nil,
`ErrNotVerified` (cancelled or wrong) or `ErrUnavailable`.
- Windows Hello through `UserConsentVerifier`
  (`IUserConsentVerifierInterop::RequestVerificationForWindowAsync`, owned
  by Lumen's window) when it is set up; otherwise as decided below.
- `store.ReplaceBlockPIN(ctx, pin)` replaces the salt and hash in one
  transaction. Blocks are untouched.

**Surface.** `LibraryService.ResetBlockPIN(newPIN)`: verifies with Windows,
then replaces the PIN. The Blocked screen stays locked afterwards (the user
unlocks with the new PIN), and the PIN rate limit and idle lock are
unchanged. Errors: `signin_failed`, `signin_unavailable`.

**UI.** The Blocked screen's PIN form gets "Forgot PIN?": the user types a new
PIN twice, then Windows asks them to sign in.

**Risks and tests.** The Windows call can't run unattended, so the service
takes a `verify` function (production: `signin.Verify`) and tests inject
outcomes: success replaces the PIN and keeps blocks; failure and
unavailability change nothing; the rate limiter and lock state are as before.
The real dialog is exercised in acceptance, including a wrong sign-in
(cancelled through UI Automation).

## U6 — Event blocks (Wave 2)

**Data.** Event block = an `event_tags` row of kind `block` (U1) and a
`blocks` row `kind = 'event'`, `target_id` = that tag. 0023 adds the
`blockVisible` branch (`EXISTS` in `event_members` for the tag) and the
visibility triggers.

**Behaviour.** Settling keeps the members equal to the event as it regroups,
so photos that join hide, photos that leave come back, and a split keeps
both halves blocked. Label mode removes the event from Events and its name
from the Details panel; photos stay.

**Surface.** `SetBlock(kind=event, targetID=event id)` resolves the event's
current members (store); `Blocks()` names it with its stored name.
EventsView card menu and the Viewer/Details panel ("Block this event").

**Risks and tests.** Joiner hidden in the same transaction as its ingest or
metadata save; leaver visible again; split; gap change; unblock restores;
label mode; settle cost at 100k (benchmark).

## U7 — Face blocks (Wave 2)

**Data** (0023):

```sql
CREATE TABLE face_blocks (          -- a blocked face: where it was, to find it again
  id INTEGER PRIMARY KEY,
  asset_id INTEGER NOT NULL REFERENCES assets (id) ON DELETE CASCADE,
  at_ms INTEGER NOT NULL, x REAL NOT NULL, y REAL NOT NULL, w REAL NOT NULL, h REAL NOT NULL,
  face_id INTEGER REFERENCES faces (id) ON DELETE SET NULL,
  lookalikes INTEGER NOT NULL
);
CREATE TABLE face_block_vectors (face_block_id, model, vec, PRIMARY KEY (face_block_id, model));
CREATE TABLE face_block_faces (face_block_id, face_id REFERENCES faces ON DELETE CASCADE,
  asset_id, PRIMARY KEY (face_block_id, face_id));   -- index on asset_id
```

`blocks` row: `kind = 'face'`, `target_id = face_blocks.id`.

**Identity across reindexing.** `SaveFaceVersions` already keeps a face's id
when a re-analysis finds a face at the same spot (IoU ≥ 0.5). Face blocks
add a second anchor: the stored box. When the anchor photo is analysed again
(including after a `FacesVersion` bump), the face at the stored box becomes
the anchor, its vector is stored under the new model, and look-alikes are
recomputed for that model. Faces are compared only with a block vector of
their own model. During a model upgrade the faces stage processes face-block
anchor photos first, so new-model look-alikes are covered as early as
possible.

**Look-alikes.** Cosine similarity ≥ `core.FaceLookalikeMin` against the
anchor's vector, computed in the store inside `SaveFaceVersions`' transaction
(new faces covered in the same commit) and on block creation. The threshold
is measured on `LUMEN_FACE_PHOTOS` with the indexer's verified people as
labels, leaning as decided below; the test asserts both rates.

**Behaviour.** Photo mode hides every photo with a covered face; label mode
hides the covered faces' boxes and names (Details panel, People faces, covers).

**Surface.** `SetBlock(kind=face, targetID=face id, …)` plus a look-alikes flag:
`LibraryService.BlockFace(faceID, mode, lookalikes)`. BlockDialog gains the
"this face only / and look-alikes" choice. Viewer and Details panel face
menus.

**Risks and tests.** Re-analysis at a new model keeps the block (store test
with two model versions); look-alike threshold on the corpus; new photo of a
look-alike hidden in its save transaction; deleting the anchor face keeps the
block by box; benchmark of look-alike matching at 300k faces.

## U8 — Covered-media browser (Wave 2)

**Store.** `BlockCoverage(ctx, blockID)` returns the present, untrashed
assets the block's target matches (whatever its mode), as grid items, each
with the other photo-mode blocks that also hide it (id and name). It uses
`blockMatch`.

**Service.** `LibraryService.BlockedItems(blockID)` and
`MoveCoveredToTrash(ids)`, both behind `BlockGate`. Media serving follows
`servable` with the gate.

**UI.** Settings → Blocked: each block opens a virtualised grid of its
covered photos; clicking opens the Viewer (refactored to take an item source,
so the library grid, Trash and the browser share it); "Move to Trash";
a line under each photo hidden by other blocks ("Also hidden by …"). Leaving
the screen locks it and closes the browser and Viewer.

**Risks and tests.** Locked access returns `blocks_locked` and 404 for media;
the "still hidden by" list with two overlapping blocks; a blocked video plays
only while unlocked.

## U9 — Duplicates: pre-select the best copy (Wave 2)

**Rule** (`store/bestcopy.go`, pure and table-tested), comparing two items:
1. resolution (width × height), higher first;
2. format and re-save signals: an original-camera file (EXIF camera and EXIF
   date) before one without; then lossless or camera-native formats (PNG,
   TIFF, HEIC/HEIF, AVIF) before JPEG before WebP/GIF/BMP; a messenger
   re-save name (`IMG-YYYYMMDD-WA…`, `… (1).jpg`, `-edited`) last;
3. file size, larger first;
4. metadata completeness: EXIF date, GPS, camera, each one point;
5. ties: the oldest file.
For exact copies (one photo in several files, identical bytes) only the path
differs: the oldest file is kept, then the shortest path.

`core.DuplicateItem` gains the fields the rule reads and `best`. The UI
pre-selects it; the user can pick another. "Review" confirms: the others go
to Trash (similar photos) or, for exact copies, as decided below. Nothing
happens before the confirmation.

**Tests.** Each rule step decides a pair on its own; the full order on mixed
groups; the UI keeps the user's choice across reloads and does nothing until
confirmed.

## U10 — Organise on disk (Wave 3)

**Data.** 0026:

```sql
CREATE TABLE organise_runs (id INTEGER PRIMARY KEY, template TEXT NOT NULL, created_at INTEGER NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('planned', 'running', 'done', 'undoing', 'undone', 'failed')));
CREATE TABLE organise_moves (run_id INTEGER NOT NULL REFERENCES organise_runs (id) ON DELETE CASCADE,
  seq INTEGER NOT NULL, file_id INTEGER, asset_id INTEGER NOT NULL, from_path TEXT NOT NULL, to_path TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('planned', 'moving', 'moved', 'undoing', 'undone', 'skipped', 'failed')),
  error TEXT, PRIMARY KEY (run_id, seq)) WITHOUT ROWID;
CREATE TABLE organise_dirs (run_id INTEGER NOT NULL REFERENCES organise_runs (id) ON DELETE CASCADE,
  path TEXT NOT NULL, PRIMARY KEY (run_id, path)) WITHOUT ROWID;     -- folders Lumen created
```

**Plan (dry run).** `fileops.Plan(template, scope)` renders each visible
file's destination from the template's tokens (decided below), in the
destination root decided below; names that collide (with a file on disk,
another planned file, or a case-folded twin) are resolved as decided; files
already in place are left out. Blocked, trashed, hidden and missing assets
are never planned (`visible`), nor is a destination inside a blocked folder.
Only visible names reach folder names (a label-blocked place or event name is
never written to disk).

**Run.** For each move: journal `moving` (committed), `MoveFileExW` without
replace, then one transaction sets `files.path` and `moved`. Scans are held
while a run moves files (`index.Indexer.HoldScans`), and the rescans queued
meanwhile find the database already matching the disk, so they change
nothing. A file changed since the preview (size or mtime) is skipped.

**Crash.** At start, a `moving` row is resolved from the disk: only `to`
exists → finish it; only `from` exists → back to `planned`; both or neither →
`failed`, nothing touched. The run can then be resumed or undone.

**Undo** moves `moved` rows back in reverse order with the same journaling,
never overwriting (a conflict is reported), and removes the folders Lumen
created if they are empty.

**Surface.** Settings → "Organise files on disk" (a dedicated view):
template editor with token chips and a live example, source scope, preview
(virtualised list with counts of moves, skips and renamed collisions),
"Move N files", progress, Resume, Undo. Services: `PlanOrganise`,
`StartOrganise(runID)`, `UndoOrganise(runID)`, `OrganiseRuns()`; event
`organise:progress`.

**Risks and tests.** Collision; a file appearing at the destination between
preview and run (never overwritten); crash injected between journal and move
and between move and commit (resume and undo both tested); blocked, trashed,
missing assets left alone; albums, tags, people and blocks still attached
after the move; scans held. All on scratch copies under `t.TempDir()` or
a scratch folder.

## U11 — XMP sidecar export (Wave 3)

**Format.** Pure Go, no dependency: an `x:xmpmeta` packet with `dc:subject`
(the photo's applied Things), `Iptc4xmpExt:PersonInImage` plus MWG face
regions (`mwg-rs:Regions`) for named people, `xmp:Rating` 5 for favourites,
and a `lumen:` marker. Blocked labels and photo-blocked, hidden or trashed
assets are never exported: names written beside photos would leave the app.

**Data.** 0027: `xmp_exports (asset_id, path, hash)`: which sidecars Lumen
wrote and what it wrote, so it only ever rewrites its own and skips
unchanged ones.

**Run.** Opt-in in Settings (off by default), then a background pass (pause
and battery respected) that writes changed sidecars through `fileops`. It
never modifies an original and never overwrites a sidecar it did not write.

**Tests.** Golden packet; foreign sidecar left alone; blocked label omitted;
re-export after a tag change; originals byte-identical after export.

## Waves and ownership

At most four builders run at once. Each owns a disjoint set of files; shared
files (`SettingsView.vue`, `DetailsPanel.vue`, `Viewer.vue`,
`indexer.go`'s `Start`) are owned by one unit per wave, and the others add a
component or function of their own and ask the lead to mount it.

- **Wave 1:** U1 Things/Events, U2 Trash + `fileops` recycle, U3 cache cap,
  U4 OCR, U5 PIN reset.
- **Wave 2:** U6 event blocks, U7 face blocks, U8 covered-media browser, U9
  best copy.
- **Wave 3:** U10 organise, U11 XMP.

## Decisions

The product owner took the recommended option on all seven questions
(1 October 2026):

| # | Question | Decision |
| --- | --- | --- |
| 1 | Pin and rename | Pinned Things and Events sort first in their views (newest pin first) and survive restarts. Renaming a built-in Thing changes its label everywhere, including the search box's name matching and suggestions; the AI keeps its original prompts. The old name stops matching; "Reset name" restores it. |
| 2 | XMP | Sidecars only, pure Go, no new dependency, named `IMG_1.jpg.xmp`. Never overwrite a sidecar Lumen didn't write. Favourites are `xmp:Rating` 5. Blocked labels and blocked photos are never exported. |
| 3 | Trash | No auto-empty. "Delete from disk" (Recycle Bin, confirmed) exists in Trash, and in Duplicates for the extra files of exact copies. |
| 4 | Organise | Tokens `{Year} {Month} {MonthName} {Day} {Event} {Place} {Camera} {Name} {Ext}`, default `{Year}/{Month}/{Event}/{Name}`. Each file moves only inside its own source. Collisions get ` (2)`, ` (3)`…; nothing is overwritten. |
| 5 | OCR | SQLite FTS4. Photos whose text has every query word (prefix match) come first, then the semantic results without duplicates. English recogniser. |
| 6 | PIN reset | Windows Hello through `UserConsentVerifier` when set up; otherwise a CredUI prompt for the account password, checked with `LogonUser`. |
| 7 | Look-alikes | Lean to hiding more: the threshold catches at least 95% of the person's other faces on the corpus. |
