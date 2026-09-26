# Local AI Photo Gallery — End-to-End Plan

Sep 25, 2026 · @Awais Jameel

## 1. Vision & Principles

A Windows-first desktop app that organizes large photo and video libraries automatically using on-device AI, while leaving the user in full control of how their memories are arranged. Working name: **Lumen** (placeholder).

**Core pillars**

- **Fully local and private.** Every model runs on the user's machine. No accounts, no uploads, no telemetry by default. The app should work with the network cable unplugged. This is the headline promise.
- **The user stays in control.** AI suggests; the user decides. Auto-generated groupings (people, things, places, events) are views on top of the library, never a forced folder structure. Nothing is moved, renamed or deleted on disk without explicit consent.
- **Simple surface, power underneath.** The default experience is point it at your folders and it just works. Advanced controls (confidence thresholds, model choice, rule-based albums, bulk edits) live behind an "Advanced" toggle or secondary panels.
- **Fast at scale.** Designed from day one for hundreds of thousands of photos and videos: instant scrolling, sub-second search, background indexing that never blocks the UI.
- **Non-destructive and portable.** The user's original files are the source of truth. Metadata the app creates can optionally be written back as XMP sidecars so the organization survives even if the app is uninstalled.

**Target users**

- *Everyday users* with years of messy camera-roll dumps, WhatsApp images and phone backups. They want one click and good results.
- *Photo enthusiasts* who want more control than Windows Photos or Google Photos gives them, without handing their library to a cloud service.

**What it is not (v1):** not a photo editor, not a cloud sync service, not a social/sharing platform.

## 2. Product & UX

### First run

1. Welcome screen with one question: *"Which folders hold your photos and videos?"* Pre-suggest Pictures, Videos, OneDrive/Camera Roll and any DCIM-style folders found.
2. User clicks **Start**. The library appears within seconds (filenames + EXIF dates + thumbnails stream in).
3. AI indexing runs in the background with a small, calm progress indicator ("Understanding your photos… 12,400 of 180,000"). The app is fully usable while this runs.
4. No model downloads, no settings, no account. Models ship with the installer.

### Main window layout

- **Left sidebar (collapsible):** Library, People, Things, Places, Events, Albums, Favourites, Videos, Duplicates, Trash (app-level, not disk).
- **Top bar:** one big search box ("Search your memories… try *beach sunset* or *Ali at birthday*"), view toggle (grid / timeline), zoom slider.
- **Centre:** virtualized grid grouped by date, with a scrubbable timeline on the right edge (year / month jump).
- **Right panel (hidden by default):** details for the selected item: date, camera, location, detected people, tags, and editable fields.

### Key flows

- **Search:** a single box handles everything. Natural language (CLIP), person names, tags, dates ("June 2023"), places, file types. Filter chips appear under the box to refine.
- **People:** faces are auto-clustered into unnamed groups. User names a cluster once; the app applies it everywhere. Merge, split, hide, and "not this person" corrections feed back into clustering.
- **Things & Events:** auto-suggested categories (Beach, Food, Documents, Screenshots, Pets…) and auto-detected events (a burst of photos in one place over a day or weekend). Users can rename, hide or pin any of them.
- **Albums:** manual albums plus **Smart Albums** built from rules ("person = Mom AND tag = garden AND year ≥ 2022"). Smart Albums are the main power-user feature.
- **Organize on disk (optional, explicit):** a preview-first tool to move/rename files into a folder scheme the user defines (e.g. `Year/Month/Event`). Always shows a dry run and supports undo.

### Progressive disclosure

| Everyday default | Advanced (behind a toggle) |
| --- | --- |
| Automatic categories | Custom label vocabularies, confidence thresholds |
| Name a face once | Manual face box editing, re-run clustering |
| Simple search box | Query syntax, saved searches |
| Auto-detected events | Event gap tuning (hours between photos) |
| Balanced performance | GPU/CPU choice, indexing schedule, model size |

### Design direction

