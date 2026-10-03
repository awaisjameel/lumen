# Lumen: automatic update plan

Date: 4 October 2026. Status: planned; implementation has not started.
Roadmap: Phase 12, with packaging and trust contracts established in 7A.

The owner requested automatic discovery of new versions, an **Update**
button, background downloading/preparation and a **Restart to update**
prompt. Installation must preserve the library and require neither manual
uninstallation nor a visit to a download page in the normal success path.
This extends [the next development plan](next-development-plan-2026-10-04.md).

## 1. Product flow

1. Once automatic checks are enabled, check shortly after startup without
   delaying the first grid, then approximately daily while Lumen is open.
   Add jitter, bounded retries and backoff; coalesce manual and scheduled
   checks. Do not install a Windows service or task just to check while closed.
2. Show a quiet notification: **Lumen {version} is available**, with
   **What's new**, **Update** and **Later**. Keep the update available in
   Settings → About → Updates. Avoid repeated notifications for the same
   release and disruptive prompts during immersive viewing.
3. Clicking **Update** downloads, authenticates and stages that exact
   release in the background. The current version remains usable. Show
   downloaded bytes, total size, progress and meaningful phase labels;
   allow cancellation and retry before activation.
4. After the entire runtime is verified and ready, show **Update ready —
   restart Lumen to finish**, with **Restart now** and **Later**. A WebView
   reload alone cannot activate a new Go executable or DLLs. Never restart
   automatically because a download finished.
5. On **Restart now**, save pending settings, reach safe job checkpoints,
   close media streams and workers, close the database, then hand activation
   to a separate helper. The helper waits for the owning process to exit,
   applies the staged release and launches Lumen with the same library.
6. The new process confirms the intended version and healthy startup.
   Show one success notice. On failure, recover the previous runnable
   release when schema compatibility permits, explain the result and offer
   retry. Keep a repair route available if automatic recovery cannot finish.

“Ready” means downloaded, verified and staged, not that the running app has
already changed. A final brief activation happens after restart approval.
Later preserves the staged candidate across ordinary app restarts; a normal
exit does not silently authorise activation. Cancelling a download removes
only owned temporary data and leaves the installed version intact.

## 2. Privacy and settings

The owner's 4 October request supersedes Phase 6's absolute no-network
decision **for update checking and delivery only**. Photo processing,
search and AI remain local; no photo, path, library record, diagnostic
report, account identifier or telemetry is uploaded by the updater.

Retain an explicit first-use **Enable automatic update checks** choice and
a settings toggle, respecting the original opt-in requirement. Existing
installations stay offline until that choice is made. Once enabled,
discovery runs automatically; downloading starts when the user clicks
Update. **Check now** works as an explicit one-off network action even
with automatic checks disabled. Disabling checks cancels scheduled checks;
an already requested download has its own clearly available Cancel action.

Disclose that release hosts receive normal request information such as IP
address and a minimal updater user-agent. Checks use fixed HTTPS release
endpoints, send no library context and require no GitHub token or account.
Use bounded platform TLS verification and supported system proxy behaviour;
never bypass certificate errors. Restrict redirects to documented delivery
hosts, and do not execute remote scripts or load remote release-note assets.
Authenticode certificate/revocation services may also be contacted during
verification; document and bound that behaviour rather than promising
that only GitHub receives requests.

When implementation ships, update installer welcome text, onboarding,
About/help, README and release notes together: current copy says Lumen
never connects to the internet. Describe the limited opt-in exception
accurately, retaining offline photo processing and offline installation.
Add regression coverage for no automatic requests when checks are disabled
and no library payloads in any updater request; preserve other privacy gates.

| Setting               | Planned behaviour                                                                                                             |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| Automatically check   | Opt-in; startup and daily checks while running.                                                                               |
| Check now             | Manual check with last-check time, last result and installed version.                                                         |
| Release channel       | Stable by default; Preview explicit. Channel classification belongs to signed release metadata, not just the numeric version. |
| Metered connections   | Defer automatic checks when reliably detected; warn before a requested large download and allow an explicit choice.           |
| Battery and bandwidth | Avoid competing with viewing/indexing; optional battery defer and a measured bandwidth limit.                                 |
| Notifications         | Quiet availability notice, Later and per-release dismissal; readiness remains visible in Settings.                            |
| Staged update         | Show version, size and readiness; allow discard and retry without changing the installed application.                         |

