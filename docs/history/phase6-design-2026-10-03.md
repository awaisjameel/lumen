# Phase 6: design — 3 October 2026

Phase 6 polishes Lumen and ships v1 (plan §7): the installer, onboarding and
empty states, error handling and local crash logs, a performance pass against
the plan's targets including the politeness controls deferred from Phase 5
(`AUDIT_REPORT.md` §5, decision 1), and what a beta with real users' libraries
needs. This note says how each part is built and tested. It binds the work as
`CONVENTIONS.md` does; the plan's §1, §6 and §9 are the source of truth
for behaviour.

## Decisions

Later decision, 4 October 2026: the owner requested automatic update checks
and installation. [The Phase 12 update plan](automatic-updates-plan-2026-10-04.md)
supersedes decision 1 for future update-only network activity, with opt-in
checks and a user-requested download/restart. The table below records the
Phase 6 baseline; the current application has no updater implementation.

The product owner settled the plan's open questions on 3 October 2026:

| #   | Question                                                    | Decision                                                                                                                                          |
| --- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Updates (plan §7: "auto-update, to decide: must be opt-in") | **No network at all.** Lumen never makes a request. Settings shows the version and the releases page; the user installs a new version themselves. |
| 2   | Install location                                            | **Per user**: `%LOCALAPPDATA%\Programs\Lumen`, no administrator prompt.                                                                           |
| 3   | WebView2 runtime on a PC without it                         | **Bundle Microsoft's offline standalone runtime installer** (~210 MB), so installing never needs the internet either.                             |
| 4   | Politeness defaults                                         | **Pause for full screen or games: on. Pause when the PC is busy: on.** "Index only when idle" is an Advanced setting, off by default (plan §6).   |

Code signing needs a certificate, which does not exist yet (plan §8, risks).
The build signs when one is configured and says so when it is not; nothing
else waits on it.

## P1 — Politeness: full screen, busy PC, idle only

Plan §6: "pause or throttle when on battery, when the user is gaming or in
full screen, or when CPU is busy. Advanced setting: index only when idle."
Battery and the manual pause exist. The three new holds join them in the
indexer's one hold check, so every stage, its model release and the status
already follow them.

**What the indexer reads about the PC.** `internal/index` gets one sampler
interface, consumed by one loop that replaces `powerLoop`:

```go
// pc is what the indexer reads about the computer to give way to its user.
type pc interface {
	onBattery() bool
	fullScreen() bool               // a game, full-screen video or presentation is in front, not Lumen
	sinceInput() time.Duration      // since the user last used the keyboard or mouse
	cpuTimes() (cpuTimes, bool)     // cumulative: the machine's busy and total time, and Lumen's own
}
```

The Windows sampler (`pc_windows.go`) uses:

- `GetSystemPowerStatus` (moved from `power_windows.go`).
- `SHQueryUserNotificationState`: busy, Direct3D full screen, presentation
  mode or a full-screen Store app counts as full screen, unless the
  foreground window is Lumen's own (`GetForegroundWindow`'s process), so
  Lumen's viewer never pauses Lumen.
- `GetLastInputInfo` against `GetTickCount64`.
- `GetSystemTimes` for the machine, and for Lumen a Windows job object the
  process puts itself in once (`sync.OnceValue`; process-wide by nature, as
  the GPU lock is). A job's accounting includes children that have already
  exited: FFmpeg and WebView2, so Lumen's own short FFmpeg runs never count
  as "other programs". Without a job (assignment refused) it falls back to
  the process's own times.

`pc_other.go` reports mains power, no full screen, no input and no load.

**Rules** (`politeness.go`, pure, unit-tested with a fake `pc`):

- Poll every 5 s (`pcPoll`); battery used to be read every 20 s and is
  as cheap as the rest.
- _Busy_: other programs' share of all CPU time, averaged over the last 30 s
  (6 samples), is at least 70 %; it stays busy until that average falls to
  50 % or less. The hysteresis stops it flapping at the line.
