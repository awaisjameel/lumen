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
- After all accepted #31 work and its verification gates are complete, notify
  the owner to review `feat/31-feature-completion`. Record the accepted scope,
  child PRs, validation and explicit deferrals for that review. Do not create
  or merge a final PR to `main` without further instructions.

The existing CI workflow checks pull requests regardless of their base branch,
so child PRs retain the same checks. The open latency gate below still applies
to PR #37 and the eventual integration acceptance.

## Current child sequence, 3 November 2026

PR #37 was merged into the integration branch at `6038d23`; #35 and #36 are
closed. Their recorded native evidence and outstanding milestone latency gate
below remain historical evidence, not new results from this checkout.

Continue with one child branch and PR at a time: #39 (history ordering), #40
(bounded favourite Undo), #41 (partial Undo reconciliation), #43 (tagging
follow-up truthfulness), #42 (smart-album preview cost), #44 (place metadata),
#45 (draft lifetime), #38 (navigation contracts), then #46 (command contracts).
Each branch starts from the latest integration head after the preceding PR
merges. The source-backed candidates must first be reproduced; #46 is preventive
work. #38 establishes navigation identities before #46 coordinates command
identities with them. Related #28, #29 and #30 retain their own scope. No final
PR to `main` is opened while the user reviews the accumulated parent branch.

### #39: Search history ordering

