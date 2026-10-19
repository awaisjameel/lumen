# Phase 7 implementation review

Reviewed on 4 October 2026, starting from `5ff1d5b` on `phase7/frontend`.
The scope is 7A–7C in [the development plan](next-development-plan-2026-10-04.md),
checked against [the baseline inventory](phase7-inventory-2026-10-04.md) and
[the implementation record](phase7-design-2026-10-04.md).
Changes remain local and uncommitted.

## Findings repaired

| Finding | Repair and regression evidence |
| --- | --- |
| An appearance or panel edit during startup could save cached defaults over the library's other choices; a late or repeated load could overwrite the edit. | Preference reads are coalesced, successful initialisation is retained, and edits wait for the library before merging their patches. Failed reads prevent writes and can be retried. Deferred-response tests cover concurrent startup edits and repeat loads. |
| The UI ignored the normalised preference set returned by Go and could roll back to a value the engine never stored. | The save owner retains the actual returned set; only the newest save applies it. A later failure rolls back to that canonical set. |
| A stale first-paint cache could apply an unsupported text scale before the engine responded. | Cached text scale is rounded and clamped to the bridge's contract limits. A regression checks negative cached scale. |
| Reading preferences while a save replaced their cache entries could return part of each choice. | The reader uses the same settings mutex as the writer. A concurrent read/write regression checks that every result is a complete saved set. |
| Each file-copy row's folder button opened the default copy instead of the chosen path. | The row passes its exact path through the existing typed bridge and service, which still validates membership. The generic command keeps its default-copy behaviour. |
| Grid keys could change selection or toggle the inspector while an overlay owned the keyboard; composing Escape could clear selection. | Grid handling checks the shared overlay coordinator, event consumption and composition before dispatching or navigating. |
| Collection menus repeatedly scanned every collection command for each card, and a previously rendered menu could invoke an obsolete command object. | `useItemCommands` indexes commands by item, owns registration cleanup, and resolves the current command when selected. Albums, Events, People and Places share it. Registration removal uses identity sets. Tests cover busy state, replacement, removal and scope disposal. The palette also resolves current registrations. |
| The crash notice still rendered a separate banner, contrary to the single notification owner. | It now uses the shared persistent toast, with a report action that stays open while saving. Cancellation, failure, retry, dismissal and dismissal during an outstanding save are tested. |
| A closing toast's delayed removal could take a newly offered notification with the same logical id away. | Closing and new toasts have different presentation ids; pending replacements still coalesce. Close callbacks apply only to their current notification. The real presenter regression exercises replacement inside the removal interval. |
| At 520 CSS px, the Settings accent selector squeezed its description into a thin column despite passing horizontal-overflow checks. | Settings rows query the actual content column, and the shared row gives text and controls bounded columns. Controls wrap or move below their descriptions. The production WebView screenshot confirms the readable narrow layout. |
| Resizing a scoped grid could mutate its virtual layout during observer delivery, producing an undelivered-notifications warning. | Grid, covered-media, duplicate and timeline measurements share a cancellable frame scheduler; the affected grids' TanStack measurements also use its frame scheduling option. The scheduler regression covers coalescing and disposal. Native tracing identified the scoped-grid transition; the repaired production tour no longer reports it. |
| Wails beta.26's late `context canceled` response after an aborted read surfaced as an unhandled rejection and a false failure toast. | The bridge recognises only the runtime's cancelled-promise wrapper with that exact runtime cancellation cause. The rejection listener marks that expected cancellation handled. Other late failures and plain runtime failures remain reportable, with regression coverage. |
| An organise run's journal could become terminal while its scan hold and retry cleanup still kept the service busy. The UI could offer Undo and receive `organise_busy`. | Public run state remains running/undoing until cleanup ends; terminal progress is emitted when the owner releases its place, before another action can emit progress. The deterministic regression pauses between journal completion and worker return, and checks that receiving completion permits immediate Undo. |
| The inspector imported VueUse without declaring it directly. | `@vueuse/core` is now a direct dependency at the already integrated version. |
| Nuxt UI's build dependency included an affected esbuild release. | The graph uses `esbuild` 0.28.2 through one override, including the font tooling's copy. The seven-day installation policy is retained. |

## Coverage

| Area | Review boundary |
| --- | --- |
| Integration and offline assets | Vue/Vite integration, shared build/test options, one `UApp`, explicit imports, local icon manifest and API fallback suppression, system fonts, production-only routes and bundled licences. |
| Architecture and contracts | Core preference and capability types, store ownership and migration, service registration and error mapping, the bridge boundary, generated bindings, enums and numeric-limit contract checks. |
| Appearance and layout | Theme/system changes, portal inheritance, curated accents, density, text scaling, persisted panel widths, sidebar modes, inspector sheet, forced colours and reduced motion. |
| Shared interactions | Command registration and availability, palette and shortcut reference, focus return, overlay ownership, shared loading/empty/error states, notifications, confirmations and Undo. |
| Library and viewing | Canonical scoped timeline, virtualisation and focus priority, live refresh and selection, Favourites/Videos/Trash/search scopes, preview/original capability decisions, navigation, direct/converted playback and metadata. |
| Collections | People naming and face corrections, Things prompt identity and pin/reset/rename, offline Places, Event naming and pinning, manual/smart Albums and membership. |
| Privacy and file operations | PIN/gate state, immediate media revocation, covered browsing, blocks and Undo, duplicate review semantics, Trash/recycle confirmations, organise previews and journalled resume/undo. |
| Settings and startup | Existing options, search and categories, save ordering and rollback, legacy preferences, onboarding source comparison, incremental browsing, local About/report/licence workflows. |
| Release boundary | Version agreement, Windows production build, generated frontend embedding, installer payload tests and packaging. RAW rendering and automatic updating remain the later-phase contracts recorded in the feasibility documents. |

