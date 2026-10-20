# Phase 5 blocking implementation — 30 September 2026

## Current scope

Migration 0014 adds one `blocks` table with typed target kinds and two modes.
Individual assets and folders always hide media. People, Things, places, and
albums can hide their label alone or also their current matching media. Manual
albums follow membership; smart albums evaluate their saved rule. Multiple
blocks can cover one asset; removing one does not override the others.

The store's shared visibility predicate is used by timeline buckets and days,
search exclusion, albums, People, Things, Places, Events, duplicates, counts,
and index progress. Media handlers check it even for already cached images;
the video streamer uses the guarded asset file lookup. Face clustering still
sees blocked assets so existing and new faces can be assigned while hidden.
Triggers invalidate event grouping and the search exclusion cache when a
block or relevant membership changes. The `assets_timeline` partial index
remains usable.
Migration 0015 also invalidates cached visibility when tag or face membership
changes the matches of a blocked smart album, including existing 0014 libraries.
Block changes also emit `library:changed` so mounted Places, People, Things,
album and duplicate lists reload after blocking or Undo.

The typed Wails bridge offers block creation, mode changes, a PIN-gated Blocked
list, unblocking and an eight-second Undo of the most recent block. The PIN
uses a random salt and PBKDF2-SHA256 (600,000 iterations), with a brief
attempt limit in the running process. Leaving the screen locks its list again.
Changing an existing block requires PIN unlock; new blocks can still be added
while locked. Updating a rule expires its previous Undo authorization.
The list resolves current target names; blocks survive file renames and asset
moves. A person block follows a user or automatic merge. Existing hidden
people and user-hidden Things migrate to photo blocks; the internal sink
Thing stays hidden and is not converted.

## Verification

- `scripts/lint.sh` passed: Go lint, deadcode, govulncheck, frontend
  typecheck/ESLint/Prettier, 53 Vitest tests, Ruff and ShellCheck.
- `go test -race -count=1 ./internal/... ./services/...` passed on the second
  run. The first run hit the existing timing-sensitive Windows watcher test;
  that package passed in isolation and in the full rerun.
- `wails3 generate bindings -ts -i` and `wails3 build` passed.
- In a scratch library, the production WebView2 app passed native interaction
  checks: create the PIN, block a place label-only, Undo and see its card
  return, unlock Blocked with the PIN, block a place with photos, get HTTP 404
  from an already cached thumbnail URL, unblock it, and get HTTP 200 again.
  Both runs had no page exceptions. The scratch app was stopped afterward.
- Store/service regressions cover migration, PIN validation, overlap, label
  versus photo mode, new files in blocked folders, new smart album members,
  hidden search names, naming/merge exclusion, and Undo. A 100,000-asset
  blocked timeline count took about 26 ms per call on the local i7-13700H;
  this is a development benchmark, not low-end hardware or full-library proof.

## Remaining Phase 5 blocking work

- Event blocks need dynamic group ownership when Events regroup; face blocks
  need stable identity across reindexing and a tested look-alike rule.
- The Blocked screen needs an authenticated covered-media browser and the
  planned path from it to Trash. Trash itself is not implemented yet.
- A forgotten PIN cannot yet be reset through Windows sign-in. Until that
  recovery path exists, users must retain their PIN to manage existing blocks.
- Real large libraries, removable drives, blocked-video playback and native
  interaction with every block kind still need acceptance testing. The scratch
  WebView2 run covered places, PIN, Undo and thumbnail denial only.

Organise-on-disk and XMP sidecar export remain after blocking in Phase 5.

## Staged-code audit follow-up

The 53 staged files were reviewed across storage, services, generated contracts,
and frontend workflows. The audit fixes are working-tree changes; the original
staging selection was preserved. No next-phase features were started.

Resolved findings:

1. Smart-album tag and person matches could change without invalidating Events
   or search-exclusion caches. Migration 0015 fixes both trigger families;
   regressions cover adding and rejecting person matches and adding tag matches.
2. A locked caller could update an existing photo block to label-only. The
   writer transaction now rejects existing-rule changes without PIN unlock.
   Protected service operations retain their authorization lock through the
   database operation; updating a rule also invalidates its old Undo.
3. Long-lived browser media caches could bypass a subsequent visibility check.
   Image and direct-video responses now use `private, no-cache`, retaining
   conditional and Range requests while requiring visibility revalidation.
   Converted video retains `no-store`. Disk thumbnail caching is unchanged.
4. Timeline privacy refreshes retained rendered items and an open viewer until
   lazy day reloads completed. They now clear those immediately and discard
   pre-block responses, including while the grid is detached.
5. Drive-root folder blocks used a doubled separator, and the details action
   extracted `C:` instead of `C:\`. Both paths are fixed, with drive-root, UNC,
   case-insensitive, sibling-prefix, and component regressions.
6. Navigation could let a stale unlock cleanup lock the next Blocked screen.
   The bridge orders unlock/lock calls, and the screen discards stale results
   and serializes unblock confirmation with other actions.
7. A stale PIN response or completed save could overwrite or close a replacement
   block dialog. Async results and completion now belong to their captured request.
8. PIN verification accepted unbounded input and unexpected stored hash lengths.
   It now applies creation's format bounds and fails closed on damaged hash data.
   Service coverage also verifies rate-limit recovery and locked mode denial.

Final checks:

- Full lint and frontend regression gates passed; no Go or npm vulnerabilities
  were reported. All backend packages passed the uncached race suite on rerun.
  The first concurrent run encountered the previously recorded Windows watcher
  timeout; that test then passed in isolation and in the full rerun.
- Binding regeneration and the production Wails build passed without generated
  contract drift. Both smart-album trigger regressions also passed a focused
  post-build race run.
- The production WebView2 app used a new isolated version-14 scratch library,
  upgraded it to 15, and passed PIN setup/unlock, label/photo blocking, Undo,
  unblocking, and mode switching. The identical versioned thumbnail URL returned
  200 before blocking, 404 afterward through the normal browser cache, and 200
  after an authenticated switch to label-only. Leaving Blocked required PIN
  unlock again. The 720x480 viewport had no horizontal overflow or page exceptions.
- Backend media coverage exercises cached thumbnails, sprites, video frames,
  direct-video Range requests, converted video denial, and restored playback.
  Native all-kind/video and real large-library acceptance remain as listed above.
- Scratch artifacts are under ignored `bin/audit-block-review-20260930/`. The
  owned app process was stopped, and the scratch database ended with no blocks.