Calm and photo-first: near-black or neutral backgrounds so images carry the colour, generous spacing, one accent colour, native-feeling Windows 11 styling (Mica-like surfaces, rounded corners), full keyboard navigation, light and dark themes.

## 3. Architecture

### Stack

| Layer | Choice (checked 26 Sep 2026) | Why |
| --- | --- | --- |
| Desktop shell | Wails v3 (3.0 beta, released Aug 2026) | Go backend plus web frontend in Windows' built-in WebView2; generates typed TypeScript functions from Go services; small installer. The desktop API is frozen for 3.0; v2 is the stable fallback |
| Core engine | Go 1.27, with cgo enabled | Easy to read and review, goroutines and channels fit the indexing pipeline, one binary plus a few DLLs |
| Frontend | Vue 3.5 + TypeScript 7 + Vite 8 | Vue 3.5 is the current stable line; move to 3.6 (Vapor mode) once it leaves release candidate. TypeScript 7 is the new Go-native compiler; Vite 8 builds with Rolldown |
| UI state and lists | Pinia 3, Vue Router 5, TanStack Virtual | Standard Vue state and routing; a virtualized grid that renders only visible rows |
| Database | SQLite via mattn/go-sqlite3 (cgo) + sqlite-vec | Single local file, zero setup, vector search in the same database. SQLite's own vec1 extension (approximate search, pre-1.0) is one to watch |
| ML runtime | ONNX Runtime (DirectML build) via yalue/onnxruntime\_go | Runs on the CPU anywhere and on any DirectX 12 GPU on Windows 10 and 11 |
| Image decoding | libvips 8.18 via govips, with libheif for HEIC | Fast, low-memory thumbnails; JPEG, PNG, WebP, HEIC, AVIF |
| Video | FFmpeg 9.0 (LGPL build), run as bundled ffmpeg and ffprobe processes | Keyframes, duration, codec info, poster frames; no cgo and a clean licence boundary |
| Metadata | Pure-Go EXIF reader for fast reads (pick in Phase 0); bundled ExifTool for writing tags back | ExifTool is the safest writer for JPEG metadata and XMP sidecars |
| Hashing | BLAKE3 or xxh3 for content; a pHash/dHash library for near-duplicates | Exact and near-duplicate detection |
| File watching | fsnotify | Picks up new, changed and deleted files |
| Reverse geocoding | Offline GeoNames dataset | Place names without any network call |
| Build and packaging | Wails 3 build tasks, NSIS installer, llvm-mingw as the C toolchain for cgo | ONNX Runtime, libvips and SQLite need a C compiler at build time on Windows |

### Go project layout

```text
lumen/
├─ main.go          # Wails app: window, services, events
├─ internal/
│  ├─ core/         # domain types, config, errors
│  ├─ scanner/      # folder walking, hashing, file watching
│  ├─ media/        # libvips decoding, thumbnails, EXIF, FFmpeg
│  ├─ ai/           # ONNX sessions: faces, CLIP, OCR
│  ├─ index/        # job queue, worker pools, pipeline
│  ├─ store/        # SQLite schema, migrations, queries, vectors
│  └─ search/       # query parsing, ranking, filters
├─ services/        # thin Wails services the UI calls (Library, Search, People…)
├─ frontend/        # Vue app; Wails generates the TypeScript bindings into it
└─ build/           # build tasks, installer, icons, bundled DLLs and models
```

### How the pieces talk

- The UI calls Go through Wails services (Search, GetPage, NamePerson, CreateSmartAlbum…). Wails generates a typed TypeScript function for each, returning small JSON payloads.
- Long-running work reports back through Wails events (index:progress, library:changed) so the UI updates live.
- Thumbnails are served by a custom asset handler on the app's own origin (e.g. /thumb/\<id>/\<size>) straight from the disk cache, so the WebView loads them like normal images without base64 overhead.
- SQLite in WAL mode: a pool of read connections for UI queries plus one dedicated writer goroutine for the indexer, so they never block each other.

### Coming from Laravel/Node

