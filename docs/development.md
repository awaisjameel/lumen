# Developing Lumen on Windows

Use **Git Bash** on Windows x64, from the repository root unless a block says
otherwise. PowerShell is used only for explicitly labelled Windows commands.
The application is offline; dependency setup, npm installation, Go module
resolution and vulnerability checks use the network on the development PC.

## Setup

Needs Go 1.27.3, a supported Node release (the exact ranges are below), npm 12.2.1+, a C compiler for cgo, Git Bash, 7-Zip on `PATH`, and Python 3.15.1 (`ruff.toml` targets 3.15). Toolchain used:

- Go 1.27.3 and [llvm-mingw](https://github.com/mstorsjo/llvm-mingw) 20261020 (UCRT x86_64) unpacked under `%LOCALAPPDATA%\Programs`, both on `PATH`
- User environment: `CGO_ENABLED=1`, `CC=clang`, `CXX=clang++`
- Wails CLI pinned to the beta the project uses: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.30`

`scripts/deps.sh` fetches libvips 8.18.7 (static web build), ONNX Runtime 1.24.4 with DirectML and the Visual C++ runtime it needs (four DLLs taken from Microsoft's pinned redistributable), FFmpeg 9.0 (LGPL), SigLIP 2, the YuNet and SFace face models, the PP-OCRv5 text detector and English recogniser, the linters and NSIS (into `deps/tools/`), and the WebView2 runtime the installer carries (into `deps/installer/`). It keeps each shipped component's licence text (in `deps/licenses/`) and builds the camera RAW worker (`native/lumen-raw`, LibRaw compiled with the same clang). It then prepares the models (`scripts/prepare_models.py`): the vision tower gets a fixed batch shape so DirectML can compile it, the text tower's 256k-token table is stored as int8 (565 MB to 369 MB, agreeing with the original at cosine ≥ 0.9996), and SFace gets a variable batch size so every face in a photo is recognised in one run. Everything that ships beside `Lumen.exe` lands in `deps/runtime/` and is hard-linked into `bin/`.

Compression adds pinned libjpeg-turbo 3.2.0 and libjxl 0.12.0 tools in `deps/runtime/compress/`. OxiPNG 10.2.1 is built from its locked source with a pinned portable Rust 1.99.1 toolchain (the `gnullvm` target) and the same clang. The Rust compiler and Cargo cache stay in `deps/`; only the encoder ships.

Check the setup with `wails3 doctor`, then run `wails3 build` once: `Lumen.exe` embeds the built frontend (`frontend/dist`), so on a fresh clone no Go package compiles, and `go test` and `scripts/lint.sh` fail, until it exists. Line endings are pinned to LF by `.gitattributes` (gofmt and Bash need it), whatever `core.autocrlf` says.

Then turn on the repository's git hooks, once per clone, with `scripts/install-hooks.sh`. **commit-msg** holds messages to Conventional Commits (`type(scope): summary`) and refuses any that credit or mention an AI assistant. **pre-commit** checks the staged files' formatting (gofmt, Prettier, Ruff) and refuses this machine's absolute paths. **pre-push** runs the frontend check, Go vet and the documentation's links.

Install the prerequisites before cloning. Check the versions selected by this
shell; changing Windows PATH does not update an already-open Git Bash window:

```sh
git clone https://github.com/awaisjameel/lumen.git
cd lumen
go version
node --version
npm --version
python --version
clang --version
7z i
export CGO_ENABLED=1 CC=clang CXX=clang++
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.30
scripts/deps.sh
npm --prefix frontend ci
npm --prefix e2e ci
python -m pip install -r e2e/requirements.txt
source scripts/env.sh
wails3 doctor
wails3 build
scripts/install-hooks.sh
```

The supported Node ranges are `^22.23.4 || ^24.22.1 || >=26.12.1`;
CI uses Node 24.22.1 and npm 12.2.1. `frontend/package.json` and
`e2e/package.json` enforce those ranges. Install npm 12.2.1 for the selected
Node (`npm install --global npm@12.2.1`), then check `npm --version` again.
On this repository's prepared development machine, portable tools may live
under ignored `deps/toolchains/`; they are not fetched or placed on PATH by
`scripts/env.sh`. That script adds **only the native runtime**. A Python without
pip needs pip provisioned before the fixture requirements can be installed.
The CI toolchain pins are in [the setup action](../.github/actions/setup/action.yml).

Running a source build also needs the Microsoft Edge WebView2 Runtime. If it is
absent, dependency preparation supplies the offline installer in
`deps/installer/MicrosoftEdgeWebView2RuntimeInstallerX64.exe`; it does not install
it on your development PC. `wails3 doctor` can report optional platform tools,
SignTool or NSIS as absent: unsigned packaging uses the pinned NSIS under
`deps/tools/nsis/`, and unsupported mobile/MSIX targets are not required.

`scripts/deps.sh` prepares a private Python environment for models, fetches
pinned tools and builds native workers. It requires the external toolchains
above; it does not install Go, Node, Python or llvm-mingw for you. Allow several
GB of free disk space for archives, models, toolchains and build caches.

## Development and builds

```sh
# Repository root, in every new Git Bash session.
export CGO_ENABLED=1 CC=clang CXX=clang++
source scripts/env.sh
wails3 dev
```

Close the development app before building a production executable. To keep
manual development away from your own library, set a scratch location first:

```sh
export LUMEN_DATA_DIR="$PWD/e2e/.work/manual-library"
export LUMEN_DEVTOOLS_PORT=9449
wails3 dev
```

```sh
# Repository root. Each command finishes before the next begins.
wails3 generate bindings -f '-tags production' -clean=true -ts -i
git diff -- frontend/bindings
wails3 build
wails3 task package
```

The production executable is `bin/Lumen.exe`. Packaging also builds the stable
launcher and independent update helper, the versioned runtime ZIP, notices and
`bin/Lumen-<version>-x64-setup.exe`. See [installer acceptance](../e2e/installer/README.md).
Bindings are generated, never edited. A service or wire-type change needs
generation and review in the same change. Never run frontend and Go production
builds concurrently: Go embeds the finished `frontend/dist`.

The version and copyright have one source, `build/config.yml`. Change the version
only through [the release workflow](releasing.md). After changing the copyright,
run `wails3 task common:update:build-assets`, then restore
`license: "PolyForm-Noncommercial-1.0.0"` in `build/linux/nfpm/nfpm.yaml`, which
Wails resets to its template licence. `TestVersionsAgree` and
`TestLicenceAndCopyrightAgree` guard these assets. Packaging generates
`bin/THIRD_PARTY_NOTICES.txt` with `scripts/notices.py`; signing prerequisites and
the reviewed `SIGN_THUMBPRINT` / `TIMESTAMP_SERVER` inputs belong to
[production signing](production-signing.md#signing-in-the-release-workflow).

## Focused and full checks

```sh
# Repository root; env.sh must be sourced in this shell.
go test -race -timeout 900s -run TestTagByNameRefusesABlockedThing ./internal/store
go test -race -p 1 -count=1 -timeout 1800s . ./internal/... ./services/... ./cmd/... ./scripts/...
scripts/lint.sh
scripts/lint.sh --no-bench
scripts/bench.sh
scripts/check-docs.sh
python scripts/check_links.py
python scripts/check_links_test.py
```

Name the packages, never `go test ./...`: `build/` contains unsupported mobile
templates and `spikes/` is a separate module. Model tests skip when the runtime
is absent. Real-face and RAW tests also need the explicitly supplied corpora
described in [the e2e guide](../e2e/README.md#tests-on-real-files-in-go).
`scripts/lint.sh --fix` applies automatic fixes; review its diff afterwards.
Latency budgets must not be raised merely because a run failed.

```sh
# These examples stay at the repository root.
npm --prefix frontend run check
npm --prefix frontend test
(cd frontend && npx --no-install vitest run src/shared/commands.test.ts)
npm --prefix frontend run format
node e2e/run.mjs --list
node e2e/run.mjs viewer formats
SECTIONS=themes,video node e2e/suites/interface.mjs
```

Native suites require the built executable, the locked e2e npm dependencies
and Pillow. They run sequentially on isolated libraries. Full instructions,
fixture downloads, optional upgrade/performance checks and `--clean` behaviour
are in [e2e/README.md](../e2e/README.md). Do not run `--clean` during another
capture or test. [Documentation screenshots](screenshots/README.md) have their
own reproducible capture command and media attribution.

## Troubleshooting the build

| Symptom | Check |
| --- | --- |
| `EBADENGINE` | Run `node --version`, `npm --version` and `type -a node npm`; select supported versions before retrying `npm ci`. |
| `libvips-42.dll` missing / Go test will not start | Run `scripts/deps.sh`, then `source scripts/env.sh` in this shell. |
| cgo or linker errors | Confirm `go env CGO_ENABLED CC`, the llvm-mingw UCRT x64 compiler and native dependencies; a different clang distribution is not equivalent. |
| `frontend/dist` missing | Finish `wails3 build` once before running Go tests or lint. |
| Locked executable or DLL | Close only your scratch/development app gracefully before rebuilding; do not kill a user's normal library process. |
| No face/RAW tests ran | Supply the opt-in corpus variables; a skip is not acceptance. |

## Environment variables

| Variable | Purpose |
| --- | --- |
| `LUMEN_DATA_DIR` | Library, logs and WebView profile; defaults to `%LOCALAPPDATA%\Lumen`. A scratch path gives an independent instance. |
| `LUMEN_RUNTIME_DIR` | DLL/model runtime location; normally beside the application executable. |
| `LUMEN_DEVTOOLS_PORT` | Test-only loopback DevTools port; off by default. Use a distinct free port. |
| `LUMEN_FACE_PHOTOS` | Opt-in real-face corpus for Go tests; never commit private photos. |
| `LUMEN_RAW_SAMPLES` | Opt-in real-camera RAW corpus; the e2e guide explains the pinned CC0 download. |

## Quality checks and CI

`scripts/lint.sh` runs the static checks, the frontend tests, the documentation's links and the large-library latency gate. It runs every step and reports all failures at the end; `scripts/lint.sh --fix` first applies every automatic fix.

CI ([.github/workflows/ci.yml](../.github/workflows/ci.yml)) runs on every pull request, on GitHub's Windows runners, with the toolchain and `deps/` set up as [Setup](#setup) describes ([.github/actions/setup](../.github/actions/setup/action.yml)). It checks the pull request's commit messages, then runs three kinds of job side by side: a production build, which must leave the bindings and `go.mod` as committed, followed by `scripts/lint.sh --no-bench`; the Go race tests, split three ways (the store's randomised oracle tests, the rest of the store, and every other package, the Go tools in `scripts/` included); and the latency gate. The race tests run one package at a time (`-p 1`) with a 30-minute per-package timeout, so native models and filesystem tests do not compete on the smaller runners. The toolchain, `deps/` and each job's Go build cache are cached as soon as they are ready, so a failing run does not send the next one back to a cold build. A merge to `main` runs CI again only when it changes what those caches hold (Go modules, the frontend's lockfile, the dependency scripts or the CI set-up), because only caches saved on `main` are shared with later pull requests; a release tag always runs it in full. The latency gate is only reported in the job summary: the runners are slower than the machine the budgets were set on, so `scripts/lint.sh` on a developer's machine stays the binding gate. Each tool is pinned and configured in the repository:

| Code | Formatting | Linting | Config |
| --- | --- | --- | --- |
| Go | gofmt, goimports | golangci-lint 2.15 (vet, staticcheck, errcheck, gosec, errorlint, revive and more), deadcode, govulncheck | `.golangci.yml` |
| Frontend | Prettier 3.9 | ESLint 10 with typescript-eslint (strict, type-aware) and eslint-plugin-vue, plus `vue-tsc`; import boundaries between `design`, `shared`, features and `app` | `frontend/eslint.config.js`, `frontend/.prettierrc.json`, `frontend/tsconfig.json` |
| Python | Ruff | Ruff | `ruff.toml` |
| Shell |  | ShellCheck |  |

The Go tools, Ruff and ShellCheck live in `deps/tools/` (from `scripts/deps.sh`); the frontend's are dev dependencies (`npm ci`). npm enforces a seven-day release-age window through `min-release-age=7` in `frontend/.npmrc`, and engine checks reject unsupported Node and npm versions. Exclusions are rare and each says why, in the config or on a `//nolint:linter // reason` line. Editors do the same on save: `.editorconfig` sets line endings and indentation, and `.vscode/settings.json` runs these formatters and linters from the pinned copies (install the recommended extensions).

How code is written (errors, context, SQLite, logging, the Go-to-TypeScript contract, Pinia and Vue, tests) is set out in [CONVENTIONS.md](../CONVENTIONS.md). [AGENTS.md](../AGENTS.md) summarises the architecture and the rules that are easy to break, for anyone (or any tool) about to change the code.

The latency gate seeds 100,000 photos once and times store reads and writes with no blocks, a folder block, and a folder, Thing and person block. It includes thousands of events and a day with 10,000 imported photos. The [bottleneck audit](history/bottleneck-audit-2026-10-13.md) explains why small fixtures missed these problems. Allow one to two minutes on an i7-13700H with a warm toolchain. The readable ceilings in `internal/store/testdata/library-budgets.txt` catch substantial regressions and allow ordinary timing noise; a missing or unused budget also fails. Raise a ceiling only by hand, with a reason and at least five repeat measurements in review, using the headroom rule in that file. A failing run is a reason to investigate, not to raise a budget.
