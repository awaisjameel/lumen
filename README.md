# Lumen

<img src="frontend/public/lumen.png" width="64" height="64" alt="Lumen aperture logo" />

A Windows-first desktop photo and video gallery that organises large libraries with on-device AI. Fully local: no accounts, no uploads, no telemetry. The AI models, FFmpeg and the city database ship inside the installer, and Lumen never downloads anything at run time.

- **Search in plain language** ("dog on a beach at sunset", "Ali and Sara at the beach"), with results in a fraction of a second.
- **People, Things, Places and Events** found automatically. Your corrections always win over the AI.
- **A fast timeline** for hundreds of thousands of photos, a full viewer and video playback for codecs WebView2 cannot play itself.
- **Camera RAW, HEIC, JPEG XL**, animated and multi-page images, and most video containers.
- **Privacy tools**: PIN-protected blocking of people, Things, places, events, albums, photos and folders.
- **Careful with your files**: originals are never modified. Removals go to the Windows Recycle Bin, and moves are previewed, journalled and undoable.

The full tour of what Lumen does is in [docs/features.md](docs/features.md).

## Privacy and the network

Lumen makes no network request. Photos, thumbnails, faces and search all stay on the PC.

The one exception is an **opt-in updater** (`internal/update`). It is built but inert: its trust configuration (`internal/update/release-config.json`) is empty, so builds from this repository never contact a server. Enabling it needs a real publisher certificate, quorum-held signing keys and hosting, as set out in [the production signing process](docs/production-signing.md). Until then, new versions are installed over the old one with the offline installer.

Settings → Updates and about → About Lumen links to the releases page, which opens in the browser only when clicked.

## Status