Think of Wails services as your controllers, the store package as your models and repositories, the index package's goroutine worker pools as your queue workers, and Wails events as broadcasting. Go feels close to PHP: plain structs, explicit error returns, no ownership rules. A few focused days on goroutines, channels and error handling is enough before starting.

## 4. AI Pipeline

### Indexing stages

Each file moves through ordered stages. Cheap stages run first so the library is browsable almost immediately; expensive AI stages trail behind.

1. **Discover** – walk folders, record path, size, modified time. (milliseconds per file)
2. **Fingerprint** – fast content hash (xxHash3 / BLAKE3) to detect exact duplicates and survive renames and moves.
3. **Metadata** – EXIF/video metadata: capture date, GPS, camera, orientation, duration.
4. **Thumbnails** – two sizes (≈256 px grid, ≈1024 px preview), stored as WebP/JPEG in a cache folder.
5. **Perceptual hash** – pHash/dHash for near-duplicate and burst detection.
6. **Content embedding** – CLIP-family vision embedding for semantic search and tagging.
7. **Faces** – detect, align, embed; later clustered into people.
8. **Derived** – auto-tags, events, places, quality scores (blur, exposure).

Every stage records a per-file status and model version, so a stage can be re-run later (e.g. after upgrading a model) without redoing the others.

### Content understanding (CLIP)

- **Model:** ship `SigLIP 2 base` (patch16-224, Apache-2.0, 768-dim embeddings) exported to ONNX, shipped in the installer (no larger variant is downloaded; everything ships in one bundle). MobileCLIP2 is ruled out: Apple's research-only model licence does not allow commercial use or redistribution.
- **Search:** the user's text goes through the text encoder; the result is compared (cosine similarity) against stored image embeddings via sqlite-vec. Top results are then filtered by any date/person/place chips.
- **Auto-tags:** a built-in vocabulary of \~300–1,000 everyday labels (beach, food, dog, document, screenshot, receipt, sunset, snow…) is pre-embedded once. Each photo gets the labels scoring above a threshold. Users can add their own labels, and those work instantly because it's zero-shot: no retraining needed.
- **Special detectors** are worth adding for things users care about but CLIP handles loosely: screenshots, documents/receipts, memes, blurry shots.

### Faces

- **Detection:** YuNet (OpenCV Zoo, ONNX, under 1 MB) returns boxes plus 5 landmarks, fast enough for CPU-only machines.
- **Alignment + recognition:** align crops to 112×112 using landmarks, then `SFace` (OpenCV Zoo, Apache-2.0) produces a 128-dim identity embedding. The go-onnxface project is a Go reference for this exact flow.
- **Clustering:** group embeddings with HDBSCAN or incremental agglomerative clustering. New faces are first matched against named people (nearest neighbour + threshold), then leftovers are clustered.
- **User feedback loop:** naming, merging, splitting and "not this person" are stored as constraints and respected on every re-cluster, so corrections are never lost.
- **Licensing check:** InsightFace weights (SCRFD, ArcFace, the buffalo packs) are non-commercial, research-only, so they are excluded. YuNet + SFace is the permissively licensed pair. SFace is less accurate than ArcFace, so Phase 0 must test clustering on real family photos.

### Videos

- Extract metadata and a poster frame with FFmpeg.
- Sample keyframes (e.g. scene changes, capped at \~1 frame per 5–10 s and a max per video), run CLIP and face detection on those frames.
- A video's search embedding is the set of its frame embeddings; search hits can jump to the matching timestamp.
- Hover-scrub previews in the grid use a small sprite sheet generated during indexing.

### Places & events

- GPS → place names with an offline reverse-geocoding dataset (city/region/country).
- **Events:** split the timeline wherever there is a large time gap (default 12 h) or a large location jump; name them automatically ("Lahore, 14–16 Mar 2025").

### Hardware acceleration