Update preferences and the update journal are **application/install scoped**,
under a per-user updater directory outside replaceable runtime files.
They do not switch with the photo library. This is an explicit exception
to Phase 10's library-scoped SQLite preference owner: library/viewer
preferences stay there; update policy has one separate global owner.
Use typed/versioned contracts, atomic persistence, migration and ordered
saves. Portable appearance presets cannot enable checks, switch update
channels or start downloads. No background download or forced restart
preference is part of the initial scope.

## 3. Current Windows baseline and integration choice

The checked repository uses Wails **v3.0.0-beta.26**, a per-user x64 NSIS
installer, a stable executable path, bundled DLLs/FFmpeg/models/notices
and an owned-runtime inventory. Version authority is `build/config.yml`
(`0.9.1` at inspection), propagated to executable/installer resources.
The release location is `https://github.com/awaisjameel/lumen/releases`.
Signing is optional in the current build. These are local implementation
facts, not evidence that a signed public release or update feed exists.

The pinned Wails module includes `pkg/updater` with providers, signature
verification, progress events, staging and a restart helper. Evaluate and
reuse suitable framework facilities before adding a second updater. Two
specific integration gaps must be addressed:

- The Windows path swaps a single executable target; archive extraction
  expects one top-level target. Lumen needs a consistent **whole runtime**,
  not a new EXE beside old DLLs/models. Do not pass Setup.exe to an API
  expecting an application binary.
- In the inspected version, the periodic loop calls `CheckAndInstall`.
  Set framework periodic checking to zero and let Lumen's scheduler call
  **Check only**. Downloads require the user's Update action. Use the
  shared Nuxt UI surface rather than a competing built-in update window.

12A must select and document one complete activation strategy: a bounded,
journalled runtime transaction with a signed helper, or a hardened NSIS
update mode that provides equivalent backup/recovery. Preserve the current
installer and per-user scope. The existing silent installer alone is
insufficient: it removes the previous owned runtime before writing the new
payload and has no whole-update rollback transaction. `/S` hides its UI;
it does not supply integrity, safe shutdown or crash recovery.

Keep the stable launch path, shortcuts, installation location and Add/Remove
Programs entry correct. If a versioned-runtime/launcher layout wins the
spike, define its bootstrap migration, DLL/model lookup, registry updates,
uninstaller ownership and fallback before using it. Unmanaged/portable/dev
builds expose only tested capabilities and do not guess a writable install
directory or overwrite a source checkout. An unsupported installation can
offer an explicit repair/download link without breaking browsing.

## 4. Trusted release delivery

Release hosting is transport, not the root of trust. SHA-256 from the same
unsigned download page is not sufficient authentication. Choose a maintained,
pinned Go TUF client if feasible in 12A; otherwise document and review an
equivalent signed metadata protocol with its precise limitations. Do not
claim TUF protection merely because an artifact has a signature.

Required trust contracts:

- Embed the initial release trust root in updater-capable builds. Sign
  release metadata and artifact hashes/lengths; fail closed for missing,
  invalid, expired or untrusted metadata. Bound metadata and download sizes.
- Bind product ID, channel, platform/architecture, target version, minimum
  supported Windows/updater version and payload inventory to that metadata.
  Select by explicit identity, not “first EXE attached to latest release”.
- Persist accepted metadata versions and expiry checks to reject replay,
  downgrade and stale/frozen feeds. Handle incorrect clocks visibly without
  bypassing checks. Planned rollback is an authorised local recovery to a
  journalled installed release, not acceptance of an older remote update.
- Define trusted key rotation, revocation and expired-feed recovery before
  shipping. A feed cannot replace its own trust anchor without an authorised
  transition. Protect signing secrets outside the repository; use separate
  restricted publication/signing credentials and an offline recovery root.
- Require timestamped Authenticode signing of Lumen-owned executable entry
  points, updater/helper, installer and uninstaller for public in-app updates.
  Verify Windows trust and the authorised publisher policy, including
  legitimate certificate renewal, before running downloaded code. Bundled
  third-party binaries remain covered by the signed inventory and hashes;
  do not require every third-party DLL to carry Lumen's publisher signature.
