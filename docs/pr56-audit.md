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

## Confirmed findings and evidence

| Finding and impact | Origin and root cause | Repair and regression evidence | Acceptance still needed |
| --- | --- | --- | --- |
| **High: membership Undo could modify a blocked album.** A visible photo could be added to or removed from a label-blocked album, including after an inverse was retained for retry. | Existing single membership admission checked album kind and asset presence; the integrated batch/recovery path reused it without checking album visibility. | `setAlbumItem` now checks the canonical album blocking predicate inside its transaction. The before regression failed for both add and remove; the repaired store race regression verifies unchanged membership on refusal and successful retry after unblocking. Native `undo-recovery` now exercises real header/inspector controls and the PIN gate. | Final Windows native result. |
| **Medium: single-photo favourite Undo overwrote newer explicit intent.** A later same-value batch confirmation did not protect the photo from an older single-control inverse. | Existing single-control Undo acquired a new write lease at activation, bypassing the PR's shared ownership retained by batch Undo. | Single and batch controls now share the bounded favourite inverse owner. The before regression observed the incorrect `SetFavourite(false)`. Regressions cover supersession and blocked-target retry after navigation without changing the current selection. Native `favourite-undo` now checks the single-control versus newer batch case. | Final Windows native result. |
| **Medium: committed collection changes lost Undo on refresh failure; late saves changed a replacement header.** | Header code treated saving and refreshing as one operation, and local completion/error state had no collection lifetime guard. The PR's persistent metadata/draft work exposed this existing owner mismatch. | Names and pins retain their committed notification before refresh. Captured collection refresh owners and synchronous lifetime retirement prevent an old save from closing a new draft, clearing its busy state or showing an error in another collection. Two before regressions failed; all 19 header tests pass, including a newer pending save. Native `album-draft` adds a real committed header save followed by an injected read failure and Undo. | Final Windows native result. |
| **Medium: acceptance could hide unexpected resource failures.** | The existing shared harness and two new fault suites blanket-filtered `Failed to load resource`. Thus unrelated bridge/media failures could disappear from a green report. | Browser errors are attributed to current generated method identities and scenario context. Only exact expected refusals account for their individual resource messages; page errors always fail. Recorder regressions verify that unrelated methods, media failures, duplicate messages and errors outside the injection context remain failures. | Final native runs under stricter attribution. |
| **Medium: two base comparisons stopped before exercising the candidate.** | The integrated album-draft and collection-place suites assumed notification attributes and bounded inverse identities introduced after the current PR base. Child-parent acceptance had not exercised this older base. | Run 42314147744 reproduced both selector failures. The base draft test now selects its actual open toast; the place comparison observes its committed correction without requiring a candidate inverse identity. Candidate selectors and assertions remain strict. | Repeat comparison against current `main`. |

## Review coverage and state ownership

The review covered all 242 files in the initial diff against `bae949c`, plus
the audit repairs and their callers. This included generated models/bindings,
compiler fixtures, all changed component/store tests, native fixtures and
harnesses, documentation, scripts and CI. The following evidence supports
areas where no further defect was confirmed; it is not a claim about untested
hardware or every possible application interaction.

| Workflow | Authoritative owners and evidence examined |
| --- | --- |
| Commands and navigation | Executable command definitions, per-registration runner admission/retirement, shortcut overlays, typed destination codecs/provider/adapters, exhaustive root views and generated Go SPA manifest. Reviewed every migrated production caller, compiler-negative fixtures and valid counterparts, AST checks and three mutation checks. Current static checks and 25 command/74 navigation native checks pass in run 42314147744, including pending keyboard ownership, retries, history, reload/PIN relock, themes and a narrow drawer. |
| Albums and collection metadata | Editor admission and local draft lifetime, coalesced list/preview reads, library content revision and header refresh ownership, bridge types, service confidence/validation, transactional album creation identity and definition revision, canonical visibility predicates and bounded three-item preview. Store tests cover reference visibility, conflict/idempotent saves and ID reuse. Run 42314147744 passes all 26 preview checks; header/draft candidate acceptance remained blocked by its base fixture. |
| Undo and corrections | Shared per-field/album turns and retained inverse leases, transactional changed-row outcomes, bounded scheduling/paged recovery, notification lifetime and current inspector reconciliation. Store and component tests cover partial failure, cancellation, retry, newer edits, disposal and correction provenance. Run 42314147744 passes 23 Undo-recovery checks, including the newly blocked-album case; all favourite behavioural assertions pass, with its diagnostic classification corrected separately below. |
| Tagging/OCR | Thin service distinguishes annotation commit from scheduling; the indexer owns serialised/coalesced retries and current opt-in. Store eligibility and durable jobs recover after restart without replaying annotations. Reviewed cancellation before/after commit, failed queue transactions, shutdown and later user corrections. Run 42314147744 passes all 18 tagging-recovery checks. |
| Collection recovery and search history | Existing protected block manager and event override semantics, typed recovery links, and recent-search request ordering. Component tests exercise delayed failed clear/reload against newer intent; run 42314147744 passes 51 collection-recovery and seven search-history checks. |
| Build and infrastructure | Current generated service contracts and navigation manifest, compiler fixture rejection counts, test-runner exit propagation and exact native fault attribution. Production build leaves bindings/modules unchanged; static checks pass in run 42314147744. No release trust, runtime networking, performance budget or original-media write policy changes were introduced. |

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
- Final native/race acceptance is pending the corrected run. No historical
  child pass or reporting-only latency measurement is counted as a final pass.

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
