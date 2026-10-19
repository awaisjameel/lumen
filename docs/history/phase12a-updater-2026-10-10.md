# Phase 12A: trust and activation foundations

Date: 10 October 2026. Status: started, not complete. This follows the
[updater feasibility record](phase7-updater-feasibility-2026-10-04.md) and
[automatic update plan](automatic-updates-plan-2026-10-04.md). Lumen 0.11.0
still makes no off-device request and contains no update service or scheduler.

## Trust choice and the first executable experiment

Select `github.com/theupdateframework/go-tuf/v2` **v2.4.2** for the 12A
experiment. Its module checksum is
`h1:w7976/W8uTwlsegP5nRymlpjPgrwSh+AXUf85is6nJk=`; the tag resolves to
`f5edbde31e5507f46db2069402dc38903fe6d9d4`. The standalone module in
`spikes/updater/` pins all resolved dependencies in `go.mod`/`go.sum`; none
is added to the application module or installer. The upstream package is
Apache-2.0 licensed.

Use the complete TUF client refresh workflow for release metadata. The
embedded root bootstraps trust, while accepted roots and metadata versions
belong to one durable installation-scoped owner. Separate root, targets,
snapshot and timestamp authorities; require an offline multi-key root and
an operational rotation/revocation procedure before public delivery. Fixture
keys are generated only in memory and are not production keys.

`spikes/updater/trust_test.go` exercises the real client through an in-memory
fetcher, with no socket or live feed. Five top-level tests and their subcases
pass under the race detector:

- Metadata refresh and exact product target lookup make no runtime request.
- A requested payload must match its authenticated length and SHA-256.
- Unsigned, wrongly signed, expired, malformed, missing and oversized
  metadata fail before any runtime request. Tampering with release identity
  remains rejected even when the online snapshot/timestamp roles re-sign
  their links to the altered targets metadata.
- A fresh client rejects a timestamp older than its persisted accepted one.
- A replacement root requires the old authority and its own new authority;
  a self-signed substitution fails.

The target experiment signs product/channel/platform/architecture/version
and schema policy as TUF target custom metadata. Application eligibility is
still a separate typed validation step: an authenticated target may be for
the wrong product, channel, architecture, Windows version or schema. The
experiment does not yet implement those production rules or an inventory
parser. Public metadata will bind the complete payload inventory, helper
identity and minimum supported updater as well.

Source review found integration work to resolve before promoting the client:

- Its fetcher interface has no context parameter, and the default fetcher
  retries with `context.Background`. Supply a request-scoped bounded transport
  with cancellation and an explicit origin/redirect policy.
- Client instances are not concurrent-safe, and each refresh runs once.
  The installation-scoped coordinator must serialise refresh/download and
  construct each refresh with the currently accepted root.
- Construction persists the supplied root. Repeatedly supplying the original
  embedded root would overwrite the cached rotated root. Prove accepted-root
  persistence and revoked-key refusal across restart before shipping.
- Its cache writes a temporary file, closes, renames and reads back, without
  `File.Sync`. The passing restart test is not a power-loss durability test.
  Persist accepted trust transitions durably under Lumen's owner and probe
  interruption before relying on anti-replay state after power loss.
- `DownloadTarget` buffers the downloaded target. Use authenticated target
  information with Lumen's bounded streaming download and final length/hash
  verification for the large runtime, rather than loading it into memory.

TUF acceptance is not Windows publisher acceptance. The installer, stable
launcher, application and independent helper must carry timestamped
Authenticode signatures. Verify Windows trust and the authorised publisher,
including renewal/revocation rules, before downloaded code runs. Production
key custody, protected signing access, feed hosting and expiry refresh are
still external prerequisites; no live release was published or contacted.

## Whole-runtime activation direction selected for the next spike

Use an **independent helper and versioned runtime generations**, retaining
per-user NSIS and a stable `Lumen.exe` launch path. This is the selected
experiment direction, not an implemented or accepted activation capability.
In-place replacement of dozens of files can leave a mixed generation after
interruption; staging a complete generation before switching one activation
record makes the coherence boundary explicit.

Proposed installation layout:

```text
<installation>/Lumen.exe                  signed launcher, no cgo or runtime DLLs
<installation>/uninstall.exe              NSIS-owned
<installation>/.lumen-runtime-files.txt   installer ownership
<installation>/runtime/<generation>/     app EXE, DLLs, tools, models, notices
<installation>/.update/                   same-volume owned staging/recovery
%LOCALAPPDATA%/LumenUpdater/              policy, trust history, activation journal
%LOCALAPPDATA%/Lumen/                     existing default photo library
```

