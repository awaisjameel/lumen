# Lumen

A Windows-first desktop photo gallery that organises large libraries with on-device AI. Fully local: no accounts, no uploads, no telemetry. See [the plan](Local%20AI%20Photo%20Gallery%20—%20End-to-End%20Plan.md) for the full vision and roadmap.

## Status

**Phase 1 (fast library browser)** is working:

- First-run folder picker with suggested folders; sources never overlap
- Scanner: parallel content hashing (xxh3-128), date guess from file names, skip hidden/system folders
- Recursive folder watching (`ReadDirectoryChangesW`) with debounced rescans, plus a reconcile scan on startup
- Durable job queue in SQLite (`asset_jobs`): metadata, thumbnails, then the AI, newest first, **on-screen days first**; resumes after a crash; changing a stage's version re-runs only that stage
- EXIF (date, size, orientation, camera, GPS) via imagemeta, with goexif as a fallback for JPEG/TIFF
- Thumbnails (256 and 1024 px) plus a 64-bit perceptual hash, served at `/media/thumb/{id}/{size}`
- Virtualized, day-grouped grid with a timeline scrubber, keyboard navigation, zoom
- Viewer (instant preview, then the original), details panel, favourites, light/dark themes
- Exact duplicates collapse into one asset with several file paths

**Phase 2 (search and Things)** is working:

- Thumbnails through **libvips** (JPEG, PNG, WebP, GIF, TIFF, AVIF, shrink-on-load); **HEIC** through Windows' decoder (WIC), falling back to the bundled FFmpeg; BMP through Go
- **SigLIP 2** image embeddings on the GPU through DirectML (CPU fallback), batches of 16, loaded only while there is work
- **Natural-language search** from the search box (Ctrl+F): results best first, cut relative to the best match so a query matching nothing returns nothing; an in-memory int8 index answers in milliseconds
- **Things**: about 180 built-in labels, the broad kind of each photo (People, Screenshots, Documents…), labels users add ("Eiffel Tower", found across the library at once), tags users add or remove on any photo (their edits always win), a confidence slider
- AI work waits while on battery (a setting allows it), and pauses with the rest

Not yet: people, videos (toggle exists; FFmpeg is bundled but unused for video), albums, events, trash, organise-on-disk, the 2 GB cache cap, OCR.

**Phase 0 spikes are done**: libvips and HEIC, sqlite-vec, SigLIP 2, YuNet + SFace, PP-OCRv5, all measured on a real laptop. Numbers and the plan changes they imply are in [spikes/README.md](spikes/README.md).

## Setup (Windows)

Needs Go 1.27, Node 22+, a C compiler for cgo, Git Bash, and Python 3 (only to prepare the models). Toolchain used:

