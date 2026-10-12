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
  journal and expose a read-only update surface. Separate shared runtime file
  leases prevent the helper switching while any installed library is open.
- Policy and trust live in `%LOCALAPPDATA%\LumenUpdater\<installation identity>`;
  they never enter portable appearance presets or the library's settings.
  Atomic state files are flushed before Windows write-through replacement.
- Automatic discovery is off by default, check-only, startup/daily and
  single-flight. Unknown/metered connection costs defer scheduled discovery;
  battery defer and a bandwidth limit are typed policy choices. Manual Check
  remains a one-off action. Runtime download and activation require distinct
  explicit actions; ordinary exit cannot approve activation.
- The pinned go-tuf v2.4.2 client performs complete refresh using bounded,
  context-scoped HTTPS transport. Accepted rotated roots and metadata versions
  are durably saved together, including accepted progress before a later role
  failure. Discovery never fetches the runtime. Fixed origins, TLS verification
  and bounded redirects apply to delivery too; no library payload is sent.
- Downloads stream with length/hash validation, cancellation and progress.
  A failed transfer starts from zero; unsafe Range resumption is not attempted.
  Staging accepts regular inventory files only, rejecting traversal, Windows
  aliases, duplicate/colliding paths, links, unexpected entries and expansion
  beyond signed limits. Generation verification uses root-scoped file access.
- The frontend has Updates settings, plain-text notes, ordered snapshots,
  availability/readiness notifications, Cancel, Discard, Later and restart
  confirmation. It saves pending interface preferences before restart and
  ignores late status responses older than an event. Public delivery capability
  remains disabled until compiled trust and signing policy are complete.

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
previous selected runtime intact.

After services stop, the application obtains a real SQLite online backup,
including committed WAL pages, checks its integrity and flushes it. Only a
successful database close acknowledges readiness to the helper. The helper
renames one complete generation and atomically selects it in a transaction-bound
pending-health record. It writes no SQL.

First launch checks the intended version/schema, database integrity, runtime
capabilities and the embedded frontend's health call. The sole writer
connection is held, and indexing/organiser/compression recovery wait behind the
health gate. The durable health checkpoint precedes admitting normal writes.
Before that checkpoint, recovery uses the old application's store API to
restore the matching library backup. After acceptance it cannot restore an old
database over new work. Recovery snapshots and inventoried old runtimes remain
for seven healthy days. Archived accepted transactions preserve grace periods
across successive upgrades; cleanup retains unrelated files and retries failures.

Normal launches verify inventory without invoking certificate services, so
disabling automatic checks retains offline startup. Requested preparation and
activation verify timestamped Windows publisher signatures against compiled
certificate digests. A failed signature has no unsigned fallback.

## Production signing and release process

The [production runbook](production-signing.md) and `scripts/release.ps1` cover
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
The final local gates passed:

| Gate                              | Result                                                                                                                    |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `scripts/lint.sh`                 | Passed, including zero Go lint issues, deadcode, frontend checks, Python, ShellCheck and installer regressions            |
| Go race suite                     | All named application/internal/service/command packages passed; ordinary real-fixture skip conditions remain              |
| Frontend regressions              | 587/587 in 57 files                                                                                                       |
| Production build and NSIS package | Passed after serialising module discovery and frontend installation                                                       |
| Extracted package inventory       | 45/45 hashes and payload checks                                                                                           |
| Packaged native workflows         | 322/322 across 12 suites, including Updates 15/15                                                                         |
| Scratch managed-layout acceptance | 12/12, including Unicode paths, stable launching, leases, graceful exit and unrelated-file preservation                   |
| Static launcher/helper PE imports | Only `kernel32.dll`; no replaceable runtime DLL dependency                                                                |
| Signing-tool fixtures             | Protected quorum root creation, signed feed, online refresh, exact target withdrawal and dual-quorum root rotation passed |
| Rotated trust after restart       | Persisted root 2 survives reconstruction; revoked original authority cannot sign root 3                                   |

Ignored evidence is in `e2e/.work/phase12-*.log`,
`e2e/.work/package-0.12.0/payload.json`, per-suite reports/screenshots under
`e2e/.work/results/`, and `e2e/.work/layout-*/report.json`. The package task
and script checks are reproducible from the tracked scripts.

The installer is unsigned: 887,140,364 bytes (about 846 MiB), SHA-256
`a32b69b7242c71c4142a99c0f7e35a2c2df1c080af60c2dcc3bfafd4182710a0`.
The complete runtime ZIP is 670,867,241 bytes (about 640 MiB), SHA-256
`8dd2305da1f0c43f31f5358f7834f7ead7633e1f43a7a523b77a45a2e2e2867b`.
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
