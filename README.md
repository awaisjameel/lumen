# Lumen

A Windows-first desktop photo gallery that organises large libraries with on-device AI. Fully local: no accounts, no uploads, no telemetry. See [the plan](Local%20AI%20Photo%20Gallery%20—%20End-to-End%20Plan.md) for the full vision and roadmap.

## Status

**Phase 1 slice (fast library browser, no AI yet)** is working:

- First-run folder picker with suggested folders; sources never overlap
- Scanner: parallel content hashing (xxh3-128), date guess from file names, skip hidden/system folders
- Recursive folder watching (`ReadDirectoryChangesW`) with debounced rescans, plus a reconcile scan on startup
- Durable job queue in SQLite (`asset_jobs`): metadata then thumbnails, newest first, **on-screen days first**; resumes after a crash; bumping a stage's version re-runs only that stage
- EXIF (date, size, orientation, camera, GPS) via imagemeta, with goexif as a fallback for JPEG/TIFF
- Thumbnails (256 and 1024 px) plus a 64-bit perceptual hash, served at `/media/thumb/{id}/{size}`
- Virtualized, day-grouped grid with a timeline scrubber, keyboard navigation, zoom
- Viewer (instant preview, then the original), details panel, favourites, light/dark themes
- Exact duplicates collapse into one asset with several file paths

Not yet: search/CLIP, people, videos (toggle exists, no FFmpeg yet), HEIC/AVIF thumbnails (need libvips), albums, events, trash, organise-on-disk, the 2 GB cache cap.

**Phase 0 spikes are done**: libvips and HEIC, sqlite-vec, SigLIP 2, YuNet + SFace, PP-OCRv5, all measured on a real laptop. Numbers and the plan changes they imply are in [spikes/README.md](spikes/README.md).

## Setup (Windows)

Needs Go 1.27, Node 22+, and a C compiler for cgo. Toolchain used:

- Go 1.27.1 and [llvm-mingw](https://github.com/mstorsjo/llvm-mingw) (UCRT x86_64) unpacked under `%LOCALAPPDATA%\Programs`, both on `PATH`
- User environment: `CGO_ENABLED=1`, `CC=clang`, `CXX=clang++`
- Wails CLI pinned to the beta the project uses: `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.26`

Check with `wails3 doctor`.

## Commands

```sh
wails3 dev                    # run with hot reload
wails3 build                  # production build -> bin/Lumen.exe
go test -race ./internal/...  # engine tests, with the race detector
go vet . ./internal/... ./services/...
staticcheck . ./internal/... ./services/...   # go install honnef.co/go/tools/cmd/staticcheck@latest
cd frontend && npm run typecheck
```

Name the Go packages rather than `./...`: `build/` holds Wails' mobile templates, which only build for their own platforms.

Handy environment variables:

- `LUMEN_DATA_DIR`: use another data folder (default `%LOCALAPPDATA%\Lumen`). Handy for a scratch library.
- `LUMEN_DEVTOOLS_PORT`: expose the WebView's DevTools protocol on `127.0.0.1` so UI tests (e.g. puppeteer-core) can drive the real app. Off by default.

## Layout

```text
main.go              wiring: paths, logging, store, indexer, services, window
internal/core        domain types, paths, the wall-clock date convention
internal/store       SQLite schema (migrations/), all queries, job queue
internal/scanner     folder walk, hashing, Windows folder watcher
internal/media       EXIF, filename dates, thumbnails, perceptual hash
internal/index       background pipeline: scan loop, stage workers, events
services/            thin Wails services (LibraryService, MediaService)
frontend/src/bridge  the only place the UI calls the engine
frontend/src/{library,onboarding,settings,shared}   UI by feature
spikes/              Phase 0 experiments (own Go module; models in spikes/.cache)
```

**Dates.** EXIF dates have no time zone, so every date is stored as "wall-clock seconds": the camera's clock time encoded as UTC (`core.WallClock`). The UI formats everything in UTC, so a photo taken at 23:30 stays on its own day.

## Deviations from the plan (so far)

- **TypeScript 6.0**, not 7: `vue-tsc` can't drive the Go-native TS 7 compiler yet. Revisit when Volar supports it.
- **Pinia 4** (plan said 3; 4 is the current stable).
- **Thumbnails are pure Go for now** (JPEG, PNG, GIF, WebP, BMP, TIFF). HEIC/AVIF jobs are marked `skipped`, not failed, and re-run automatically once a libvips thumbnailer ships behind the same `media.Thumbnailer` interface.
- **Large files are hash-sampled**: files over 64 MB (mostly videos) hash size plus three 4 MB windows instead of every byte.
- **Folder watching** uses `ReadDirectoryChangesW` directly (fsnotify has no recursive watch on Windows yet).
- **One writer connection** instead of a writer goroutine: SQLite writes serialise on it, and callers batch hundreds of rows per transaction.