- Reverify complete payload hashes before activation and preserve the checked
  files against substitution between verification and use. No unsigned or
  hash-only fallback when signature services or metadata fail.

Publish immutable, versioned payloads alongside the ordinary offline
installer. Initial scope uses a complete application/runtime package
without redundantly bundling WebView2 into every update. Check runtime
prerequisites and use a separately authenticated Microsoft redistributable
only when needed. Show actual download/staging/backup disk costs; models
make these significant. Component reuse or delta delivery is a later
optimisation, requiring full hash verification and complete-package fallback.

Release pipeline: build → required gates → inventory/notices → sign →
verify clean-machine install/update → upload immutable artifacts → publish
authenticated release metadata last. Keep version/tag/resources/feed in
agreement. Publishing remains a separate authorised operation. Refresh
expiring metadata on a documented cadence even when no new version ships;
monitor that process and retain old payloads needed for supported upgrades.
Withdraw a bad release through authenticated metadata. Revalidate availability
before activation; if freshness cannot be established, explain the delay.

## 5. Backend ownership, durability and activation

`internal/update` owns policy, authenticated release selection, scheduler,
download, staging and recovery state. Services expose typed status/check/
download/cancel/discard/restart commands through generated bindings. One
frontend store consumes snapshots plus generation-tagged events and
survives remounts; component lifetimes never own the update process. The
helper owns runtime activation only, not photo actions or library SQL.
Reuse existing shutdown/job coordination and installer inventory contracts.

Persist an atomic journal outside the replaceable installation tree:
`idle → checking → available → downloading → verifying → staging → ready →
quiescing → applying → awaiting-health → complete`, with explicit cancelled,
failed and recovering outcomes. Store authenticated candidate identity,
owned paths/hashes, installation identity, previous runtime inventory,
compatibility policy and durable checkpoints. Show truthful state after
restart; a readiness flag alone is not proof that staged files remain valid.

Download streams use bounded timeouts, progress, memory and disk budgets.
Resume only when Range support and a validator bind bytes to the exact
authenticated artifact; otherwise restart the download. Verify the final
complete hash regardless. Prevent concurrent check/download/apply operations
and stale events from changing the selected candidate. Polls must not discard
a user-requested download or replace a staged candidate silently.

Stage onto the installation volume before activation. Reject path traversal,
unexpected/duplicate entries, unsafe links/reparse points and excessive
archive expansion. Every runtime path belongs to the authenticated inventory;
do not recursively wipe an installation directory or remove unrelated files.
Validate free space for download, expanded runtime, recovery copy and any
database backup. Fail before shutdown when capacity/permissions are inadequate.

Activation takes an installation-wide lease, including Lumen processes
using different library directories. Gracefully quiesce active writes and
file-operation transactions; compression/organiser jobs checkpoint or finish
their current commit. If this cannot complete within a bounded timeout,
keep the old app and explain **Waiting for active work**. Never kill it
mid-transaction. Spawn the helper hidden, identify the parent by a process
handle/identity rather than a reusable PID alone, and confirm helper readiness
before requesting normal application shutdown. Preserve library location
through validated relaunch arguments; do not persist blocked-media unlocks.

The helper revalidates install identity and staged files, backs up the owned
runtime, applies with a recoverable journal and updates version/registration.
No mixture of runtime generations can be launched. Power loss at any step
must leave either the previous release runnable or a validated recovery
entry point. Retries are bounded; sharing/antivirus locks yield a visible,
recoverable error. Cleanup removes only verified, updater-owned leftovers.

Binary rollback requires a **database compatibility policy**. Before any
schema-changing update, obtain a consistent SQLite backup using the existing
store's backup mechanism while jobs are stopped; copying only the live `.db`
ignores WAL data. Validate backup integrity and preserve preferences, sources,
annotations, blocks and durable job journals. Gate first-launch mutations,
indexing and user interaction until migrations, database open, required
DLL/model capability probes and frontend readiness pass a bounded health
handshake tied to the update transaction. Only then release normal work.
If validation fails before new writes, restore the matching database/runtime
pair. Never automatically restore an old backup over work produced after a
healthy upgrade. Later failures require compatible recovery or an explicit
repair flow. Retain recovery material through health acceptance and a
documented grace period, with clear storage/cleanup ownership.