- Use ONNX Runtime's **DirectML** execution provider so any DirectX 12 GPU helps on Windows 10 and 11; fall back to CPU automatically. DirectML is now in maintenance mode: Microsoft puts new work into Windows ML, which adds NPU and vendor-tuned GPU providers on Windows 11 24H2 and later. It runs the same ONNX models, so it can be added later without changing them.
- Batch inference (e.g. 16–64 images per batch) and keep sessions warm.
- Optional **quantized (INT8)** models for low-end laptops.

## 5. Data Model

One SQLite database file (e.g. `%LOCALAPPDATA%\Lumen\library.db`) in WAL mode, plus a thumbnail cache folder beside it. Migrations are versioned from day one.

### Core tables (simplified)

```sql
-- Where the user told us to look
CREATE TABLE sources (
  id INTEGER PRIMARY KEY, path TEXT UNIQUE, enabled INTEGER DEFAULT 1
);

-- One row per unique piece of content
CREATE TABLE assets (
  id INTEGER PRIMARY KEY,
  content_hash BLOB UNIQUE,        -- BLAKE3 / xxh3
  kind TEXT,                       -- 'photo' | 'video'
  taken_at INTEGER,                -- unix time, from EXIF or fallback
  width INTEGER, height INTEGER, duration_ms INTEGER,
  lat REAL, lon REAL, place_id INTEGER,
  phash INTEGER,                   -- perceptual hash
  camera TEXT, favourite INTEGER DEFAULT 0, hidden INTEGER DEFAULT 0,
  quality REAL
);

-- Physical files (an asset can live in several places)
CREATE TABLE files (
  id INTEGER PRIMARY KEY, asset_id INTEGER REFERENCES assets(id),
  source_id INTEGER, path TEXT UNIQUE, size INTEGER, mtime INTEGER
);

-- Per-stage processing state
CREATE TABLE asset_jobs (
  asset_id INTEGER, stage TEXT, status TEXT, model_version TEXT,
  error TEXT, updated_at INTEGER,
  PRIMARY KEY (asset_id, stage)
);

CREATE TABLE people (id INTEGER PRIMARY KEY, name TEXT, cover_face_id INTEGER, hidden INTEGER DEFAULT 0);

CREATE TABLE faces (
  id INTEGER PRIMARY KEY, asset_id INTEGER, frame_ms INTEGER,
  x REAL, y REAL, w REAL, h REAL, score REAL,
  person_id INTEGER, cluster_id INTEGER, assigned_by TEXT  -- 'auto' | 'user'
);

CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT UNIQUE, source TEXT); -- 'ai' | 'user'
CREATE TABLE asset_tags (asset_id INTEGER, tag_id INTEGER, score REAL, source TEXT,
  PRIMARY KEY (asset_id, tag_id));

CREATE TABLE albums (id INTEGER PRIMARY KEY, name TEXT, kind TEXT, rule_json TEXT); -- manual | smart
CREATE TABLE album_items (album_id INTEGER, asset_id INTEGER, position INTEGER);

CREATE TABLE events (id INTEGER PRIMARY KEY, name TEXT, start_at INTEGER, end_at INTEGER, place_id INTEGER);
```

### Vector tables (sqlite-vec)

```sql
-- SigLIP 2 base: 768-dim image/frame embeddings
CREATE VIRTUAL TABLE clip_vectors USING vec0(
  asset_id INTEGER PRIMARY KEY, frame_ms INTEGER, embedding float[768]
);
-- SFace: 128-dim face identity embeddings
CREATE VIRTUAL TABLE face_vectors USING vec0(
  face_id INTEGER PRIMARY KEY, embedding float[128]
);
```

### Sizing and search strategy

- 300,000 items × 768 floats × 4 bytes ≈ **920 MB** for SigLIP 2 vectors. Storing them as int8 (or float16) cuts that to \~230–460 MB with little quality loss. Face vectors are small (128 floats each).
- sqlite-vec brute-force KNN over a few hundred thousand vectors is typically fast enough for interactive search, especially with int8 and filters applied first. If profiling shows it's too slow, add an in-memory HNSW index (e.g. `usearch` via its Go bindings) loaded at startup, with SQLite remaining the source of truth. sqlite-vec's planned DiskANN index or SQLite's vec1 may make that unnecessary.
- Full-text search over filenames, tags, people and places via **SQLite FTS5**, merged with vector results.

