# Phase 7A: updater feasibility — 4 October 2026

## Scope

This is a local feasibility record for Phase 12. Whole-runtime updating is
feasible in principle, but neither Wails' executable replacement nor the
current silent installer meets Lumen's activation and recovery contract.
No updater, request, setting, control, signing configuration or release is
introduced by this investigation. Only this document is written.

**Evidence labels:** “Verified” means inspected local source or read-only
command output on 4 October 2026. “Inference” means a conclusion from that
source which still needs a native experiment. “Required contract” describes
future implementation. “External prerequisite” needs credentials, a host,
an installed fixture or another resource outside this inspection.

Read: `CONVENTIONS.md`; all of
`docs/history/automatic-updates-plan-2026-10-04.md`; sections 7 and 8 of
`docs/history/next-development-plan-2026-10-04.md`; the Phase 6 decisions and
installer design; `main.go`, `main_windows.go`, Taskfiles, Windows NSIS
sources, packaging scripts and `services/system.go`. Additional source
tracing covers lifecycle owners, file operations and store migrations.

The current offline decision is explicit in
`docs/history/phase6-design-2026-10-03.md:13-29`. Its future update-only exception is
specified by `docs/history/automatic-updates-plan-2026-10-04.md:47-75`, and does not
authorise network activity in this task. `services/system.go:22-31,65-72`
opens an allowlisted releases link in the user's browser; it implements no
release check. The loopback video server at `services/stream.go:62-78` is
local playback, not an external release request.

Commands use PowerShell. Every Go invocation sets
`$env:GOCACHE='<scratch>\phase7\gocache'`, with `GOTOOLCHAIN=local` and
`GOPROXY=off`. No build, package, test, install, download, signature-service
verification or git mutation was run. Source inspection establishes
contracts; it does not prove installed upgrade acceptance.

## What Wails provides (evidence)

**Verified.** `go.mod:3,9` declares Go `1.27.0` and
`github.com/wailsapp/wails/v3 v3.0.0-beta.26`. The command
`go env GOMODCACHE` returned:

```text
%USERPROFILE%\go\pkg\mod
```

