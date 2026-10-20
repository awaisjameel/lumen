# Phase 12: updater implementation

Date: 12 October 2026. The owner accepted beta 0.11.0 before authorising this
work. The working release-candidate version is 0.12.0. This record supersedes
the implementation status in the 12A experiment record, not its trust and
activation requirements.

## Implemented ownership and contracts

- `internal/update` owns installation policy, discovery, streaming delivery,
  strict ZIP staging, publisher checks, durable trust/status and activation.
  `core.Update*` types reach the UI through generated bindings and the bridge.
  `UpdateService` is registered through the normal error-code marshaler.
- One process coordinates an installation. Other library windows observe its
  journal, expose a read-only update surface and refuse every operation; one
  of them takes over when the coordinating window closes. Separate shared
  runtime file leases prevent the helper switching while any installed
  library is open.
- Policy and trust live in `%LOCALAPPDATA%\LumenUpdater\<installation identity>`;
  they never enter portable appearance presets or the library's settings.
  Atomic state files are flushed before Windows write-through replacement.
  An unreadable journal falls back to the default policy and never stops
  Lumen opening; trust and activation records are never guessed at.
- Automatic discovery is off by default, check-only, startup/daily and
  single-flight. Unknown/metered connection costs defer scheduled discovery;
  battery defer and a bandwidth limit are typed policy choices. A deferred
  check is asked about again after 15 minutes and a failed one after an hour;
  only an authenticated answer counts as the day's check. Manual Check
  remains a one-off action. Runtime download and activation require distinct
  explicit actions; ordinary exit cannot approve activation.
- The pinned go-tuf v2.4.2 client performs complete refresh using bounded,
  context-scoped HTTPS transport. Accepted rotated roots and metadata versions
  are durably saved together, including accepted progress before a later role
  failure. Discovery never fetches the runtime. Fixed origins, TLS verification
  and bounded redirects apply to delivery too; no library payload is sent.
- Downloads stream with length/hash validation, cancellation and progress.
  A failed transfer starts from zero and leaves nothing staged; unsafe Range
  resumption is not attempted. A connection that delivers nothing for a
  minute fails instead of waiting out the hour. The archive is removed once
  its runtime is staged.
  Staging accepts regular inventory files only, rejecting traversal, Windows
  aliases, duplicate/colliding paths, links, unexpected entries and expansion
  beyond signed limits. Generation verification uses root-scoped file access.
  Links are refused inside the folders Lumen owns (the installation and its
  updater state); what lies above them is the user's choice of location.
- The frontend has Updates settings, plain-text notes, ordered snapshots,
  availability/readiness notifications, Cancel, Discard, Later and restart
  confirmation. It saves pending interface preferences before restart and
  ignores late status responses older than an event. Why an operation stopped
  is a typed `core.UpdateFailure`, not a string the interface matches. Public
  delivery capability remains disabled until compiled trust and signing
  policy are complete.

## Runtime, database and installer

The installer bootstraps a stable `Lumen.exe` launcher and `lumen-update.exe`
helper, both built with `CGO_ENABLED=0`. The application and full runtime live
under `runtime/<generation>/`. The existing NSIS inventory still owns cleanup;
generation cleanup removes inventoried files and empty directories only.
Over-install from flat 0.11.0 preserves the library and unrelated files.

Approved restart rechecks release freshness/identity, validates the staged
runtime/helper, records a transaction and closes launch admission. The helper
acknowledges readiness before normal application shutdown. It waits for the
parent's PID **and creation identity**, then the installation-wide exclusive
runtime lease; it never kills an active file transaction. Timeout leaves the
previous selected runtime intact, returns a renamed generation to staging so
that the restart can be tried again without a download, reopens launches and
starts the previous release again through the launcher.

The helper holds its own lock file for as long as it runs. A launch that finds
a barrier or a pending selection with that lock free knows the helper died
(a crash, a power cut, a sign-out) and settles the activation itself; while
the helper lives, the launch is refused and the helper remains the only owner
of recovery. Recovery reads the activation record under the exclusive lease,
so a launch that waited for a first launch to finish can never restore the
old library over one whose health was committed meanwhile.

After services stop, the application obtains a real SQLite online backup,
including committed WAL pages, checks its integrity and flushes it. Only a
successful database close acknowledges readiness to the helper. The helper
renames one complete generation and atomically selects it in a transaction-bound
pending-health record. It writes no SQL.

First launch checks the intended version/schema, database integrity, runtime
capabilities and the embedded frontend's health call. Capabilities that
depend on the PC (the AI models loading, the video server) are held to what
the previous release managed on that PC, recorded when the restart was
requested: a PC that never had them is not refused every update. A failed
health check ends the new application at once, which starts recovery. The sole writer
connection is held, and indexing/organiser/compression recovery wait behind the
health gate. The durable health checkpoint precedes admitting normal writes.
Before that checkpoint, recovery uses the old application's store API to
restore the matching library backup. After acceptance it cannot restore an old
database over new work. Recovery snapshots and inventoried old runtimes remain
for seven healthy days. Archived accepted transactions preserve grace periods
across successive upgrades; cleanup retains unrelated files and retries failures.