## Verification

| Check | Current-run result |
| --- | --- |
| Complete repository gate | `scripts/lint.sh` passes: Go formatting/lint, deadcode, govulncheck; Vue/TypeScript, ESLint and Prettier; **456 frontend tests in 43 files**; Ruff; **4 installer payload tests**; ShellCheck. |
| Go races | The root and every `internal/...` package passed the full race run, including the new preference regression. That run exposed the organise completion failure in `services`; after repairing it, the **entire service race suite passes** (162.8 s). Both organise completion regressions also pass **20 race-enabled repetitions**. |
| Contract generation | Production binding regeneration processes 326 packages, 5 services, 102 methods, 19 enums, 41 models and 3 events, with **no generated diff**. |
| Release build | The final Windows production build and unsigned `Lumen-0.10.0-x64-setup.exe` package succeed, including local runtime payload and third-party notices. `git diff --check` passes. |
| Native route and offline tour | **37 production WebView2 screens** at 1440, 1040, 720 and 520 CSS px, with a fresh review profile: **no horizontal overflow and no attempted external requests**. Screenshots were inspected, including narrow Settings, forced colours, dark/compact/120% text and emulated 200% DPI. |
| Native interactions | **43/43 checks pass**: theme persistence/system changes/portal inheritance, palette and shortcuts, viewer/grid keys and focus return, confirmation Escape, filtered removal, live refresh, reduced motion, forced colours, emulated 125/150/200% DPI, responsive navigation/inspector, direct/converted video and immediate blocked-media revocation. |
| Native file operation | On two isolated copies, the actual UI preview, confirmation, move, completion, Undo confirmation and restoration pass. Both files return to their original paths, the service reports `undone`, and the UI has no operation failure. The original corpus and user library are untouched. |
| Feasibility evidence | `spikes/phase7/verify_record.py` passes for the saved seven-section, 33-case/five-run record and 20 FFmpeg rows. This checks the existing evidence, rather than establishing new RAW support. |
| Dependencies | `npm audit --omit=dev` has **zero findings**. The full audit retains the four development-tool findings described below. |

Evidence lives in the ignored `.task/phase7-review/` logs and
`<scratch>/phase7-review-2026-10-04/` native reports/screenshots. The final gate
log is `gate-final-complete.log`; the repaired service run is
`services-race-final.log`; the final package is `package-complete.log`.
The completed native reports are `tour-complete/tour.json`,
`accept-complete/accept.json` and `organise-native.json`.

The final tour has no ResizeObserver warning or unhandled rejection.
Chromium still logs Wails' optional `/wails/custom.js` HEAD probe (404),
and a late cancelled runtime request can be 422. Privacy acceptance
deliberately requests revoked media and receives 404. These responses were
traced; unexpected late failures remain reportable.

The timing run on the indexed 3,000-photo synthetic fixture measures a
**625 ms process-start median** across five launches (606–3444 ms; the first
sample is 3444 ms), **128 ms reload median** across eight reloads, and
**16.7 ms median / 16.9 ms p99 / 18.4 ms maximum** over ten seconds of
scrolling, with no frame over 33.4 ms. This is one laptop and an already
indexed fixture, with OS caches warm; it does not establish first-install
or large-library timing. The existing multi-run search/viewer comparisons
remain in the implementation record and were not rebenchmarked here.

Memory after eight reloads and a full-timeline scroll measured **1389 MB**
of summed working set across Lumen and its WebView processes after 45 s.
That is an after-browsing sample, not the fresh-idle measurement in the
implementation record. A fresh process left on the first grid for 45 s
measures **467 MB for the repaired build and 504 MB for baseline 0.9.1**
on the same database/profile and at 1440×900. Each is one sample, so the
difference is not evidence of a memory improvement. Baseline 0.9.1 measures
**1302 MB after the same eight reloads, ten-second scroll and 45-second
wait**, against the repaired build's 1389 MB. Both builds retain much
larger working sets after browsing. These single samples do not establish
a leak or an improvement; memory-pressure reclamation and prolonged
large-library browsing remain acceptance boundaries. The raw reports are
`performance-complete.json`, `memory-complete.json` and
`memory-browsed-baseline.json` in the native evidence directory. The timing
harness's `idleMemoryMB` field is the after-browsing sample described here.

## Remaining acceptance boundaries

The review does not turn a finite test corpus into proof for every library,
machine or codec. Real-camera RAW support remains unimplemented and is not
claimed. Full-library scale, real face/GPS accuracy, low-end hardware,
HEVC/GPU combinations, removable-drive recovery, physical touch input and
actual Windows contrast/DPI settings need their own acceptance runs.
Native fault injection, wrong-PIN cooldown, Windows sign-in recovery and
crash/report file-dialog handling rely on component/service tests; they
were not all exercised through native dialogs in this review. The native
organise check uses two files; interruption, resume and conflict safety
remain covered by the real-filesystem service/runner tests.
Installer installation, upgrade and uninstall are distinct from building
the package. Signing and a live update service are outside this phase.

The full npm audit still reports four high-severity development-tool findings
through `@vue/eslint-config-typescript` → `fast-glob` → `micromatch` →
`braces`. They share [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm),
for which the upstream advisory lists no patched release. The lint tool
uses repository-controlled glob patterns and these packages are not bundled
into the desktop UI. Downgrading the established ESLint configuration is
not a compatible repair. This remains an upstream dependency finding;
the production dependency audit has zero findings after the esbuild repair.
