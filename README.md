# Lumen

<img src="frontend/public/lumen.png" width="64" height="64" alt="Lumen aperture logo" />

A Windows-first desktop photo gallery that organises large libraries with on-device AI. Fully local: no accounts, no uploads, no telemetry. See [the plan](Local%20AI%20Photo%20Gallery%20—%20End-to-End%20Plan.md) for the full vision and roadmap.

Brand assets and their regeneration workflow are documented in [the branding guide](docs/branding.md).

## Status

**Phase 1 (fast library browser)** is working:

- First-run folder picker with suggested folders; sources never overlap
- Scanner: parallel content hashing (xxh3-128), date guess from file names, skip hidden/system folders
- Recursive folder watching (`ReadDirectoryChangesW`) with debounced rescans, plus a reconcile scan on startup
- Durable job queue in SQLite (`asset_jobs`): metadata, thumbnails, then the AI, newest first, **on-screen days first**; resumes after a crash; changing a stage's version re-runs only that stage
- The sidebar status says what indexing is doing; hovering it lists what is at work now and what comes next, and which files couldn't be read and why (damaged, too slow to read, locked by another app)
- EXIF (date, size, orientation, camera, GPS) via imagemeta, with goexif as a fallback for JPEG/TIFF
- Thumbnails (256 and 1024 px) plus a 64-bit perceptual hash, served at `/media/thumb/{id}/{size}`
- Virtualized, day-grouped grid with a timeline scrubber, keyboard navigation, zoom
- Viewer (instant preview, then the original), details panel, favourites, light/dark themes
- Exact duplicates collapse into one asset with several file paths

**Phase 2 (search and Things)** is working:

- Thumbnails through **libvips** (JPEG, PNG, WebP, GIF, TIFF, AVIF, shrink-on-load); **HEIC** through Windows' decoder (WIC), falling back to the bundled FFmpeg; BMP through Go; camera RAW through LibRaw and JPEG XL through the bundled native decoder
- **SigLIP 2** image embeddings on the GPU through DirectML (CPU fallback), batches of 16, loaded only while there is work
- **Natural-language search** from the search box (Ctrl+F): results best first, cut relative to the best match so a query matching nothing returns nothing; an in-memory int8 index answers in milliseconds
- **Things**: about 180 built-in labels, the broad kind of each photo (People, Screenshots, Documents…), labels users add ("Eiffel Tower", found across the library at once), tags users add or remove on any photo (their edits always win), a confidence slider
- AI work waits while on battery (a setting allows it), and pauses with the rest
- Indexing gives way to the user's programs: Lumen runs below normal priority, its stages share half the CPU's threads between them (Settings → Indexing and performance lets the user give it fewer or more, at once), FFmpeg decodes with two threads (long videos are sampled by seeking to each frame), and the GPU, which also draws the screen, rests a third of the time. A playing video still converts at normal priority

**Phase 3 (people)** is working:

- **Faces**: YuNet finds faces (with five landmarks) in each photo decoded at 2048 px; each is aligned to SFace's template and turned into a 128-dim identity vector. Both models run on the GPU through DirectML (CPU fallback; the two give the same faces and vectors), taking turns with the image model, loaded only while there is work; a 1,466-photo library took about 45 s on a laptop
- **People**: faces are grouped by average-linkage clustering (a group's similarity is the dot product of mean vectors, so merging is cheap), incrementally as photos arrive. Two faces in one photo are never one person; blurred or turned-away faces may join someone but never start a new person
- **User corrections always win**: naming, merging (naming someone with a name already taken asks "same person?"), "not this person" (never put back), moving faces to someone else or a new person (splitting), choosing their picture, blocking (Phase 5). A face placed by the user never moves; a named or blocked person is never merged automatically
- **People view**, a person's photos (their page is the timeline, filtered), a face review that lists the least-alike faces first, people in the details panel ("Who is this?" for faces not recognised yet)
- **Search by person**: names in a search narrow it to photos of all of them ("Ali and Sara at the beach"); names alone give their photos, newest first. The search box suggests people as you type

**Phase 4 (videos)** is working:

- Videos are included when the user enables them in Settings. Bundled FFmpeg and ffprobe read capture date, dimensions, duration, codecs, camera, and location without changing the original file.
- Indexing makes a poster and a 4×4 hover preview sprite. SigLIP 2 and face recognition share one streaming frame-sampling pass (about one per five seconds, at most 24), with independent photo/video versions and stage retries. Search results jump to the best matching moment.
- The viewer plays supported files directly and converts other codecs to H.264/AAC while playing. Seeking a converted video starts a new stream at that point; converted copies are not kept on disk. Video streams from a small server on 127.0.0.1 (a random secret in every path), because the app's own origin hands WebView2 a response only once it is complete.

**Phase 5 (organization) is working:**

- **Albums**: manual albums collect photos and videos without moving files. Membership follows an asset when its file is renamed or moved; the Details panel adds or removes the selected item. Smart albums save a rule for media kind, favourites, inclusive capture dates, a Thing and a person, and update as the library changes. Albums have live counts, covers, rename/edit and deletion.
- **Places** groups GPS-tagged media by nearby city using a bundled, offline GeoNames index. Existing libraries are resolved in the background after an upgrade. Places show live visible counts and open filtered timelines.
- **Events** groups visible media after a configurable time gap (12 hours by default) or a location jump over 100 km, and regroups as the library changes. What the user says about an event (its name, a pin, a block) follows its photos: a split keeps it on both halves, and photos that join take it on. Reset restores the automatic name.
- **Pins and renames**: Things and Events can be pinned (pinned first, newest pin first, kept across restarts). Things can be renamed; a renamed built-in keeps its AI prompts and matches and goes by the new name everywhere, search included, and "Reset name" restores the original.
- **Blocking** covers people, single faces (optionally with their look-alikes), Things, places, events, albums, photos/videos and folders. A label can be hidden alone or with the photos it covers; photos and folders always hide media. One visibility rule feeds grids, search, counts, covers, media URLs and the video server. Search reports exact blocked person, Thing, place and album names, with whole-word Unicode matching and longer-name precedence.
- **Settings → Blocked** is PIN-protected: attempts are rate-limited, and it locks after 10 idle minutes or when the user leaves it. It lists every block with mode changes and unblocking (a new block offers a brief Undo), and opens each block's covered photos in the Viewer. A forgotten PIN is reset through Windows sign-in: Windows Hello when it is set up, otherwise the account password. Hidden people and user-hidden Things from earlier versions became photo blocks on upgrade.
- **Trash**: "Move to Trash" in the Viewer and Details panel needs no confirmation and offers Undo. Trash restores items, or deletes them from disk to the Windows Recycle Bin after a confirmation; nothing is ever emptied automatically. A recycled file's asset keeps its names, tags and albums in case the file comes back.
- **Duplicates** lists exact copies and likely similar photos (from the perceptual hashes) and pre-selects the best copy: highest resolution, then the original camera file and format, file size, metadata, and age. Nothing happens until the user confirms: similar photos go to Trash, and extra files of exact copies go to the Recycle Bin.
- **Text in images** (off by default): Lumen reads the text in photos the AI sorts as screenshots or documents (PP-OCRv5, English, on the CPU) into SQLite full-text search. Photos whose text has every word of a query come first in search, then the semantic results.
- **Organise on disk** (Settings → Organise files on disk) moves files into a folder scheme built from a template (`{Year} {Month} {MonthName} {Day} {Event} {Place} {Camera} {Name} {Ext}`, default `{Year}/{Month}/{Event}/{Name}`), each file within its own source folder. A preview lists every move, skip and renamed collision first. Nothing is ever overwritten: a name collision gets ` (2)`, ` (3)`… Each move is journalled, so a run survives a crash and can be resumed or undone, and scans wait while files move. Blocked, trashed and missing files are left alone, and blocked names never become folder names.
- **XMP sidecars** (off by default): a background pass writes `IMG_1.jpg.xmp` beside each photo with its Things, its named people and their face regions, and a 5-star rating for favourites. Lumen rewrites only sidecars it wrote and never touches an original. Blocked labels, and blocked, hidden or trashed photos, are never exported. **Delete Lumen’s sidecar files** in Settings takes them all back: it turns the setting off and moves every sidecar Lumen wrote, and that nobody has edited since, to the Recycle Bin.
- **Thumbnail cache cap**: thumbnails and video previews stay under a cap (2 GB by default, set in Settings). The least recently viewed are removed first, and a removed thumbnail is rebuilt the next time it is shown.

Design and decisions for the last part of Phase 5 are in [docs/phase5-design-2026-10-01.md](docs/phase5-design-2026-10-01.md); the original blocking scope is in [the blocking note](docs/blocking-phase5-2026-09-30.md).

**Phase 6 (polish and ship v1) is working; the beta (0.9.0) is next:**

- **Installer** (`task package`): `bin\Lumen-0.9.0-x64-setup.exe` (872 MB, about a minute to install) installs Lumen per user, in `%LOCALAPPDATA%\Programs\Lumen`, with no administrator prompt. It carries the runtime folder (DLLs, FFmpeg, models) and Microsoft's standalone WebView2 runtime, which it installs only on a PC without one, so installing never needs the internet. It refuses to replace or remove a running Lumen. The uninstaller removes only what Setup put there, and asks whether to remove the library too (no by default; a silent uninstall keeps it). The installer and Lumen.exe are signed when a certificate is configured; there is none yet.
- **No network, still**: Lumen never checks for updates. Settings → About Lumen shows the version and the releases page, which opens in the browser only when clicked.
- **Politeness**: besides the manual pause and waiting for mains power, indexing waits while a game, full-screen video or presentation is in front, and while other programs keep the processor above 70 % for 30 seconds (until they fall to 50 %). Both are on by default. Advanced: index only while the user is away (no input for 5 minutes). Lumen's own work never counts as "other programs": the process and everything it starts share one Windows job, whose processor time is measured. The sidebar status says why indexing waits.
- **Crash reports and diagnostics, local only**: a crash's account from the Go runtime is kept in `crashes\` beside the library, and the next start says Lumen closed unexpectedly. A panic in a call from the UI is kept as a report and the app runs on. The UI's own failures reach `lumen.log`, rate-limited, and an unexpected one tells the user. Settings → About Lumen opens the log folder and saves a problem report (the log, crash reports and a description of the PC and library, no photos), which the user may choose to share.
- **Notices**: `THIRD_PARTY_NOTICES.txt` beside Lumen.exe (Settings → About Lumen → Licences) is generated from the build itself (`scripts/notices.py`): the native components and models `deps.sh` pins, the Go modules linked into Lumen.exe and the packages bundled into the UI, each with its licence text.
- **Performance pass** ([docs/performance-2026-10-03.md](docs/performance-2026-10-03.md)), measured in the real app: cold start to a usable grid in about a second, 60 fps scrolling, searches in 120–200 ms once the text model is loaded, 454 MB in all when idle. On mains power 3,000 photos are browsable in 55 s and fully understood in 264 s on the integrated GPU, with indexing on half the processor.
- Onboarding, empty states and the beta guide for testers ([docs/beta-testing.md](docs/beta-testing.md)). Design and decisions: [docs/phase6-design-2026-10-03.md](docs/phase6-design-2026-10-03.md).

**Phase 7 (the Nuxt UI frontend and design system)** is built
([record](docs/phase7-design-2026-10-04.md), [baseline inventory](docs/phase7-inventory-2026-10-04.md), [implementation review](docs/phase7-review-2026-10-04.md)):

- Every screen is rebuilt on Nuxt UI 4 and Tailwind CSS 4 inside the same standalone Vue app: one shell (resizable, collapsible sidebar; icon rail at medium width; navigation drawer when narrow), one page header, one set of loading, empty and failure states, one card, tile, selection bar and settings row.
- Design tokens for light and dark themes, six accent colours, comfortable and compact density and a 90–130 % text size, all in Settings → Appearance. They are saved with the library (SQLite), not in the WebView; an older Lumen's theme and thumbnail size are carried over once.
- A command registry behind toolbars, menus, shortcuts and the command palette (Ctrl+K); a shortcut reference (`?`); one overlay coordinator; one notification channel (toasts, with Undo).
- Icons, fonts and styles are local: the build bundles only the icons the manifest names, and nothing is ever fetched. Reduced motion and Windows contrast themes are honoured.
- Contracts for later phases: per-asset media capabilities (`AssetDetail.caps`), the typed preference contract, and feasibility records for [RAW and rendered images](docs/phase7-media-feasibility-2026-10-04.md) and [the updater](docs/phase7-updater-feasibility-2026-10-04.md). No updater exists yet and Lumen still makes no network request.

**Phase 8 (viewing, playback, details and file tools)** is implemented locally:

- Image fit/actual size, pointer-centred wheel/pinch zoom and pan, fullscreen, a virtual filmstrip, slideshow, comparison and reversible display rotation/flip. Canonical source and AI pixels remain unchanged.
- Cancellable HEIC/TIFF viewing renders report the available resolution. Native workers are bounded to 4096 pixels on the long side; very large sources retain their preview until tiled decoding is available.
- Configurable playback, audio conversion and text subtitles, opt-in resume history, and explicit frame export. The Blocked browser does not record playback history.
- A resizable metadata inspector, multi-selection across virtual pages, shared batch actions, validated Windows hand-off, previewed rename/move with journal recovery and Undo, and original/JPEG/PNG export with per-file outcomes.
- Implementation, verification evidence and limits: [Phase 8 record](docs/phase8-implementation-2026-10-05.md).

**Phase 9 (camera RAW first, then broader formats)** is implemented locally:

- **Camera RAW**: Adobe DNG, Canon (CR2, CR3, CRW), Nikon (NEF, NRW), Sony (ARW, SR2, SRF), Fujifilm (RAF), Olympus and OM System (ORF), Panasonic (RW2) and Pentax (PEF). LibRaw develops the sensor data in a worker process of its own (`lumen-raw.exe`), with a memory budget and a lifetime per file. One neutral development (the camera's white balance and colour matrix, sRGB, upright) feeds thumbnails, the perceptual hash, the AI and the viewer, so it does not have the camera's own JPEG look. A file LibRaw cannot develop shows its embedded camera preview, labelled as one; a file it cannot read at all is skipped with a reason. A RAW and the JPEG beside it stay two photos, and are never offered as similar copies.
- **One format catalogue** (`internal/core/formats.go`) names every extension, its kind, its family and what the WebView is sent; the scanner, decoders and services read it.
- **JPEG XL** uses the bundled native decoder (extended in Phase 11 to retain colour profiles, transparency and EXIF); **animated PNG** joins the animations the WebView plays from the original; a **multi-page TIFF** is turned through page by page in the viewer.
- **More video containers**: MPG/MPEG, TS, OGV and ASF, converted as they play. A TypeScript source named `.ts` is told from a transport stream by its first packets.
- The tested camera matrix, measurements, decisions and limits: [Phase 9 record](docs/phase9-formats-2026-10-07.md).

**Phase 10 (customisation and preference UX)** is implemented locally:

- **More to choose**: motion, the viewer's background, the screen Lumen opens on, how thumbnails fill their squares and which badges they carry, how photos open and what the wheel does, held animations, viewing copy quality, video looping and subtitles from the start, a stronger focus ring.
- **Keyboard shortcuts** can be reassigned, with collision checks; Escape and Tab keep their jobs.
- **Photo formats** can be left out of indexing without losing what the library already holds; viewing copies have their own size and Clear button.
- **Export presets**, a Reset for each settings section and for all, and **customisation files** that carry the interface preferences between libraries with a preview before anything is applied. A file holds no folder, history, PIN or block and cannot turn on a feature that writes files.
- What each preference is, its default, range, owner and limits: [Phase 10 record](docs/phase10-customisation-2026-10-08.md).

**Phase 11 (lossless image compression)** is implemented locally through 11A–D:

- Individual and bulk actions in the grid, viewer and inspector open the Tools workspace. Analyse first, review measured results, then save selected new copies. Originals remain unchanged.
- Compatible JPEG/PNG optimisation, reversible JPEG-to-JPEG-XL with byte-exact JPEG restoration, and conservatively supported PNG-to-JPEG-XL conversion use pinned offline encoders and independent preservation checks.
- Durable per-file history, pause/cancel, restart recovery, duplicate-copy choice, destination collision protection, related-copy annotations, and portable compression defaults with category reset.
- Supported inputs, measurements, verification and limits: [Phase 11 record](docs/phase11-compression-2026-10-09.md). Optional same-format replacement and Undo (11E) remain deferred.
- Sources above 16 megapixels are skipped for now, and history keeps the newest 100 runs: [Phase 11 audit](docs/phase11-audit-2026-10-10.md), which corrects the record where they differ.

**Next development is planned:** automatic update checking with background
preparation and a user-approved restart. The updater's limited network exception and
recovery design are in [the automatic update plan](docs/automatic-updates-plan-2026-10-04.md).
The phased scope, architecture,
migration rules and acceptance gates are in
[the next development plan](docs/next-development-plan-2026-10-04.md).
Phase 12 has not started.

**Phase 0 spikes are done**: libvips and HEIC, sqlite-vec, SigLIP 2, YuNet + SFace, PP-OCRv5, all measured on a real laptop. Numbers and the plan changes they imply are in [spikes/README.md](spikes/README.md).

## Setup (Windows)

Needs Go 1.27.2, Node 22.22.2+ / 24.15+ / 26+ and npm 11.19+ (matching `frontend/package.json`), a C compiler for cgo, Git Bash, 7-Zip on PATH, and Python 3.14 (`ruff.toml` targets it). Toolchain used:

- Go 1.27.2 and [llvm-mingw](https://github.com/mstorsjo/llvm-mingw) (UCRT x86_64) unpacked under `%LOCALAPPDATA%\Programs`, both on `PATH`
- User environment: `CGO_ENABLED=1`, `CC=clang`, `CXX=clang++`
- Wails CLI pinned to the beta the project uses: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26`

Then, once, from Git Bash:

```sh
scripts/deps.sh   # ~1.2 GB of downloads, pinned and checked, into deps/ (git-ignored)
```

It fetches libvips 8.18 (static web build), ONNX Runtime 1.24.4 with DirectML, FFmpeg 9.0 (LGPL), SigLIP 2, the YuNet and SFace face models, the PP-OCRv5 text detector and English recogniser, the linters and NSIS (into `deps/tools/`), and the WebView2 runtime the installer carries (into `deps/installer/`), keeps each shipped component's licence text (in `deps/licenses/`), then prepares the models (`scripts/prepare_models.py`): the vision tower gets a fixed batch shape so DirectML can compile it, the text tower's 256k-token table is stored as int8, which takes it from 565 to 369 MB while agreeing with the original at cosine ≥ 0.9996, and SFace gets a variable batch size so every face in a photo is recognised in one run. Everything that ships beside `Lumen.exe` lands in `deps/runtime/` and is hard-linked into `bin/`. Nothing is ever downloaded at run time.

Check with `wails3 doctor`.

Compression adds pinned libjpeg-turbo 3.1.2 and libjxl 0.12.0 tools in
`deps/runtime/compress/`. OxiPNG 9.1.5 is built from its locked source using
a pinned portable Rust 1.99.0 GNU toolchain and the existing clang compiler.
The Rust compiler and Cargo cache remain in `deps/`; only the encoder ships.
Dependency licence texts are collected for the installer notices. These are
development downloads; the installed app remains offline.

Line endings are pinned to LF by `.gitattributes` (gofmt and bash need it), whatever `core.autocrlf` says.

## Code quality

`scripts/lint.sh` runs the static checks and frontend regression tests and fails if any does; `scripts/lint.sh --fix` first applies every automatic fix. Each tool is pinned and configured in the repository:

| Code     | Formatting       | Linting                                                                                                   | Config                                                                             |
| -------- | ---------------- | --------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| Go       | gofmt, goimports | golangci-lint 2.14 (vet, staticcheck, errcheck, gosec, errorlint, revive and more), deadcode, govulncheck | `.golangci.yml`                                                                    |
| Frontend | Prettier 3.9     | ESLint 10 with typescript-eslint (strict, type-aware) and eslint-plugin-vue, plus `vue-tsc`; import boundaries between `design`, `shared`, features and `app` | `frontend/eslint.config.js`, `frontend/.prettierrc.json`, `frontend/tsconfig.json` |
| Python   | Ruff             | Ruff                                                                                                      | `ruff.toml`                                                                        |
| Shell    |                  | ShellCheck                                                                                                |                                                                                    |

The Go tools, Ruff and ShellCheck live in `deps/tools/` (from `scripts/deps.sh`); the frontend's are dev dependencies (`npm ci`). npm enforces a seven-day release-age window through `min-release-age=7` in `frontend/.npmrc`; engine checks reject unsupported Node/npm versions. Exclusions are rare and each says why, in the config or on a `//nolint:linter // reason` line. How code is written (errors, context, SQLite, logging, the Go-to-TypeScript contract, Pinia and Vue, tests) is set out in [AUDIT_CONVENTIONS.md](AUDIT_CONVENTIONS.md).

Editors do the same on save: `.editorconfig` sets line endings and indentation, and `.vscode/settings.json` runs these formatters and linters from the pinned copies (install the recommended extensions).

## Commands

```sh
wails3 dev                    # run with hot reload
wails3 build                  # production build -> bin/Lumen.exe
source scripts/env.sh         # before go test / vet / run: puts deps/runtime (libvips) on PATH
go test -race -timeout 900s . ./internal/... ./services/... # wiring, engine and service tests (AI tests use deps/runtime)
scripts/lint.sh               # every formatter, linter and type check (--fix applies fixes first)
cd frontend && npm run check  # the frontend's alone: vue-tsc, ESLint, Prettier (lint:fix and format fix)
npm run test                 # Vitest: async store, viewer and settings regressions
node e2e/run.mjs             # end-to-end suites on bin/Lumen.exe in its WebView2 (after wails3 build)
```

Name the Go packages rather than `./...`: `build/` holds Wails' mobile templates, which only build for their own platforms.

`wails3 task package` builds Lumen and its installer, `bin\Lumen-<version>-x64-setup.exe` (build/windows/nsis/project.nsi, with the NSIS that `deps.sh` pins), after writing `bin\THIRD_PARTY_NOTICES.txt`. To sign Lumen.exe, the installer and the uninstaller, pass `SIGN_THUMBPRINT=` (a code-signing certificate in the Windows certificate store, where a hardware token's or a signing service's appears) and `TIMESTAMP_SERVER=`, with the Windows SDK's `signtool` on `PATH`. The version is set once, in `build/config.yml`; after changing it, run `wails3 task common:update:build-assets` (`TestVersionsAgree` fails until then). The template's Docker, server and MSIX tasks are left out of `Taskfile.yml`: Lumen needs cgo with the Windows libvips, and its installer must carry the runtime folder.

Before moving between implementation phases, also regenerate the bindings with
`wails3 generate bindings -f '-tags production' -clean=true -ts -i` (as the build does), check `git diff -- frontend/bindings`, and run
`wails3 build`. The frontend must finish building before Go embeds `frontend/dist`;
do not run independent frontend and Go production builds concurrently. Browser
component tests use jsdom; actual codec playback, native full screen and files
moving on disk are checked by the end-to-end suites in [`e2e/`](e2e/README.md),
which drive the built app over the DevTools protocol on scratch libraries, with
fixtures they generate themselves (one suite for each use case: onboarding, every
screen, interface, collections, organise, viewer, file tools, branding, formats).
See [the latest audit](docs/audit-2026-10-01.md) for findings, verification evidence
and remaining acceptance limits (earlier audits in `docs/` are historical records).

Handy environment variables:

- `LUMEN_DATA_DIR`: use another data folder (default `%LOCALAPPDATA%\Lumen`). Handy for a scratch library: it runs beside the user's Lumen rather than handing over to it.
- `LUMEN_RUNTIME_DIR`: where the DLLs, `ffmpeg\` and `models\` are (default: beside `Lumen.exe`).
- `LUMEN_DEVTOOLS_PORT`: expose the WebView's DevTools protocol on `127.0.0.1` so UI tests (e.g. puppeteer-core) can drive the real app. Off by default.
- `LUMEN_FACE_PHOTOS` (tests only): a folder of real photos of people. The face tests run on it and skip without it; keep such photos out of the repo.
- `LUMEN_RAW_SAMPLES` (tests only): a folder of real camera RAW files. The RAW tests run on it and skip without it; `e2e/fixtures/fetch_raw.py` downloads a pinned CC0 set into `e2e/.cache/raw`.

## Layout

```text
main.go              wiring: paths, logging, store, AI, indexer, services, window
internal/core        domain types, paths, the wall-clock date convention, case folding
internal/store       SQLite schema (migrations/), all queries, job queue, tags, embeddings
internal/scanner     folder walk, hashing, Windows folder watcher
internal/media       EXIF, filename dates, thumbnails and their cache, perceptual hash
internal/media/vips  the libvips calls (cgo), one C function per job
internal/media/wic   Windows Imaging Component decoding (HEIC)
internal/compress    verified lossless candidates, durable compression coordination and restoration
native/lumen-raw     the camera RAW worker (C++, LibRaw linked statically); scripts/deps.sh builds it into deps/runtime
internal/ai          ONNX Runtime sessions, SigLIP 2 image/text encoders, tokenizer, face detection/alignment/recognition
internal/search      in-memory int8 vector index, search, Things vocabulary and tagging
internal/people      face clustering (incremental, constrained by the user's corrections), names in searches
internal/places      offline reverse geocoding: the bundled GeoNames cities (cities.tsv.gz)
internal/index       background pipeline: scan loop, stage workers (OCR, XMP export, cache eviction too), model loading, CPU budget, politeness (battery, full screen, busy PC, idle), events
internal/fileops     every change to files in the user's folders: Recycle Bin, organise plans and journalled moves, sidecars
internal/diag        crash reports (the runtime's crash output, recovered panics), the problem report, the PC's description, Lumen's version
internal/signin      Windows sign-in check for a forgotten PIN (Windows Hello, or the account password)
internal/xmp         the XMP sidecar packet (pure Go)
internal/parallel    the worker pool loops run on
internal/testenv     finds deps/runtime for tests, skipping cleanly without it
e2e/                 end-to-end suites on the built app in WebView2, their fixture generators and perf scripts (e2e/README.md)
services/            Wails services (Library, Search, People, Media, System, Compression), the block gate, the loopback video server, error codes
frontend/src/bridge  the only place the UI calls the engine
frontend/src/design  design tokens (tokens.css), the icon manifest, how the appearance preferences are applied
frontend/src/shared  compositions (Page, PageHeader, FeatureState, CollectionCard, MediaTile…), the preference store, command registry, overlay coordinator, notifications, i18n
frontend/src/app     the shell: navigation, global overlays, command palette, shortcut reference; a development-only component showcase
frontend/src/{library,search,things,people,onboarding,settings}   UI by feature
frontend/src/albums  manual and smart album browser and rule editor
frontend/src/{places,events}   GPS places and automatic event groups
frontend/src/duplicates  exact-copy and similar-photo review, best copy pre-selected
frontend/src/trash   Trash actions and Undo
frontend/src/organise  the organise-on-disk template, preview, progress and undo
frontend/src/about   Settings → About Lumen, and the notice after a crash
scripts/             deps.sh (native dependencies), prepare_models.py, env.sh, lint.sh, build_places.py, notices.py, installer_payload.py
docs/                audits (dated records), the blocking note, the Phase 5, 6 and 7 records, the development plans, GeoNames provenance, the beta guide
build/windows/nsis   the installer (project.nsi; wails_tools.nsh is generated from build/config.yml)
spikes/              Phase 0 experiments (own Go module; models in spikes/.cache)
```

**Dates.** EXIF dates have no time zone, so every date is stored as "wall-clock seconds": the camera's clock time encoded as UTC (`core.WallClock`). The UI formats everything in UTC, so a photo taken at 23:30 stays on its own day.

**Tags.** A Thing applies to a photo when SigLIP's probability for its best prompt passes the confidence setting (default 0.02; scores from 0.002 up are stored, so the slider needs no re-scan). Generic concepts rarely pass on probability alone, because SigLIP learned from specific web captions, so ten broad kinds of photo are also assigned by best fit. On one real library this found 94% of the people photos, against 16% on probability alone. Prompts were chosen by measuring them on that library.

**People.** Clustering joins groups of faces while their average cosine similarity is at least 0.42 (Phase 0 found every group at that threshold held one person). A new person needs clear faces in three photos: sharpness (variance of the Laplacian of the aligned face) at least 15, head turn at most 0.45 eye distances. On 204 office photos with 1,000 faces, 92% were placed into 28 people, each checked by eye to hold one person; the rest wait, unplaced, for more photos of them. The faces stage and clustering never undo the user: see `internal/people/cluster.go`.

**Places.** City names come from [GeoNames](https://www.geonames.org/), licensed under CC BY 4.0. Lumen bundles a reduced 30 September 2026 `cities5000` snapshot (69,760 populated places), with region and country names, so lookups make no network request. A city within 75 km is an approximate label, not an exact administrative boundary. See [data provenance](docs/geonames.md).

## Deviations from the plan (so far)

- **TypeScript 6.0**, not 7: `vue-tsc` can't drive the Go-native TS 7 compiler yet. Revisit when Volar supports it.
- **Pinia 4** (plan said 3; 4 is the current stable).
- **Search is an in-memory int8 scan**, not sqlite-vec: SQLite stores the vectors, a flat int8 copy in memory is searched (Phase 0: 17 ms at 300k vs 351 ms). No vector extension is loaded.
- **Search results are ordered by relevance**, not by date: SigLIP's probabilities vary too much between queries for a fixed cut-off to work, and a relevance order keeps weak matches at the end.
- **libvips is the static "web" build** (one 18 MB DLL, no ImageMagick), called through Lumen's own small cgo wrapper (`internal/media/vips`, one C function per job) rather than govips: BMP goes through Go, HEIC through WIC or FFmpeg (libvips reads its colour profile).
- **Events are grouped when read**, not stored in an `events` table: consecutive visible media split by the time gap or a 100 km jump, cached until something they depend on changes. What the user sets on an event (name, pin, block) is an event tag whose members are the event's photos, kept equal to the event as it regroups (`store/eventtags.go`).
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
- **Camera RAW runs in a worker Lumen builds itself** (decided 7 October 2026): LibRaw's source is pinned and compiled by `deps.sh` with the same llvm-mingw clang cgo uses, into one static `lumen-raw.exe`. LibRaw's own Windows binaries need the Visual C++ runtime, which a clean PC may lack. It is used under its CDDL-1.0 option. No JPEG or zlib library is compiled in, so lossy-JPEG and deflate-compressed DNG are not read.
- **The FFmpeg build is the stock LGPL one** (150 MB unpacked, mostly codecs Lumen never uses). Trimming it needs a from-source LGPL build of FFmpeg, a toolchain of its own; packed in the installer the stock build is 63 MB of its 872 MB. Revisit if the installer's size matters more than that build.
- **No updates in the app** (decided 3 October 2026): the plan left auto-update open, as opt-in. Lumen makes no network request at all; Settings shows the releases page.
- **Installed per user** (decided 3 October 2026), not in Program Files: the library is per user already, and installing or updating needs no administrator.
- **The installer carries the WebView2 runtime** (decided 3 October 2026, about 200 MB in it) rather than Wails' downloading bootstrapper, so installing needs no internet either.