Normal launches check that the selected generation is exactly its inventory
(every file, at its size, no links, nothing else) without reading it and
without invoking certificate services, so start-up does not hash a gigabyte
of models and disabling automatic checks retains offline startup. Every byte
is proven when a generation is installed, before it is selected, and before
the previous one is selected again in recovery. Requested preparation and
activation verify timestamped Windows publisher signatures against compiled
certificate digests. A failed signature has no unsigned fallback.

Cleanup has one owner per place: the coordinator removes abandoned staging,
backups and acknowledgements of activations that failed or aged out, and
what a crash left of a metadata refresh; recovery removes the generation it
refused; Setup and uninstall remove inventoried generations and staging.

## Production signing and release process

The [production runbook](../production-signing.md) and `scripts/release.ps1` cover
public certificate provisioning, protected signing access, quorum root creation
and rotation, signing and verification, immutable runtime metadata, online
expiry refresh, withdrawal and publication order. Root and targets use distinct
2-of-3 Ed25519 quorums; snapshot/timestamp have separate keys. Private-key
creation is restricted to a new protected external directory, outside the
checkout. Local fixture keys have not been promoted into the build.

No publicly trusted code-signing certificate/provider credentials or production
root/feed have been supplied. The checked Windows certificate store contains
no configured Lumen signing identity, and SignTool is not on PATH. The default
proposed metadata endpoint has not been deployed. Those are provisioning and
publication boundaries, not passing production acceptance.

## Verification and remaining acceptance

Focused Go race tests cover trust signatures/expiry/replay, signed discovery
without payload requests, cancellation/single-flight, disabled startup checks,
inventory/path corruption, runtime leases, ordered policy, WAL-inclusive backup
and the first-launch writer barrier. Frontend regressions cover event/snapshot
ordering, cancellation while Check waits and failed ordered policy saves.

A same-day audit repaired the implementation and added regressions for what
it found: a launch barrier left by a dead helper (it closed every launch
until an uninstall), recovery racing a committed health check, an observing
window restarting or writing the journal, a recovered application killed
when the helper's deadline passed, the first window of an updated Lumen
opening hidden, archives and failed generations left on disk, a full hash of
the runtime on every launch, health refusing PCs that never had AI, a
stalled download, scheduler retries, a moved installation folder and the
superscript device names. The results below are from that repaired tree.

The final local gates passed:

| Gate                              | Result                                                                                                                    |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `scripts/lint.sh`                 | Passed, including zero Go lint issues, deadcode, frontend checks, Python, ShellCheck and installer regressions            |
| Go race suite                     | All named application/internal/service/command packages passed; ordinary real-fixture skip conditions remain              |
| Frontend regressions              | 594/594 in 58 files                                                                                                       |
| Production build and NSIS package | Passed after serialising module discovery and frontend installation                                                       |
| Extracted package inventory       | 45/45 hashes and payload checks, on the build before the audit; not repeated on the rebuilt installer                     |
| Native workflows                  | 322/322 across 12 suites on the rebuilt `bin/Lumen.exe`, including Updates 15/15                                          |
| Scratch managed-layout acceptance | 13/13, including Unicode paths, stable launching past an abandoned barrier, leases, graceful exit and unrelated files     |
| Static launcher/helper PE imports | Only `kernel32.dll`; no replaceable runtime DLL dependency                                                                |
| Signing-tool fixtures             | Protected quorum root creation, signed feed, online refresh, exact target withdrawal and dual-quorum root rotation passed |
| Rotated trust after restart       | Persisted root 2 survives reconstruction; revoked original authority cannot sign root 3                                   |

`TestWatchReportsChangesAndReleasesHandle` in `internal/scanner`, which this
phase does not touch, failed twice under the race detector during the audit
(no change reported within its 30 seconds) and passed in the final run and
alone; it is a timing flake to look at separately.

Activation itself (the helper's switch, the health launch, recovery) still
cannot run natively without a signed build and a trust configuration: its
logic is covered by the fixture tests above, not by an installed update.

Ignored evidence is in `e2e/.work/phase12-*.log`,
`e2e/.work/package-0.12.0/payload.json`, per-suite reports/screenshots under
`e2e/.work/results/`, and `e2e/.work/layout-*/report.json`. The package task
and script checks are reproducible from the tracked scripts.

The rebuilt installer is unsigned: 888,686,866 bytes (about 848 MiB), SHA-256
`0b88cbd210808772dbca6308692abe92e498a643005cf594c5a1f871db4413a1`.
The complete runtime ZIP is 670,877,856 bytes (about 640 MiB), SHA-256
`1db7093661bca75735a8ad9fb2b2b7ee0b6771c55fce8c1864278cc9501c6204`.
There is no public upload, active metadata deployment or production signing
claim for either artifact.

`source-map-js` was updated to 1.2.2, released 30 September and eligible under
the seven-day policy. `npm audit --omit=dev` reports zero advisories. Four
development dependency findings trace to the existing unpatched braces
advisory; no check or advisory was suppressed.

Public Phase 12E acceptance still requires the real certificate, reviewed
production custody/hosting, two genuinely signed installed releases, every
supported upgrade hop, native swap/health fault injection, older Windows and
signing/revocation/expiry operations. Fixture cryptography, the beta owner's
acceptance and local native UI checks do not supply those results. Accordingly
the application flow is implemented for local review, while Phase 12 as a
publicly shipped and accepted updater remains open.