Accepted scope is the bounded repair in
[#39](https://github.com/awaisjameel/lumen/issues/39): keep optimistic clearing,
prevent an older failure from replacing newer results, and recover from the
engine's authoritative filtered history. No schema, service signature,
generated binding or search-ranking change is needed.

Tracing `SearchBox.vue` → `search/recent.ts` → bridge → `SearchService` →
`store/recent.go` confirms that SQLite owns persisted history and the service
filters blocked names on reads. The frontend's unconditional snapshot rollback
is the defect. The new deterministic regressions fail ten cases on the original
implementation, including both newer-operation paths and repeated clears.

The repaired clear uses the same revision ownership as load/remember. Every
clear failure is reported. Only a still-current failure starts an authoritative
read; its result also yields to newer work. A failed recovery read logs its
error and keeps the list empty. A newer pending operation owns recovery; if it
also fails, reopening the search box retries the read. Cached names are never
restored by clear failure. This defines frontend response ownership without
adding another persistent history owner or changing store write ordering.

Fresh verification in this Linux workspace:

- The focused history store and mounted search-box tests pass 25/25, including
  the reopened box after an older clear fails.
- All 958 frontend tests in 110 files pass, as do type checking, ESLint,
  formatting and the production frontend build. Its existing bundle-size
  advisory remains; no build threshold was changed.
- The history SQLite regression passes with the race detector:
  `go test -race -count=1 -timeout 900s ./internal/store -run 'RecentSearch|RememberSearch|ClearRecent'`.
  `go vet ./internal/core ./internal/store` also passes.
- Documentation links, link-checker regressions and all eight screenshot
  capture records pass. No visible design change needs gallery recapture.
- The new `search-history` native suite is registered, syntax-checked and wired
  into Windows CI after the production build. It exercises clear, a subsequent
  query, navigation, reopening and restart using a scratch library. A syntax
  check is not native acceptance; actual Windows results are pending.
- Full named-package Go race/static checks and the Windows production
  executable/package cannot be certified by this Linux environment. The
  attempted full race run reports missing GTK/WebKit and libvips development
  dependencies plus Windows-path test failures; it was stopped after those
  blockers were established. The local large-library gate fails at the
  Windows-path block setup (`invalid block`) and reports missing benchmark
  cases. These are failures, not performance acceptance. Results are retained
  under `work/issue31/`; no tests, dependency versions or benchmark budgets
  were weakened.

Implementation and verification status remain distinct from acceptance,
integration and release. #39 and #31 stay open until the required evidence is
available. The remaining children have not been implemented by this entry.

### #39 integration result and #40 accepted scope

[#47](https://github.com/awaisjameel/lumen/pull/47) is merged at `f9eeb2f` and
#39 is closed. [Windows CI](https://github.com/awaisjameel/lumen/actions/runs/41696646790)
passes the production build, unchanged generated contracts/modules, static
checks, all three Go race shards, 958 frontend tests and native history
acceptance (7/7). The earlier pending status above records the Linux checkpoint.
The reporting-only latency job contains one over-budget measurement:
unblocked `SaveEmbeddings64` 113.838/70 ms. The parent milestone's performance
gate remains open; no package, real-corpus, hardware or release acceptance is
inferred from this child run.

The next child is [#40](https://github.com/awaisjameel/lumen/issues/40), based on
that integration head. Accepted before implementation:

- Capture previous favourite flags in bounded units, beside the corresponding
  forward writes. Undo only targets flags actually changed by that action.
- Reuse the existing transactional `ApplyBatch` inverse, with at most 200 IDs
  in a call and one outstanding unit. No heterogeneous-value service or new
  wire contract is needed for a uniform Favourite/Unfavourite action.
- Keep ephemeral Undo ownership independent of the originating view. Reuse
  notifications, progress, overlays and paged outcomes for cancellation,
  failed-only retry and a separate cancelled remainder.
- Coordinate favourite writes through one per-app owner. A newer explicit
  favourite edit supersedes the older Undo for that target; queued/in-flight
  writes must finish in that order. Completed inverses are never replayed.
- Reconcile the current grid/counts after every outcome, retaining mutation
  outcomes separately from refresh errors. Do not restore an old selection or
  focus after navigation. This small inverse/outcome helper can support #41;
  album/date/place inverse repairs remain in that later child.
- Demonstrate the original scheduling failure, exact mixed-flag restoration,
  bounded reads/writes/rendering, progress, cancellation, retries, newer edits
  and navigation. Add native large-selection acceptance and record measured
  calls/payloads and the actual platform gates without changing budgets.

### #40 implementation checkpoint

The controlled regressions reproduce 500 outstanding inverse writes, one
500-item previous-value read and a validation request retransmitting all 500
IDs on the original implementation. Capture/current-state reads and mutation
units now carry at most 200 IDs; the bridge sends only the revision for
admission, which the existing store contract validates. A new store race
regression proves that omitting IDs still rejects a stale revision. The
application's resolved selection identity remains with its existing owner.

`favouriteBatch.ts` records only genuinely changed flags. Grid/viewer edits,
batch tools and selection changes share per-app favourite turns and ownership;
later explicit requests supersede earlier inverse ownership even if they later
fail or are cancelled. Undo reads current flags before writing, so retry of an
unconfirmed response can recognise an already committed inverse without
replaying it. Transport failures are labelled unconfirmed rather than claiming
that no mutation occurred. Normal batch admission still checks visibility
beside each store write.

The shared ephemeral inverse helper retains per-item outcomes, bounded units,
cancellation/resume, failed-only retry and an independent refresh retry. Its
global dialog survives originating-view disposal, pages 100 rows and resets
pagination when switching jobs. It does not restore an old selection/focus or
persist a second Undo journal. A loaded-grid regression also reproduces an old
Undo day read overwriting a newer single favourite save; committed flags now
invalidate older cell reads without reloading unfiltered timeline groups.
A second regression confirms that an inner single-asset save queue could
schedule an older click after a newer batch. Single edits now reserve ownership
and enter the shared turn at the click; superseded queued edits are skipped,
and a refused newest write reads its persisted flag before rolling back.
Existing album/date/place inverse repairs remain with #41.

Fresh local evidence: all 988 frontend tests in 113 files, type checking,
ESLint, formatting, the production frontend build, documentation integrity and
the revision-only store race test/vet pass. The PR-base frontend builds for
comparison. [#48](https://github.com/awaisjameel/lumen/pull/48) targets the parent;
Windows native `favourite-undo` acceptance and full-tree checks are pending; no desktop
responsiveness or latency improvement is claimed from mocked scheduling tests.
The native suite records before/after calls, payloads, elapsed restoration time
and frame gaps, then exercises cancellation, navigation, a blocked-row failure,
retry, newer edits and restart. Its baseline uses compiled base frontend assets
with the candidate's unchanged favourite engine on copied synthetic libraries.

The [first Windows run](https://github.com/awaisjameel/lumen/actions/runs/41716415864)
passes production, contract/module cleanliness, static checks, 985 frontend
tests, all Go race shards and history acceptance (7/7). Favourite acceptance
stops in its harness before mutation because Ctrl+A is not a grid selection
shortcut; it now uses the real **Select all results** menu. Native acceptance
remains pending. The unchanged reporting-only latency gate exceeds folder
`Events` 6.293/5 ms, unblocked `SaveEmbeddings64` 143.184/70 ms and unblocked
`SaveFaces64` 8.122/8 ms; the parent performance gate remains open.


The [second Windows run](https://github.com/awaisjameel/lumen/actions/runs/41727734809)
passes build/contracts/static checks, 986 frontend tests and every Go race shard.
On the same candidate engine and copied 500-photo fixture, the compiled parent
frontend launches 500 inverse calls concurrently (53,392 request bytes,
1,587.63 ms, maximum observed frame gap 343.8 ms). The candidate makes three
200/200/100 writes with one outstanding write, plus three current-state reads
(4,426 request bytes, 275.89 ms, maximum frame gap 15.7 ms). These are single
instrumented synthetic observations, not a general latency or hardware claim.
Navigation, cancellation/resume, retained focus, 100-row paging and compact
layout pass. Native acceptance remains incomplete: Escape closes both the
outcomes dialog and its recovery toast, so failed-only retry cannot proceed;
an interactive toast's changing duration also briefly renders progress above
100%. Both now have failing-then-passing mounted regressions. Notifications
cede Escape to portalled overlays, interactive work uses its own progress, and
a dedicated notification layer sits below dialogs so their controls stay clear.
The history harness now restarts after fixture setup instead of reloading while
startup requests may still be pending; uncaught errors remain a failing check.
Final Windows acceptance is pending. This run's unchanged latency gate exceeds
folder `SaveFaces64` 7.737/7 ms, folder `Things` 11.903/10 ms, combined-block
`SaveFaces64` 8.008/8 ms, unblocked `SaveEmbeddings64` 141.542/70 ms and unblocked
`SaveFaces64` 8.469/8 ms. The parent performance gate remains open.


The [third Windows run](https://github.com/awaisjameel/lumen/actions/runs/41731572756)
passes build/contracts/static checks, 988 frontend tests, all Go race shards and
history acceptance (7/7). Favourite checks reach 17/18: progress has no invalid
countdown and dialog controls stay above notifications. The Escape fix needs a
further refinement: preventing the shared key in a toast registered before the
dialog also prevents that dialog from closing. The mounted regression now
reproduces the real mounting order, asserts the dialog closes and the same
recovery toast survives its delayed-removal window, then verifies explicit
dismissal still releases it. The presenter retains that toast synchronously
through Nuxt's existing update/removal contract while leaving the keyboard event
for the dialog. Final native retry/newer-edit/restart acceptance remains pending.
The repeated comparison observes 500 versus 3 inverse writes and 53,392 versus
4,426 request bytes; elapsed time is 1,342.05 versus 258.54 ms, maximum observed
frame gap 281.2 versus 15.7 ms. The unchanged latency gate still exceeds folder
`SaveFaces64` 7.964/7 ms, combined-block `SaveFaces64` (rounded to 8.000/8 ms),
unblocked `SaveEmbeddings64` 143.417/70 ms and unblocked `SaveFaces64` 8.066/8 ms.


### #40 integration result and #41 accepted scope

[#48](https://github.com/awaisjameel/lumen/pull/48) is merged at `f4309b3` and
#40 is closed. [Final Windows CI](https://github.com/awaisjameel/lumen/actions/runs/41735275110)
passes production, unchanged generated contracts/modules, static checks, 988
frontend tests, all three Go race shards, native history 7/7 and favourite Undo
22/22. The compact outcomes screenshot was inspected. The final synthetic
comparison records 500 versus 3 inverse writes, maximum outstanding writes 500
versus 1, 53,392 versus 4,426 request bytes, 1,580.16 versus 280.86 ms and maximum
frame gaps 328.2 versus 15.7 ms. The unchanged reporting-only store gate exceeds
unblocked `SaveEmbeddings64` 236.320/70 ms; parent performance acceptance remains
open. Earlier pending entries above are intermediate checkpoints, not current
acceptance. No installer, real-corpus, hardware or release result is inferred.

The next child, [#41](https://github.com/awaisjameel/lumen/issues/41), starts from
that parent head. Its accepted scope, before implementation:

- Classify and reproduce partial inverses in selection album actions,
  `details/AlbumPicker`, date/place correction sequences and the adjacent
  `AlbumPhotoPicker`. Single date/place forward calls are atomic store writes;
  their array-based component inverse sequences can commit before a later call
  fails. The current details-panel UI opens date/place for one asset; no bulk
  correction control is added or claimed. The photo picker also loses its Undo
  offer after a partial forward success.
- Reuse the session-local inverse/outcome/dialog owner from #40. Add per-item
  units for heterogeneous corrections, accurate failed/cancelled outcomes,
  failed-only retry, cancelled-item resume and independent refresh recovery.
- Generalise the existing favourite turn/lease implementation for date, place
  and album membership; do not duplicate its ownership logic. New explicit
  edits supersede older inverses. Include adjacent membership actors so one
  surface cannot bypass that policy. Album deletion invalidates its pending
  membership inverses before a potentially reused album ID can be targeted.
  Assets themselves are retained when their files disappear; do not retarget
  an inverse to another selection, path or newly selected item.
- Capture current correction metadata beside the forward write under its
  field turn. Keep original/user provenance semantics: a prior user correction
  is reinstated; a previously uncorrected value restores the latest metadata,
  including absent GPS. Retain opaque inverse targets when a correction itself
  covers its asset; keep the store's existing admission rules.
- Report actual membership changes from the existing transactional batch path
  with a small typed outcome field. Reuse that path for single and multiple
  membership edits, eliminating whole-album capture and stale checkbox guesses.
  Keep the canonical SQL mutation in store and regenerate the wire bindings.
  Album inverses retain the batch path's visibility checks and recover refused
  targets after unblocking; they never widen admission to hidden metadata.
- Reconcile current grids/counts and album lists after every inverse outcome;
  refresh errors must not replay committed mutations. Preserve current detail
  error/retry presentation and live metadata invalidation after navigation.
  Add real-control native partial-failure/retry/navigation/restart acceptance,
  fresh applicable Go/frontend/build/static/performance checks and living-guide
  evidence. Preserve #36 event-name semantics and #40 favourite acceptance.

### #41 implementation checkpoint

The focused before-repair regressions fail for selection membership, the
multi-album details picker and array-based date/place inverses. The photo
picker also omits Undo for the subset committed before a forward failure.
The correction forward calls are atomic, and the production details panel
opens each correction for one photo; array tests establish component recovery
without claiming a bulk correction UI.

The implementation reuses the #40 outcome owner with typed compound identities
and one-item heterogeneous units. Membership capture now comes from the
canonical SQL mutation's committed `changed` result, including no-op/repeated
targets, with regenerated bindings. One field/album turn registry serves all
membership controls, corrections and favourite controls. Deletion retires old
album leases before ID reuse. Current metadata is captured beside correction
writes; original/user provenance and self-covering correction admission remain
unchanged. Reconciliation waits for the selected day's lazy rows before reading
the current inspector, and read failures retain independent retry.

Local regression coverage includes partial success, failed-only retry,
cancellation/resume, composite identities, no-op/newer edits, ID reuse,
unconfirmed submitted subsets, stale correction props, refresh-only retry and
current inspector failure/selection settlement. On 4 November 2026, all 1,012
frontend tests across 115 files, frontend type/lint/format checks, the production
frontend build, documentation checks, focused store race tests (2.139 s) and
core/store vet pass. Native Windows acceptance and fresh full-tree gates remain
pending in [PR #49](https://github.com/awaisjameel/lumen/pull/49); the new `undo-recovery` real-control suite
covers partial membership inverses, navigation, provenance, self-coverage,
album deletion/replacement and restart. No parent completion, installer or
machine-independent performance acceptance is claimed by this checkpoint.

The first Windows run, [41845808446](https://github.com/awaisjameel/lumen/actions/runs/41845808446),
passes the production build, clean binding/module check, static/frontend/docs
checks and both store race shards. Service/contract tests pass in the remaining
shard, which fails an unchanged XMP shutdown test's nonblocking caller-signal
assertion; the export releases its internal lifetime wait before the calling
goroutine sends that signal. No indexer code or assertion is changed; a bounded
fresh run is required. Native history passes 7/7 and favourite Undo 22/22.
The new native harness initially clicks before the album modal mounts; its
failure screenshot confirms the modal. It now waits for that real control,
without bypassing the flow. Partial-inverse native acceptance remains pending.
The reporting-only store benchmark still exceeds the unchanged unblocked
`SaveEmbeddings64` ceiling: 172.782/70 ms. The parent performance gate remains
open; no budget is raised.

The second run, [41849133172](https://github.com/awaisjameel/lumen/actions/runs/41849133172),
passes every Go race shard, including the unchanged shutdown test, and repeats
the clean build, static/frontend/docs checks, native history 7/7 and favourite
Undo 22/22. The new native suite confirms the mixed selection inverse, original
failed-only retry, navigation and current focus (5 passing checks), then stops
at a harness label mismatch: the actual picker control is **Apply albums**.
That selector is corrected against the current i18n source, alongside a review
of every remaining control label. CI runs the new recovery suite first to
surface its acceptance failures earlier; all three suites remain enabled.
The second reporting-only benchmark exceeds `SaveEmbeddings64` unblocked
(157.591/70 ms) and `SaveFaces64` unblocked/folder (8.069/8 and 8.430/7 ms).
These actual measurements remain recorded with the parent gate open.

The third run, [41852230572](https://github.com/awaisjameel/lumen/actions/runs/41852230572),
passes every Go race shard, the clean production build and static/frontend/docs
checks. Native history passes 7/7 and favourite Undo 22/22. All 18 behavioural
checks of `undo-recovery` pass: partial selection/picker results and failed-only
retry, provenance, newer edits, self-coverage, deleted/reused album IDs and
restart. Its strict frontend-error check still fails on two `not found`
console errors. Acceptance remains open. Focused reproduction finds that late
file-inspection rejections are logged after selection changes or inspector
disposal; two new tests fail before a request/signal ownership guard and pass
after it. A third test preserves reporting of current inspection failures.
The native report now retains phase/read diagnostics for any remaining refusal;
the error check is not filtered or relaxed.

The fourth completed run, [41856887336](https://github.com/awaisjameel/lumen/actions/runs/41856887336),
passes the clean Windows build, static/frontend/docs checks (1,015 tests) and
all Go race shards. Native history is 7/7 and favourite Undo 22/22. The new
suite again passes all 18 behavioural checks. Diagnostics identify its one
remaining console error precisely: a current `Media.Inspect` refusal after the
photo is blocked during the deleted-album scenario. The obsolete inspection
guard removed the other error. Two further regressions fail before handling
this known refusal through the inspector's existing unavailable/retry state;
both pass after it, including retirement of pending tag/face/membership reads
and a read-only retry. Current unexpected inspection errors still report.
No runtime error filter or native acceptance assertion is weakened. The latest
reported budget overruns are unblocked `SaveEmbeddings64` 218.628/70 ms and
folder `SetEventName` 45.554/5 ms; the parent performance gate stays open.

The delayed run finalises after the confirmed GitHub Actions incident. Its
first recovery fixture could not open a cold WebView's DevTools port; no
functional acceptance is claimed for that attempt. A bounded failed-job retry
of [41861257245](https://github.com/awaisjameel/lumen/actions/runs/41861257245/attempts/2)
passes native recovery 19/19 and history 7/7. The recovery report has no
unexpected frontend errors. The preserved favourite suite passes its benchmark
checks, then hits a clickability race with its selection menu open (screenshot
retained). Shared control helpers now use Puppeteer's stable-geometry locators
for real clicks; no JavaScript bypass or relaxed assertion is introduced.
Favourite failures also retain their phase and stack. The recovery fixture
holds a real inspection while blocking its target, making the discovered
visibility refusal deterministic rather than dependent on timing. Full native
acceptance of that harness refinement remains pending.

### #41 integration and #43 scope

[#41](https://github.com/awaisjameel/lumen/issues/41) is integrated through
[PR #49](https://github.com/awaisjameel/lumen/pull/49), parent `e0ddcabc`, tested
head `845ca6a`. Native recovery passes 20/20, favourite Undo 22/22 and history
7/7 in [run 41875612779](https://github.com/awaisjameel/lumen/actions/runs/41875612779).
The held inspection returns the real typed `not_found` refusal; no unexpected
frontend error or external request is recorded. Reports/screenshots are in
artifact `13348670335`. All 1,017 frontend tests, clean Windows production
build/bindings/modules, static/docs checks and all Go race command logs pass.
Every job step, including cleanup, reports completed/success (or intentional
skipped); three job summaries were still finalising after the Actions incident
at merge. No test/build/cleanup step remained pending, and no check or protection
was bypassed. Hosted budget overruns remain: unblocked `SaveEmbeddings64`
126.181/70 ms and `SaveFaces64` unblocked/folder 8.231/8 and 7.890/7 ms. The
parent performance gate remains open.

### #43: Truthful tagging and owned text scheduling recovery

Accepted scope, recorded before implementation: reproduce the single-item
commit/queue/UI sequence, distinguish pre-commit failure from post-commit
cancellation or scheduling failure, and preserve drafts only for genuine
mutation failures. Recover from persisted eligibility under the indexer
lifetime, with current opt-ins/corrections and truthful activity feedback.
No second annotation journal or annotation replay is introduced.

The original behaviour is reproduced at `c218f19` in
[run 41883020481](https://github.com/awaisjameel/lumen/actions/runs/41883020481),
artifact `13350513665`. A real Details-panel save first rejects an annotation
write and preserves its draft; a second attempt commits `Screenshots/user`
while the injected OCR INSERT failure returns HTTP 422. The modal remains open
with **Something went wrong: queue unavailable**, and activity says **Up to
date** despite the missing job. Service race regressions independently fail
for that fault and post-commit cancellation; bulk/pre-commit controls pass.

The implementation in [PR #50](https://github.com/awaisjameel/lumen/pull/50)
starts from parent `e0ddcabc`. Single and bulk tagging share one committed
outcome policy. The indexer owns one coalesced recovery loop, retries failed
startup reconciliation and serialises scheduling with opt-in changes.
`passState` reuses the XMP pass's cancellable admission and wake-up mechanism;
its two owners retain separate state and policy. Recovery reads current store
eligibility and hands work to durable OCR jobs. Healthy idle libraries do no
periodic scheduling query; pause continues to control processing. Optional
canonical progress feedback explains automatic retry and local reporting.

At `5020a8d`, [run 41901058715](https://github.com/awaisjameel/lumen/actions/runs/41901058715)
passes the Windows production build, clean bindings/modules, static/docs checks,
all 1,019 frontend tests and all Go race shards. Native tagging establishes
successful committed saves, failed-write draft retention, in-session recovery,
a second fault held across restart, real recognised text and persistence without
duplicating annotations. Its initial activity assertion reads before the next
coalesced event and fails (16/17); artifact `13357874526` preserves that evidence.
The refined suite waits for rendered retry feedback, measures its arrival and
also verifies its removal after recovery. This retains the product assertions.
Native Undo passes 20/20, favourite Undo 22/22 and history 7/7 in that run.

The native suite uses two generated documents in a scratch library. No fixture
replays annotations, touches originals or downloads runtime models. The same
acceptance suite retains stable menu geometry and waits for asynchronous grid
selection before checking focus. Final current-head verification and artifact
references are recorded in PR #50 and [issue #43](https://github.com/awaisjameel/lumen/issues/43);
only a verified child is merged into the parent. The parent hardware performance
gate remains open and its budgets are unchanged.

### #43 integration and #42 accepted scope

PR #50 is merged into the parent at `fcd81324`, verified head `f9e016eb`.
[Run 41904815481](https://github.com/awaisjameel/lumen/actions/runs/41904815481)
passes every job: clean Windows production/bindings/modules, zero Go lint
issues, static/docs checks, 1,019 frontend tests and all Go race shards.
Native tagging recovery passes 18/18, Undo recovery 20/20, favourite Undo 22/22
and history 7/7; artifact `13360237689` retains reports/screenshots. The rendered
retry notice appears and is removed after recovery; both documents retain their
user tags and real recognised text through restart. This supersedes the initial
early-event assertion above. Hosted budget overruns remain in SaveEmbeddings64
(unblocked 248.399/70 ms, folder/tag/person 100.551/70) and SaveFaces64
(folder 11.702/7 ms, folder/tag/person 8.236/8); no budget is loosened.

[#42](https://github.com/awaisjameel/lumen/issues/42) starts from `fcd81324` on
`perf/42-smart-album-preview`. Its current frontend composes timeline filters
and reads every matching day for photo/favourite intersections. The repair
will remove rule evaluation from that frontend and reuse the store's saved
smart-album `assetFilter`, rule/reference validation, visibility predicates and
grid fields. One typed read returns an exact count and at most three newest
matching items, in a read-only snapshot. Missing/blocked references fail closed;
SQL remains in store, and generated access remains in the bridge.

Keep the existing 200 ms debounce, cancellation, revision ownership and immediate
clear on privacy invalidation. Failed reads retain no old images; the editor
offers the existing retry control pattern without changing draft/reset policy
(#45 owns that separate issue). Verify inclusive dates, mixed media/favourites,
Thing/person combinations, corrections, visibility, grid thumbnail metadata
and concurrent-write snapshot consistency against independent saved membership.
First retain a failing 100-day call-count regression. Native acceptance will
compare the built parent and candidate frontends against the same synthetic
large fixture and engine, recording real call counts, response bytes and
latency. Synthetic measurements do not replace the configured-machine gate;
existing budgets remain unchanged.

The retained 100-day regression fails on the parent with 100 day calls while
returning count 300 and three samples. The bounded frontend uses one
`PreviewAlbum` call and no timeline/day reads. Local verification passes all
1,022 frontend tests, type/lint/format checks, four new store race regressions
and store vet. The store matrix checks 288 combinations against independent
fixture membership and saved albums, plus rotated thumbnail metadata, invalid
and blocked references, bounded payloads and a concurrent-write snapshot seam.
A local fixture preflight confirms 100 days and 100,000 rows: 80,000 photos and
6,666 favourite videos, three samples and 139/213-byte preview bodies. Those
development-host reads are not configured-machine latency acceptance.

The native suite compares five reads of each amplified draft in the built
parent and candidate, recording actual bridge requests, response bytes and
latency. Generated photos/videos separately exercise date boundaries,
Unicode references, membership parity, failed-read retry and stale/cancelled
work. Final Windows/native results and artifact references are recorded in
[#42](https://github.com/awaisjameel/lumen/issues/42) and its child PR; the parent
performance gate is separate and remains open until its own acceptance.

### #42 integration and #44 accepted scope

PR #51 is merged into the parent at `5123a7f8`, verified head `52d1c329`.
[Run 41918558533](https://github.com/awaisjameel/lumen/actions/runs/41918558533)
passes production/bindings/modules, static/docs checks, all 1,022 frontend
tests and all Windows Go race shards. Artifact `13368105341` retains native
preview 26/26, tagging 18/18, Undo recovery 20/20, favourite Undo 22/22 and
history 7/7, with inspected screenshots.

Five readings per amplified rule on equivalent 100,000-row/100-day scratch
metadata fixtures show 101 calls becoming one. Photo response bytes fall from
4,175,493 to 139; favourite-video bytes from 1,596,105 to 213. Median summed
bridge read times are 952.14 to 21.15 ms and 612.35 to 20.87 ms respectively;
control-to-preview medians are 1,231.13 to 373.22 ms and 877.44 to 292.65 ms.
Every parent reading makes 100 day calls; every candidate reading makes one
bounded preview and no timeline/day calls. This is synthetic hosted evidence.
The unchanged budgets still report SaveFaces64 folder 8.375/7 ms,
SaveEmbeddings64 unblocked 188.659/70 ms, SaveFaces64 unblocked 11.956/8 ms
and Things unblocked 11.331/10 ms. Configured-machine acceptance remains open.

[#44](https://github.com/awaisjameel/lumen/issues/44) starts at `5123a7f8` on
`fix/44-collection-place-refresh`. A mounted real `LibraryView` regression
confirms that a completed same-count/day correction refresh leaves Karachi in
the header after the metadata source changes to Lahore. The original header
watches identity/count and has no admission for content-only changes. Its top
place names are plain text, so stale link behaviour is not claimed for that
original surface.

The accepted repair reuses the library's existing refresh revision and mutation
reconciliation owner, without another library-event subscription. Background
changes share a trailing three-second read; correction and Undo reconciliation
read immediately through the same coalesced owner. Cancel superseded I/O and
guard current view/unmount, clear private names before reads, and provide
accessible read-only retry. Current names become links using the existing
place route; route contracts remain in #38. Store query/visibility semantics,
generated contracts, originals and automatic event-name Undo are unchanged.

Targeted header/library/store coverage passes 83 tests, including the repaired
mounted regression, a 100-revision coalescing test, failed privacy refresh/retry,
late identity reads, thumbnail-only admission and unmount cleanup. A scratch fault preflight confirms the
backend query refuses `p.name` while its fixture column is renamed and reads
normally after restoration. The native suite compares the built parent and
candidate in copied generated libraries, then checks manual/smart albums,
Things/events, Undo, current destinations, burst work, privacy/error/retry and
back/forward navigation. Fresh full Windows/native acceptance is pending;
final results are recorded in #44 and its child PR before integration.

The first [Windows run 42008684964](https://github.com/awaisjameel/lumen/actions/runs/42008684964)
passes production/current bindings/modules, static/docs, 1,029 frontend tests
and every Go race shard. Native artifact `13406424909` confirms the built
parent's stale Karachi header beside a corrected Lahore inspector. All four
candidate collection kinds pass correction/Undo and current destinations;
ten background corrections make one top-place read. Privacy clearing, actual
backend refusal, retry, block Undo and abandoned-view checks also pass.
Collection acceptance is 21/22 because aborting a same-view read again on the
ordinary change from a privacy refresh produces Wails' late rejection report.
The repair now lets its coalesced same-view read settle; identity, privacy
and unmount still cancel. A new regression preserves that distinction.

The preview suite's candidate passes, but its parent assertion still expected
pre-#42 amplification even though the parent now includes #42. The harness
now selects its expected contract from that checkout's generated bindings:
legacy parents must amplify; bounded parents and candidates must retain one
call, no timeline/day reads and a response below 1 KiB. Historical before/after
evidence above is unchanged. Existing other native suites pass tagging 18/18,
Undo recovery 20/20, favourite Undo 22/22 and history 7/7. The hosted report
retains SaveEmbeddings64 unblocked 181.463/70 ms; budgets remain unchanged.
Fresh verification of these refinements is pending.

A further focused regression reproduces a false identity change when an album's
privacy invalidation briefly loses its cached smart kind. Both presentations
still resolve the same album wire view. Metadata identity now follows that
stable view, matching the existing mosaic convention; the native privacy fault
case runs in the smart album to retain coverage of the invalidation interval.
The metadata baseline also reads its parent's public header revision-prop
contract before measuring outcomes: original parents must demonstrate the stale
header, while later parents must retain live metadata and current destinations.

The next [run 42013873221](https://github.com/awaisjameel/lumen/actions/runs/42013873221)
at `965ca66` passes Windows production/current bindings, static/docs checks,
all 1,031 frontend tests and every Go race shard. Preview now passes 27/27;
tagging 18/18, Undo recovery 20/20, favourite Undo 22/22 and history 7/7 remain
passing. Metadata reproduces the original parent defect but stops at an
unspecified candidate control timeout (4/5 before completion); its trace was
reset before that step and screenshot capture did not finish. Artifact
`13408554758` retains those actual results. The native harness now waits for
the first rendered grid before enabling interception, reloads both frontends
under the installed observer, and records control stages plus bounded failure
diagnostics. Interaction deadlines and assertions are unchanged. Fresh native
acceptance remains pending. The hosted report retains SaveEmbeddings64 folder
63.089/60 ms and unblocked 185.352/70 ms; budgets remain unchanged.

### #44 integration and #45 accepted scope

PR #52 is merged into the parent at `e80a1d08`, verified head `d9f1c80`.
[Run 42018179926](https://github.com/awaisjameel/lumen/actions/runs/42018179926)
passes production/current bindings/modules, static/docs, all 1,031 frontend
tests and every Windows Go race shard. Artifact `13410543832` retains metadata
22/22, preview 27/27, tagging 18/18, Undo recovery 20/20, favourite Undo 22/22
and history 7/7. Inspected screenshots show private header names removed during
the read fault and the correct current destination after navigation. There are
no uncaught page errors or unexpected console/instrumentation errors. Ten
background corrections use one 32-byte place read (5.2971 ms on that fixture).
Hosted overruns remain SaveFaces64 folder 7.327/7 ms and SaveEmbeddings64
unblocked 121.135/70 ms; budgets and configured-machine acceptance are unchanged.

[#45](https://github.com/awaisjameel/lumen/issues/45) starts from `e80a1d08`
on `fix/45-album-draft-privacy`. Its mounted regression confirms that an unrelated
delayed privacy reload erases a valid unsaved name and changes an edit into
creation. A second regression covers a saved definition changing behind the
open editor. Shared privacy invalidation must continue clearing cached lists.

The accepted repair keeps the draft in its existing editor and admits it only
against fresh visibility. Unknown visibility suspends private form content and
previews; a failed read offers read-only recovery. A still-visible target
resumes its local fields/focus without overwriting newer input. Confirmed
removal or blocking closes and clears the edit. Missing/blocked rule references
are redacted and require an explicit valid choice, never silently broadening
the rule. Save ownership includes the editor session and immutable target;
navigation, retry and late completions cannot create or change another album.

Conflict and identity checks belong in the store's write transaction as well
as the UI. A small album creation token distinguishes SQLite ID reuse (the
existing membership Undo contract deliberately permits reuse). A derived
metadata revision excludes changing counts/covers. Guarded saves compare those
tokens, reject stale or recreated targets, and acknowledge an already committed
identical result without replaying it. The same store validation/write helper
serves compatibility calls, and current editor/name-Undo surfaces use the
guarded contract. There is no persisted draft cache or second mutation journal.
Rule-reference visibility validation remains shared with preview/save logic.
Wire changes require regenerated bindings and error/contract coverage. Fresh
store/service, mounted/native global-Undo/privacy and full-system verification
are required before this child is integrated.

### #45 implementation checkpoint

The confirmed mounted regressions fail on parent `e80a1d08`: unrelated privacy
invalidation discards a valid draft and changes edit mode into creation; a
concurrent saved definition is silently overwritten. The repair leaves shared
privacy invalidation intact and keeps draft ownership in the existing editor.
Uncertain visibility removes form/preview, read-only retry retains local fields,
and confirmed missing/recreated targets close and clear. Hidden/missing rule IDs
are retired and require an explicit selection. Conflicts retain local input and
block saving until an explicitly confirmed reload or cancellation.

`SaveAlbum` checks immutable creation identity and derived definition revision
inside the existing writer transaction, preventing ID reuse and lost updates.
Counts/covers do not conflict; an identical retry performs no write. Name-only
header saves/Undo preserve server rules and use returned committed versions.
The legacy update signature remains compatible with compiled parent clients
and shares its canonical implementation. Bindings and the typed error chain
are regenerated/aligned. No persistent draft cache or membership lease is added.

Local editor/header regressions, identity/CAS/idempotence/privacy/migration
store tests and static checks are being run. A new `album-draft` native suite
is wired before the six existing regression suites in Windows CI. Native,
full Windows and final local results remain pending; this checkpoint claims no
runtime acceptance or completion of #45. Existing latency budgets are unchanged.

The first Windows run [42027465896](https://github.com/awaisjameel/lumen/actions/runs/42027465896)
passes production/current bindings/modules, static checks, 1,043 frontend tests,
service/core and store-oracle race checks. The remaining store shard finds that
`downgradeToV18` retained migration 0040's column while rewinding `user_version`.
Its shared fixture now removes both the column and trigger; the failure is
reproduced locally before this correction, and album/place/upgrade race tests
pass afterwards (4.821 s). A separate regression establishes that count/cover
changes do not invalidate a definition version.

Native album-draft setup incorrectly called the store-only `TagByName` name;
it now uses the existing bound `TagAssets` API. The inherited collection-place
parent comparison also sampled an empty header during its refresh. Repaired
parents now await the actual current destination under the existing interaction
deadline; legacy parents retain the original delayed stale-state reproduction.
The draft fixture creates its edited album last so its later deletion actually
exercises SQLite ID reuse. No production admission, assertion, deadline or
performance budget is loosened. Fresh full native acceptance remains pending.
The unchanged hosted overrun is SaveEmbeddings64 unblocked 103.061/70 ms.

A further mounted regression confirms that a save-window conflict can precede
its background event: the first reload implementation reapplied the stale list
entry. **Reload saved version** now reads the authoritative list after explicit
confirmation, keeps the editor busy during that read, and checks the same
session/visibility before adopting its result. A refused read retains the local
conflict draft behind read-only recovery. The regression fails before the fix;
fresh focused/full frontend and Windows/native verification are pending.

[Run 42052902380](https://github.com/awaisjameel/lumen/actions/runs/42052902380),
head `b63b6568`, passes Windows production/current bindings/modules, static and
1,045 frontend tests, and every Go race shard. Artifact `13429357191` records
album-draft 25/25, preview 27/27, tagging 18/18, Undo recovery 20/20, history 7/7
and favourite Undo 22/22. The inspected paused-editor screenshot contains no
private draft fields and disables saving. Collection metadata is 21/22: its
parent helper observes a correct link, then reads an emptied caption after a
later revision, mixing two DOM instants. The helper now captures caption and
destinations atomically within the existing deadline for parent and candidate.
No production metadata assertion or interaction deadline is relaxed.

A mounted follow-up confirms a reference privacy gap: a blocked/deleted preview
response can arrive before the collection refresh and leave the old Thing name
in the form. Both error-code regressions fail before the repair. The existing
preview owner now exposes only its current classified reference refusal; the
editor invalidates its selected reference caches and uses the same admission
path. Confirmed missing IDs require an explicit replacement. Read-only reference
retry refreshes choices without restoring retired IDs; an obsolete preview
cannot invalidate a newer rule. The inherited native preview suite now checks
redacted names as well as cleared samples and explicitly reselects a restored
reference. Generic unavailable-target feedback also omits protected names.
Fresh full frontend/Windows/native acceptance of these final changes is pending.
The unchanged hosted overrun on this head is SaveEmbeddings64 unblocked
116.973/70 ms; the configured-machine gate remains open.

Two final mounted regressions confirm that cached loaded choices cannot admit
a pending reference read, and that confirmed saved-version reload must refresh
its reference choices along with the album. Both fail before the repair by
retiring a still-valid new Thing ID. Reference admission now includes loading,
missing-ID reconciliation waits for settlement, and confirmed reload reads all
three existing list owners together. A delayed first render also lost its
initial field focus; its assertion fails before seeding the intended generic
name-field focus and selection. Current focused tests pass 61/61, with a full
1,051-test frontend pass. The native conflict fixture now creates its selected
Thing after the editor opens and asserts that reload adopts the valid new
choice. Fresh final-head acceptance remains pending; no budget is changed.

[Run 42058774883](https://github.com/awaisjameel/lumen/actions/runs/42058774883),
verified head `38c208ce`, passes every Windows job: production/current bindings/
modules, static/frontend and all Go race shards. Artifact `13432725928` records
album drafts 25/25, metadata 22/22, previews 27/27, tagging 18/18, Undo recovery
20/20, history 7/7 and favourite Undo 22/22. There are no uncaught page errors or
unexpected instrumentation errors. The inspected paused-editor screenshot has
no private fields, and the parent metadata report records one atomic Lahore
caption/link snapshot (`/places/1172451`) matching the store and membership.
The revised preview suite confirms immediate reference-name/sample redaction
and explicit restoration after read-only retry. The unchanged hosted overrun
is SaveEmbeddings64 unblocked 185.861/70 ms; hardware acceptance remains open.

The seven final admission/focus/native-fixture/documentation changes above are
now prepared for their own full Windows/native run, including the new 26th
draft case. This record accepts the verified candidate's results without
claiming runtime acceptance for those final refinements yet.

The final admission candidate, head `885de9de` in
[run 42062544358](https://github.com/awaisjameel/lumen/actions/runs/42062544358),
passes production/current bindings/modules, static checks, 1,051 frontend tests
and every Go race shard. Metadata 22/22, preview 27/27, tagging 18/18, Undo
recovery 20/20, history 7/7 and favourite Undo 22/22 pass. The extended draft
suite stops at 14/15 during its confirmed-reload wait: immediate `$eval` throws
while the form is deliberately absent for reference admission. Artifact
`13434896530` has no page errors; its inspected failure screenshot/DOM show the
correct current name, newly created Thing and enabled Save after readmission.
The predicate now observes temporary absence as pending via one DOM evaluation
under the unchanged timeout. No application code or assertion is weakened.
Full completion of the extended 26-check journey remains pending. Hosted
reports retain folder SaveFaces64 7.048/7 ms and unblocked SaveEmbeddings64
166.493/70 ms; budgets and the configured-machine gate remain unchanged.

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