- _Away_: no input for 5 minutes (`awayAfter`).
- The indexer keeps what it observed (`fullScreen`, `busy`, `away`) and the
  three settings, and derives one reason, in this order: full screen, busy,
  then idle-only-while-not-away. `held(stage)` becomes
  `paused || waiting() != WaitNone || (AI && onBattery && !aiOnBattery)`.
- Scanning and folder watching go on, as they do while paused (cheap).
  Playback conversion is foreground work and never waits.

**Settings** (`core`): `SettingPauseFullScreen` (default on),
`SettingPauseBusy` (default on), `SettingIdleOnly` (default off), with
`Settings.PauseFullScreen`, `PauseBusy`, `IdleOnly`, and
`LibraryService.SetPauseFullScreen/SetPauseBusy/SetIdleOnly`, each stored and
then applied at once through the indexer's `setHold`.

**Progress** gets `Waiting core.WaitReason` (`""`, `full_screen`, `busy`,
`idle`). The sidebar status names it ("Waiting while a full-screen app is
open", "Waiting while your PC is busy", "Waiting until you're away"), and the
Settings → Background work section gets the three switches (idle-only under
Advanced).

**Tests:** the rules over a fake `pc` (each hold, the order, hysteresis, a
setting turning a hold off releases waiting workers at once); a Windows test
that the sampler's job accounting counts a child that already exited;
frontend tests for the status line and the switches.

## P2 — Error handling, crash reports and diagnostics

Plan §7: "error handling, crash logs (local only)". Today Go errors reach
`lumen.log`, but a crash (an unrecovered panic, a fatal runtime error) goes
to stderr, which a GUI build does not have, and the UI's own failures go to
the WebView console, which nobody sees.

**`internal/diag`** (new package: crash reports, the problem report, and the
PC's description both use):

- `New(dataDir, version)` describes `DataDir\crashes`; `Start(now)` opens it. A non-empty
  `pending.txt` left by the last run is kept as `crash-<time>.txt` and
  remembered as "the last run crashed"; an empty one (a clean exit, power cut
  or Task Manager) is removed.
- `Start(now)` creates `pending.txt` with a header (version, start time,
  Windows version) and hands it to `debug.SetCrashOutput`; the returned
  disarm, run when `run` returns, stops that and removes the file. It is armed
  after `application.New`, so a second Lumen, which hands over to the first
  and exits inside `New`, never truncates the first one's file.
- `RecordPanic(err, stack, now)` writes `panic-<time>-<suffix>.txt` for a panic Wails
  recovered in a bound method (`application.Options.PanicHandler`); the call
  fails with Wails' runtime error and Lumen keeps running. Without the
  handler, Wails' default exits the app.
- Reports are pruned to the newest 20.
- `WriteReport(ctx, w, info)` writes a zip: `lumen.log`, `lumen.old.log`, the
  crash reports, and `system.txt` (version, Windows version, CPU, threads,
  memory, graphics adapters, the AI device, library counts, schema version,
  settings). It holds no thumbnails or library data; the logs do name files
  and folders, which the UI says before saving.
- `System()` describes the PC (`system_windows.go`: `RtlGetVersion`, the
  registry's CPU and display adapter names, `GlobalMemoryStatusEx`).

**`services.SystemService`** (registered through `service()` like the rest):

- `About() core.About`: version, data folder, releases page, and whether
  the last run crashed. The version is Lumen.exe's own version resource as
  Explorer shows it: its ProductVersion string, found through the resource's
  translation table (Wails writes the strings language neutral and leaves
  the fixed product version at zero), else the fixed file version; "dev" for
  a build without one.
- `OpenLogFolder()`: Explorer with `lumen.log` selected.
- `SaveProblemReport() (string, error)`: a save dialog, then the zip; ""
  when cancelled.
- `LogUIError(core.UIError)`: writes the UI's failure to `lumen.log`,
  truncated and rate-limited (30 a minute, then one "dropped" line).

**The UI:**

- `shared/errors.ts`' `logError` also sends the failure to the engine's log,
  through the bridge's `logUIError` (never itself logged on failure, so it
  cannot loop).
- `main.ts` installs `app.config.errorHandler`, and listeners for `error`
  and `unhandledrejection`. Each logs, and shows the app-wide notice ("Something
  went wrong. Lumen saved the details in its log.").
- App shows a one-time banner after a crash: "Lumen closed unexpectedly last
  time. A report was saved on this PC." with "Save a problem report" and
  "Dismiss".
- Settings → **About Lumen**: version, the releases page (opened in the
  browser only when clicked; Lumen itself makes no request), "Open log
  folder", "Save a problem report…", and the GeoNames attribution CC BY 4.0
  asks for.

**Tests:** diag's crash file life cycle (a pending file becomes a report, an
empty one is removed, arming twice is refused, pruning), the zip's contents,
the UI-error rate limit, the panic handler path (`TestServiceErrorsCarryTheirCodes`
extended to the new service), bridge contract lines, frontend tests for the
error hooks, the banner and the About section.

## P3 — The installer

Plan §7: "Installer (NSIS via the Wails build tasks), code signing
certificate, auto-update". README: "`task package` is not usable yet: the
installer does not carry `deps/runtime`."

**Tools and inputs, from `scripts/deps.sh`** (pinned and checked like
everything else there):

- NSIS 3.13 (`deps/tools/nsis`), a build tool that never ships.
- WebView2's standalone x64 runtime installer (`deps/installer/`), at its
  pinned SHA-256 (Microsoft's link moves with each release; the pin is the
  CDN file it resolved to on 3 October 2026, and the script says how to
  re-pin).
- The licence texts of every bundled component, copied from their archives
  into `deps/licenses/`. `scripts/notices.py` gathers them, with the Go
  modules linked into Lumen.exe and the packages Vite bundled into the UI
  (its `build.license` list), into `THIRD_PARTY_NOTICES.txt`, which ships
  beside Lumen.exe and opens from Settings → About Lumen.

**Version.** `build/config.yml`'s `info.version` is the one source. It is
`0.9.0` for the beta. `wails3 task common:update:build-assets` carries it
into `build/windows/info.json` (the exe's version resource) and
`wails_tools.nsh` (the installer's); `TestVersionsAgree` in `main_test.go`
fails if they drift.

**`build/windows/nsis/project.nsi`**, Lumen's own:

- Per-user scope (decision 2), x64 only (the bundled DLLs are x64),
  Windows 10 or later.
- **zlib, not LZMA.** Model weights are most of the payload and neither
  packs them (zlib keeps 92 % of them). Measured on the first build: LZMA
  made an 840 MB installer that took 27 minutes to build and 4 minutes to
  install, because Setup's LZMA decoder runs at about 4 MB/s; zlib makes an
  872 MB one that builds in 5 minutes and installs in under a minute.
- Pages: welcome, folder, install, finish ("Start Lumen" and "Add a desktop
  shortcut", unticked).
- Files: `Lumen.exe`, everything in `deps/runtime` (DLLs, `ffmpeg\`,
  `models\`) and `THIRD_PARTY_NOTICES.txt`. The payload macro generated by
  `scripts/installer_payload.py` extracts the runtime files and records them
  in `.lumen-runtime-files.txt`. Upgrades and uninstall remove only those
  recorded files, then empty runtime directories. Unrelated files stay.
  An installation without an inventory keeps untracked runtime files.
- Lumen must not be running while it is replaced or removed: the installer
  and uninstaller check that `Lumen.exe` can be opened for writing and
  otherwise ask the user to close it (Retry/Cancel; silent mode fails).
- WebView2 (decision 3): if neither the machine nor the user has it
  (`pv` empty or `0.0.0.0` under EdgeUpdate's client key), the bundled
  standalone installer runs silently, per user. A failure stops the install
  with a message.
- Start menu shortcut; Add/Remove Programs entry under HKCU with version,
  publisher, size, icon and the project page.
- The uninstaller removes the program and its shortcuts. A checkbox, off by
  default, also removes the library: `%LOCALAPPDATA%\Lumen` (database,
  thumbnails, logs) and WebView2's data (`%APPDATA%\Lumen.exe`). Photos are
  never touched, and the page says so. A silent uninstall keeps the library.
- Signing: when `SIGN_THUMBPRINT` names a code-signing certificate in the
  Windows certificate store (code-signing certificates now live on hardware
  tokens or signing services, which appear there), the Windows SDK's
  `signtool` signs `Lumen.exe` before packing, and `!finalize` /
  `!uninstfinalize` run it on the installer and the uninstaller.
  (`wails3 tool sign` cannot: it refuses NSIS's `.tmp` uninstaller.)
  Without a thumbprint, the build says the installer is unsigned. Checked
  with a throwaway self-signed certificate.

**`task package`** builds the app, the notices and the installer
(`bin\Lumen-0.9.0-x64-setup.exe`) with the pinned NSIS.

**Not in v1, with reasons recorded in the README:** trimming FFmpeg (it needs
a from-source LGPL build of FFmpeg; the stock build compresses well inside
the installer), MSIX (Wails' MSIX path cannot carry the runtime folder layout
without a manifest rewrite, and NSIS is the plan's choice).

**Tests:** `TestVersionsAgree`; building the installer; a silent install to a
scratch folder, starting the installed Lumen on a scratch library (its
DevTools port), and a silent uninstall that leaves the library in place.
The "remove my library" path is checked by reading only: run on a
developer's PC it would remove their real library.

## P4 — Onboarding and empty states

The first run (plan §2) and every view with nothing to show were reviewed in
the real app on fresh scratch libraries, during the first scan and after it.
Every view already had an empty state (`shared/EmptyState`), and Things and
People say when the AI is unavailable. Fixed:

- While the first scan runs, the empty grid said "Looking for photos…" with
  the hint for a library that has none ("Add a folder in Settings…"); it now
  says photos appear as Lumen finds them.
- The Welcome screen now uses `LibraryService.SourcePathKey`, which applies
  the library's existing `core.Fold` and path cleaning. Case variants such as
  "ÉTÉ" and "été" match, while distinct composed and decomposed names stay
  separate. Locale collation must not decide which folders are shown.
  Welcome had no tests; it has now.
- Failures nothing caught now tell the user and reach the log (P2).

## P5 — Performance pass

The targets (plan §6) are measured on this laptop (i7-13700H, 20 threads,
Intel UHD) and on a low-end profile of it: Lumen limited to 4 threads by
affinity and the AI on the CPU. Measured: cold start to a usable grid, search
latency, scroll, idle memory after indexing, and the politeness holds (a
full-screen app and a CPU load pause indexing, and it resumes). Results go in
`docs/history/performance-2026-10-03.md`; regressions found are fixed with tests.

## P6 — Beta

`docs/beta-testing.md` is for testers: install, what to try, what to watch,
where the log and crash reports are, and how to save a problem report. The
README's status, commands and layout are brought up to date.

## Verification (3 October 2026)

- Gates: `scripts/lint.sh` (golangci-lint 0 issues, deadcode, govulncheck,
  vue-tsc, ESLint, Prettier, 351 Vitest tests, Ruff, ShellCheck) and the Go
  race suite on every package.
- Installer, in `<scratch>\phase6\accept`: silent install (56 s), files,
  version (as Explorer shows it), Add/Remove Programs entry, Start menu
  shortcut; the installed Lumen on a scratch library (Welcome, a folder
  added, the grid, About showing 0.9.0, the background-work defaults, a UI
  failure reaching `lumen.log` and the notice); uninstall refused while Lumen
  runs; a silent uninstall removing the program, its entry and shortcut and
  keeping the library; signing with a throwaway certificate (Lumen.exe, the
  installer and the uninstaller all signed).
- Politeness in the real app: a CPU load from another program and a
  full-screen window both held indexing and released it (see the
  performance note); a planted crash showed the crash notice once and was
  kept as a report.
- Performance: [performance-2026-10-03.md](performance-2026-10-03.md).