### Design rules

- Content hash is the identity, so moving or renaming files keeps all names, tags and albums intact.
- Every AI-produced value records its source and model version; user edits always win over AI.
- Optional XMP sidecar export for tags, people and ratings, for portability.

## 6. Performance & Scale

Target: **300,000+ photos and videos** on a mid-range Windows laptop, with a UI that never stutters.

### Performance targets (initial)

| Metric | Target |
| --- | --- |
| App cold start to usable grid | < 2 s |
| Grid scroll | 60 fps at any library size |
| Search results (text or semantic) | < 300 ms |
| Initial browsable library (metadata + thumbnails) | \~minutes for 100k items |
| Full AI index, 100k photos, mid-range GPU | a few hours, in the background |
| Idle memory use | < 500 MB |

These are goals to validate with benchmarks early, not guarantees.

### Indexing engine

- **Job queue in SQLite** (`asset_jobs`) so work survives crashes and restarts, and resumes exactly where it stopped.
- **Pipelined workers:** separate pools for I/O (reading files), CPU (decoding, hashing, thumbnails via `libvips`) and inference (one or two ONNX sessions, batched). Bounded channels between them provide back-pressure.
- **Priority:** whatever the user is looking at is indexed first; newest photos before old ones; cheap stages before expensive ones.
- **Batched writes:** commit results in transactions of hundreds of rows, never row-by-row.
- **Politeness:** pause or throttle when on battery, when the user is gaming or in full screen, or when CPU is busy. Advanced setting: "index only when idle".

### Incremental updates

- `fsnotify` watches source folders; changes go into the job queue.
- On startup, a quick reconcile pass compares size + mtime to catch changes made while the app was closed.
- A moved or renamed file is matched by content hash, so no AI work is repeated.
- Upgrading a model re-runs only that stage, in the background.

### UI performance

- **Virtualized grid:** only render visible rows; fetch pages of IDs + thumbnail URLs by date range.
- **Thumbnail cache on disk** served via the Wails asset handler; decode at the exact display size.
- Precomputed date buckets (counts per day/month) make the timeline scrubber instant.
- Keep the WebView light: no heavy per-cell components; image elements with `loading="lazy"` and fixed sizes.

### Storage footprint (rough, 300k items)

- Database with vectors: \~0.5–1.2 GB (int8 vectors keep it near the low end).
- Thumbnail cache: capped at 2 GB by default (adjustable), and movable to another drive.

## 7. Build Roadmap

Rough durations assume one experienced developer part-time-to-full-time and new to Go, with AI agents writing most of the code; treat them as ranges, not promises.

### Phase 0 — Spikes & learning (2–3 weeks)

- Go reading fluency: structs, interfaces, explicit `error` returns, goroutines, channels, `context` cancellation.
- Wails v3 hello-world with the Vue frontend: one service call, one event, one custom asset handler. Set up llvm-mingw so cgo builds work on Windows.
- Throwaway spikes: run SigLIP 2 on 1,000 images with `onnxruntime_go` + DirectML; run YuNet + SFace and check clustering on family photos; make 1,000 thumbnails (HEIC included) with govips; load sqlite-vec from Go and do a KNN query; run PP-OCRv5 on screenshots.
- **Exit:** numbers on your own machine (images/sec per stage, search latency).

### Phase 1 — Fast library browser, no AI (4–6 weeks)

- Folder selection, scanner, hashing, EXIF, thumbnails, job queue.
- SQLite schema v1 with migrations.
- Virtualized date-grouped grid, timeline scrubber, viewer, details panel.
- File watching and reconcile.
- **Exit:** 100k items browse smoothly. Already a useful app.

### Phase 2 — Semantic search & auto-tags (3–5 weeks)

