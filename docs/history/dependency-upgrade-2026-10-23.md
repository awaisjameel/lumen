# Dependency upgrade, 23 October 2026

Implementation record for [issue #28](https://github.com/awaisjameel/lumen/issues/28),
starting at `44edd34`. This is local upgrade work, not release acceptance.
The [resolved inventory](dependency-inventory-2026-10-23.json) lists every entry
in both npm lockfiles, the root Go module graph, the updater experiment and the
libraries inside the Windows libvips DLL, with previous and selected versions.
Removed entries remain in the inventory so dependency removal is reviewable.

## Selection and scope

Upstream registries and release notes were checked on 22 October UTC / 23 October
in Pakistan. npm installation and resolution retain `min-release-age=7` and
`engine-strict=true`. The actual lockfiles, rather than an unfiltered `latest`
tag, determine the installed versions. Transitive dependencies remain within
their parents' compatibility ranges; an older transitive major is not a reason
to override an upstream API contract.

The root application, its launcher/update/release commands, frontend, e2e tools,
native build scripts and CI are maintained. `spikes/updater` remains a runnable,
development-only trust experiment and follows the production TUF/Sigstore
dependencies. `spikes/go.mod` records the original model/codec experiments; only
its Go directive and inherited requirements are aligned with the replaced root
module. Experimental SQLite-vector/model API choices remain historical and are
not shipped. The Android, iOS, macOS, Linux and Docker build
templates are not supported release targets; their presence does not establish
that they build or ship.

## Application packages

| Component                                   | Before                     | Selected                   | Migration and upstream evidence                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------------------------------------------- | -------------------------- | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Wails Go module, CLI and JavaScript runtime | beta.26                    | beta.30                    | Keep all three together. [beta.28](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.28) and [beta.29](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.29) repair Windows WebView recovery, callback ownership and reference counting. [beta.30](https://github.com/wailsapp/wails/releases/tag/v3.0.0-beta.30) repairs Windows menus, Explorer metadata and dispatch failures. Regenerate bindings; retain the service error wrapper. |
| Sigstore                                    | 1.10.6                     | 1.11.1                     | [Release notes](https://github.com/sigstore/sigstore/releases/tag/v1.11.1); signature verification and trust policy stay at the existing owner. No new provider, network path or trust configuration.                                                                                                                                                                                                                                                      |
| `x/image`, `x/sys`, `x/text`                | 0.46.0 / 0.48.0 / 0.42.0   | 0.47.0 / 0.49.0 / 0.43.0   | [Go module index](https://pkg.go.dev/golang.org/x/image), [Windows interfaces](https://pkg.go.dev/golang.org/x/sys), [text](https://pkg.go.dev/golang.org/x/text). Keep decoding, path handling and `core.Fold` contracts; exercise media, watcher and name-order regressions.                                                                                                                                                                             |
| Other Go dependencies                       | Per inventory              | Per inventory              | Upgrade the retained indirect requirements, including crypto, TUF dependencies, protobuf, WebSocket and metadata support. The inventory distinguishes modules linked into shipped executables from the larger upstream tool/test graph.                                                                                                                                                                                                                    |
| Nuxt UI                                     | 4.11.2                     | 4.12.0                     | [Release notes](https://github.com/nuxt/ui/releases/tag/v4.12.0); retain standalone Vite integration, explicit component imports and bundled icons/fonts.                                                                                                                                                                                                                                                                                                  |
| Vue / Vue Router / Pinia                    | 3.5.43 / 5.3.1 / 4.0.3     | 3.5.44 / 5.4.0 / 4.1.0     | [Vue](https://github.com/vuejs/core/releases/tag/v3.5.44), [router](https://github.com/vuejs/router/releases/tag/v5.4.0), [Pinia](https://github.com/vuejs/pinia/releases/tag/v4.1.0). No route or store contract change.                                                                                                                                                                                                                                  |
| VueUse                                      | 14.4.0                     | 15.0.0                     | [Breaking changes](https://github.com/vueuse/vueuse/releases/tag/v15.0.0) remove `templateRef`, change timer options and throttle defaults, and drop Node 20. Lumen directly uses only `createReusableTemplate`, which remains supported. Nested upstream VueUse majors retain their parents' supported ranges.                                                                                                                                            |
| TanStack Vue Virtual                        | 3.13.39                    | 3.13.46                    | [Upstream releases](https://github.com/TanStack/virtual/releases); retain the virtual grid/timeline contracts.                                                                                                                                                                                                                                                                                                                                             |
| Vite / Vue plugin                           | 8.3.1 / 6.0.9              | 8.3.4 / 6.0.10             | [Vite fixes](https://github.com/vitejs/vite/releases/tag/v8.3.4), [Vue plugin](https://github.com/vitejs/vite-plugin-vue/releases). Clear the reported Vite development-server/editor advisories without changing the local server binding.                                                                                                                                                                                                                |
| Vitest / jsdom                              | 5.0.1 / 30.1.0             | 5.0.4 / 30.1.2             | [Vitest](https://github.com/vitest-dev/vitest/releases/tag/v5.0.4), [jsdom](https://github.com/jsdom/jsdom/releases). The mocker fixes cover the Windows traversal and mock-registration advisories.                                                                                                                                                                                                                                                       |
| ESLint / Vue plugin / TypeScript-ESLint     | 10.11.0 / 10.11.0 / 8.70.0 | 10.12.0 / 10.11.1 / 8.71.2 | [ESLint](https://github.com/eslint/eslint/releases/tag/v10.12.0), [Vue plugin](https://github.com/vuejs/eslint-plugin-vue/releases/tag/v10.11.1), [TypeScript-ESLint](https://github.com/typescript-eslint/typescript-eslint/releases/tag/v8.71.2). Remove the ten TypeScript-ESLint overrides: the Vue config now resolves the same supported version. Keep strict rules and layer checks.                                                                |
| Vue type checker                            | 3.3.11                     | 3.3.13                     | [3.3.12](https://github.com/vuejs/language-tools/releases/tag/v3.3.12), [3.3.13](https://github.com/vuejs/language-tools/releases/tag/v3.3.13). Move imported-object template callbacks and derived menu/count expressions into typed script functions/computeds; do not add casts or disable checks.                                                                                                                                                      |
| Prettier / globals / Lucide data            | 3.9.8 / 17.12.0 / 1.2.137  | 3.9.10 / 17.13.0 / 1.2.144 | Regenerate lockfiles and format affected Vue files. The icon manifest still selects the only bundled icons.                                                                                                                                                                                                                                                                                                                                                |
| Puppeteer Core                              | 25.12.0                    | 25.14.0                    | [Upstream releases](https://github.com/puppeteer/puppeteer/releases); no browser download is added. Native suites continue attaching to the installed WebView2.                                                                                                                                                                                                                                                                                            |

Unchanged direct packages are included in the inventory too: imagemeta, SQLite,
goexif, go-tuf, xxh3, Iconify Vue, Tailwind, `@eslint/js`, Vue's ESLint config and
test utilities, and eslint-config-prettier.

## Toolchains, native runtime and build tools

| Component                                   | Before                  | Selected                | Migration                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| ------------------------------------------- | ----------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Go                                          | 1.27.2                  | 1.27.3                  | [Patch release](https://go.dev/doc/devel/release#go1.27.3); root and updater experiment directives, documentation and CI's version-file selection agree. Rebuild Go analysis tools and the Wails CLI with this compiler.                                                                                                                                                                                                                                |
| Node / npm                                  | 24.21.0 / 11.19.0       | 24.22.1 / 12.2.1        | [Node security release](https://nodejs.org/en/blog/release/v24.22.1), [npm](https://github.com/npm/cli/releases/tag/v12.2.1). Raise supported engine floors to the patched Node 22/24/26 lines. Pin only the reviewed esbuild and vue-demi installation hooks in `allowScripts`; npm 12 blocks unreviewed hooks by default.                                                                                                                             |
| Python                                      | 3.14 (local 3.14.4)     | 3.15.1                  | [Security/maintenance release](https://www.python.org/downloads/release/python-3151/). CI pins the patch; Ruff targets 3.15. Use a separate preparation venv so a restored 3.14 environment cannot satisfy the new setup accidentally.                                                                                                                                                                                                                  |
| LLVM-MinGW                                  | 20260922                | 20261020 UCRT x64       | [Release](https://github.com/mstorsjo/llvm-mingw/releases/tag/20261020). Update URL, SHA-256 and cache keys together. Invalidate LibRaw objects/worker when the compiler, flags, source or LibRaw path changes.                                                                                                                                                                                                                                         |
| libvips Windows web/static                  | 8.18.6                  | 8.18.7                  | [Packaging release](https://github.com/libvips/build-win64-mxe/releases/tag/v8.18.7). Replace extraction when the verified archive hash changes, including on a warm checkout; copy the selected DLL/notices regardless of archive timestamps. Preserve the web/static licence profile.                                                                                                                                                                 |
| libjpeg-turbo                               | 3.1.2                   | 3.2.0                   | [Release notes](https://github.com/libjpeg-turbo/libjpeg-turbo/releases/tag/3.2.0); keep the GCC runtime-independent distribution and update recorded codec provenance.                                                                                                                                                                                                                                                                                 |
| OxiPNG                                      | 9.1.5                   | 10.2.1                  | [10.0 breaking changes](https://github.com/oxipng/oxipng/releases/tag/v10.0.0), [10.2](https://github.com/oxipng/oxipng/releases/tag/v10.2.0), [10.2.1](https://github.com/oxipng/oxipng/releases/tag/v10.2.1). Lumen uses `--interlace keep`, neither the removed numeric values nor renamed dry-run/zopfli flags. Keep `--locked`, static CRT linking and full transitive notices. Its updated libdeflate builds without the old compiler workaround. |
| Rust                                        | 1.99.0                  | 1.99.1 gnullvm          | [Stable channel manifest](https://static.rust-lang.org/dist/channel-rust-stable.toml); update rustc, Cargo and standard-library URLs and all three checksums.                                                                                                                                                                                                                                                                                           |
| ONNX / tokenizers / NumPy preparation tools | 1.23.0 / 0.23.2 / 2.5.3 | 1.23.2 / 0.23.3 / 2.5.4 | [ONNX](https://pypi.org/project/onnx/1.23.2/), [tokenizers](https://pypi.org/project/tokenizers/0.23.3/), [NumPy](https://pypi.org/project/numpy/2.5.4/). Pin the complete resolved environment in `scripts/model-requirements.txt`, refresh it when pins change, and include the file in preparation/CI invalidation. Checkpoints, tokenizer vocabulary and application stage versions stay at their existing semantic contract.                       |
| Pillow fixture tooling                      | Previously unpinned     | 13.0.0                  | [Release](https://pypi.org/project/pillow/13.0.0/); add `e2e/requirements.txt` and document installation. Nothing Python ships in the installer.                                                                                                                                                                                                                                                                                                        |
| golangci-lint / deadcode / govulncheck      | 2.14.0 / 0.51.0 / 1.8.0 | 2.15.0 / 0.52.0 / 1.9.0 | [golangci-lint](https://github.com/golangci/golangci-lint/releases/tag/v2.15.0), [Go tools](https://pkg.go.dev/golang.org/x/tools/cmd/deadcode), [vulnerability checker](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck). Keep the enabled rules and checks.                                                                                                                                                                                      |
| Ruff                                        | 0.16.8                  | 0.17.2                  | [Release](https://github.com/astral-sh/ruff/releases/tag/0.17.2); pin and verify the Windows archive.                                                                                                                                                                                                                                                                                                                                                   |
| setup-python action                         | 7.0.0                   | 7.1.0                   | [Release](https://github.com/actions/setup-python/releases/tag/v7.1.0); use the resolved full commit SHA. Other action pins remain current; inventory includes their exact commits.                                                                                                                                                                                                                                                                     |

Binding generation now scans only the application Go source roots, its generated
outputs and the module files. Downloaded Node/npm toolchains, experiments and
scratch JavaScript files no longer invalidate the bindings or expand that scan.
Packaged native processes also run with only Windows/System32 on `PATH`; the
test harness retains its toolchain path. This prevents a developer DLL from
masking an incomplete installer payload.

The newer errcheck checks errors received from channels. Cancellation paths now
consume their completion result explicitly; the health launcher uses a
completion-only channel because its durable health record decides the outcome.
Failure branches in the existing tests include the received error in their
diagnostics. Go lint rules and existing test assertions remain enabled.

WebView2's offline installer was refreshed from Microsoft's official link. Its
bootstrapper file version moves from 1.3.271.7 to 1.3.287.9. Windows reports
`Valid` / “Signature verified”, publisher Microsoft Corporation, signing
certificate thumbprint `21CA9DD6DA7C57C3A599880D17317D7BDA31899C`.
The pinned SHA-256 is
`0625fb5c1e75be58a164e14f95aa58a3e5badb859b3d17c952385f3e4a9ff3e8`.
This verifies the downloaded installer, not a host installation or clean-machine
test. FFmpeg remains the durable September month-end LGPL build of 9.0.2;
[upstream](https://ffmpeg.org/releases/) has no newer stable release. LibRaw
0.22.2, libjxl 0.12.0, DirectML 1.15.4, ShellCheck 0.11.0 and NSIS 3.13 remain
current. Model weights and the offline city data are immutable data inputs,
not automatically substituted for a differently trained model or newer atlas.

## Deliberate exceptions and remaining advisories

- Wails beta.34 is newer, but its npm runtime was published on 20 October.
  beta.30, published on 15 October at 15:17 UTC, is the newest matching runtime
  old enough at selection time. Reassess beta.34 after 27 October; its Windows
  focus and COM-vtable fixes still need native acceptance. Stable Wails 2 is a
  different API line, not an upgrade target for this v3 application.
- TypeScript 7.0.2 is stable, but eligible TypeScript-ESLint 8.71.2 declares
  `typescript >=4.8.4 <6.1.0`. Keep TypeScript 6.0.3 until the type-aware lint
  stack supports 7; do not force or silence that peer contract.
- [ONNX Runtime DirectML's NuGet index](https://api.nuget.org/v3-flatcontainer/microsoft.ml.onnxruntime.directml/index.json)
  still ends at 1.24.4, although core ONNX Runtime is now 1.31.0. Keep the
  DirectML runtime, DirectML 1.15.4 and onnxruntime_go 1.27.0 paired: newer Go
  bindings request a newer C API: [onnxruntime_go 1.36.0's header](https://github.com/yalue/onnxruntime_go/blob/v1.36.0/onnxruntime_c_api.h)
  requests API 29, beyond the bundled 1.24.4 runtime. Replacing the GPU provider is a separate
  migration requiring real-model/hardware comparison.
- Keep the esbuild 0.28.2 override. Removing it resolves font tooling to
  0.27.7 and reintroduces
  [GHSA-g7r4-m6w7-qqqr](https://github.com/advisories/GHSA-g7r4-m6w7-qqqr).
  Remove it only when that parent resolves a patched compatible release.
- [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm)
  was reported for `braces` 3.0.3 through
  `@vue/eslint-config-typescript → fast-glob → micromatch`, a development-only
  path that is not bundled. GitHub withdrew
  the advisory at 12:53 UTC on 23 October; with the same lockfile, `npm audit`
  now reports zero vulnerabilities for both the frontend and e2e packages.
  npm's earlier proposal, a Vue-config downgrade that would undo the ESLint 10
  integration, was not applied.
- [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) describes the unmaintained
  `golang.org/x/crypto/openpgp` package. It is reported at module level for the
  latest selected `x/crypto` 0.58.0, with no fixed version. Lumen imports none of
  its affected packages or symbols; govulncheck reports zero affected
  vulnerabilities. Keep OpenPGP out of the import graph and repeat the scan as
  dependencies change.

## Verification

Local evidence is stored under `e2e/.work/issue28/` (ignored). The candidate is
`bin/Lumen-0.12.0-x64-setup.exe`; its extracted payload is under
`e2e/.work/issue28/package-extracted/`. These are unsigned local build artifacts.

- The pre-change Windows production build passed. Its 600-photo AI/indexing
  run took 70 seconds. A five-launch startup measurement had a 1,032 ms median;
  idle private memory was 287 MB. The old performance harness timed out while
  stepping the viewer, so it does not establish viewer-next latency.
- The initial pre-change 100,000-photo latency run failed seven ceilings:
  folder Events; combined-block RebuildBlocks, SaveEmbeddings64, SaveFaces64,
  SaveTags64 and SaveThumbnails64; and unblocked SaveFaces64. This run overlapped
  build/setup work. Retain it as a noisy baseline; the sequential repeats below
  passed without changing any budget.
- The upgraded production build, NSIS packaging and canonical binding generation
  passed. All 45 extracted-payload checks pass, including the offline WebView2
  installer. The 16 tracked module/lock/binding files checked across generation
  and packaging have no unexpected drift. The initial frontend run passed 110
  files / 945 tests; the final integrated run also passed all 945 tests.
- The full named-package race command passed with the upgraded toolchain and
  32 real RAW samples: 1,560 passing test/subtest events, seven individual skips,
  20 tested packages and seven packages without tests. Four skips require a
  separately supplied real-face corpus; the remaining skips are helper-process
  or symlink conditions. Watcher cancellation, update recovery, image/frame
  cancellation, thumbnail eviction and shutdown regressions pass after adapting
  the new linter checks. Go lint reports zero issues.
- The updated auxiliary updater experiment passed its race suite. The four
  new extraction regressions pass: a changed pin replaces DLL/notices and
  removes stale files, a matching pin reuses extraction, corrupt extraction
  preserves the previous build and retries, and unstamped caches refresh.
- Re-preparing with the updated Python/model tooling produced byte-identical
  SHA-256 hashes for all 16 model/runtime-data files. The downloaded checkpoints,
  prepared model outputs and tokenizer therefore do not require stage-version
  changes or re-embedding existing libraries.
- Go's Unicode tables are byte-identical between 1.27.2 and 1.27.3, as are the
  selected `x/text` Unicode 17 normalisation and collation tables. `core.Fold`
  remains based on standard-library simple folding. The persisted name/path
  keys therefore do not need an index migration for these upgrades.
- The first upgraded latency run failed folder SaveFaces64 (8.504 ms, ceiling
  7 ms). The integrated repeat failed folder SaveFaces64 (11.499 / 7 ms),
  combined-block DayItemsBigDay (56.109 / 50 ms), unblocked SaveFaces64
  (12.806 / 8 ms) and SaveThumbnails64 (20.471 / 20 ms). Other workspace work was
  active on this host. After the other validation finished, sequential previous
  and candidate 100,000-photo runs passed every budget in all three block
  configurations. The previous run used Go 1.27.2 and its saved module files;
  the candidate used Go 1.27.3 and its selected module graph. Both used the same
  current native compiler and benchmark source. Logs are
  `before-store-quiet.log`, `before-store-budgets.log` and `store-quiet.log`.
  No budget or test threshold has been relaxed.
- The final integrated lint run passed Go analysis, deadcode, govulncheck,
  frontend type/lint/format checks and tests, Ruff, both four-test Python
  regression suites, documentation links and ShellCheck. The latency gate
  above was its only failing step and passed on the separate repeat. govulncheck found no affected imported
  packages or reachable symbols; the development-only npm advisory recorded
  under deliberate exceptions has since been withdrawn.
- Extracted native suites passed onboarding 7/7, interface 55/55, collections
  8/8, Organise 7/7, viewer 63/63, file tools 14/14, branding 18/18, formats
  55/55, customisation 45/45, compression 41/41 and updater defaults 15/15.
  The initial route tour passed layout/offline checks but logged a local fetch
  interrupted during its startup reload. The harness now registers its request
  handler before enabling interception and waits for network idle before
  reloading. Its repeat passed 5/5 checks across all 68 screen visits, including
  the unchanged console-error assertion. All 12 default suites now pass
  (333 checks), with the tour rerun separately.
- The installer Sandbox harness stopped at its prerequisite check: the previous
  `Lumen-0.11.0-x64-setup.exe` is unavailable. It did not run a host installer or
  establish clean Windows installation, upgrade or uninstall acceptance.
- The saved previous executable wrote a 24-photo library, manual and smart
  albums, Unicode tags, favourites, date/location corrections, preferences,
  PIN-protected blocking and pending OCR jobs for visible and blocked photos.
  The candidate preserved this state and completed both durable jobs: 16/16
  upgrade checks pass. The harness waits for persisted job completion and the
  service's progress notification to settle before asserting both views.
- The stable-launcher scratch-install check passed 13/13, including Unicode
  installation paths, admission, recovery of an abandoned activation barrier,
  graceful exit and cleanup preserving unrelated files. The harness now waits
  for the WebView accessibility document before closing the launcher child;
  the first attempt closed it during WebView controller creation.
- Clean installed upgrade/uninstall on Windows 10/11, real face photos,
  additional GPU/hardware and camera-quality checks, and public signing remain
  independent acceptance gates. The updater trust file stays empty.

The opt-in `upgrade` native suite now builds an old-library fixture using a saved
previous executable, then opens it with the candidate. It checks user state,
PIN/privacy and completion of queued OCR work through the real service bridge.
Its generated 24-photo corpus does not prove real-face correction preservation;
the Go correction/people regressions and separate real-face checks cover that
part of the acceptance plan.

## Local performance samples

Both corrected performance runs completed without search or viewer errors on
the same 600-photo synthetic corpus. The saved previous executable loaded
libvips 8.18.6 beside it; the candidate loaded 8.18.7. They used identical model
data. These are sequential samples on a shared, non-isolated Windows host;
they do not establish a statistically stable regression or improvement.

| Measure                                     |        Previous |       Candidate |
| ------------------------------------------- | --------------: | --------------: |
| Full indexing, including embeddings/faces   |            70 s |            58 s |
| Startup to usable grid, five-launch median  |        1,296 ms |        1,182 ms |
| Idle private memory, after 60 seconds       |          285 MB |          288 MB |
| Reload to usable grid, eight-reload median  |          448 ms |          547 ms |
| Loaded JavaScript / CSS, decoded            | 1,054 / 247 KiB | 1,075 / 254 KiB |
| Scroll frame gap, p95                       |         99.9 ms |         50.1 ms |
| First semantic search, including model load |        1,892 ms |        1,697 ms |
| Warm search, five-query median              |          228 ms |          205 ms |
| Viewer open                                 |           89 ms |           88 ms |
| Viewer next, ten-step median                |           84 ms |           57 ms |
| Private memory after use                    |        2,347 MB |        2,335 MB |

Startup sampling follows an unmeasured launch; it does not flush the OS file
cache. The higher initial reload median and extra 21 KiB JavaScript / 7 KiB CSS
remain visible in this record. A follow-up alternated the build order over
three rounds of five reloads each: previous round medians 299/301/300 ms,
candidate 225/229/294 ms, pooled medians 300 versus 233 ms. It did not reproduce
the initial reload slowdown. The raw follow-up is
`e2e/.work/issue28/reload-comparison.json`. Store repeats also passed all budgets
as recorded above. Raw full performance reports are
`e2e/.work/results/perf/issue28-before-corrected.json` and
`e2e/.work/results/perf/issue28-after.json`. The original baseline viewer timeout
was a harness selector error: it could select a thumbnail instead of the loaded
original. The corrected selector targets the viewer's original image, and the
performance command now fails if a search/viewer measurement fails.

## Native runtime deployment gate

A PE import audit found an existing installer dependency gap: the shipped
`onnxruntime.dll` imports `MSVCP140.dll`, `MSVCP140_1.dll`, `VCRUNTIME140.dll`
and `VCRUNTIME140_1.dll`; `onnxruntime_providers_shared.dll` also imports
`VCRUNTIME140.dll`. None is in the runtime payload. This developer PC supplies
them from System32, which is still searched with the restricted app `PATH`.
Passing native tests here therefore does not prove that AI loads on a clean PC.
The audit covered 23 executables/DLLs and 341 imports; its complete output is
`e2e/.work/issue28/runtime-imports.json`.

Microsoft documents both
[app-local deployment and redistribution requirements](https://learn.microsoft.com/en-us/cpp/windows/redistributing-visual-cpp-files).
App-local deployment preserves the current offline, per-user installer;
running the central redistributable requires elevation. Microsoft's documented
redistribution grant depends on Visual Studio licensing.

The owner chose app-local deployment and confirmed the licence on 23 October.
`scripts/deps.sh` now pins Microsoft's versioned download of the x64
redistributable 14.51.36247.0 by SHA-256 (its signature is valid, publisher
Microsoft Corporation), extracts the four DLLs from its x64 runtime cabinet
without installing or running anything, and checks each against its own pinned
SHA-256. Each DLL is Authenticode-signed by Microsoft. They land in
`deps/runtime/`, so the build, the installer and the update package carry them
like every other runtime file, and the notices name them with the licence
link. No developer-System32 DLL was copied, and nothing is downloaded at run
time. Windows Update does not service app-local copies; a Lumen update
replaces them.

`scripts/runtime_imports.py`, a new `scripts/lint.sh` step, reads every
executable and DLL in the runtime and fails when an import, direct or
delay-loaded, is neither beside it nor part of Windows 10 and later. Before the
DLLs were added it reported exactly the five imports above; it passes on
`deps/runtime/` and on the extracted installer's runtime. With the DLLs in
place, the running application loaded all four from its own folder, not
System32, and ONNX Runtime started. The rebuilt installer's payload check
passed 49/49 (the 45 earlier checks and the four DLLs), and the onboarding
(7/7) and interface (55/55) suites passed against the extracted installer with
only Windows on the application's `PATH`. System32 on this PC still holds its
own copies, so a clean Windows installation remains the final proof.

## Audit follow-up, 23 October

A review of the staged change against the issue corrected these points before
commit:

- The record and inventory moved to `docs/history/`, beside the earlier
  dependency audit, and the audit index lists them in date order.
- `main.go` and the bridge still described Wails beta.26. Both behaviours were
  re-read in the beta.30 sources: a bound method still uses only its service's
  error marshaller, and a late cancelled response still arrives as
  `CancelledRejectionError`. The comments and `AGENTS.md` now name beta.30.
- `scripts/release.ps1` ran the old race command; it now matches CI and the
  documentation (`-p 1 -count=1 -timeout 1800s`).
- `build/darwin/Taskfile.yml` was still the beta.26 template. It now matches
  beta.30's, the only build template that changed between the two releases.
  macOS remains an unsupported target.
- CI's npm cache key no longer includes the e2e lockfile, which CI never
  installs. The cache step no longer touches `lumen-raw.exe`: its build stamp
  decides a rebuild, and the stamp omits the compiler's install directory, so
  the same compiler in another location does not rebuild LibRaw.
- The `upgrade` suite's blocked-media check now has a control: the visible
  photo's thumbnail must be served while the blocked one is refused.
- The npm advisory above was withdrawn upstream; both audits report zero.

After these changes, the full named-package race command passed with 32 RAW
samples (20 tested packages), `scripts/lint.sh` passed every step including all
latency budgets and 945 frontend tests, binding regeneration produced no diff,
both module graphs stayed tidy, and the `upgrade` suite passed 16/16.

The Visual C++ runtime gap under "Native runtime deployment gate" predated this
upgrade. It is closed by app-local deployment, as recorded in that section.
Acceptance on a clean Windows installation is still outstanding.
