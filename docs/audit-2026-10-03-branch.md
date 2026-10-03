# Branch review — 3 October 2026

Reviewed `phase6/ship-v1` at `ad65be2` against `main` at `628c907`:
three commits, 83 changed files. The working tree and index were clean when
the review began. The repairs below remain local and unstaged.

The review covered the complete branch diff and traced its changed behaviour
through startup, indexing and background workers, persisted settings, the
Wails services and generated bindings, Vue stores and views, diagnostics,
dependency preparation, notices, build assets and installer packaging.
Existing store visibility, stage scheduling, model release, error codes,
bridge ownership and settings-save queues remain the shared architecture.

## Findings and repairs

| Priority | Finding                                                                                                                                                                                                                                                          | Repair and regression coverage                                                                                                                                                                                                                                                                                                                                         |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1       | Saving a problem report truncated the chosen destination immediately; a later failure removed it. An existing file could be lost.                                                                                                                                | Write to an exclusive temporary file beside the destination, complete and sync it, close it, check cancellation, then rename. Tests cover successful replacement, failed saves, cancellation and temporary-file cleanup. Report compression observes cancellation between reads and captures the opening size of an active log.                                        |
| P1       | Installer upgrade and uninstall deleted every DLL and recursively removed `models` and `ffmpeg`, including unrelated files in a chosen folder.                                                                                                                   | Generate extraction instructions and an installed inventory from the same payload list. Cleanup removes recorded files and empty directories only. It rejects wildcard paths, paths outside the install folder and junctions or symbolic links. Four executable NSIS regressions cover unrelated files, obsolete payload files, invalid inventory paths and junctions. |
| P2       | Starting crash capture twice touched the active pending file before refusing the call. A failure to preserve a previous crash still proceeded to truncate it. Old stop callbacks could disarm a later run. Panics at the same millisecond overwrote one another. | Serialise the complete capture lifecycle under its existing mutex, reject a second start before touching files, stop startup when preservation fails, make stop idempotent and exclusively create panic report files. Tests also preserve incomplete crash headers.                                                                                                    |
| P2       | Failed CPU samples retained a busy hold indefinitely. Falling own-process counters could contaminate the averaging window.                                                                                                                                       | Discard unavailable or reset samples and their old window, releasing stale holds. Regression tests verify both cases.                                                                                                                                                                                                                                                  |
| P2       | `QUNS_APP` was treated as proof of full screen, although it reports a running Store app.                                                                                                                                                                         | Also check the foreground window against its monitor bounds. Tests cover windowed, maximised and full-screen windows, including negative monitor coordinates. See [Microsoft's enumeration contract](https://learn.microsoft.com/en-us/windows/win32/api/shellapi/ne-shellapi-query_user_notification_state).                                                          |
| P2       | Welcome used locale collation to deduplicate folders, which can equate distinct names that the library keeps separate.                                                                                                                                           | Use `LibraryService.SourcePathKey`, backed by the existing `core.Fold` and path cleaning, through regenerated bindings and the bridge. Backend and UI regressions cover case variants and distinct composed/decomposed names.                                                                                                                                          |
| P2       | Overlapping problem-report requests could open multiple dialogs and clear their shared busy flag early. About read failures were invisible. A synchronous logging failure could escape into the global error handler.                                            | Coalesce saves, expose About load errors, and absorb both synchronous and asynchronous failures in the bridge's logging function. Add frontend regressions for each path.                                                                                                                                                                                              |
| P2       | A second instance opened the shared log before single-instance handover, risking contention with the first instance's rotation.                                                                                                                                  | Open the log only after `application.New` has completed handover.                                                                                                                                                                                                                                                                                                      |
| P3       | The diagnostic description omitted the design's library schema version. Notices split `GOROOT` on spaces. Documentation described old diagnostic APIs and overstated scale acceptance.                                                                           | Read schema version through the store, include it in the report, split Go environment output by lines, align the design with the implementation and label the 100,000-photo estimates as extrapolations.                                                                                                                                                               |

The installer failure path for an unavailable WebView2 runtime now aborts
before the finish page can offer to start an unusable app. Payload extraction
and inventory-write failures also abort with an explicit error.

The inventory is the cleanup authority; an older experimental installation
without one retains untracked runtime files. Cleanup follows the
[NSIS guidance on preserving unrelated files](https://nsis.sourceforge.io/Docs/Chapter4.html#4.9.1.9).

## Verification

All results below were obtained during this review on the repaired tree:

- `scripts/lint.sh`: zero Go lint issues; deadcode and govulncheck passed;
  Vue type checking, ESLint, Prettier, Ruff and ShellCheck passed;
  **355 frontend tests across 34 files** and **four NSIS regressions** passed.
- `go test -race -count=1 -timeout 900s . ./internal/... ./services/...`:
  all named packages passed.
- Regenerated production Wails bindings and compared every tracked binding's
  SHA-256 before and after regeneration: **no drift**. The new folder-key
  binding was generated from Go.
- `wails3 build`: Windows production build passed.
- `wails3 task package`: full installer build passed;
  `bin/Lumen-0.9.0-x64-setup.exe`, 872,435,073 bytes, unsigned as configured.
  Notice generation included 14 Go modules and nine bundled UI packages.
- `npm --prefix frontend audit --omit=dev`: **zero vulnerabilities**.
- `git diff --check`: passed. Dependency manifests and lockfiles are unchanged.

Native WebView2 smoke testing used the rebuilt production executable and a
fresh scratch library with two repository photo fixtures. Verified:

- planted previous-crash recovery, notice display and dismissal;
- Unicode folder keys across the actual Wails transport;
- About showing version `0.9.0`;
- background-work defaults and persistence through the actual UI switches;
- indexing resuming after a pause and both thumbnails decoding in the grid;
- the global UI error notice and its entry in the native log;
- clean shutdown removing the pending crash output;
- no unexpected page errors.

Local smoke evidence is under `bin/review-native-pkRidU/`, including
`library.png`, `settings.png` and the scratch library's log. The harness is
`bin/review-native.mjs`. No tester's or personal photo library was indexed.

## Remaining verification limits

Full `npm audit` reports four high-severity dependency entries from one
upstream advisory: `braces` through `micromatch`, `fast-glob` and
`@vue/eslint-config-typescript`. It affects development tooling; production
dependencies audit cleanly. The current installed/latest `braces` is 3.0.3,
and [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
lists no patched release. This remains an upstream tooling finding; checks
and dependency pins were preserved.

This review does not establish 100,000-photo throughput, low-end physical
hardware behaviour, every GPU/codec combination, real-person face accuracy,
removable-drive recovery, DPI/accessibility acceptance across devices, or a
clean Windows machine's offline WebView2 installation. The complete installer
was compiled; its cleanup macros were executed in isolated fixtures. A full
installation, library-removal uninstall and real-certificate signing were not
rerun on this machine.

The branch's earlier 3,000-photo performance record remains historical
evidence. Its large-library estimates are now explicitly labelled as such.
No changes were committed, pushed, merged or published.