- CLIP stage, sqlite-vec storage, search box with natural language.
- Built-in tag vocabulary, Things view, custom user labels.
- Indexing progress UI, pause/resume, battery awareness.
- **Exit:** "beach sunset" finds the right photos across a large library.

### Phase 3 — People (4–6 weeks)

- Face detection, alignment, embeddings, clustering.
- People view: name, merge, split, hide, correct.
- Search by person; combine with text ("Ali at the beach").
- **Exit:** clustering quality you'd trust on your own family photos.

### Phase 4 — Videos (3–4 weeks)

- FFmpeg bundling, metadata, poster frames, keyframe sampling.
- CLIP + faces on keyframes; jump-to-timestamp from search.
- Hover-scrub previews and a video player.

### Phase 5 — Organization power (3–5 weeks)

- Albums and Smart Albums (rule builder).
- Duplicates and near-duplicates review screen.
- Events and Places (offline geocoding).
- Optional organize-on-disk with dry run and undo; XMP sidecar export.

### Phase 6 — Polish & ship v1 (3–4 weeks)

- Installer (NSIS via the Wails build tasks), code signing certificate, auto-update (to decide: must be opt-in, since the app promises no network use at all).
- Onboarding, empty states, error handling, crash logs (local only).
- Performance pass against the targets; testing on low-end hardware.
- Beta with real users' messy libraries.

### Later

- macOS and Linux builds (Wails supports both; the C libraries — ONNX Runtime, libvips, SQLite — need per-platform builds; test HEIC and GPU providers: CoreML on Mac).
- RAW formats, OCR search for text in images, quality/best-shot picking, memories/slideshows, local LLM captions, optional phone import over LAN.

## 8. Open Questions & Risks

### Decisions to make

- **Frontend framework:** Svelte (lighter, great for a fast grid) or React (bigger ecosystem, likely more familiar)?
- **Business model:** free/open source, one-time paid licence, or freemium? This affects which model licences are acceptable.
- **Default models:** small-and-fast vs larger-and-more-accurate as the default; ship both or download the larger one on demand (the only optional network use)?
- **Disk organization:** should v1 include moving/renaming files on disk, or stay purely a view layer until trust is earned?
- **Minimum hardware:** oldest Windows version and lowest RAM/GPU you'll support.

### Risks

| Risk | Mitigation |
| --- | --- |
| Model licences (some face models are non-commercial) | Audit every model licence in Phase 0; keep the model layer swappable |
| cgo on Windows (ONNX Runtime, libvips, SQLite) complicates builds and debugging | Set up the toolchain in Phase 0; wrap each C library behind one Go package; build the installer in CI on every change |
| Wails v3 is still in beta | Pin the exact beta version; its desktop API is frozen; Wails v2 is the fallback |
| SFace (the permissive face model) is less accurate than ArcFace | Test on real family photos in Phase 0; conservative thresholds; keep the face model swappable |
| Indexing too slow on low-end laptops | Quantized models, DirectML, stage priorities, idle-only mode |
| Face clustering quality disappoints | Conservative thresholds, strong correction UX, user edits always win |
| HEIC/RAW/video codec edge cases | Bundle FFmpeg + libheif; test against a corpus of messy real files |
| Installer size (models + FFmpeg, near 1 GB, all in one bundle) | fp16 models; shrink the SigLIP text tower's vocabulary table; libvips "web" build; LZMA-compressed installer |
| Windows SmartScreen warnings | Buy a code-signing certificate before public release |

### Immediate next steps

1. Pick the frontend framework and working name.
2. Collect a test corpus: 10k–50k of your own mixed photos and videos, including HEIC, WhatsApp images, screenshots and long videos.
3. Do the Phase 0 spikes and record real throughput numbers.
4. Revisit this plan with those numbers and adjust the targets and timeline.

## 9. Decisions Locked (2026-09-25)