The helper executes outside the replaceable generation and has no dependency
on its DLLs. Build launcher/helper with `CGO_ENABLED=0`; verify their PE imports
and native process behaviour. The launcher selects a generation only through
a validated activation record and must refuse an unresolved/invalid state.
It preserves absolute `LUMEN_DATA_DIR` and approved relaunch arguments, clears
development runtime overrides and never carries a blocked-media unlock.
The app resolves codecs/models/notices beside its generation's executable.

Contracts to implement and prove in the activation spike:

1. Stage only authenticated inventory paths on the installation volume.
   Reject traversal, alternate streams, case-folded duplicates, unsafe links,
   reparse points and excessive expansion. Verify the entire generation and
   preserve verified bytes against substitution before use.
2. Coordinate an installation-wide activation lease with launches and all
   running libraries. A library-specific single-instance mutex is insufficient.
   Hold process identities/handles; bounded waiting must leave the old release
   usable, without killing a file transaction.
3. Drain pending settings and synchronous writes, checkpoint compression and
   organiser publication, close streams/workers and acknowledge database close.
   Current waiting `Stop` methods are useful, but not a complete admission/drain
   barrier. The deferred `Store.Close` error in `main.go` is still unchecked.
4. Add the consistent backup API under `internal/store`: current code has no
   production SQLite backup method. Validate WAL-inclusive backup integrity
   and signed schema compatibility before shutdown. Never let the helper write
   SQL or restore a live library by copying only its `.db` file.
5. Journal readiness, previous generation, the matched database backup and
   activation intent durably before switching. The launcher/help entry point
   must recover or refuse every partially committed state; no mixed runtime
   may launch. Probe Windows replacement/flush semantics, locked files and
   power-loss equivalents at each transition.
6. First launch uses a bounded, transaction-bound health mode. Migration,
   database integrity, version, required DLL/model capabilities and frontend
   readiness must pass before indexing, job recovery or user writes are
   released. Retain the prior generation and backup through health acceptance
   and a defined grace period.
7. Restore a compatible runtime/database pair only before new work is accepted.
   Never restore an old library backup over changes made after healthy startup.
   Later failure needs compatible runtime recovery or an explicit repair flow.

The initial updater-capable NSIS release must migrate the flat 0.11.0 layout
through ordinary over-install. Define removal of the old owned flat payload,
launcher resources, current-generation selection, custom paths, shortcut and
HKCU registration, new ownership inventory and uninstall cleanup together.
Unrelated files and photo libraries stay outside cleanup. Unmanaged development
or portable builds cannot infer a writable installation from their EXE path.

Wails beta.26 was re-read from the pinned local module. Its positive
`CheckInterval` calls `CheckAndInstall`; its Windows helper replaces the single
executable; absent signatures are accepted by `runVerification`; and successful
process creation is not an application health handshake. Do not initialise
that timer or use its stock activation path for this design. Suitable progress,
provider and hidden-helper readiness patterns may be reused when they satisfy
the contracts, without adding a second production updater owner.

## Evidence, packaging and the next gate

The 0.11.0 package was rebuilt and its shipped runtime passed the local gates
recorded in [packaged beta acceptance](beta-package-acceptance-2026-10-10.md).
Offline Sandbox installation was attempted, but Windows Application Control
refused the unsigned installer. This does not establish clean-machine install,
over-install or uninstall acceptance, and no security setting was changed.

This first 12A slice passes:

```sh
go -C spikes/updater test -race -count=1 -timeout 120s -v .
go -C spikes/updater vet .
```

12A remains open. Next implement the isolated launcher/helper generation-switch
experiment, root-state durability/rotation probes, schema backup and bootstrap
packaging contracts, then exercise swap/recovery on a clean Windows environment
that permits the test packages. Complete 12A needs measured payload/staging/
backup costs, native fault injection and signing contracts. Public update
acceptance later needs two genuinely signed installed versions. Check UI,
scheduled discovery, requested downloads and production activation belong to
12B–E and are not supplied by this trust experiment.

Primary references: [go-tuf v2.4.2](https://github.com/theupdateframework/go-tuf/tree/v2.4.2),
[TUF client workflow](https://theupdateframework.github.io/specification/latest/#detailed-client-workflow),
[Windows publisher verification](https://learn.microsoft.com/en-us/windows/win32/api/wintrust/nf-wintrust-winverifytrust),
and [Windows Sandbox configuration](https://learn.microsoft.com/en-us/windows/security/application-security/application-isolation/windows-sandbox/windows-sandbox-configure-using-wsb-file).
