# Feature completion audit

This is the working inventory for [issue #31](https://github.com/awaisjameel/lumen/issues/31),
started on 2 November 2026 against `bae949c`. It records the accepted work
before implementation. An implementation reference establishes that a flow
exists; it does not establish fresh runtime acceptance. Historical results
are not counted as results for this checkout.

## Branch and pull request workflow

[`feat/31-feature-completion`](https://github.com/awaisjameel/lumen/tree/feat/31-feature-completion)
is the integration branch for #31, created from `main` at `bae949c`.

- Create a separate working branch from the integration branch for each new
  child or sub-issue. Open its PR against `feat/31-feature-completion` and
  link both the child issue and parent #31.
- The existing [PR #37](https://github.com/awaisjameel/lumen/pull/37), from
  `fix/31-collection-recovery`, targets the integration branch and covers
  #35 and #36 together, including their shared native suite and audit record.
- Review and merge accepted child PRs into the integration branch. Keep
  implementation/verification status distinct from delivery to `main`;
  merging a child PR does not complete #31 or establish a shipped release.
- After all accepted #31 work and its verification gates are complete, open
  one PR from `feat/31-feature-completion` to `main`. Record the accepted
  scope, child PRs, final validation and explicit deferrals in that PR.
  Until then, child PRs target the integration branch and the final PR waits.

The existing CI workflow checks pull requests regardless of their base branch,
so child PRs retain the same checks. The open latency gate below still applies
to PR #37 and the eventual integration acceptance.

## First milestone: collection recovery and event name Undo

Accepted scope, recorded before implementation:

- **[A31-001 / #35](https://github.com/awaisjameel/lumen/issues/35) (P2, defect):** make the empty Things, Places and Events screens'
  recovery links reach the PIN-protected Blocked screen. The existing
  `/blocked` links hit the router's catch-all and return to the Library.
- **[A31-002 / #36](https://github.com/awaisjameel/lumen/issues/36) (P2, defect):** restore the previous event name override when
  undoing a rename or Reset name from an event's timeline header. The header
  currently saves the displayed automatic name as an override, so Undo
  silently stops future automatic naming. The Events card already uses
  `nameOverride` correctly.

Acceptance requires regressions that fail on the original code, native
scratch-library checks through the real controls, persistence after restart,
the existing PIN boundary, and the repository checks. Neither repair changes
the engine contract, schema, original media, network policy or screenshots'
visual design. The bounded child issues are linked below and registered as
sub-issues of #31. The epic remains open beyond this milestone.

The order is #35 first, because hiding the last collection breaks the offered
route back to privacy controls, then #36, because Undo changes a persistent
naming choice. Both are P2 broken journeys with bounded repairs; neither needs
an original-file mutation or a new product policy.

## Inventory

Status **implemented** means source and regression coverage exist, with no
reproduced gap in this initial review; runtime acceptance is tracked separately
below. **Defect** is a reproduced discrepancy selected for repair. **Partial**,
**missing**, **deliberately deferred**, **external acceptance blocked** and
**superseded** are used only with the evidence or decision that warrants them.
This is an initial inventory, not a claim that every interaction has been
exhaustively audited.

All engine calls pass through the [bridge](../frontend/src/bridge/index.ts)
and [generated bindings](../frontend/bindings/lumen/services).
SQL and migrations belong to [store](../internal/store); library events and
grid refresh belong to [LibraryService](../services/library.go) and the
[library store](../frontend/src/library/store.ts). The matrix calls out the
specific owners beyond those common layers.
Service validation and typed failures use [error codes](../services/errors.go),
the bridge and [localised error text](../frontend/src/shared/errors.ts);
[contract tests](../services/contract_test.go) guard the mirrors. Collection
lists use the shared list stores and live reload, while privacy changes clear
cached entries through [block refresh](../frontend/src/shared/block.ts).

| Feature / flow and expected behaviour | Implementation and regression evidence | Reproducible gap / disposition | Performance and privacy implications | Status at inventory |
| --- | --- | --- | --- | --- |
| First run: choose folders explicitly, add none automatically | `onboarding/Welcome.vue`, `services/library.go`, `store/sources.go`; `Welcome.test.ts`, `services/sources_test.go`, native `onboarding` | None reproduced; source overlap uses Unicode path rules | No scan before consent; originals stay in place | Implemented |
| Sources: watch subfolders, reconcile external changes and unavailable drives | `scanner/watch_windows.go`, `index/sources.go`, `library/SourcesNotice.vue`; scanner/index tests, `SourcesNotice.test.ts` | Removable-drive and permission matrices still need real hardware | Durable identity preserves annotations; no disappearance merely because a drive is offline | Implemented; hardware acceptance open |
| Indexing: resumable stages, pause, politeness, CPU budget, failures and retries | `index/`, `store/jobs.go`, `library/IndexStatus.vue`; `store/queue_test.go`, index tests, `IndexStatus.test.ts` | No missing stage identified; crash/race coverage is distinct from arbitrary power loss | Durable SQLite queue, bounded workers, models load on demand | Implemented |
| Timeline: dates, filters, virtual scrolling, selection and navigation | `library/LibraryView.vue`, `library/store.ts`, `services/selection.go`; colocated frontend tests, `services/selection_test.go`, native `viewer`, `interface` | None reproduced in initial review | Wall-clock UTC dates; kept counts, selection revisions and visibility filtering | Implemented |
| Viewer: zoom, pan, orientation, comparison, slideshow, details | `library/Viewer.vue`, `media/ImageSurface.vue`, `services/viewing.go`; component/service tests, native `viewer` | No missing control identified | Viewing copies are bounded; display changes preserve originals | Implemented |
| Search: descriptions, people, relevance, refinements and cancellation | `search/SearchBox.vue`, `services/search.go`, `internal/search/`; component/service tests, native `interface`, `tour` | Real-library relevance and hardware latency are separate acceptance | In-memory int8 vectors; blocked IDs filtered before results | Implemented |
| OCR: opt-in English recognition and text search | `services/ocr.go`, `index/ocr.go`, `ai/ocr.go`, `store/ocr.go`; OCR/search regressions | English and eligible image kinds are deliberate limits | Local CPU model; durable OCR jobs; no upload | Implemented |
| People: names, merge/split, rejection, cover, face review and search | `people/`, `services/people.go`, `internal/people/cluster.go`; face/correction tests | Relationships and Me identity belong exclusively to [#30](https://github.com/awaisjameel/lumen/issues/30); a real-face corpus remains opt-in | User corrections win; blocked identities and faces stay hidden | Implemented base flow; #30 separate |
| Things: built-in/custom labels, manual corrections, pin/name/confidence | `things/ThingsView.vue`, `services/things.go`, `store/tags.go`; Things and tag regressions | Recovery link to blocked content is A31-001 | Stored scores avoid rescanning; manual overrides beat tagging | Defect in recovery; tagging implemented |
| Places: offline atlas, preview, country filter and correction | `places/PlacesView.vue`, `internal/places/`, `services/corrections.go`; Places/correction tests | Recovery link is A31-001; city distance is documented approximation | Bundled city data; no map tiles or geocoding requests | Defect in recovery; grouping implemented |
| Events: grouping, name/reset, pin, split/merge and blocks | `events/EventsView.vue`, `library/CollectionHead.vue`, `store/events.go`, `store/eventtags.go`; event oracle tests, `CollectionHead.test.ts` | A31-001 recovery; A31-002 automatic name Undo | Incremental grouping; annotations follow members, no persistent event table | Defects selected |
| Manual albums: membership, name, cover, deletion, adding photos | `albums/AlbumsView.vue`, `library/AlbumPhotoPicker.vue`, `store/albums.go`; album/picker tests, native `collections` | No confirmed missing album operation in initial review | Membership follows asset identity and respects visibility | Implemented |
| Smart albums: typed rules reevaluate metadata, favourites, tags and people | `albums/`, `store/albums.go`; `store/albums_test.go`, native `collections` | None reproduced | Existing rule/count budgets apply; hidden media excluded | Implemented |
| Blocking: labels/photos/folders, PIN, idle lock, Undo and covered viewer | `settings/BlockedView.vue`, `services/blocks.go`, `services/gate.go`, store visibility; gate/media/block tests, native `interface` | Engine boundary exists; A31-001 prevents discovery from three empty views | Every media route must enforce the same visibility; no encryption claim | Implemented boundary; recovery defect |
| Duplicates: choose best, inspect, explicitly recycle redundant copies | `duplicates/`, `services/duplicates.go`, store duplicates; duplicate component/service tests | None reproduced; exact large-file matching uses documented hash sampling | Last verified copy retained; no permanent deletion | Implemented |
| Trash: soft delete, Undo, restore and confirmed Windows Recycle Bin | `trash/`, `services/trash.go`, store trash; Trash/service tests, native `collections`, `viewer` | None reproduced | Recycle Bin is explicit; nothing emptied automatically | Implemented |
| Rename/move/organise: preview, collisions, journal, recovery and Undo | `organise/`, `media/FileTools.vue`, `services/organise.go`, `internal/fileops/`; journal/service tests, native `organise`, `viewer`, `file-tools` | Real removable-drive and disk-failure matrices remain external acceptance | Preserve originals and block rules; never overwrite collisions | Implemented |
| Export: original/JPEG/PNG, presets, cancellation and per-file outcomes | `media/FileTools.vue`, `services/exports.go`, fileops; export/service tests, native `viewer`, `customise` | None reproduced | Bounded jobs; atomic publication preserves existing destinations | Implemented |
| XMP: opt-in owned sidecars, annotations, recovery and recycling | `services/xmp.go`, `index/xmp.go`, `internal/xmp/`, `store/xmp.go`; XMP/dirty queue tests | None reproduced | Never rewrite another application's sidecar or original | Implemented |
| RAW and other images: catalogue, worker, preview fallback and labels | `core/formats.go`, `internal/media/`, `native/lumen-raw/`; media fixtures, native `formats` | DNG compression variants and preview size limits are documented | Worker/capped decoding; real-camera corpus acceptance is separate | Implemented with deliberate limits |
| Video: opt-in scan, direct/converted playback, tracks, subtitles, frames and resume | `library/VideoPlayer.vue`, `services/stream.go`, media; stream/player tests, native `viewer`, `interface`, `formats` | Codec/GPU/Windows combinations require target hardware | Secret loopback URL, Host check, block gate; covered playback records no history | Implemented |
| Compression copies: analysis, verified copies, pause/cancel/restart and JPEG restoration | `compression/`, `internal/compress/`, `services/compression.go`; coordinator/service tests, native `compression` | 16 MP, 256 MiB and newest-100 history limits stay documented | Independent preservation checks; original bytes retained | Implemented with deliberate limits |
| Same-format compression replacement and Undo | [Phase 11 record](history/phase11-compression-2026-10-09.md), [audit limits](history/phase11-audit-2026-10-10.md#limits-that-remain-for-the-owner) | Not selected: needs preserved originals, publication journal, safe Undo, cancellation and fault/restart design before UI | File-changing product slice; cannot share the copies-only promise without design | Deliberately deferred |
| Customisation: appearance, shortcuts, reset, portable presets and persistence | `settings/SettingsView.vue`, `shared/preferences.ts`, `core/preferences.go`; preference/customisation tests, native `customise` | None reproduced; legacy `?section=compression` is explicitly mapped to Files | Database owns settings; export excludes PIN, history and sources | Implemented |
| Diagnostics: local crash notice, logs, licence list and problem report | `about/`, `services/system.go`, `internal/diag/`; diagnostics/service tests, native `collections` | Real crash reporting is distinct from fault-free launch | Reports stay local and may contain paths; user chooses whether to share | Implemented |
| Installer: per-user offline install, running-app guard, retained library | `build/windows/nsis/`, `scripts/installer_payload.py`; payload tests, [acceptance harness](../e2e/installer/README.md) | Clean-machine/older-Windows/signing execution needs external acceptance | Bundle WebView2 and runtime; no runtime download | Implemented; external acceptance blocked |
| Updates: policy, authenticated discovery/download, staging, backup and recovery | `internal/update/`, launcher/helper, update tests, native `updates`; [production prerequisites](production-signing.md) | Empty trust is intentional. Certificate/key custody, hosting, signed feeds, real signed upgrade hops and faulted activation remain unaccepted | Sole opt-in network exception; must stay inert without trust | External acceptance blocked |
| Historical vector extension, event table and runtime model downloads | [Architecture decisions](architecture.md#deviations-from-the-plan) | Replaced by int8 memory search, read-time event grouping and bundled models | Avoid new native/network dependencies | Superseded |
| Other platforms, translations, captions and LAN import | [Original later roadmap](plan.md#later), [known limits](../README.md#known-limits) | Not selected; require explicit product slices rather than treating roadmap text as a regression | Offline policy and Windows acceptance remain binding | Deliberately deferred |

## Bounded child issues

The A31 identifiers are stable audit references. GitHub issues
[#35](https://github.com/awaisjameel/lumen/issues/35) and
[#36](https://github.com/awaisjameel/lumen/issues/36) track their implementation;
neither child nor the epic is closed by this document.

### A31-001: Restore blocked-collection recovery navigation

Tracked in [#35](https://github.com/awaisjameel/lumen/issues/35).

Priority P2. Open Things, Places or Events with no visible collections and
choose **Manage blocked**. Expected: the Blocked screen requests the PIN.
Actual at the inventory commit: `/blocked` matches the catch-all and opens
Library. The same failure affects the last collection disappearing after a
block. Tests even declared a fictitious `/blocked` route.

Scope: the three links and their regression fixtures. Acceptance: all three
controls resolve to the registered Blocked route, require the existing PIN,
allow unblocking, and leave the gate locked after navigation away. Check a
fresh empty view, a view emptied by blocking, keyboard activation, and restart.
Dependencies: none; no new routes or PIN bypass.

### A31-002: Preserve automatic event naming when undoing header edits

Tracked in [#36](https://github.com/awaisjameel/lumen/issues/36).

Priority P2. Open an automatically named event's timeline, rename it, and
choose **Undo**. Expected: `nameOverride` is empty again. Actual at the
inventory commit: the previous displayed automatic name is saved as a custom
override. A subsequent date/place correction cannot update that title.

Scope: header rename/reset history only. Acceptance: Undo of a rename restores
an absent override or the exact previous override; Undo of Reset name restores
a custom override; navigation does not retarget Undo; state survives restart
and an automatic name can change with corrected metadata. The Events card's
existing correct behaviour remains consistent. Dependencies: existing Event
wire type and store reset semantics; no binding or migration change.

## Verification and remaining work

Verification on 3 November 2026, on `fix/31-collection-recovery`:

| Gate | Current result |
| --- | --- |
| Regression before/after | Four failures on the original code; all 51 focused component tests pass after repair. The original production build reproduces all three wrong links in both themes at 1440/800 px and the persisted event override after Undo. |
| Frontend and static checks | `scripts/lint.sh --no-bench` passes, including all 947 frontend tests in 110 files, Go lint/vet/deadcode, govulncheck, Python, ShellCheck, installer/dependency regressions, runtime DLL imports and docs checks. |
| Production build/package | Baseline `wails3 build` and repaired `wails3 task package` pass. The local 0.12.1 installer is unsigned; no version bump, release or installation on the host was performed. |
| Native repair acceptance | `node e2e/run.mjs collection-recovery`: 51/51. Both themes, both widths, keyboard links, last-collection recovery, PIN unlock, unblocking, gate relock, automatic/custom name Undo, later location correction and restart pass; no external request or frontend error. |
| Broader native acceptance | Freshly indexed 300-photo scratch library: onboarding 7/7, collections 8/8, tour 5/5 (68 route/width combinations, with no sideways overflow, external request or console/page error). Synthetic photos supply no named People or GPS places for the tour; the repair suite supplies its own corrected places and events. |
| Extracted installer | Payload hashes/inventory 49/49; native recovery 51/51 and onboarding 7/7 pass against the extracted runtime with only Windows/System32 on the application's PATH. This is extracted-payload acceptance, not a clean-machine installation. |
| Go race regressions | Full named-package race command passes: 20 tested packages and 1,560 test/subtest passes, with the cached real-camera RAW corpus supplied. Four corpus-dependent face tests and one symbolic-link subtest skip; two child-process-only helpers skip in the parent run as designed. Seven packages have no test files. |
| Large-library latency | Baseline `scripts/bench.sh` passes all 126 measurements for 100,000 photos in three block configurations. The first post-run gate has one `SaveFaces64` failure (11.502 ms against 8 ms); five focused repeats pass at 4.392, 4.468, 4.342, 4.446 and 4.643 ms. A full rerun has four over-budget timings (listed below). A quiet-machine verification remains open. Budgets are unchanged. |
| Generated contracts | Package generation leaves `frontend/bindings`, `go.mod` and `go.sum` unchanged. No exported method, wire type or schema changed. |
| Documentation/screenshots | Link-checker regressions and all relative links pass; all eight gallery images agree with their capture record. No gallery recapture is needed for these behaviour-only repairs; fresh native PIN-screen captures were inspected in light and dark. |

The accepted defects are implemented and functionally verified locally.
Milestone performance acceptance remains open until the full gate passes on an
idle machine. The child issues remain open for this gate and review/merge of
PR #37 into `feat/31-feature-completion`; delivery to `main` follows the
integration workflow above. The rest of #31 is an ongoing audit, not a claim
that every supported interaction has passed native acceptance in this run.

Ignored evidence is retained under `e2e/.work/issue31/`: the before/after
component logs, `native-before.json`, `native-names-before.json`,
`native-recovery-after.json`, `native-broader-after.log`, `package-after.log`,
`lint-after.log`, `package-acceptance.log`, `package-recovery-after.json`,
`race-after.jsonl`, `race-after.log`, `bench-before.log`, `bench-after.log`,
`bench-faces-repeat.log` and `bench-final.log`. The broader suites' fresh library and
screenshots are in `acceptance/`. Standard repair-suite reports/screenshots
remain under `e2e/.work/results/collection-recovery/`.

Dependency observations are recorded separately from the accepted repairs:
the unchanged frontend lockfile currently reports 19 moderate development-only
findings through glob/minimatch, while `npm audit --omit=dev` reports zero.
Go govulncheck reports no reachable vulnerability and one finding in a required
module that this code does not call. No advisory or check was suppressed, and
dependency upgrades remain related to #28 rather than part of these fixes.

Remaining external acceptance includes a real-person corpus, symbolic-link
creation where Windows grants the privilege, target codec/GPU/removable-drive
and permission matrices, interactive Windows sign-in for a forgotten PIN,
clean-machine and older-Windows installation, and real signed installed update
activation/recovery. The [beta checklist](beta-testing.md#what-to-try),
[corpus instructions](../e2e/README.md#tests-on-real-files-in-go),
[installer harness](../e2e/installer/README.md) and
[signing prerequisites](production-signing.md) own those follow-ups. Local
tests, extracted payload checks and synthetic photos do not supply those results.

The corpus skips are `TestFacesOnRealPhotos`, `TestFacesOnTheGPUMatchTheCPU`,
`TestIndexerFindsPeople` and `TestFaceLookalikeThresholdOnTheFaceCorpus`.
The privilege-dependent skip is
`TestAnExportPreviewRefusesMissingFoldersFilesAndLinks/links`.
`TestCrashForAParent` and `TestSpinForAParent` are helper entry points exercised
by their parent tests, rather than standalone acceptance gates.

The full latency rerun exceeds these unchanged budgets: folder `Things`
10.279/10 ms; combined-block `RebuildBlocks` 901.424/900 ms; combined-block
`SaveFaces64` 11.719/8 ms; and unblocked `SaveFaces64` 8.123/8 ms. The baseline
unblocked face write is 5.045 ms, and the first post-run folder/combined values
are 5.597/5.801 ms. There is no diff in Go/core/store or the Go manifests; a
system-load sample shows active editor processes. Later processor-counter
samples show 95–97% CPU use, and the owner confirms that other heavy work is
running. These observations are consistent with workload variation but do not
isolate its cause. Do not count this gate as passed or change the budgets to
close it; retain all results and rerun `scripts/bench.sh` during an idle window.

Related deliveries: [#28](https://github.com/awaisjameel/lumen/issues/28)
(dependencies, closed), [#29](https://github.com/awaisjameel/lumen/issues/29)
(documentation, closed), and [#30](https://github.com/awaisjameel/lumen/issues/30)
(People relationships and Me, open at inventory). Public signing and update
delivery remain governed by [production signing](production-signing.md) and
[release status](release-status.md); no new public release is claimed here.