Lumen is in beta. The owner accepted beta 0.11.0 on 12 October 2026; 0.12.0 is the current candidate. Installers are published on the [releases page](https://github.com/awaisjameel/lumen/releases) as pre-releases, unsigned until a code-signing certificate is in place ([Releasing](#releasing)); you can also build one from source (below).

| Phase | Scope | State | Record |
| --- | --- | --- | --- |
| 0 | Spikes: libvips and HEIC, vector search, SigLIP 2, YuNet + SFace, PP-OCRv5 | Done | [spikes/README.md](spikes/README.md) |
| 1 | Fast library browser: scanner, watcher, job queue, thumbnails, timeline, viewer | Working | [audits](docs/history/README.md#audits) |
| 2 | Natural-language search and Things | Working | |
| 3 | People: face detection, recognition and clustering | Working | |
| 4 | Videos: metadata, previews, AI on frames, playback with conversion | Working | |
| 5 | Organisation: albums, places, events, blocking, Trash, duplicates, text in images, organise on disk, XMP sidecars | Working | [design](docs/history/phase5-design-2026-10-01.md), [blocking](docs/history/blocking-phase5-2026-09-30.md) |
| 6 | Polish and ship v1: installer, politeness, crash reports, onboarding, performance | Working | [design](docs/history/phase6-design-2026-10-03.md), [performance](docs/history/performance-2026-10-03.md) |
| 7 | Nuxt UI frontend and design system | Built | [design](docs/history/phase7-design-2026-10-04.md), [review](docs/history/phase7-review-2026-10-04.md) |
| 8 | Viewing, playback, details and file tools | Built | [record](docs/history/phase8-implementation-2026-10-05.md) |
| 9 | Camera RAW and broader formats | Built | [record](docs/history/phase9-formats-2026-10-07.md) |
| 10 | Customisation and preference UX | Built | [record](docs/history/phase10-customisation-2026-10-08.md) |
| 11 | Lossless compression (11A–D); same-format replacement with Undo (11E) is deferred | Built | [record](docs/history/phase11-compression-2026-10-09.md), [audit](docs/history/phase11-audit-2026-10-10.md) |
| 12 | Application updates; public signing and delivery remain open | Built, inert | [record](docs/history/phase12-implementation-2026-10-12.md) |
| | Interface redesign (October 2026) | Merged | [record](docs/history/redesign-2026-10-19.md) |

Every record, audit and plan is listed in [the history index](docs/history/README.md). Where a record and the code differ, the code and this README win.

### Known limits

- Windows 10 and 11 (64-bit) only. The macOS, Linux and mobile build templates carry the branding but are untested.
- The installer is not signed yet, so Windows SmartScreen warns before installing it.
- English only.
- Compression skips sources above 16 megapixels, and its history keeps the newest 100 runs.
- Developed RAW and rendered HEIC/TIFF viewing copies are limited to 4096 pixels on the long side. DNG with lossy-JPEG or deflate-compressed sensor data is not read.
- Same-format compression replacement and Undo are not included: compression saves new copies only.

## Installing

Download `Lumen-<version>-x64-setup.exe` from the [releases page](https://github.com/awaisjameel/lumen/releases), check it against the release's `SHA256SUMS.txt`, and run it. It installs Lumen for the current user in `%LOCALAPPDATA%\Programs\Lumen` with no administrator prompt, and carries Microsoft's WebView2 runtime for PCs without one, so installing needs no internet either. The library lives in `%LOCALAPPDATA%\Lumen`. The uninstaller asks whether to remove the library (no by default). Testers should read [the beta guide](docs/beta-testing.md).

## Building from source (Windows)

Needs Go 1.27.2, Node 22.22.2+ / 24.15+ / 26+ and npm 11.19+ (matching `frontend/package.json`), a C compiler for cgo, Git Bash, 7-Zip on `PATH`, and Python 3.14 (`ruff.toml` targets it). Toolchain used:

- Go 1.27.2 and [llvm-mingw](https://github.com/mstorsjo/llvm-mingw) (UCRT x86_64) unpacked under `%LOCALAPPDATA%\Programs`, both on `PATH`
- User environment: `CGO_ENABLED=1`, `CC=clang`, `CXX=clang++`
- Wails CLI pinned to the beta the project uses: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26`

Then, once, from Git Bash:

```sh
scripts/deps.sh   # ~1.2 GB of downloads, pinned and checked, into deps/ (git-ignored)
```

It fetches libvips 8.18 (static web build), ONNX Runtime 1.24.4 with DirectML, FFmpeg 9.0 (LGPL), SigLIP 2, the YuNet and SFace face models, the PP-OCRv5 text detector and English recogniser, the linters and NSIS (into `deps/tools/`), and the WebView2 runtime the installer carries (into `deps/installer/`). It keeps each shipped component's licence text (in `deps/licenses/`) and builds the camera RAW worker (`native/lumen-raw`, LibRaw compiled with the same clang). It then prepares the models (`scripts/prepare_models.py`): the vision tower gets a fixed batch shape so DirectML can compile it, the text tower's 256k-token table is stored as int8 (565 MB to 369 MB, agreeing with the original at cosine ≥ 0.9996), and SFace gets a variable batch size so every face in a photo is recognised in one run. Everything that ships beside `Lumen.exe` lands in `deps/runtime/` and is hard-linked into `bin/`.

Compression adds pinned libjpeg-turbo 3.1.2 and libjxl 0.12.0 tools in `deps/runtime/compress/`. OxiPNG 9.1.5 is built from its locked source with a pinned portable Rust 1.99.0 GNU toolchain and the same clang. The Rust compiler and Cargo cache stay in `deps/`; only the encoder ships.

Check the setup with `wails3 doctor`, then run `wails3 build` once: `Lumen.exe` embeds the built frontend (`frontend/dist`), so on a fresh clone no Go package compiles, and `go test` and `scripts/lint.sh` fail, until it exists. Line endings are pinned to LF by `.gitattributes` (gofmt and Bash need it), whatever `core.autocrlf` says.

Then turn on the repository's git hooks, once per clone, with `scripts/install-hooks.sh`. **commit-msg** holds messages to Conventional Commits (`type(scope): summary`) and refuses any that credit or mention an AI assistant. **pre-commit** checks the staged files' formatting (gofmt, Prettier, Ruff) and refuses this machine's absolute paths. **pre-push** runs the frontend check, Go vet and the documentation's links.

## Commands

```sh
wails3 dev                    # run with hot reload
wails3 build                  # production build -> bin/Lumen.exe
wails3 task package           # build + NSIS installer -> bin/Lumen-<version>-x64-setup.exe
source scripts/env.sh         # before go test / vet / run: puts deps/runtime (libvips) on PATH
go test -race -count=1 -timeout 900s . ./internal/... ./services/... ./cmd/...   # wiring, engine, service and release-tool tests
scripts/lint.sh               # static checks, frontend tests and large-library latency (--fix applies fixes first)
scripts/bench.sh              # 100,000-photo store latency gate alone (one to two minutes)
cd frontend && npm run check  # the frontend's alone: vue-tsc, ESLint, Prettier
npm run test                  # Vitest (jsdom)
node e2e/run.mjs              # end-to-end suites on bin/Lumen.exe in its WebView2 (after wails3 build)
```

Name the Go packages rather than `./...`: `build/` holds Wails' mobile templates, which only build for their own platforms, and `spikes/` is its own module. Tests that need real models skip cleanly without `deps/runtime`.

The frontend must finish building before Go embeds `frontend/dist`; never run the frontend and Go production builds concurrently. After changing an exported service method or a wire type, regenerate the bindings with `wails3 generate bindings -f '-tags production' -clean=true -ts -i` (as the build does) and check `git diff -- frontend/bindings`.

`wails3 task package` writes `bin\THIRD_PARTY_NOTICES.txt` (generated from the build by `scripts/notices.py`) and builds the installer from `build/windows/nsis/project.nsi` with the NSIS that `deps.sh` pins. To sign Lumen.exe, the installer and the uninstaller, pass `SIGN_THUMBPRINT=` (a code-signing certificate in the Windows certificate store, where a hardware token's or a signing service's appears) and `TIMESTAMP_SERVER=`, with the Windows SDK's `signtool` on `PATH`. The version and the copyright are set once, in `build/config.yml`. `scripts/version.sh bump` changes the version (see [Releasing](#releasing)); after changing the copyright, run `wails3 task common:update:build-assets` and restore `license: "PolyForm-Noncommercial-1.0.0"` in `build/linux/nfpm/nfpm.yaml`, which that task resets to the template's (`TestVersionsAgree` and `TestLicenceAndCopyrightAgree` check both). The template's Docker, server and MSIX tasks are left out of `Taskfile.yml`: Lumen needs cgo with the Windows libvips, and its installer must carry the runtime folder.

Component tests run in jsdom. Codec playback, native full screen and files moving on disk are checked by the end-to-end suites in [e2e/](e2e/README.md), which drive the built app over the DevTools protocol on scratch libraries with fixtures they generate themselves, one suite per use case: onboarding, a tour of every screen, interface, collections, organise, viewer, file tools, branding, formats, customisation, compression and updates. Installer acceptance is in [e2e/installer](e2e/installer/README.md).

Handy environment variables:

- `LUMEN_DATA_DIR`: use another data folder (default `%LOCALAPPDATA%\Lumen`). A scratch library runs beside the user's Lumen rather than handing over to it.
- `LUMEN_RUNTIME_DIR`: where the DLLs, `ffmpeg\` and `models\` are (default: beside `Lumen.exe`).
- `LUMEN_DEVTOOLS_PORT`: expose the WebView's DevTools protocol on `127.0.0.1` so UI tests can drive the real app. Off by default.
- `LUMEN_FACE_PHOTOS` (tests only): a folder of real photos of people. The face tests run on it and skip without it; keep such photos out of the repository.
- `LUMEN_RAW_SAMPLES` (tests only): a folder of real camera RAW files. The RAW tests run on it and skip without it; `e2e/fixtures/fetch_raw.py` downloads a pinned CC0 set into `e2e/.cache/raw`.

## Releasing

A release is a version bump merged into `main`, then a tag that the release workflow builds into a draft for review:

1. On an up-to-date `main`, run `scripts/version.sh bump 0.13.0`. It moves to a `release/v0.13.0` branch, sets the version in `build/config.yml`, regenerates the build assets (restoring the nfpm licence), runs `TestVersionsAgree` and `TestLicenceAndCopyrightAgree`, and commits `chore(release): Lumen 0.13.0`. Push the branch, open a pull request and merge it once CI passes.
2. On `main`, with that merge pulled, run `scripts/version.sh tag`. It refuses unless `main` matches `origin/main` and the version is untagged, then pushes the annotated tag `v0.13.0`.
3. The tag starts [the release workflow](.github/workflows/release.yml), which:
   - checks the tag matches `build/config.yml` and sits on `main`;
   - runs CI's checks;
   - builds the installer, signed when signing is configured;
   - creates a **draft** release with the installer, the runtime archive, `THIRD_PARTY_NOTICES.txt`, `SHA256SUMS.txt` and, once the repository is public, signed build provenance (`gh attestation verify`).

   An unsigned build is marked as a pre-release, and its notes tell users how to check the download.
4. Review the draft: the generated notes, the files, and the installer on a test PC. Then publish it. Published files are never replaced: a fix is a new version. Turning on GitHub's immutable releases (Settings → General → Releases) enforces that.

Signing in the workflow switches on with the repository variables `SIGN_THUMBPRINT` and `TIMESTAMP_SERVER`, once a signing provider's setup step is added ([production signing](docs/production-signing.md#signing-in-the-release-workflow)). The updater's authenticated feed is a separate, local step with offline keys, described in the same document; the release workflow never changes the updater's trust configuration.

## Code quality

`scripts/lint.sh` runs the static checks, the frontend tests, the documentation's links and the large-library latency gate. It runs every step and reports all failures at the end; `scripts/lint.sh --fix` first applies every automatic fix.

CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) runs on every pull request and every push to `main`, on GitHub's Windows runners, with the toolchain and `deps/` set up as [Building from source](#building-from-source-windows) describes ([.github/actions/setup](.github/actions/setup/action.yml)). It checks the pull request's commit messages, runs `scripts/lint.sh --no-bench` and the Go race tests, and makes a production build, which must leave the bindings and `go.mod` as committed. It also runs the latency gate, but only reports it in the job summary: the runners are slower than the machine the budgets were set on, so `scripts/lint.sh` on a developer's machine stays the binding gate. Each tool is pinned and configured in the repository:

| Code     | Formatting       | Linting                                                                                                                                                       | Config                                                                             |
| -------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Go       | gofmt, goimports | golangci-lint 2.14 (vet, staticcheck, errcheck, gosec, errorlint, revive and more), deadcode, govulncheck                                                     | `.golangci.yml`                                                                    |
| Frontend | Prettier 3.9     | ESLint 10 with typescript-eslint (strict, type-aware) and eslint-plugin-vue, plus `vue-tsc`; import boundaries between `design`, `shared`, features and `app` | `frontend/eslint.config.js`, `frontend/.prettierrc.json`, `frontend/tsconfig.json` |
| Python   | Ruff             | Ruff                                                                                                                                                          | `ruff.toml`                                                                        |
| Shell    |                  | ShellCheck                                                                                                                                                    |                                                                                    |

The Go tools, Ruff and ShellCheck live in `deps/tools/` (from `scripts/deps.sh`); the frontend's are dev dependencies (`npm ci`). npm enforces a seven-day release-age window through `min-release-age=7` in `frontend/.npmrc`, and engine checks reject unsupported Node and npm versions. Exclusions are rare and each says why, in the config or on a `//nolint:linter // reason` line. Editors do the same on save: `.editorconfig` sets line endings and indentation, and `.vscode/settings.json` runs these formatters and linters from the pinned copies (install the recommended extensions).

How code is written (errors, context, SQLite, logging, the Go-to-TypeScript contract, Pinia and Vue, tests) is set out in [CONVENTIONS.md](CONVENTIONS.md). [AGENTS.md](AGENTS.md) summarises the architecture and the rules that are easy to break, for anyone (or any tool) about to change the code.

The latency gate seeds 100,000 photos once and times store reads and writes with no blocks, a folder block, and a folder, Thing and person block. It includes thousands of events and a day with 10,000 imported photos. Every P1 finding in the [bottleneck audit](docs/history/bottleneck-audit-2026-10-13.md) was invisible at 3,000 photos; this step keeps the product's acceptance size in the regular gate. Allow one to two minutes on an i7-13700H with a warm toolchain. The readable ceilings in `internal/store/testdata/library-budgets.txt` catch substantial regressions and allow ordinary timing noise; a missing or unused budget also fails. Raise a ceiling only by hand, with a reason and at least five repeat measurements in review, using the headroom rule in that file. A failing run is a reason to investigate, not to raise a budget.

## Layout

```text
main.go              wiring: single-instance hand-over, paths, logging, store, AI, indexer, services, window
cmd/lumen-launcher   the stable Lumen.exe the installer creates; selects the installed runtime generation
cmd/lumen-update     the update helper: activates a prepared runtime, never touches the library
cmd/lumen-trust      offline release tool: role keys and a quorum-signed update root
cmd/lumen-feed       release tool: prepares authenticated update metadata locally, never publishes
internal/core        domain and wire types, paths, the format catalogue, the wall-clock date convention, case folding, name order, preferences
internal/store       SQLite schema (migrations/), all queries, job queue, tags, embeddings, kept counts
internal/scanner     folder walk, hashing, Windows folder watcher
internal/media       EXIF, filename dates, decoders, thumbnails and their cache, perceptual hash, rendering
internal/media/vips  the libvips calls (cgo), one C function per job
internal/media/wic   Windows Imaging Component decoding (HEIC)
internal/compress    verified lossless candidates, durable compression coordination and restoration
internal/ai          ONNX Runtime sessions, SigLIP 2 image/text encoders, tokenizer, face detection/alignment/recognition, OCR
internal/search      in-memory int8 vector index, search, Things vocabulary and tagging
internal/people      face clustering (incremental, constrained by the user's corrections), names in searches
internal/places      offline reverse geocoding: the bundled GeoNames cities (cities.tsv.gz)
internal/index       background pipeline: scan loop, stage workers (OCR, XMP export, cache eviction too), model loading, CPU budget, politeness, events
internal/fileops     every change to files in the user's folders: Recycle Bin, organise plans and journalled moves, sidecars
internal/update      updater policy, authenticated discovery and delivery, staging, activation and recovery (inert without release trust)
internal/diag        crash reports, the problem report, the PC's description, Lumen's version
internal/signin      Windows sign-in check for a forgotten PIN (Windows Hello, or the account password)
internal/xmp         the XMP sidecar packet (pure Go)
internal/parallel    the worker pool loops run on
internal/testenv     finds deps/runtime for tests, skipping cleanly without it
native/lumen-raw     the camera RAW worker (C++, LibRaw linked statically); scripts/deps.sh builds it into deps/runtime
services/            Wails services (Library, Search, People, Media, System, Compression, Update), the block gate, the loopback video server, error codes
frontend/src/bridge  the only place the UI calls the engine
frontend/src/design  design tokens (tokens.css), the icon manifest, how the appearance preferences are applied
frontend/src/shared  compositions (Page, PageHeader, FeatureState, CollectionCard, MediaTile, SelectionDock…), preferences, commands, shortcuts, overlays, notifications, i18n
frontend/src/app     the shell: navigation, global overlays, command palette, shortcut reference; a development-only component showcase
frontend/src/{library,search,media}   timeline, viewer, search, file tools and filmstrip
frontend/src/{people,things,places,events,albums}   the collections
frontend/src/{duplicates,compression,trash,organise}   the tools
frontend/src/{settings,onboarding,about,updates}   Settings, first run, About Lumen and crash notice, application updates
e2e/                 end-to-end suites on the built app, their fixture generators, installer acceptance and perf scripts
scripts/             deps.sh and its parts, prepare_models.py, env.sh, lint.sh, bench.sh, version.sh, the hook checks, notices.py, installer and update payloads, release.ps1, data builders
.github/             CI and release workflows, and the shared toolchain set-up they use
.githooks/           commit-msg, pre-commit and pre-push (scripts/install-hooks.sh turns them on)
build/               Wails build config (config.yml holds the version), icons, the NSIS installer in build/windows/nsis
docs/                the feature tour, beta guide, production signing, branding, GeoNames provenance, the original plan; docs/history holds the dated records
spikes/              Phase 0 experiments and later feasibility probes (own Go module; models in spikes/.cache)
```

## How it works

**Indexing.** Files are identified by an xxh3-128 hash of their contents (files over 64 MB hash their size and three 4 MB windows). Each asset goes through metadata, thumbnails (256 and 1024 px, plus a 64-bit perceptual hash), the image embedding, faces, text recognition and XMP export, through a durable job queue in SQLite: newest first, on-screen days first, resumed after a crash. Changing what a stage produces bumps its version, which re-runs only that stage. Indexing runs below normal priority on half the processor's threads by default, rests the GPU a third of the time, and waits on battery, behind a game or full-screen video, and while other programs keep the processor busy.

**Dates.** EXIF dates have no time zone, so every date is stored as "wall-clock seconds": the camera's clock time encoded as UTC (`core.WallClock`). The UI formats everything in UTC, so a photo taken at 23:30 stays on its own day.

**Tags.** A Thing applies to a photo when SigLIP's probability for its best prompt passes the confidence setting (default 0.02; scores from 0.002 up are stored, so the slider needs no re-scan). Generic concepts rarely pass on probability alone, because SigLIP learned from specific web captions, so ten broad kinds of photo are also assigned by best fit. On one real library this found 94% of the people photos, against 16% on probability alone. Prompts were chosen by measuring them on that library.

**People.** Clustering joins groups of faces while their average cosine similarity is at least 0.42 (Phase 0 found every group at that threshold held one person). A new person needs clear faces in three photos: sharpness (variance of the Laplacian of the aligned face) at least 15, head turn at most 0.45 eye distances. On 204 office photos with 1,000 faces, 92% were placed into 28 people, each checked by eye to hold one person; the rest wait, unplaced, for more photos of them. The faces stage and clustering never undo the user: see `internal/people/cluster.go`.

**Places.** City names come from [GeoNames](https://www.geonames.org/), licensed under CC BY 4.0. Lumen bundles a reduced 30 September 2026 `cities5000` snapshot (69,760 populated places), with region and country names, so lookups make no network request. A city within 75 km is an approximate label, not an exact administrative boundary. See [data provenance](docs/geonames.md).

**Visibility.** One rule feeds grids, search, counts, covers, media URLs and the video server, so a blocked item disappears everywhere at once. Blocked media is served only while the PIN-protected Blocked settings are open.

## Deviations from the plan

[The original plan](docs/plan.md) set the vision and roadmap. These decisions depart from it:

- **Source-available, not open source** (decided 15 October 2026): the plan said open source from day one. Lumen is licensed under the PolyForm Noncommercial License (see [Licence](#licence)).
- **TypeScript 6.0**, not 7: `vue-tsc` can't drive the Go-native TS 7 compiler yet. Revisit when Volar supports it.
- **Pinia 4** (plan said 3; 4 is the current stable).
- **Search is an in-memory int8 scan**, not sqlite-vec: SQLite stores the vectors, a flat int8 copy in memory is searched (Phase 0: 17 ms at 300k vs 351 ms). No vector extension is loaded.
- **Search results are ordered by relevance**, not by date: SigLIP's probabilities vary too much between queries for a fixed cut-off to work, and a relevance order keeps weak matches at the end.
- **libvips is the static "web" build** (one 18 MB DLL, no ImageMagick), called through Lumen's own small cgo wrapper (`internal/media/vips`, one C function per job) rather than govips: BMP goes through Go, HEIC through WIC or FFmpeg (libvips reads its colour profile).
- **Events are grouped when read**, not stored in an `events` table: consecutive visible media split by the time gap or a 100 km jump. The list is kept in memory; triggers record the dates a change touched (migration 0033), and only the runs of photos around them are regrouped, with a whole regroup when the gap changes or the record has been outrun. What the user sets on an event (name, pin, block) is an event tag whose members are the event's photos, kept equal to the event as it regroups (`store/eventtags.go`).
- **Large files are hash-sampled**: files over 64 MB (mostly videos) hash size plus three 4 MB windows instead of every byte.
- **Folder watching** uses `ReadDirectoryChangesW` directly (fsnotify has no recursive watch on Windows yet).
- **One writer connection** instead of a writer goroutine: SQLite writes serialise on it, and callers batch hundreds of rows per transaction.
- **Stages hand work on through SQLite job queues**, not in-memory channels: each stage reads bounded batches of its pending jobs, so work survives crashes and restarts, and the batches bound memory as channels would.
- **People are clusters, named or not** (no separate `cluster_id`): a new group of faces is an unnamed person, and naming is how the user confirms it. Face vectors live in the `faces` table (float32 BLOBs), not a vector extension.
- **New faces cluster with named and unnamed people together**, rather than matching named people first: named people are never merged away automatically, and this order was tuned on real photos.
- **About 180 built-in Things**, not 300–1,000: each was kept because it measured well on a real library. More are added only with measurement.
- **Faces are decoded at 2048 px** for their stage (not the 1024 px preview): nearly every face is then big enough to recognise. Detecting on tiles as well found only 3% more faces, for five times the work.
- **Videos stream from a loopback server**, not the app's asset handler: on Windows, Wails buffers each response until it is complete, which would hold a whole conversion in memory before playback started.
- **Names and paths compare with Unicode case folding** (`core.Fold`, also the SQL function `fold()`), not SQLite's `NOCASE`, which folds only A–Z: "Émile" and "émile" are one person, as Windows treats "ÉTÉ" and "été" as one folder.
- **AI models ship beside `Lumen.exe`** (`models\` in the runtime folder), read-only from the installer, rather than in the per-user app-data folder; the database and thumbnail cache are in `%LOCALAPPDATA%\Lumen`.
- **Text in images is in v1**, not "Later", and only for photos the AI sorts as screenshots or documents. It is searched with SQLite FTS4, which go-sqlite3 includes by default; FTS5 would need a build tag on every build and test.
- **Removing files goes to the Windows Recycle Bin**, never a permanent delete, and only after the user confirms. Duplicates pre-select the best copy (the plan's "quality/best-shot picking" for copies), but nothing moves until the user confirms.
- **Camera RAW is in scope** (the plan parked it) **and runs in a worker Lumen builds itself** (decided 7 October 2026): LibRaw's source is pinned and compiled by `deps.sh` with the same llvm-mingw clang cgo uses, into one static `lumen-raw.exe`. LibRaw's own Windows binaries need the Visual C++ runtime, which a clean PC may lack. It is used under its CDDL-1.0 option. No JPEG or zlib library is compiled in, so lossy-JPEG and deflate-compressed DNG are not read.
- **The FFmpeg build is the stock LGPL one** (150 MB unpacked, mostly codecs Lumen never uses). Trimming it needs a from-source LGPL build of FFmpeg, a toolchain of its own; packed in the installer the stock build is about 63 MB of roughly 0.9 GB. Revisit if the installer's size matters more than that build.
- **Updates are opt-in and inert by default** (decided 4 October 2026, replacing the 3 October decision to have no updater): checks happen only when the user enables them or asks, downloads only when requested, and only against compiled-in release trust, which is empty until production signing is in place. No other feature makes a network request.
- **Installed per user** (decided 3 October 2026), not in Program Files: the library is per user already, and installing or updating needs no administrator.
- **The installer carries the WebView2 runtime** (decided 3 October 2026, about 200 MB of it) rather than Wails' downloading bootstrapper, so installing needs no internet either.

## Licence

Lumen is licensed under the [PolyForm Noncommercial License 1.0.0](LICENSE) (SPDX `PolyForm-Noncommercial-1.0.0`): free for any noncommercial purpose, including personal use and use by charities, schools and public bodies, but not for commercial use. Its source is available, but it is not an open-source licence. The components it ships keep their own licences, listed in `THIRD_PARTY_NOTICES.txt` beside `Lumen.exe` (Settings → Updates and about → About Lumen → Licences).