- **Frontend framework:** Vue with Vite. Lighter than React, larger ecosystem than Svelte, clean reactivity for a live-updating grid. Virtualization handled via a library or hand-rolled.
- **Business model:** Open source from day one. Fits the fully-local, private, no-telemetry spirit and boosts credibility for a privacy-focused app. Keep the model layer swappable and audit each model licence early — models must have licences that permit open-source distribution and any commercial use. An open-source app is typically free; any revenue would come later from donations or an optional paid convenience, not from selling the app itself.
- **Default models:** Everything ships in one installer: SigLIP 2 base for search and tags, YuNet + SFace for faces, PP-OCRv5 mobile for OCR, FFmpeg for videos, all permissively licensed. After installing, the app never needs the internet: no model downloads, not even optional ones (decided 2026-09-26, replacing the earlier "larger model as an optional download"). Phase 0 puts the installer near 1 GB, mostly the SigLIP text tower; shrink it where quality allows (see spikes/README.md).
- **Disk organization:** Version one ships both modes with a simple choice: in-app organization by default (safe, non-destructive), plus an optional switch to actually move and rename files on disk. On-disk mode always requires a preview-first dry run and full undo.
- **Minimum hardware:** Windows 10 and up. CPU-first so it runs on any machine; if a DirectX 12 GPU is present it is used automatically via DirectML. Minimum 8 GB RAM.

* **Name:** Keep **Lumen** for now.
* **Development approach:** AI coding agents write the code; the builder develops reading fluency in Go to review, catch wrong turns, and debug — control through understanding, not writing every line.

- **Engine language:** Go, with Wails v3 as the desktop shell — final, decided 2026-09-26. Chosen over Rust because Go is far easier to read and review when AI agents write the code. The speed cost (roughly 10–30% on CPU-heavy work) matters little here: the heaviest work runs inside ONNX Runtime, libvips and FFmpeg, which are native code either way.

* **RAW formats:** Parked for later, not in version one.
* **OCR (text inside images):** In version one, but as an optional add-on the user turns on — search screenshots, receipts and documents by their text. Off by default because it is slow (about 0.5 s per screenshot); the model (PP-OCRv5 mobile, Apache-2.0, about 13 MB) ships in the installer, so turning it on needs no download. When enabled it indexes text in the background, only for images classified as screenshots or documents.

- **Performance targets:** Realistic goals to measure and adjust after early experiments, not hard requirements. Performance is a top priority in spirit, but no target is a contractual line.
- **Metadata portability:** Write tags, people and ratings back to the files so the user's work is never trapped in Lumen. Preferred method is embedding directly into the file's metadata where it is safe (e.g. JPEG); fall back to a sidecar file for formats where embedding is risky (e.g. HEIC, some videos). Opt-in, since it modifies originals — respects the "never touch originals without consent" pillar.

* **Deletion & Trash:** Two options. **Soft delete** removes a photo from Lumen's view only, leaving the file on disk untouched (app-level Trash). **Hard delete** removes the actual file from disk. Any real deletion always goes to the Windows Recycle Bin — never a permanent wipe — so a mistaken delete is always recoverable.

- **Auto-tag confidence:** A slider in Advanced settings lets the user choose where to sit between cautious-and-accurate (fewer, surer tags) and generous-and-comprehensive (more tags, more mistakes).
- **Real-time tagging:** The user can correct or add tags directly, in the moment, on any photo. User edits always win over the AI and are never overridden later.

* **Duplicates handling:** Lumen finds exact duplicates and near-duplicates (bursts, lower-quality re-saves). It pre-selects the best version (usually highest quality) to save the user time, but never deletes anything until the user confirms. Confirmed deletions go to the Recycle Bin.

- **Event detection gap:** Default split at a **12-hour** gap between photos (or a large location jump), leaning toward fewer, bigger events so a long single day stays grouped. Adjustable in Advanced settings.

* **Language / localization:** English only for version one. Build the UI so other languages (e.g. Urdu) can be slotted in later without a rewrite — keep the door open without paying the translation cost now.

- **Themes:** Both light and dark from day one in version one. Dark makes photos pop; light suits bright rooms — both are genuinely needed.

