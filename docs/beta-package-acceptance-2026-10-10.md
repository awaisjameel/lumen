# Beta 0.11.0: packaged acceptance

Date: 10 October 2026. Source baseline: `main` at
`2e68787d7cdb6e3025a3328bf948af34403475a7`, including the
[Phase 11 audit repairs](phase11-audit-2026-10-10.md). The beta guide now names
0.11.0 and explains upgrades, RAW, compression, restoration and current limits.

## Package and local gates

`wails3 task package` rebuilt the executable, notices, payload inventory and
unsigned installer. Frontend production output completed before Go embedded it.
The first attempt overlapped the task's `npm ci` with running frontend tests,
causing a Windows dependency-file lock. The package task then ran sequentially
and the complete interrupted gate was rerun successfully. Generated production
bindings have no tracked diff.

| Artifact             | Evidence                                                                                                                                                                             |
| -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Installer            | `bin/Lumen-0.11.0-x64-setup.exe`, 879,016,043 bytes                                                                                                                                  |
| SHA-256              | `5fcf71c11cf74e3353f12758b63197773e32d21508baef44477e5ce7644a161f`                                                                                                                   |
| Signing              | Unsigned; no signing certificate configured for this build                                                                                                                           |
| Runtime dependencies | 37 files, 839,046,255 bytes before the application executable/notices                                                                                                                |
| Payload verification | 41/41 checks: every runtime file, application, notices and bundled offline WebView2 installer match their build inputs; no unexpected payload files apart from known NSIS UI plugins |

7-Zip extracted the rebuilt installer into
`e2e/.work/release-2026-10-10/extracted/`. Extraction from Git Bash used
`MSYS2_ARG_CONV_EXCL='*'`: otherwise argument conversion misinterpreted the
output-directory switch and no files were extracted. The extractor's NSIS
`BadCmd=13` diagnostic does not invalidate the payload result: every expected
file was extracted and its complete SHA-256 was checked.

`e2e/installer/package.mjs` verifies that payload and runs native workflows
against the extracted `Lumen.exe`. It clears `LUMEN_RUNTIME_DIR` and removes
`deps/runtime` from the child PATH, so the app uses its shipped runtime.
Every app run has an isolated scratch library; the suites run sequentially.

| Gate                                                                        | Result                                                                                                                                                                       |
| --------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `scripts/lint.sh`                                                           | Passed: Go lint/formatting, deadcode, govulncheck, frontend type/lint/format checks, 584 Vitest tests in 56 files, Python, ShellCheck and four installer payload regressions |
| `go test -race -p 1 -count=1 -timeout 900s . ./internal/... ./services/...` | Passed in all named packages, including services; real-fixture skips retain their ordinary conditions                                                                        |
| Packaged onboarding                                                         | 7/7                                                                                                                                                                          |
| Packaged screen tour                                                        | 5/5; 68 screens at four widths, no sideways overflow or off-device request                                                                                                   |
| Packaged interface                                                          | 44/44                                                                                                                                                                        |
| Packaged collections                                                        | 8/8                                                                                                                                                                          |
| Packaged organise                                                           | 7/7                                                                                                                                                                          |
| Packaged viewer                                                             | 63/63                                                                                                                                                                        |
| Packaged file tools                                                         | 14/14                                                                                                                                                                        |
| Packaged branding                                                           | 18/18                                                                                                                                                                        |
| Packaged formats                                                            | 55/55                                                                                                                                                                        |
| Packaged customisation                                                      | 45/45                                                                                                                                                                        |
| Packaged compression                                                        | 41/41                                                                                                                                                                        |
| Total native checks                                                         | **307/307** across 11 suites                                                                                                                                                 |

The existing suite filters for expected optional Wails resource errors and
resize notices were preserved. No checks were weakened. The tools suite records
17 ResizeObserver loop notices during resizing separately from its passing
workflow/error checks.

Ignored evidence: `e2e/.work/release-2026-10-10/` contains build/lint/race/native
logs; `e2e/.work/package-0.11.0/payload.json` contains payload hashes;
`e2e/.work/results/` contains per-suite reports and screenshots. These files
can be regenerated using [the installer acceptance scripts](../e2e/installer/README.md).

## Installed acceptance attempted, still open

A reusable offline Windows Sandbox harness now lives in `e2e/installer/`.
It maps packages/scripts read-only and only its scratch results writable,
disables networking/clipboard sharing, and refuses any ordinary host session.
Its intended matrix is previous 0.10.0 install, over-install to 0.11.0,
same-version reinstall, refusal while the installed app is running, silent
uninstall keeping the library, install again and uninstall. It also checks a
custom Unicode path, installed file hashes, registry/shortcut agreement,
favourite/album/preference persistence and unchanged original/unrelated files.

The first attempt could not use CIM to query adapters in the guest, so the
harness was corrected to use .NET network-interface state. An execution then
ended with `0x80370107` (container forcefully exited). A fresh 4 GiB Sandbox
retry produced the concrete installation blocker:

```text
This command cannot be run due to the error:
An Application Control policy has blocked this file.
```

The guest confirmed a clean account, then refused the unsigned previous beta
at `Start-Process`, before installation. It wrote a failed report and shut down.
No Windows security policy was changed. The report is under
`e2e/.work/installer-b2d37bb327e14ee68f76b08f5d6e9f99/results/`.

The guest's dependency-free DevTools adapter was separately smoke-tested on
the extracted native app in a host scratch library: About/version/data identity,
nested array arguments, preference objects, album creation with mixed arguments
and an empty array, and service-error propagation passed. Calls run inside the
WebView, where Wails serves its bound methods. This validates the adapter;
it does not replace running the blocked guest installation matrix.

Therefore **clean-machine installation, installed upgrade/reinstall/uninstall
and WebView2 prerequisite installation are not accepted** by this run. The
test harness is available, but its full installed matrix remains unexecuted.
Run it in a clean environment that permits the package or with appropriately
signed packages; do not treat extracted-runtime checks as installed acceptance.

Windows 10/older-Windows testing, interactive library removal, public signing,
SmartScreen reputation and a representative camera-photo compression matrix
also remain open. The formats suite uses its pinned camera fixtures, and the
compression suite generated fixtures; neither establishes arbitrary camera or
photo-library quality. This run did not repeat performance or npm dependency
audits and does not supersede their previously recorded limitations.

## Handoff to 12A

The local packaged beta is verified within the boundaries above. The next
development slice has begun as an isolated trust experiment and activation
design, recorded in [Phase 12A](phase12a-updater-2026-10-10.md). The application
remains offline. Clean installed acceptance and signing are explicit release
gates, not completed by starting the experiment.