## 6. Milestones and acceptance

Numbering does not make this dependent on finishing lossless compression.
Trust/installer/shutdown spikes begin in 7A; update UI uses the new shell.
The updater can ship as a separate verified slice after those foundations,
before or alongside later media phases. It must coordinate whichever jobs
are implemented at that release, including compression when added.

| Milestone | Deliverable                                                                         | Required evidence                                                                                                                  |
| --------- | ----------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 12A       | Trust/feed choice, whole-runtime activation spike, signing and bootstrap contracts. | Pinned Wails API review, fixture feed verification, clean-VM swap/recovery, signing requirements and packaging size measurements.  |
| 12B       | Opt-in scheduled checks, notifications, release notes and Updates settings.         | Check-only timer, single-flight behaviour, offline/rate-limit handling, channel rules and no library data in requests.             |
| 12C       | Requested background download, verification, persistent staging and restart prompt. | Cancel/retry/resume, corrupt signatures/hashes, bounded extraction, low disk and readiness surviving restart.                      |
| 12D       | Safe shutdown, helper activation, healthy relaunch and rollback.                    | Native process locks, multi-library instances, job checkpoints, schema/WAL backup and fault injection at every durable transition. |
| 12E       | Authenticated release pipeline and installed-version acceptance.                    | Two genuinely signed packaged versions, every supported upgrade hop, clean/older Windows machines and update/uninstall regression. |

Test both same-runtime and changed-DLL/model upgrades, Unicode/space/custom
paths, removable/different volumes, locked files, antivirus interference,
permission failures, proxy/TLS errors, offline startup, HTTP 429/5xx,
interrupted transfers and cancelled shutdown. Test replayed/expired/revoked
metadata, wrong architecture/product/channel, certificate renewal and
release withdrawal after staging. Inject helper/app crashes and power-loss
equivalents during backup, apply, migration and health acknowledgement.
Confirm shortcuts, registry version, inventory cleanup and uninstall agree
with the activated release; unrelated files and photo originals survive.

Verify Settings/About, notification, keyboard/focus, narrow window, light/dark,
reduced motion and truthful restart states in native WebView2. Run affected
Go/race tests, bridge regeneration, frontend checks, `scripts/lint.sh`,
production build and signed packaging. Local mock-feed checks do not prove
real-host delivery or real installed upgrade acceptance.

Current releases without an updater cannot discover/install their first
updater-capable version themselves. That initial upgrade uses the existing
installer **over the current installation**, without manual uninstall;
later versions use the in-app workflow. Include previous-version/bootstrap
acceptance and explain this once in release notes. No actual feed, network
behaviour or code-signing configuration is changed by this planning document.

## Sources and baseline

- [Pinned Wails updater configuration](https://github.com/wailsapp/wails/blob/v3.0.0-beta.26/v3/pkg/updater/config.go), [lifecycle/scheduler](https://github.com/wailsapp/wails/blob/v3.0.0-beta.26/v3/pkg/updater/updater.go), [archive extraction](https://github.com/wailsapp/wails/blob/v3.0.0-beta.26/v3/pkg/updater/extract.go) and [Windows helper](https://github.com/wailsapp/wails/blob/v3.0.0-beta.26/v3/pkg/updater/helper_windows.go). Inspected the installed module, not just newer documentation; recheck at implementation.
- [NSIS command-line usage](https://nsis.sourceforge.io/Docs/Chapter3.html): silent mode and installation-directory behaviour.
- [The Update Framework specification](https://theupdateframework.github.io/specification/latest/): authenticated metadata, trust rotation, expiry and replay protection; application installation/recovery remains Lumen's responsibility.
- [Microsoft WinVerifyTrust](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-winverifytrust): executable publisher/integrity trust verification.
- Local baseline: `go.mod`, `build/config.yml`, `build/windows/Taskfile.yml`, `build/windows/nsis/project.nsi`, `main.go`, `internal/diag`, `frontend/src/about`, `AUDIT_CONVENTIONS.md` and `docs/phase6-design-2026-10-03.md`.
