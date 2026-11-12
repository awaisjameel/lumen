# PR #56 integration audit

Audit of [PR #56](https://github.com/awaisjameel/lumen/pull/56), begun on
6 November 2026 (UTC). The initial head was
[`e09f966`](https://github.com/awaisjameel/lumen/commit/e09f96639a67011b4743be958dd943d6440fea3c)
and the current base was
[`bae949c`](https://github.com/awaisjameel/lumen/commit/bae949c3e2d4d1b9342b63b103917f9621c17ed7).
The original working tree was clean, with one worktree. All eleven children
were already integrated; no review comments or review threads were present.
Fixes remain on `feat/31-feature-completion`. Nothing is merged or released;
[#31](https://github.com/awaisjameel/lumen/issues/31) remains open.

## Accepted implementation checkpoint

[Windows run 42411663796](https://github.com/awaisjameel/lumen/actions/runs/42411663796)
verifies implementation commit
[`b391b3f`](https://github.com/awaisjameel/lumen/commit/b391b3ffc4149226d5a6ba200c3e0cecdb28c2a3)
against `main` at `bae949c`. Production build, unchanged generated bindings and
modules, full static/docs checks, **1,177 frontend tests in 121 files**, all
three named-package Go race shards and **unsigned installer packaging pass**.
Static verification retains 21 command and 18 navigation negative compiler
fixtures, their valid counterparts, three navigation mutation checks and five
native-harness regressions. The PR description records current-head checks;
this checkpoint remains stable evidence for the implementation beneath the
subsequent audit-documentation update.

All **394 native checks across 16 suites pass** against the production executable:

| Suite | Passed |
| --- | ---: |
| Commands | 25/25 |
| Navigation | 74/74 |
| Collection recovery | 51/51 |
| Album drafts | 28/28 |
| Collection places | 22/22 |
| Smart-album previews | 26/26 |
| Tagging recovery | 18/18 |
| Undo recovery | 23/23 |
| Search history | 7/7 |
| Favourite Undo | 24/24 |
| Onboarding | 7/7 |
| Collections | 8/8 |
| Organise | 7/7 |
| File tools | 14/14 |
| Customisation | 45/45 |
| Unconfigured updates | 15/15 |

The [native artifact](https://github.com/awaisjameel/lumen/actions/runs/42411663796/artifacts/13590407929)
contains the inspected JSON reports and screenshots; the separately preserved
[command artifact](https://github.com/awaisjameel/lumen/actions/runs/42411663796/artifacts/13589898380)
is also available. Representative captures include
`album-draft/committed-header-refresh-failure.png`,
`undo-recovery/blocked-album-undo.png`,
`favourite-undo/single-undo-superseded.png` and dark narrow navigation. The wider
review also inspected fullscreen export controls, the narrow unavailable-updater
screen and shortcut-conflict focus. Injected errors are explicitly attributed;
unexpected page, transport and resource failures remain failures.

The 500-photo favourite comparison records **500 outstanding individual inverse
writes → three serial writes of 200/200/100 IDs**, with request payloads
53,392 → 4,426 bytes. Observed completion was 1,172.74 → 252.31 ms and maximum
frame gap 234.3 → 15.7 ms on that runner. These are synthetic native observations,
not calibrated-machine acceptance. The hosted store benchmark still exceeds
its unchanged budget: unblocked SaveEmbeddings64 **239.694/70 ms**.

The audit fixes are ready for owner review. The performance and external gates
below remain open; this does not authorise merging, release or closing #31.

## Confirmed findings and evidence

| Finding and impact | Origin and root cause | Repair and regression evidence | Verification status |
| --- | --- | --- | --- |
| **High: membership Undo could modify a blocked album.** A visible photo could be added to or removed from a label-blocked album, including after an inverse was retained for retry. | Existing single membership admission checked album kind and asset presence; the integrated batch/recovery path reused it without checking album visibility. | `setAlbumItem` now checks the canonical album blocking predicate inside its transaction. The before regression failed for both add and remove; the repaired store race regression verifies unchanged membership on refusal and successful retry after unblocking. Native `undo-recovery` now exercises real header/inspector controls and the PIN gate. | Run 42411663796 passes the native regression. |
| **Medium: single-photo favourite Undo overwrote newer explicit intent.** A later same-value batch confirmation did not protect the photo from an older single-control inverse. | Existing single-control Undo acquired a new write lease at activation, bypassing the PR's shared ownership retained by batch Undo. | Single and batch controls now share the bounded favourite inverse owner. The before regression observed the incorrect `SetFavourite(false)`. Regressions cover supersession and blocked-target retry after navigation without changing the current selection. Native `favourite-undo` now checks the single-control versus newer batch case. | Run 42411663796 passes the native regression. |
| **Medium: committed collection changes lost Undo on refresh failure; late saves changed a replacement header.** | Header code treated saving and refreshing as one operation, and local completion/error state had no collection lifetime guard. The PR's persistent metadata/draft work exposed this existing owner mismatch. | Names and pins retain their committed notification before refresh. Captured collection refresh owners and synchronous lifetime retirement prevent an old save from closing a new draft, clearing its busy state or showing an error in another collection. Two before regressions failed; all 19 header tests pass, including a newer pending save. Native `album-draft` adds a real committed header save followed by an injected read failure and Undo. | Run 42411663796 passes the native regression. |
| **Medium: acceptance could hide unexpected resource failures.** | The existing shared harness and two new fault suites blanket-filtered `Failed to load resource`. Thus unrelated bridge/media failures could disappear from a green report. | Browser errors are attributed to current generated method identities and scenario context. Only exact expected refusals account for their individual resource messages; page errors always fail. Recorder regressions verify that unrelated methods, media failures, duplicate messages and errors outside the injection context remain failures. | All 394 native checks and the recorder regressions pass in run 42411663796. |
| **Medium: a native cleanup exception could leave CI green.** | New album-draft/navigation cleanup catches recorded `failure` after all checks passed, but the shared finalizer only checked the assertion list. | The finalizer now converts a recorded exception into a failed check and nonzero exit. A subprocess regression failed before the repair (exit 0 despite the exception), then passed; its successful-report counterpart still exits 0. | Subprocess and native verification pass in run 42411663796. |
| **Medium: two base comparisons stopped before exercising the candidate.** | The integrated album-draft and collection-place suites assumed notification attributes and bounded inverse identities introduced after the current PR base. Child-parent acceptance had not exercised this older base. | Run 42314147744 reproduced both selector failures. The base draft test now selects its actual open toast; the place comparison observes its committed correction without requiring a candidate inverse identity. Candidate selectors and assertions remain strict. | Both current-main comparisons pass in run 42411663796. |
| **Low: XMP shutdown acceptance raced the caller.** | Run 42406902126 failed an unchanged test also present on `main`: `joinLifetime` releases its wait group in the export's defer, before the caller sends its result. `Stop` cannot order that later send. | The test now checks persisted sidecar ownership immediately after Stop and separately awaits the caller, deliberately delaying result publication. A deterministic `synctest` check asserts cancellation and that Stop remains blocked until joined work is released. Production shutdown code is unchanged. | Windows race run 42411663796 passes; local index compilation remains unavailable without libvips headers. |

## Review coverage and state ownership

The review covered all 242 files in the initial diff against `bae949c`, plus
the audit repairs and their callers. This included generated models/bindings,
compiler fixtures, all changed component/store tests, native fixtures and
harnesses, documentation, scripts and CI. The following evidence supports
areas where no further defect was confirmed; it is not a claim about untested
hardware or every possible application interaction.

| Workflow | Authoritative owners and evidence examined |
| --- | --- |
| Commands and navigation | Executable command definitions, per-registration runner admission/retirement, shortcut overlays, typed destination codecs/provider/adapters, exhaustive root views and generated Go SPA manifest. Reviewed every migrated production caller, compiler-negative fixtures and valid counterparts, AST checks and three mutation checks. Static checks and 25 command/74 navigation native checks pass in run 42411663796, including pending keyboard ownership, retries, history, reload/PIN relock, themes and a narrow drawer. |
| Albums and collection metadata | Editor admission and local draft lifetime, coalesced list/preview reads, library content revision and header refresh ownership, bridge types, service confidence/validation, transactional album creation identity and definition revision, canonical visibility predicates and bounded three-item preview. Store tests cover reference visibility, conflict/idempotent saves and ID reuse. Run 42411663796 passes 26 preview, 28 draft and 22 metadata checks, including the corrected base comparisons and committed-header regression. |
| Undo and corrections | Shared per-field/album turns and retained inverse leases, transactional changed-row outcomes, bounded scheduling/paged recovery, notification lifetime and current inspector reconciliation. Store and component tests cover partial failure, cancellation, retry, newer edits, disposal and correction provenance. Run 42411663796 passes all 23 Undo-recovery and 24 favourite checks, including blocked-album retry and single-control supersession. |
| Tagging/OCR | Thin service distinguishes annotation commit from scheduling; the indexer owns serialised/coalesced retries and current opt-in. Store eligibility and durable jobs recover after restart without replaying annotations. Reviewed cancellation before/after commit, failed queue transactions, shutdown and later user corrections. Run 42411663796 passes all 18 tagging-recovery checks. |
| Collection recovery and search history | Existing protected block manager and event override semantics, typed recovery links, and recent-search request ordering. Component tests exercise delayed failed clear/reload against newer intent; run 42411663796 passes 51 collection-recovery and seven search-history checks. |
| Build and infrastructure | Current generated service contracts and navigation manifest, compiler fixture rejection counts, test-runner exit propagation and exact native fault attribution. Production build leaves bindings/modules unchanged; static checks and unsigned packaging pass in run 42411663796. No release trust, runtime networking, performance budget or original-media write policy changes were introduced. |

Session Undo is deliberately ephemeral: committed state and OCR recovery
survive restart, but inverse offers and unsaved drafts are not durable journals.
The asset-ID-reuse hypothesis did not justify another API: production retains
asset identities rather than deleting rows; album deletion/reuse has separate
creation-identity and inverse-retirement checks. No speculative rewrite was made.

## Verification ledger

- Initial frontend baseline: **1,172 tests in 121 files passed**. This is fresh
  baseline evidence, not a final-head result.
- Favourite regression failed before the repair; focused store/selection/write
  ownership verification passed **72 tests** afterwards.
- Membership regression failed before the repair; focused album/batch race
  tests passed afterwards. The full Windows store shard then exposed an XMP
  trigger fixture using the newly prohibited API. Its setup now inserts the
  membership directly, like the adjacent event-membership trigger fixture;
  its original dirty-work assertions are retained. No privacy check is removed.
- Header regressions failed before the repair and **19 tests passed** afterwards.
- [Windows run 42276998439](https://github.com/awaisjameel/lumen/actions/runs/42276998439)
  at `73f7af6` passed production build/current bindings, the store-oracle and
  remaining-package race shards. Static checks failed on the new test's
  `void` generic; that type is corrected to `undefined`. The store shard failed
  on the XMP setup described above. Native tests and packaging were skipped.
  **This is failed intermediate evidence, not acceptance.**
- Fresh local frontend verification of the repaired application: **1,177 tests
  in 121 files**, type/lint/format checks, 21 command and 18 navigation negative
  compiler fixtures with valid counterparts, all three navigation mutations,
  and the production frontend build pass. Docs and three error-recorder tests
  pass. Local full lint cannot run the repository's Windows `.exe` tools;
  Linux store-race results include Windows-path failures and are not acceptance.
- [Windows run 42314147744](https://github.com/awaisjameel/lumen/actions/runs/42314147744)
  at `1c4ba2b` passed production build/current bindings, full static checks and
  the store-oracle/remaining-package race shards. The store shard exposed the
  XMP fixture's missing required `added_at`; the corrected membership/XMP race
  regression passes locally. Commands and seven other native suites passed.
  Album-draft/collection-places stopped at the base selectors described above.
  Favourite behaviour passed but error attribution caught three handled
  `ValidateSelection`/`selection_changed` responses: `resultSelection` rejects
  cached revisions after writes, then reads current IDs. The suite now allows
  only that exact refusal during recapture and waits for the current selection
  before action admission. Installer packaging was skipped.
  **This remains failed intermediate evidence.** Its downloaded JSON reports
  and screenshots were inspected, including blocked-album recovery, single
  favourite supersession, compact outcomes and dark narrow navigation.
- [Windows run 42379659025](https://github.com/awaisjameel/lumen/actions/runs/42379659025)
  at `61e27a8` passed all three race shards, production/current bindings and full
  static checks. Both corrected base comparisons passed. Native results were
  commands 25/25, navigation 74/74, collection recovery 51/51, album draft 27/28,
  collection places 22/22, preview 26/26, tagging 18/18, Undo 22/23 and history
  7/7. Favourite stopped at a new fixture selector that omitted the actual
  “· all results” suffix; the selector now matches both base and candidate.
  Strict diagnostics exposed an initial reload aborting baseline thumbnails,
  expected inspector cancellation responses, and a read-fault fixture that
  also broke unrelated timeline reads. The reload now awaits loaded images and
  network idle; inspector cancellation is identified by its exact runtime
  response. The read fault now inserts a disposable invalid album rule, so the
  real Albums call fails without corrupting the active album's reads. Uncaught
  and unrelated failures still fail acceptance. Packaging was skipped.
  **This is failed intermediate evidence, despite all behavioural album/Undo
  assertions passing.**
- [Windows run 42406902126](https://github.com/awaisjameel/lumen/actions/runs/42406902126)
  at `69bb15d` passed production/current bindings, full static checks (1,177
  frontend tests), the store and store-oracle race shards, and all **96 checks
  across six additional native suites**: onboarding, collections, organise,
  file tools, customisation and unconfigured updates. The remaining-package
  shard exposed the XMP test ordering defect above. Integrated native results
  were commands 25/25, navigation 74/74, collection recovery 51/51, album draft
  13/14, places 22/22, previews 26/26, tagging 18/18, Undo 23/23, history 7/7 and
  favourites 23/24. Album commit/refresh-failure/Undo behaviour passed before
  notification dismissal timed out on a captured closing control. Dismissal now
  reacquires open toast controls and awaits their removal. All favourite
  behaviour passed; only initial baseline thumbnail requests aborted by a reload
  failed diagnostics. That reload now waits for network idle. No errors are
  exempted to accommodate either timing failure. Packaging was skipped.
  **This is failed intermediate evidence.** Artifact `native-regressions`
  (13588074969) retains the inspected JSON reports and screenshots; the command
  artifact is 13588446379. The local real-store fault probe also confirms that
  the disposable invalid rule fails Albums with the exact expected message,
  leaves the active album's timeline/days valid and restores its original state.
- Five local native-harness regressions pass, including the before-failing
  subprocess check for a recorded cleanup exception. The fault fixture passes
  pinned Ruff 0.17.2 lint/format checks.
- Run 42411663796 supplies the accepted implementation checkpoint above. No
  historical child pass or reporting-only latency measurement is counted as
  acceptance of these repairs.

## External gates

This workspace is Linux. Windows production build, WebView2 controls and
unsigned packaging are verified through the repository's Windows CI, not by
pretending that a process launch or mocked component is native acceptance.
Only synthetic scratch libraries are used. The calibrated i7-13700H latency
gate, real corpus/hardware acceptance, production trust/signing, signed upgrade
and installed-machine acceptance remain separate requirements. The historical
181.786/70 ms hosted SaveEmbeddings64 result remains historical failed evidence;
budgets are unchanged. A hosted reporting-only pass does not close calibration.
Run 42314147744 freshly measured unblocked SaveEmbeddings64 at **174.086 ms**
against **70 ms**. Run 42276998439 measured 143.524/70 ms and also exceeded two
SaveFaces64 budgets (7.577/7 and 9.769/8 ms). These are observed hosted failures,
not evidence of calibrated-machine acceptance or proof of a pre-existing cause.

Run 42379659025 measured hosted unblocked SaveEmbeddings64 at **210.932/70 ms**;
this is another failed hosted budget measurement, with no budget adjustment.

Run 42406902126 measured hosted unblocked SaveEmbeddings64 at **239.443/70 ms**,
again exceeding the unchanged budget.

## Local addendum, 11 to 12 November 2026

A second review ran on a Windows 11 workstation with the calibrated
i7-13700H, against head `6b6b594` plus the repairs below. It is local
evidence: hosted Windows CI has not yet run these commits.

| Finding and impact | Root cause | Repair and regression evidence |
| --- | --- | --- |
| **Medium: a refused album change was shown as an unknown fault.** The inspector showed “Something went wrong: not found” and the album picker wrapped the localised reason in the same generic sentence. | The PR moved both surfaces from a rejected call to batch outcome rows and rethrew a row as a plain `Error`, losing its code. | `outcomeFailure` in `shared/errors.ts` keeps the code; both surfaces use it. Three regressions failed before the repair. |
| **Low: creating an album from the inspector bypassed the membership owner.** It offered no Undo, and a failed read after the committed creation was reported as a failed creation. | `createAlbumForItem` still called the bridge and reloaded directly. | It now uses `albums/changes.ts` like the album picker and reconciles separately. A regression failed before the repair. |
| **Low: the album editor lost focus after an answered notice closed.** A draft readmitted while its Undo notice still held focus was left with focus on the page once that notice went. | Readmission deferred to the notice but nothing restored the field when the notice closed. The native album-draft check failed about one run in three on this workstation. | The editor watches the notice that held focus and restores the draft's field and caret when it goes. A component regression failed before the repair. |
| **Low: a dead direct membership writer remained in the bridge.** | Every caller had moved to the shared owner; `engine.setAlbumItem` stayed. | Removed with its test. The Go method remains for the base-frontend comparison and native fixtures. |
| **Low: a frontend built without the navigation manifest crashed the start.** | `spa` panicked on a missing or malformed `navigation.json`. | `spa` returns an error that reaches the start-up dialog; `TestClientRoutesRefuseAMissingOrMalformedContract` covers the parser. |
| **Toolchain: `govulncheck` failed on GO-2026-6859** (`net/http`, fixed in Go 1.27.4). | The pinned Go 1.27.3. | `go.mod` pins 1.27.4. That release refuses the `-l:libvips.lib` linker flag, so `internal/media/vips` now links `-llibvips`, which resolves the same import library. |
| **Native harness: nine suites failed on this workstation and on the unmodified head.** | Harness defects exposed by a faster PC and a scaled display, none in the application: clicks made while a closing menu still blocked input or while a dock was re-centring; a control drawn again between being found and clicked; a window resize dropped while the window opened; a cached base library indexed from another checkout path; a synthetic profile without application-data folders; one suite selecting before the AI stages finished; and one opening the palette before the album editor had closed. | Element clicks wait until the control rests and takes input, and selector clicks find their control again; `resize` verifies the page's size; the base library records its corpus; the navigation profile has its folders; `undo-recovery` waits for the AI stages and `commands` for the editor to close. |

Verification on this workstation, Go 1.27.4, Node 24.22.1, npm 12.2.1:

- `scripts/lint.sh` exits 0, including `govulncheck`, **1,181 frontend tests
  in 121 files** and the large-library latency gate.
- **The calibrated latency gate passes.** The three budgets the hosted runner
  exceeded measured, in two clean runs: unblocked SaveEmbeddings64
  16.469 and 18.349 ms against 70; folder SaveFaces64 3.721 and 3.556 ms
  against 7; unblocked Things 2.866 and 2.846 ms against 10. No budget was
  changed. An earlier run that overlapped another build is discarded.
- The Go race suite passes for all named packages.
- The production build leaves bindings and modules unchanged.
- **All 383 native checks across the same 16 suites pass in two consecutive
  complete runs** against the production executable and scratch libraries.
  The base-frontend comparisons need `LUMEN_BASELINE_FRONTEND` and were not
  run locally, so album-draft (26), collection-places (20),
  smart-album-preview (22) and favourite-undo (21) report fewer checks than
  CI. The fixtures used the pinned Pillow 13.0.0 on Python 3.14.4, not the
  documented 3.15.1.

Real corpus and hardware acceptance, signing, release trust, signed upgrades
and installed-machine acceptance remain open, as above.
