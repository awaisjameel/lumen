# AGENTS.md

This file provides guidance to AI Coding Agents when working with code in this repository.

## What this is

Lumen is a Windows-first desktop photo gallery with on-device AI: a Go engine and a Vue 3 UI in one Wails v3 (beta.26) window. It is fully local. **Lumen makes no network request and never downloads anything at run time**; do not add one. The only authorised exception is the opt-in updater (`internal/update`), which stays inert until release trust is configured (`docs/production-signing.md`).

Two documents are authoritative and should be read before non-trivial work:

- `README.md`: what Lumen does, status, setup, commands, layout, and the list of deliberate deviations from the plan.
- `CONVENTIONS.md`: how code is written here (errors, context, SQLite, logging, the Go-to-TypeScript contract, Pinia and Vue, tests). Where two patterns compete, it picks one.

`docs/` holds the living guides (features, beta testing, production signing, branding, GeoNames data) and the original plan. `docs/history/` holds the dated records per phase (design, implementation, audits), indexed in `docs/history/README.md`. They are historical; the newest for a phase is the one that applies, and where a record and the code differ, the code and the README win.

## Development rules

- Keep the implementation unified and clean: consistent coding standards, following the existing structure and conventions.
- Everything aligns, with no gaps, drift, conflicts, mismatches or duplication. Code is type-safe end to end and follows SOLID principles.
- Use current techniques to keep the code fully typed, robust, fast and maintainable.
- Follow best practices consistently, for the frontend, the backend and the infrastructure alike.
- Leave no inconsistencies or technical debt behind.

## Commands

Go needs cgo (`CGO_ENABLED=1`, `CC=clang` from llvm-mingw) and the native dependencies in git-ignored `deps/`, fetched once with `scripts/deps.sh` from Git Bash (about 1.2 GB). The shell scripts are Bash; run them with Git Bash, not PowerShell.

```sh
wails3 dev                                  # run with hot reload
wails3 build                                # production build -> bin/Lumen.exe
wails3 task package                         # build + NSIS installer -> bin/Lumen-<version>-x64-setup.exe

source scripts/env.sh                       # REQUIRED before go test / vet / run: puts deps/runtime (libvips DLL) on PATH
go test -race -count=1 -timeout 900s . ./internal/... ./services/... ./cmd/...
go test -race -timeout 900s -run TestTagByNameRefusesABlockedThing ./internal/store   # one test

scripts/lint.sh                             # static checks, frontend tests, doc links and large-library latency; must stay at zero
scripts/lint.sh --fix                       # apply automatic fixes first
scripts/lint.sh --no-bench                  # everything but the latency gate (CI runs that as a reported step)
scripts/bench.sh                            # large-library latency gate alone (one to two minutes; sources env.sh)
scripts/install-hooks.sh                    # once per clone: commit-msg, pre-commit and pre-push hooks (.githooks)
scripts/version.sh bump 0.13.0              # release: bump on a release/ branch, regenerate assets, commit
scripts/version.sh tag                      # on main after the merge: push v<version>, which drafts the release

cd frontend
npm run check                               # vue-tsc, ESLint (zero warnings), Prettier
npm run test                                # Vitest (jsdom)
npx vitest run src/shared/commands.test.ts  # one file; add -t "name" for one test
npm run format                              # Prettier --write

node e2e/run.mjs                            # end-to-end suites against bin/Lumen.exe (after wails3 build)
node e2e/run.mjs viewer formats             # named suites; --list shows them, --clean deletes e2e/.work
SECTIONS=themes,video node e2e/suites/interface.mjs

wails3 generate bindings -f '-tags production' -clean=true -ts -i   # regenerate frontend/bindings
```

The latency step follows the Go static checks in `scripts/lint.sh`. It seeds
100,000 photos once and checks every call in three block configurations against
`internal/store/testdata/library-budgets.txt`, without the race detector.
Budgets are ceilings for substantial regressions. Raise one only by hand, with
a reason and at least five repeat measurements in review; do not update it just
because a run failed.