The module exists at
`%USERPROFILE%\go\pkg\mod\github.com\wailsapp\wails\v3@v3.0.0-beta.26`.
In the citations below, **Wails/** means this exact module directory;
these are local source citations, not newer online documentation.

`rg --files` found `pkg/updater` with lifecycle, configuration, release and
provider interfaces, streaming download, verification, extraction, helper,
Windows spawn and window/event implementations. Providers are `github`,
`endpoint`, `appcast` and `keygen`; there is an internal semantic-version
package. `pkg/application/application.go:43-47,602` connects helper mode and
creates `App.Updater`. Lumen does not initialise it in `main.go:53-153`.

| Facility | Verified implementation | Consequence for Lumen |
| --- | --- | --- |
| Discovery | `Wails/pkg/updater/types.go:35-77,97-120` defines `Release`, `Artifact`, `Verification`, `CheckRequest` and `Provider.Check/Download`. `updater.go:201-244` implements `Check(ctx)`. | Suitable interfaces to evaluate for reuse, with an authenticated provider and explicit product selection. |
| Download and staging | `updater.go:259-332` implements `DownloadAndInstall(ctx)`, then reports `StateReady`. `download.go:24-110` writes a temporary file, streams a digest and syncs it. `updater.go:448` exposes `DownloadedPath()`. | “Install” here stages a candidate; it does not replace the active runtime. Staging is under the system temporary directory, not necessarily the installation volume. |
| Polling | `updater.go:126-154` starts a timer only for positive `CheckInterval`; its tick calls `CheckAndInstall`, which calls `Check` then `DownloadAndInstall` at `350-379`. | `CheckInterval` must remain zero. Lumen needs an opt-in scheduler calling check only. `WindowNone` hides the window but does not prevent automatic downloads. |
| Authentication primitives | `verify.go:36-61,73-101` supports SHA-256/SHA-512 and Ed25519, Ed25519ph and ECDSA P-256. `config.go:20-30` pins `PublicKey`. | A useful primitive, not a complete signed-feed protocol. A missing verification object or absent signature is accepted at `verify.go:74-91`, even when a public key is configured. Lumen must require authentication itself. |
| Extraction | `extract.go:19-22,38-65,90-115` supports ZIP and tar.gz/tgz, with 50,000 entries and 2 GiB expanded-size limits, and exactly one top-level entry. | A loose archive containing the EXE, DLLs and folders fails that shape. Wrapping them in one directory passes the shape but does not change the Windows replacement target. Limits and link rules still need Lumen-specific review. |
| Restart | `updater.go:395-442` re-executes the current application in helper mode, waits for readiness, then calls `Host.Quit`. `spawn_windows.go:19-30,44-53` hides/detaches the helper and handles job breakaway. | The readiness pattern is useful. It is not a whole-runtime helper or an installed-runtime health handshake. |
| Windows target | `updater_notdarwin.go:5` returns the executable unchanged from `bundleTarget`. `helper_windows.go:127-153` replaces that target, with rename-aside/cross-volume file handling. | It replaces `Lumen.exe`, not its parent installation directory. Passing Setup.exe would replace the application with an installer, not execute an NSIS update. |
| Recovery | `helper.go:132-176,191-225` waits up to 30 seconds for the parent, backs up `target + ".bak"`, retries replacement 20 times, restores on replacement or process-start failure, then deletes the backup after successful `cmd.Start`. | It cannot detect a subsequently failing database migration, missing DLL/model or broken frontend. It has no durable whole-runtime activation journal or database recovery transaction. |

**Verified trust limitations.** Wails' endpoint provider parses JSON, checks
schema/channel/version and selects an artifact in
`Wails/pkg/updater/providers/endpoint/endpoint.go:96-199`. Its manifest at
`314-347` carries version, channel, notes, publication date, artifacts and
optional digest/signature fields. The signature verification authenticates
the downloaded artifact digest; it does not authenticate the enclosing
version, channel or release policy. Its semantic comparison at `151`
compares against the running version, not a persisted highest accepted
metadata version. Expiry, signed root rotation and freeze protection are
not supplied by that manifest flow. `published()` at `327-334` tolerates
missing or malformed dates.

The GitHub provider's optional checksum handling is at
`Wails/pkg/updater/providers/github/github.go:168-178`. Its default matcher
at `391-420` selects the first platform/architecture filename match, with
some installer/sidecar exclusions. This is not authenticated product
identity. Lumen's setup filename does not include `windows`, so it also
does not match that provider's default Windows selection.

**Missing for Lumen.** A complete signed inventory covering the EXE,
DLLs, FFmpeg, model files, notices, helper and installer integration;
mandatory signed metadata; persistent replay/expiry/key-rotation policy;
application-scoped preferences and journal; cancellation/resume and
bounded resource policy; installation-wide exclusion; same-volume staging;
safe shutdown admission/draining; transactional activation and registration;
schema-aware recovery; a first-start health gate. The selected Wails
facilities can be reused only behind these contracts. Comments mentioning
future commits in this cached module are not evidence of absent behaviour:
the executable helper and window lifecycle already have implementations.

## Installer and runtime layout

**Verified.** `Taskfile.yml:13-30` dispatches packaging to the Windows
Taskfile. `build/windows/Taskfile.yml:102-134` builds the application,
generates notices and `bin/lumen-runtime.nsh`, then invokes the pinned
NSIS with the executable, runtime, notices and offline WebView2 inputs.
`build/config.yml:7-14` is the current identity/version authority:
`io.lumen.app`, `Lumen`, `0.9.1`.
`build/windows/nsis/wails_tools.nsh:16-28` agrees on version, executable
and uninstall registry key. Phase 6's `0.9.0` example is historical.

`build/windows/nsis/project.nsi:14,62-63` chooses per-user scope,
`%LOCALAPPDATA%\Programs\Lumen` by default, and remembers custom
`InstallLocation` in HKCU. `wails_tools.nsh:34-42,66-103` requests user
execution and requires Windows 10 or later on a supported architecture;
the package task permits only amd64.

The installed layout is:

```text
<InstallLocation>\Lumen.exe
<InstallLocation>\DirectML.dll, libvips-42.dll, onnxruntime*.dll, other runtime files
<InstallLocation>\ffmpeg\*.exe and *.dll
<InstallLocation>\models\faces\, models\ocr\, models\siglip2\
<InstallLocation>\THIRD_PARTY_NOTICES.txt
<InstallLocation>\.lumen-runtime-files.txt
<InstallLocation>\uninstall.exe
%LOCALAPPDATA%\Lumen\library.db, thumbs\, logs and reports
%APPDATA%\Lumen.exe\                         default WebView2 user data
<LUMEN_DATA_DIR>\webview\                    alternate-library WebView2 data
```

Evidence: `project.nsi:43-46,224-248`; `wails_tools.nsh:108-134`;
`internal/core/paths.go:13-50,54-85,88-110`; `main.go:197-206`.
Runtime lookup defaults to the executable's directory. Overrides are
`LUMEN_DATA_DIR` and `LUMEN_RUNTIME_DIR`, resolved absolutely at startup;
there is no current library-selection command-line contract.

`scripts/installer_payload.py:20-51` enumerates actual runtime files,
rejects links and invalid inventory names, and emits `File` plus UTF-16LE
`F<relative path>` inventory lines. Directory records are deepest first.
`bin/lumen-runtime.nsh:3-10,111-129` confirms EXE-adjacent DLLs, FFmpeg and
model directories. The installed inventory records only `deps/runtime`:
the application, notices and uninstaller are owned separately by NSIS.
It contains no payload hashes and is not a signed release manifest.

`project.nsi:153-218` removes only inventoried files, validates containment,
rejects wildcards and reparse points, and removes directories only when
empty. Without an inventory, previous untracked runtime files remain.
This preserves unrelated files but needs an explicit migration policy for
legacy installations with untracked, potentially incompatible DLLs.

**Verified command output.** A read-only enumeration returned:

```text
Runtime files: 30
Runtime bytes: 824992673
Lumen.exe: 21495808 bytes
MicrosoftEdgeWebView2RuntimeInstallerX64.exe: 212272848 bytes
Lumen-0.9.0-x64-setup.exe: 872434932 bytes
Lumen-0.9.1-x64-setup.exe: 872437390 bytes
```

These are existing local artifacts, not builds or installed-version
validation from this task. Notice generation is coupled to native licence
inputs, linked Go modules and Vite's bundled licence list by
`scripts/notices.py:5-10,56-60,77-81,106-117,119-195`.
Future update payloads must carry the matching notices. The approximately
825 MB expanded runtime alone makes staging, backup and database-backup
space material; exact update-package size remains unmeasured.

**Verified preservation.** Over-install invokes no uninstall and contains
no library removal step. It checks the installed EXE is writable, removes
the old owned runtime, writes the new payload and refreshes HKCU
registration/shortcuts (`project.nsi:109-130,224-248`). The normal library
and WebView2 data are outside this tree. SQLite preferences therefore
survive the installer filesystem operation: `internal/store/store.go:70-75`
and `internal/store/xmp.go:45-57` identify that owner. Keeping the same
WebView2 user-data directory also preserves its frontend storage at the
filesystem boundary. Application migrations may subsequently change the
database or frontend preferences; preservation of files alone is not proof
of migration compatibility.

**Invocation contract, not an executed acceptance test.** An ordinary
silent over-install would run the authenticated setup as the same user,
after all instances using that installation have exited:

```text
"<verified setup path>" /S /D=<validated existing InstallLocation>
```

NSIS requires the `/D=` override to be last, with the directory value
unquoted even when it contains spaces. This is the NSIS usage rule
referenced by `docs/history/automatic-updates-plan-2026-10-04.md:293`; it was not
retested against a prior installation here. Without `/D=`, the current
project uses the remembered HKCU location. A future caller needs correct
Windows argument construction, a bounded wait and exit-code handling.
No uninstall, elevation, interactive finish-page action or automatic
relaunch is required. Explicit failure codes are 2 for a locked EXE,
3 for missing WebView2 after installation, 4 for runtime/inventory errors,
and 64/65 for unsupported Windows/architecture (`project.nsi:124-126,
215-218,265-268`; `wails_tools.nsh:88-101`). Zero exit plus a health
check is necessary; `/S` provides no integrity or rollback.

**Verified WebView2 boundary.** Setup checks Microsoft EdgeUpdate registry
`pv` and runs the bundled standalone installer in `$PLUGINSDIR` using
`/silent /install` only when absent (`project.nsi:138-150,252-269`). The
redistributable is not a persistent Lumen runtime file. The Microsoft
runtime and WebView2 user data must not be included in Lumen runtime
replacement/rollback. Ordinary update payloads can omit the redistributable
as the update plan requires at `171-177`; missing prerequisites need a
separately authenticated repair path. Uninstall deletes the normal library
and WebView2 data only if the user explicitly checks its box; silent
uninstall keeps them (`project.nsi:272-305`).

**Inference and required contract.** A helper can activate a staged complete
runtime at the stable installation path after process exit, but a blind
directory swap is not yet compatible: custom folders may contain unrelated
files, the ownership inventory omits several entry points, and DLLs may
remain locked by other library instances, helpers or antivirus. Activation
must back up and replace only authenticated owned paths, preserve unrelated
files and settings, remove obsolete owned files, and refresh the inventory,
uninstaller, shortcuts and HKCU version together. Stage on the installation
volume and journal every recovery-relevant step outside the runtime tree.
A versioned runtime/launcher is an alternative only after its bootstrap,
DLL lookup and uninstall changes are proven; it is not the current layout.

The current EXE-writability test is a useful guard, not a lease or exhaustive
DLL-lock check. NSIS removes old runtime files before writing their
replacement, and WebView2 failure happens after the Lumen section. A crash,
full disk or extraction failure can leave an incomplete installation.
Therefore existing silent setup is suitable for the manually obtained
bootstrap release, subject to acceptance, but requires hardening or a
separate transaction before it can be an automatic activation strategy.

## Trust contract

**Verified current signing.** `build/windows/Taskfile.yml:112-134` signs
`Lumen.exe` when `SIGN_THUMBPRINT` is configured, using the Windows user's
certificate store via `signtool sign /fd SHA256 /sha1 <thumbprint>`.
`/sha1` selects the certificate; the file digest is SHA-256. Timestamping
with `/tr ... /td SHA256` occurs only if `TIMESTAMP_SERVER` is supplied.
`project.nsi:74-77` uses `!finalize` and `!uninstfinalize` to sign setup
and its generated uninstaller with the same command. Without a thumbprint,
packaging continues and prints:

```text
No SIGN_THUMBPRINT: building an unsigned installer.
```

Read-only environment/certificate-store checks returned:

```text
SIGN_THUMBPRINT configured: False
TIMESTAMP_SERVER configured: False
signtool on PATH: False
Current-user code-signing certificates with private key: 0
```

No certificate subject or private material was printed. These checks do
not exclude a remote signing service or another certificate store. Phase 6
explicitly records the missing certificate at
`docs/history/phase6-design-2026-10-03.md:28-30`; its throwaway-certificate exercise
at `226-233` is historical test evidence, not public publisher trust.
No Lumen updater initialisation or release public-key configuration was
found in the inspected application, service and packaging sources.
Existing artifact signatures were not queried because Windows trust
verification may contact certificate/revocation services. A timestamped,
trusted public release is not verified today.

**Required contract.** Host availability, TLS and a SHA-256 checksum from
the same release page do not authenticate a release. Phase 12 needs two
distinct trust layers: an embedded release-metadata root and Windows
publisher verification for executable entry points. `SIGN_THUMBPRINT` is
build configuration; it is neither the metadata root nor an adequate
permanent client publisher policy through certificate renewal.

The protocol choice remains a 12A decision: evaluate a maintained, pinned
TUF client offline when its source is available, or review an equivalent
signed protocol with explicit limitations. No TUF dependency or conformance
is established by this record. The minimum contracts from
`docs/history/automatic-updates-plan-2026-10-04.md:137-186` are:

- Use versioned, bounded, authenticated metadata with a specified canonical
  signed representation, supported algorithms, key identifiers and
  signature thresholds. For a custom protocol, define exact signed bytes,
  duplicate-field rejection and domain separation before implementation.
  Do not sign an ambiguously serialised object or only its artifact digest.
- Bind product `io.lumen.app`, channel, Windows/amd64, release version,
  minimum Windows/updater version, payload type, immutable artifact identity,
  length/hash and complete relative-path inventory. Include schema-read/write
  compatibility, helper compatibility and runtime prerequisites. Authenticate
  release notes/policy; render notes without executable remote content.
- Embed the initial root in the bootstrap executable. A feed cannot provide
  its own replacement root without an authorised rotation. Persist highest
  accepted metadata versions and freshness state outside the replaceable
  runtime and all libraries. Reject replay, expiry, downgrade, channel/product
  mismatch, withdrawn releases and frozen metadata; explain clock failures.
  Compare remote upgrades against accepted history as well as installed
  version. A journalled local recovery to a known previous runtime is a
  separate authorised transition, not an older feed becoming acceptable.
- Keep root/recovery keys offline or under equivalent protected custody;
  use restricted release-signing credentials and separate publication
  credentials. Record owners, approved signing ceremonies, rotation,
  revocation, expiry refresh, recovery and audit responsibilities. No
  private release or Authenticode key belongs in source or build logs.
- Require timestamped Authenticode signatures for Lumen-owned application,
  activation helper, installer and uninstaller. Verify Windows trust plus
  authorised publisher identity and a deliberate renewal policy. Define
  bounded revocation/network behaviour and fail closed. Third-party DLLs
  and models are covered by authenticated inventory hashes; they need not
  be re-signed as Lumen's publisher.
- Pin product release keys and allowlisted HTTPS delivery origins rather
  than transient TLS leaf certificates. Preserve normal system TLS/proxy
  validation, constrain redirects and send no library context. Reject
  missing signatures; never inherit Wails' digest-only acceptance policy.
  Rehash the complete staged runtime immediately before activation and
  protect against path/reparse-point and verification-to-use substitution.
- Publish immutable signed artifacts after gates and installed-version
  acceptance, then metadata last. Retain recovery artifacts and refresh
  expiring metadata even without a new release. No upload or publication
  was undertaken here.

**External prerequisites.** A production Authenticode certificate and
signing access, timestamp service, protected release keys/root policy,
release-host ownership and publication pipeline are absent from the
verified contract. Signature chain, timestamp, revocation, renewal, feed
rotation and real delivery cannot be accepted without them. Their absence
does not prevent offline fixture design, but blocks public in-app updates.

## Shutdown contract

**Verified real order.** The current Windows close path is:

1. Wails handles `WindowClosing`, closes/removes the window, and unregistering
   the last window posts `WM_QUIT`:
   `Wails/pkg/application/webview_window.go:366-370` and
   `application_windows.go:348-355`. The message loop returns at
   `mainthread_windows.go:52-67`; `application_windows.go:191-193` returns
   from platform run.
2. `App.Run` cancels the app context and shuts services down in reverse
   registration order through its defers (`application.go:655-670,774,
   808-827`). Lumen's registration is `main.go:125-135`. The implemented
   service stops run as **MediaService → LibraryService → background**.
   System/Search/People services have no separate shutdown hook here.
3. MediaService waits for its view flusher and attempts its final write.
   LibraryService rejects new organise work, cancels organiser lifetime and
   waits for its tracked run/recovery goroutines.
4. Background cancels its independently owned context, closes the streamer,
   waits for the server/cache-sweep workers, calls indexer `Stop`, closes the
   search engine, then destroys the AI environment (`main.go:276-314`).
   **Streamer closes before indexer Stop in this source.**
5. `app.Run()` returns to `run()`, whose database defer executes
   (`main.go:106-110,152`). Store closes reader/writer handles; report
   teardown follows through the earlier defer at `100-103`. Process exit
   finally releases loaded executable/DLL mappings and remaining handles.

Explicit `App.Quit` additionally takes Wails' cleanup path:
`Wails/pkg/application/application.go:920-972` and
`application_windows.go:196-204`. It cancels context, runs shutdown hooks
and service stops, closes windows, invokes single-instance cleanup and
posts quit before `main.go`'s database defer. A returned Quit request or
released window/instance lock therefore does not establish database close
or process exit. The helper must wait on a stable parent process handle.

| Owner | Current stop behaviour | Gap for update activation |
| --- | --- | --- |
| Frontend settings and bridge calls | `frontend/src/settings/SettingsView.vue:162-179` debounces confidence saves and flushes on unmount without awaiting persistence. Wails call contexts detach cancellation at `Wails/pkg/application/messageprocessor_call.go:60`; window call cancellation at `messageprocessor.go:209-227` cancels/removes records, without waiting for completion. | Add a pre-close save acknowledgement and stop admission of new mutations. Cancellation alone is not draining. No shared update-ready barrier currently covers all bound calls. |
| MediaService view flusher | `services/media.go:49-58`; `services/views.go:48-62`: cancel timer, wait for an ongoing periodic write, attempt final flush with a five-second context. Stop returns no flush error. | Distinguish optional thumbnail-view loss from required preferences/journal durability. Make activation readiness explicit; a successful service return does not prove the flush succeeded. |
| Organiser/Undo/recovery | `services/organise.go:60-83,119-127`: close admission under mutex, cancel lifetime, `wg.Wait`. Run and startup recovery are tracked; indexer also invokes recovery before scans. | Waiting exists. Add a bounded preflight checkpoint/status contract before irreversible shutdown so lengthy work can show “Waiting for active work” and leave the app usable. Do not force termination. |
| File-operation journal | `internal/fileops/runner.go:16-25,165-192`: journal `moving` before disk move, then settle with `context.WithoutCancel`; Undo uses the corresponding `undoing` path at `284-340`; recovery reads actual disk state at `342-435`. | Preserve journals in database backup and compatibility policy. A current file/sidecar commit must finish or remain durably recoverable before activation. Database restore must never rewind a disk operation already performed. |
| Recycle Bin deletions, duplicate deletion and other synchronous mutations | `services/library.go:65-68`, `services/trash.go:74-89,102-145`, `services/duplicates.go:23-29`: recycling mutex serialises calls; post-shell database settlement ignores cancellation. | LibraryService's organiser wait group does not cover these calls. There is no explicit Stop/wait admission barrier for them. Drain shell outcomes and their database settlement, plus other admitted writes/exports, before backup and store close. |
| Streamer and playback conversions | `services/stream.go:88-99`: set closing, cancel base context, close HTTP/listener, `active.Wait`. Background then waits for `Streamer.Run`. | Waiting exists. Propagate closure failure to the update coordinator; currently background logs it and returns nil. Check all child FFmpeg processes and any native WebView2 playback resource lifetime in installed tests. |
| Background cache worker | `main.go:290-294,301-309`: cancellation plus worker wait group for frame-cache sweep and server. | Waiting exists without an update-specific timeout/status. Ensure any later cache/render owners join the same barrier. |
| Indexer, watchers, stages and XMP | `internal/index/indexer.go:443-461`: cancel, stop watchers under mutex, wait for stage/manual work and watcher groups, release image/face/OCR sessions. Manual XMP registers with this wait group at `internal/index/xmp.go:252-265`. | Waiting exists. Preflight must stop new work and settle current batch/XMP commits; verify native/model calls can finish within the stated bound. Stop itself has no deadline or readiness result. |
| FFmpeg/probe subprocesses | `internal/media/video.go:227-232` uses context-bound `cmd.Run`; `internal/media/frames.go:172-192` starts then waits for conversion. Indexer/stream owner waits cover those paths. | Verify native child exit and DLL locks; inventory any new viewer/export subprocess owner before adding it. A helper cannot rely on cancellation without its owning wait. |
| Search loader and text inference | `internal/search/engine.go:222-235` stops loader/retries, waits on `stopped`, then locks text access and closes the text model. Background closes it after indexer stops adding embeddings. | Waiting exists for loader/text use. It does not replace draining all synchronous UI service calls that may still touch store/search resources. |
| AI and native DLLs | Indexer releases sessions before `ai.Shutdown`, which calls `ort.DestroyEnvironment` and logs failure (`internal/ai/runtime.go:53-61`). libvips is dynamically linked (`internal/media/vips/vips.go:5-11`). | Model teardown is not proof that the OS has released DLL mappings. Wait for the parent and every process using the installed runtime to exit; helper must not load those DLLs itself. |
| SQLite/store | `internal/store/store.go:120-125` closes readers, runs best-effort `PRAGMA optimize`, and returns writer-close error. `main.go:110` ignores the deferred result. No store-wide write-admission or backup API was found. | Require successful draining, consistent backup/integrity validation and an acknowledged close. Do not infer a checked WAL checkpoint from the Close comment: no explicit `wal_checkpoint` result is inspected. |
| Single instance and installation lease | `main.go:165-174` uses one ID per library. Wails creates a named Windows mutex and cleanup target window (`Wails/pkg/application/single_instance_windows.go:34-64`). | Different libraries can run the same installed EXE concurrently. Add an installation-wide activation lease and prevent starts during activation, independent of library identity. Wails currently discards the `CreateMutex` handle at line 45; `release` closes only a stored non-zero handle. Rely on process exit, not that cleanup call, for the actual mutex lifetime. |
| Reports/logging and future workers | Report teardown is deferred at `main.go:100-103`; logging deliberately stays open until process exit (`main.go:343-346`). Compression and new render workers are not current updater owners. | Keep diagnostics/library files outside replacement. Register every future worker and durable file-operation owner with the checkpoint barrier before shipping updates alongside it. |

**Inference.** Wails' helper reuses Lumen.exe and enters helper mode only
when `application.New` is reached. Lumen executes path setup and Windows
priority/own-process setup first (`main.go:54-70`). More importantly, its
libvips import library loads an installed DLL before application code runs.
The helper can therefore keep a DLL mapped while trying to replace the
whole runtime, even after its parent exits. This needs native confirmation;
it rules out assuming that the stock re-executed helper is sufficient.

**Required contract.** The activation worker must execute outside the
replaceable runtime and must not depend on its DLLs. Verify its identity,
hidden spawn/readiness and job survival before initiating normal shutdown.
Capture the validated library path and installation identity; relaunch must
preserve `LUMEN_DATA_DIR` as an absolute value without inheriting stale
runtime overrides or blocked-media unlocks. Existing Wails relaunch uses
`exec.Command(path)` with inherited environment and no application arguments
(`Wails/pkg/updater/helper.go:92-103`); that is not a new explicit Lumen
relaunch contract. Parent waiting polls a reusable PID by reopening it
(`helper_windows.go:40-50`), so use a held process handle/identity instead.

Before shutdown, stop new writes and wait for settings, journal commits,
shell operations and worker checkpoints with a bounded status the UI can
explain. If the checkpoint cannot be reached, do not enter file replacement
or kill the application. After shutdown, the helper still needs bounded
sharing/antivirus retries and a recoverable failure route. This contract
uses the existing waits without equating them to a complete update barrier.

## Recovery and schema

**Verified.** `internal/store/store.go:78-103` migrates on open, before
background services start. Migrations are embedded, ordered forward-only
SQL files; each file and its `PRAGMA user_version` update commit together
at `135-176`. The highest file currently present is
`internal/store/migrations/0027_xmp.sql`; this is a repository schema
ceiling, not a measurement of the user's database.

There are no down migrations or a newer-schema refusal in that migration
loop. An older binary can skip every migration if the database is already
newer than its files, then attempt queries against it. This is not a
compatibility guarantee. Several migrations can commit before a later
one fails, because the transaction scope is per file, not the whole set.
The runtime's stage-version requeue and search vocabulary loading can
also write after startup (`internal/index/indexer.go:339-358` and
`internal/search/engine.go:238-255`).

The database uses WAL (`internal/store/store.go:84-100`). A read-only search
was run with this exact command:

```powershell
rg -n 'Backup|backup|VACUUM INTO|sqlite.*Backup' internal/store -g '*.go' -g '!**/*test.go'
```

```text
Store backup search exit: 1
```

Inspection of Store confirms no production backup method. The update plan's
“existing store's backup mechanism” at
`docs/history/automatic-updates-plan-2026-10-04.md:237-249` is not implemented in
this checkout and must be supplied by Phase 12 under store ownership.
Copying a live `library.db` alone ignores possible WAL contents. Close
currently performs no explicit checked WAL checkpoint and its returned
error is ignored by main.

**Required recovery policy.** Signed release metadata must declare schema
read/write compatibility and the supported previous-runtime recovery
range. Stop writes and file operations, obtain and integrity-check a
consistent SQLite backup containing preferences, sources, annotations,
blocks and organise journals, and record it alongside the authenticated
previous runtime identity. Backup failure prevents activation. Preserve
WAL/SHM handling through a tested store-owned mechanism, not arbitrary
filesystem copying by the activation helper.

First start after an update needs a transaction-bound health mode. Prevent
indexing, journal recovery that mutates disk, user writes and ordinary UI
interaction until migration, database checks, required DLL/model probes,
frontend readiness and intended version all pass. Current `run()` starts
normal services immediately and degrades gracefully when models cannot
start (`main.go:121-152,218-243`), so an About version or process launch is
insufficient evidence of update health.

If health fails before new work is released, restore the matched previous
runtime/database pair when compatible. After a healthy upgrade has produced
new annotations, preferences or file operations, never restore the old
backup automatically over those writes. Use a runtime proven compatible
with the current schema or a visible repair path. Retain recovery material
through a defined grace period. Organiser journals describe real disk
moves; old metadata cannot safely undo those moves by merely restoring a
database. This is why the gate must precede job recovery as well as indexing.

## Decisions the contracts settle now

1. Phase 7A produces evidence only. Lumen remains offline; the planned
   exception, update policy and controls belong to Phase 12.
2. Discovery and requested preparation are separate. Any framework timer
   stays disabled; an opt-in check-only scheduler and the shared application
   UI own user intent. A staged release never authorises restart by itself.
3. The release unit is the complete coherent runtime with matching notices
   and authenticated inventory. A single EXE update and passing Setup.exe
   to Wails' executable-swap API are incompatible with this product.
4. Retain per-user NSIS, stable launch path, custom install location, HKCU
   registration and user data outside replacement. The initial updater-capable
   release arrives by ordinary over-install without manual uninstall.
5. Choose one whole-runtime activation strategy in 12A: an independent signed
   helper with an owned-path transaction, or hardened NSIS providing the same
   backup/journal/health recovery. Current `/S` alone is not that strategy.
6. Updater preferences, accepted trust history and activation journal have
   one application-scoped owner outside the runtime and every library.
   Library settings remain with the library; WebView2 data and originals
   remain outside the runtime transaction.
7. Pin release trust in the bootstrap and require authenticated metadata,
   payload inventory and Windows publisher verification. Unsigned or
   checksum-only fallback is not an update capability.
8. Obtain installation-wide exclusion and checkpoints before shutdown;
   wait for actual process/child exit before replacement. Reuse waiting
   Stops, add missing synchronous-call draining, and isolate the helper from
   installed DLLs. Do not kill active file transactions to meet a deadline.
9. Runtime recovery and schema recovery form one compatibility decision.
   Build a store backup API and health gate; do not advertise unconditional
   rollback or treat successful process creation as health acceptance.

## Blockers and prerequisites for Phase 12

| Kind | Prerequisite and acceptance evidence |
| --- | --- |
| External trust | Production certificate/signing access, timestamping, approved publisher renewal/revocation policy, protected metadata keys and embedded root. Require two genuinely signed packaged releases; throwaway self-signed testing does not meet this gate. |
| External delivery | Controlled release origins, immutable payloads, signed metadata publication/expiry refresh, operational key custody and authorised publication pipeline. No live host/feed was contacted or verified here. |
| Installed fixtures | Prior `0.9.x` installations, custom/Unicode paths, missing/legacy inventory, two libraries using one installation, older supported Windows and clean offline Windows/WebView2 fixtures. Existing local setup files do not establish those installations. |
| Implementation choice | Select and prove helper transaction or hardened NSIS. Include DLL/model changes, uninstaller and registry consistency, stale owned files, unrelated-file preservation, install-volume staging and a runtime-independent recovery entry point. |
| Lifecycle gaps | Settings acknowledgement, synchronous mutation/shell-operation admission and draining, installation lease, bounded checkpoint reporting, close-error handling, process-handle wait, future worker registration. Existing waiting Stops are reusable evidence, not complete activation acceptance. |
| Schema/recovery gaps | Store-owned consistent backup and integrity validation, supported schema ranges/newer-schema refusal policy, first-start health gate and recovery retention. Test partially committed migration sets and forbid restore over post-health user work. |
| Native failure experiments | Inject interruption during backup/apply/migration/health; locked DLLs, antivirus, process/job locks, permission/space failures and concurrent starts. Prove recovery leaves one coherent runtime, preserves data and never silently loses accepted work. |
| Trust fixtures and later delivery tests | Offline malformed/missing/expired/replayed/revoked metadata, altered inventory, wrong product/channel/architecture, rotation and withdrawal fixtures first; then approved live TLS/proxy/redirect/revocation and installed upgrade tests. Local source inspection cannot establish these behaviours. |

The minimum release proof is a signed previous/current pair exercised through
bootstrap over-install, requested staging, safe activation, healthy restart,
compatible recovery and uninstall on native installed fixtures. Phase 12
can follow shell integration without waiting for compression, but every
worker shipped by then must satisfy the same checkpoint contract
(`docs/history/next-development-plan-2026-10-04.md:379-400,406-466`;
`docs/history/automatic-updates-plan-2026-10-04.md:251-288`).