* **Version-one scope:** Feature-complete — photos, search, people, tagging, albums and smart albums, duplicates, events and places, and the organize-on-disk tool. OCR is an optional add-on. **Videos are included but behind a toggle that is off by default** (video indexing is the heaviest part of the pipeline; opt-in keeps indexing faster and the footprint smaller for photo-only users).

- **Data location:** Lumen's own files (database, thumbnail cache, AI models) live in the standard Windows app-data folder by default. A setting lets the user move the thumbnail cache — or the whole Lumen data folder — to another drive, since the cache can grow to several gigabytes and the C drive is often small or full.

* **Thumbnail cache cap:** Default maximum **2 GB**, adjustable in settings. When full, Lumen drops the least-recently-viewed thumbnails and regenerates them on demand, so the cache never grows out of control.

## 10. Module Architecture

Both sides are organized as self-contained modules with clear boundaries, so any one module can be worked on (by a person or an AI agent) without breaking the others, and the app can be understood one box at a time.

### Backend (Go engine)

Each Go package under internal/ has one clear job:

- **scanner** — walks folders, hashes files, watches for changes.
- **media** — decodes images and video, makes thumbnails, reads EXIF/dates.
- **ai** — runs the models: faces, CLIP tagging, OCR.
- **index** — the background work queue; orchestrates the pipeline.
- **store** — owns the SQLite database, migrations and all queries.
- **search** — turns a query into ranked results, merges text and vector search.
- **core** — shared domain types, config, errors.

Modules stay decoupled: e.g. **ai** hands its results to **store** and never needs to know how the database works.

### Frontend (Vue)

Organized by feature, not by file type:

- **library** — main photo grid and timeline.
- **search** — search box, filter chips, results.
- **people** — faces, naming, merge/split.
- **albums** — manual and smart albums.
- **duplicates** — the review screen.
- **settings** — toggles and sliders (confidence, models, indexing).
- **shared** — common UI used everywhere: buttons, dialogs, icons.
- **bridge** — the single place that holds every call to the Go backend (it wraps the TypeScript functions Wails generates), so engine communication lives in one spot instead of being scattered.

### Symmetry

A frontend feature maps to backend modules — e.g. the **people** feature talks to **ai** and **store** — so each side stays understandable on its own, and the two mirror each other.

## Sources (stack check, 26 Sep 2026)

- [Go 1.27 release notes](https://go.dev/doc/go1.27)
- [Wails v3 Beta announcement](https://v3.wails.io/blog/wails-v3-beta/)
- [Vue core releases (3.6 in RC)](https://github.com/vuejs/core/releases) and [Vue 3.6 RC status](https://blog.sparkles-editor.com/en/blog/vue-3-6-rc-upgrade-guide)
- [TypeScript 7 release](https://www.infoq.com/news/2026/08/typescript-7-released/)
- [Vite 8 with Rolldown](https://adonisjs.com/blog/adonisjs-vite-6)
- [onnxruntime\_go](https://github.com/yalue/onnxruntime_go)
- [DirectML in sustained engineering](https://onnxruntime.ai/docs/execution-providers/DirectML-ExecutionProvider.html) and [What is Windows ML](https://learn.microsoft.com/en-us/windows/ai/new-windows-ml/overview)
- [sqlite-vec Go bindings](https://github.com/asg017/sqlite-vec-go-bindings) and [SQLite vec1](https://sqlite.org/vec1)
- [govips](https://pkg.go.dev/github.com/davidbyttow/govips/v2)
- [FFmpeg downloads](https://ffmpeg.org/download.html)
- [SigLIP 2 base licence](https://huggingface.co/google/siglip2-base-patch16-224) and [MobileCLIP2 licence](https://huggingface.co/apple/MobileCLIP2-S0)
- [Face model licences: InsightFace vs YuNet + SFace](https://localai.io/docs/features/face-recognition/) and [go-onnxface](https://github.com/leandroveronezi/go-onnxface)
- [PP-OCRv5 ONNX models](https://huggingface.co/monkt/paddleocr-onnx)