- **Name the Go packages, never `./...`**: `build/` holds Wails' mobile templates, which only build on their own platforms. `spikes/` is its own Go module.
- **Always pass `-timeout`** to `go test`; the race run is slow.
- Tests that need real models go through `internal/testenv` and skip cleanly without `deps/runtime`. Face and RAW tests also skip unless `LUMEN_FACE_PHOTOS` / `LUMEN_RAW_SAMPLES` name a folder (see `e2e/README.md`).
- The frontend must finish building before Go embeds `frontend/dist`; never run the frontend and Go production builds concurrently.
- The version and the copyright are set once in `build/config.yml`. Change the version only with `scripts/version.sh bump`. After changing the copyright, run `wails3 task common:update:build-assets` and restore the PolyForm licence it resets in `build/linux/nfpm/nfpm.yaml` (`TestVersionsAgree`, `TestLicenceAndCopyrightAgree`).
- CI (`.github/workflows/ci.yml`) and the release workflow (`release.yml`, on a `v*` tag) run on Windows runners set up by `.github/actions/setup`. A release is only ever drafted; the owner publishes it (README, "Releasing"). Pin every action to a full commit SHA with its version in a comment.
- After changing an exported service method or a wire type, regenerate the bindings and check `git diff -- frontend/bindings`. Bindings are never edited by hand.

### Running the real app for a check

Launch `bin/Lumen.exe` with `LUMEN_DATA_DIR=<scratch folder>` so it opens a scratch library beside the user's own (its own single-instance ID and WebView2 data folder), and `LUMEN_DEVTOOLS_PORT=<port>` to drive the WebView over the DevTools protocol. Never touch the real `%LOCALAPPDATA%\Lumen`. Close with `taskkill /PID <pid>` (no `/F`) so shutdown is graceful. End-to-end scripts and their scratch data belong in `e2e/` (`e2e/.work` is git-ignored), written as a suite per use case (`e2e/README.md`, "Writing a suite").

A scratch library's SQLite file cannot be edited with the `sqlite3` CLI: its indexes call the custom `fold()` function. Use Python with `create_function('fold', …)`.

## Architecture

`main.go` wires everything in order: single-instance hand-over first (before the database is opened), then the store, media cache and decoders, the loopback video server, the AI models, the indexer, and the Wails services. The same executable re-runs itself as `--render-worker` for bounded image rendering; camera RAW is developed by a separate `lumen-raw.exe` (`native/lumen-raw`, C++/LibRaw, built by `deps.sh`).

### Layers (Go)

`core` → `store` → engines (`media`, `ai`, `search`, `people`, `places`, `index`, `fileops`) → `services` → `main`.

- `internal/core`: domain and wire types, paths, the format catalogue, case folding, the date convention, preferences. Everything the UI sees is a `core` type.
- `internal/store`: the **only** package that writes SQL. Schema is numbered files in `migrations/` (never edit an applied one). One writer connection; write transactions go through `Store.tx`, multi-query reads through `Store.read`.
- `internal/index`: the background pipeline. Stages (metadata, thumbnail, embedding, faces, OCR, XMP export) hand work on through the durable `asset_jobs` queue in SQLite, not channels, so work survives a crash. Changing what a stage produces means bumping that stage's version constant, which re-runs only that stage. Politeness (battery, full screen, busy PC, idle) and the CPU budget live here.
- `internal/ai` never imports `store`. Models load only while there is work and take turns on one GPU lock (DirectML, CPU fallback).
- `internal/fileops`: every change to files in the user's folders (Recycle Bin, journalled moves, sidecars). Originals are never modified or permanently deleted.
- `internal/update`: installation policy, authenticated discovery and delivery, staging, activation and recovery. It never imports `store` and writes no SQL; `main.go` takes the library backup and runs the first-launch health gate. Its only network use is the opt-in updater, and `release-config.json` is empty until release engineering supplies trust, so a checkout makes no request. `cmd/lumen-launcher` (installed as the stable `Lumen.exe`) and `cmd/lumen-update` are built with `CGO_ENABLED=0` and depend on nothing in a replaceable runtime generation.
- `services/`: thin Wails services (Library, Search, People, Media, System, Compression, Update): argument checks, error codes, calls into `internal/*`.

### Cross-cutting rules that are easy to break