- Go 1.27.1 and [llvm-mingw](https://github.com/mstorsjo/llvm-mingw) (UCRT x86_64) unpacked under `%LOCALAPPDATA%\Programs`, both on `PATH`
- User environment: `CGO_ENABLED=1`, `CC=clang`, `CXX=clang++`
- Wails CLI pinned to the beta the project uses: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26`

Then, once, from Git Bash:

```sh
scripts/deps.sh   # ~1.2 GB of downloads, pinned and hash-checked, into deps/ (git-ignored)
```

It fetches libvips 8.18 (static web build), ONNX Runtime 1.24.4 with DirectML, FFmpeg 9.0 (LGPL) and SigLIP 2, then prepares the models (`scripts/prepare_models.py`): the vision tower gets a fixed batch shape so DirectML can compile it, and the text tower's 256k-token table is stored as int8, which takes it from 565 to 369 MB while agreeing with the original at cosine ≥ 0.9996. Everything that ships beside `Lumen.exe` lands in `deps/runtime/` and is hard-linked into `bin/`. Nothing is ever downloaded at run time.

Check with `wails3 doctor`.

Line endings are pinned to LF by `.gitattributes` (gofmt and bash need it), whatever `core.autocrlf` says.

## Commands

```sh
wails3 dev                    # run with hot reload
wails3 build                  # production build -> bin/Lumen.exe
source scripts/env.sh         # before go test / vet / run: puts deps/runtime (libvips) on PATH
go test -race ./internal/...  # engine tests, with the race detector (AI tests use deps/runtime)
go vet . ./internal/... ./services/...
staticcheck . ./internal/... ./services/...   # go install honnef.co/go/tools/cmd/staticcheck@latest
cd frontend && npm run typecheck
```

Name the Go packages rather than `./...`: `build/` holds Wails' mobile templates, which only build for their own platforms.

Handy environment variables:

- `LUMEN_DATA_DIR`: use another data folder (default `%LOCALAPPDATA%\Lumen`). Handy for a scratch library.
- `LUMEN_RUNTIME_DIR`: where the DLLs, `ffmpeg\` and `models\` are (default: beside `Lumen.exe`).
- `LUMEN_DEVTOOLS_PORT`: expose the WebView's DevTools protocol on `127.0.0.1` so UI tests (e.g. puppeteer-core) can drive the real app. Off by default.

## Layout

```text
main.go              wiring: paths, logging, store, AI, indexer, services, window
internal/core        domain types, paths, the wall-clock date convention
internal/store       SQLite schema (migrations/), all queries, job queue, tags, embeddings
internal/scanner     folder walk, hashing, Windows folder watcher
internal/media       EXIF, filename dates, thumbnails and their cache, perceptual hash
internal/media/vips  the libvips calls (cgo), one C function per job
internal/media/wic   Windows Imaging Component decoding (HEIC)
internal/ai          ONNX Runtime sessions, SigLIP 2 image/text encoders, tokenizer
internal/search      in-memory int8 vector index, search, Things vocabulary and tagging
internal/index       background pipeline: scan loop, stage workers, battery, events
services/            thin Wails services (LibraryService, SearchService, MediaService)
frontend/src/bridge  the only place the UI calls the engine
frontend/src/{library,search,things,onboarding,settings,shared}   UI by feature
scripts/             deps.sh (native dependencies), prepare_models.py, env.sh
spikes/              Phase 0 experiments (own Go module; models in spikes/.cache)
```

**Dates.** EXIF dates have no time zone, so every date is stored as "wall-clock seconds": the camera's clock time encoded as UTC (`core.WallClock`). The UI formats everything in UTC, so a photo taken at 23:30 stays on its own day.

**Tags.** A Thing applies to a photo when SigLIP's probability for its best prompt passes the confidence setting (default 0.02; scores from 0.002 up are stored, so the slider needs no re-scan). Generic concepts rarely pass on probability alone, because SigLIP learned from specific web captions, so ten broad kinds of photo are also assigned by best fit. On one real library this found 94% of the people photos, against 16% on probability alone. Prompts were chosen by measuring them on that library.

## Deviations from the plan (so far)

- **TypeScript 6.0**, not 7: `vue-tsc` can't drive the Go-native TS 7 compiler yet. Revisit when Volar supports it.
- **Pinia 4** (plan said 3; 4 is the current stable).
- **Search is an in-memory int8 scan**, not sqlite-vec: SQLite stores the vectors, a flat int8 copy in memory is searched (Phase 0: 17 ms at 300k vs 351 ms). No vector extension is loaded.
- **Search results are ordered by relevance**, not by date: SigLIP's probabilities vary too much between queries for a fixed cut-off to work, and a relevance order keeps weak matches at the end.
- **libvips is the static "web" build** (one 18 MB DLL, no ImageMagick): BMP goes through Go instead.
- **Large files are hash-sampled**: files over 64 MB (mostly videos) hash size plus three 4 MB windows instead of every byte.
- **Folder watching** uses `ReadDirectoryChangesW` directly (fsnotify has no recursive watch on Windows yet).
- **One writer connection** instead of a writer goroutine: SQLite writes serialise on it, and callers batch hundreds of rows per transaction.
- **The FFmpeg build is the stock LGPL one** (150 MB unpacked, mostly codecs Lumen never uses). A trimmed build belongs with video support (Phase 4) and the installer (Phase 6).
