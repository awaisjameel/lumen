# Architecture and design decisions

This is the implemented layout and the decisions that differ from the original
plan. [CONVENTIONS.md](../CONVENTIONS.md) defines coding and contract rules;
[Development](development.md) owns setup and command recipes.

## Repository layout

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
e2e/                 end-to-end suites on the built app, their fixture generators, installer acceptance, perf scripts and the screenshot capture (e2e/docs)
scripts/             deps.sh and its parts, prepare_models.py, env.sh, lint.sh, bench.sh, version.sh, the hook checks, notices.py, installer and update payloads, release.ps1, data builders
.github/             CI and release workflows, and the shared toolchain set-up they use
.githooks/           commit-msg, pre-commit and pre-push (scripts/install-hooks.sh turns them on)
build/               Wails build config (config.yml holds the version), icons, the NSIS installer in build/windows/nsis
docs/                the feature tour, beta guide, production signing, branding, GeoNames provenance, the original plan, the screenshots and their capture; docs/README.md maps them; docs/history holds the dated records
spikes/              Phase 0 experiments and later feasibility probes (own Go module; models in spikes/.cache)
```

## How it works

**Indexing.** Files are identified by an xxh3-128 hash of their contents (files over 64 MB hash their size and three 4 MB windows). Each asset goes through metadata, thumbnails (256 and 1024 px, plus a 64-bit perceptual hash), the image embedding, faces, text recognition and XMP export, through a durable job queue in SQLite: newest first, on-screen days first, resumed after a crash. Changing what a stage produces bumps its version, which re-runs only that stage. Indexing runs below normal priority on half the processor's threads by default, rests the GPU a third of the time, and waits on battery, behind a game or full-screen video, and while other programs keep the processor busy.

**Dates.** EXIF dates have no time zone, so every date is stored as "wall-clock seconds": the camera's clock time encoded as UTC (`core.WallClock`). The UI formats everything in UTC, so a photo taken at 23:30 stays on its own day.

**Tags.** A Thing applies to a photo when SigLIP's probability for its best prompt passes the confidence setting (default 0.02; scores from 0.002 up are stored, so the slider needs no re-scan). Generic concepts rarely pass on probability alone, because SigLIP learned from specific web captions, so ten broad kinds of photo are also assigned by best fit. On one real library this found 94% of the people photos, against 16% on probability alone. Prompts were chosen by measuring them on that library.

**People.** The following figures are historical tuning evidence, not a promise of recognition accuracy on another library. Clustering joins groups of faces while their average cosine similarity is at least 0.42 (Phase 0 found every group at that threshold held one person). A new person needs clear faces in three photos: sharpness (variance of the Laplacian of the aligned face) at least 15, head turn at most 0.45 eye distances. On 204 office photos with 1,000 faces, 92% were placed into 28 people, each checked by eye to hold one person; the rest wait, unplaced, for more photos of them. The faces stage and clustering never undo the user: see `internal/people/cluster.go`.

**Places.** City names come from [GeoNames](https://www.geonames.org/), licensed under CC BY 4.0. Lumen bundles a reduced 30 September 2026 `cities5000` snapshot (69,760 populated places), with region and country names, so lookups make no network request. A city within 75 km is an approximate label, not an exact administrative boundary. See [data provenance](geonames.md).

**Visibility.** One rule feeds grids, search, counts, covers, media URLs and the video server, so a blocked item disappears everywhere at once. Blocked media is served only while the PIN-protected Blocked settings are open.

## Deviations from the plan

[The original plan](plan.md) set the vision and roadmap. These decisions depart from it:

- **Source-available, not open source** (decided 15 October 2026): the plan said open source from day one. Lumen is licensed under the PolyForm Noncommercial License (see [Licence](../README.md#licence)).
- **TypeScript 6.0**, not the plan's 7: the committed Vue toolchain uses `vue-tsc` with TypeScript 6. A compiler upgrade needs a compatibility review of that toolchain.
- **Pinia 4** (the plan said 3), as selected in the frontend manifest and lockfile.
- **Search is an in-memory int8 scan**, not sqlite-vec: SQLite stores the vectors, a flat int8 copy in memory is searched (Phase 0: 17 ms at 300k vs 351 ms). No vector extension is loaded.
- **Search results are ordered by relevance**, not by date: SigLIP's probabilities vary too much between queries for a fixed cut-off to work, and a relevance order keeps weak matches at the end.
- **libvips is the static "web" build** (one DLL, no ImageMagick), called through Lumen's own small cgo wrapper (`internal/media/vips`, one C function per job) rather than govips: BMP goes through Go, HEIC through WIC or FFmpeg (libvips reads its colour profile).
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
- **The FFmpeg build is the pinned stock LGPL one**, rather than a custom codec-minimal build. Trimming it needs a maintained from-source build and toolchain; archive and installer sizes vary with the selected dependency version.
- **Updates are opt-in and inert by default** (decided 4 October 2026, replacing the 3 October decision to have no updater): checks happen only when the user enables them or asks, downloads only when requested, and only against compiled-in release trust, which is empty until production signing is in place. No other feature makes a network request.
- **Installed per user** (decided 3 October 2026), not in Program Files: the library is per user already, and installing or updating needs no administrator.
- **The installer carries the WebView2 runtime** (decided 3 October 2026) rather than Wails' downloading bootstrapper, so installing needs no internet either.
- **The Visual C++ runtime ships beside `Lumen.exe`** (decided 23 October 2026): ONNX Runtime needs it, Windows does not include it, and Microsoft's redistributable installer needs administrator rights, which the per-user install avoids. The four DLLs are unmodified files from the pinned x64 redistributable, distributed under the Visual Studio licence terms. Windows Update does not service app-local copies, so a Lumen update replaces them. `scripts/runtime_imports.py`, run by `scripts/lint.sh`, fails if a shipped file imports a DLL that is neither beside it nor part of Windows.