- **Visibility (blocking).** One rule feeds grids, search, counts, covers, media URLs and the video server: the store's `visible`, `labelBlocked` and `blockVisible`. A new input to them needs a trigger that bumps `visible_value`, and the kept counts (migration 0037: days, Things, people, job progress) must follow it too. Blocked media is served only while `services.BlockGate` is open.
- **Names and paths** compare with `core.Fold` / SQL `fold()` / `foldKey`, never `NOCASE`, and lists of names sort with `byName` (`core.CompareNames`: accents with their letters, numbers by value), never by bytes. The SQLite driver is the custom `sqlite3_lumen`.
- **Dates** are "wall-clock seconds": the camera's clock time encoded as UTC (`core.WallClock`). The UI formats everything in UTC.
- **Formats.** An extension is recognised in exactly one place, `internal/core/formats.go`. Code asks for a format's `core.Family`, never compares extensions; which decoder reads a family is `internal/media`'s choice.
- **Videos** stream from `services.Streamer` on 127.0.0.1 (random secret path, Host check), because Wails on Windows buffers a whole response before handing it to WebView2.
- **Search** is an in-memory int8 scan over vectors stored in SQLite; no vector extension is loaded. Events are grouped when read, not stored in a table.
- **User corrections always win** over the AI (people, faces, tags); the faces stage and clustering never undo them (`internal/people/cluster.go`).

### The Go → TypeScript contract

- `frontend/src/bridge/index.ts` is the only importer of `frontend/bindings` and `@wailsio/runtime` (ESLint enforces it). It re-exports generated types and normalises `null` lists.
- Values the bindings do not carry (constants, error codes, event names) are mirrored once in the bridge with a `// core.X` comment and checked by `services/contract_test.go`. A new mirror needs a new line in that test.
- **Error chain:** sentinel error in the detecting package → `services.errorCodes` → `ErrorCode` in the bridge → `codeText` in `shared/errors.ts` → an i18n key.
- **Every service must be registered through `main.go`'s `service()`**: Wails beta.26 ignores `application.Options.MarshalError` for bound methods, so error codes are lost otherwise (`TestServiceErrorsCarryTheirCodes` guards it).
- Events are `services.Event*` constants registered with `application.RegisterEvent[T]`; the bridge exposes one `on*` function per event that returns its unsubscribe.

### Frontend

A standalone Vue 3 app (not Nuxt) using Nuxt UI 4, Tailwind 4, Pinia 4 setup stores, vue-router and TanStack Virtual. TypeScript is strict with `noUncheckedIndexedAccess`; no `any`, no `as` except `as const`.

Import direction, enforced by ESLint: `bridge` ← `design` ← `shared` ← feature folders (`library`, `people`, `albums`, …) ← `app` (the shell). `shared/` never imports a feature; features never import `app/` and reach the shell through shared registries.

- Nuxt UI components are imported by name (`import UButton from '@nuxt/ui/components/Button.vue'`); auto-import is off.
- Colours, spacing and motion come from `design/tokens.css`; icons are names in `design/icons.ts` (only manifest icons are bundled; nothing is fetched).
- One owner each: preferences in `shared/preferences.ts` (saved in the library's SQLite settings, a field of `core.Preferences` with a default, a `Normalised` rule and a reset category); commands via `useCommands` (`shared/commands.ts`); shortcuts in `shared/shortcuts.ts`; notifications via `notify()`; full-window layers via `useLayer` (`shared/overlay.ts`).
- All user-facing text comes from `t('key')` in `shared/i18n.ts`.
- Engine lists use `defineListStore` / `defineCollectionStore` (`shared/listStore`). In the library store, announce `days` changes with `notifyDays()`, not `triggerRef`.
- Tests sit beside their file (`X.test.ts`), mock the bridge with `vi.mock('../bridge', …)`, and mount Nuxt UI components with `mountInApp` (`shared/testing.ts`). Overlay content is portalled to `document.body`.

## Conventions worth knowing up front

- Comments say why, in full sentences and **British English** (the linter checks UK spelling). Test names are sentences about behaviour.
- Never weaken a check to pass it: no new `//nolint`, `eslint-disable`, `@ts-expect-error` or cast without a stated reason on the line.
- Every Go function doing I/O, SQL or model work takes `ctx` first; use `QueryContext`/`ExecContext`. Logging is `log/slog`, lower-case messages, error under `"err"`.
- A behaviour change needs a regression test that fails without it.
- Line endings are LF everywhere (`.gitattributes`); when a script writes files on Windows, write bytes, not text mode.
- Everything ships in one installer: a new runtime dependency is pinned in `scripts/deps.sh` and lands in `deps/runtime/`, with its licence picked up by `scripts/notices.py`.

## Git rules

- Never add an AI assistant as a co-author, and never mention one anywhere: commit messages, pull requests, issues, comments.
- Commit subjects are Conventional Commits (`type(scope): summary`). `scripts/check-commit-msg.sh` enforces both rules, as the commit-msg hook and in CI.
