# Phase 8 implementation and verification

Implemented locally on 5 October 2026 on `update/phase-8`, from merged main `4fc704a`, following Phase 8 of the [development plan](next-development-plan-2026-10-04.md). Changes are uncommitted. This record separates implementation and local verification from release, device and scale acceptance.

## Delivered behavior

### Photos

The shared image surface provides fit, actual available pixels, pointer-centred wheel/pinch zoom, drag-to-pan, double-click fit/actual switching, keyboard zoom and resize constraints. The viewer supports fullscreen, an optional virtual filmstrip, slideshow interval/loop and comparison with independent or linked zoom/pan. Neighbours come from the active source, including ranked search and the unlocked Blocked browser.

Rotation and horizontal flip are library presentation metadata. Viewer, comparison and tile thumbnails apply them; canonical dimensions, face crops and AI inputs retain the original pixel geometry. Reset increments the thumbnail presentation version so a browser cannot reuse an old adjusted tile. Rendered exports can bake the adjustments into a new copy.

Browser-readable images up to 16 megapixels can use their originals. Other admitted images use the existing upright sRGB decoders in a separate runtime worker, with a 20-second deadline, Windows Job Object memory limit of 768 MiB and 64 MiB output limit. One render runs at a time; retained renders are capped at 256 MiB. Source dimensions above 100 megapixels are refused by the render worker. Viewing renders stop at 4096 pixels on the long side and report their available dimensions. Cache keys include path, source size/mtime, orientation, requested size, decoder and presentation versions. Navigation cancels obsolete RPC work; source changes prevent stale publication.

### Video

Phase 7's custom player gains configurable skip interval, speed, loop, autoplay and volume defaults. FFprobe supplies actual audio/subtitle streams. Choosing an audio stream uses explicit FFmpeg stream mapping and conversion. Supported text subtitles become local WebVTT cues; unsupported bitmap subtitles are disabled and explained. Source changes cancel obsolete conversion requests and subtitle fetches.

Resume history is off by default, stored in the library only after opting in, and can be cleared. Covered and trashed videos cannot write resume history. Save current frame captures the absolute source time and opens an image-export dialog requiring an explicit destination and preview/confirmation.

### Inspector, selection and actions

The resizable inspector has Overview, Metadata, Organisation and Files sections. It reads lens/exposure/aperture/ISO, orientation, embedded-profile presence, page/frame count and video frame rate/tracks where available. Missing metadata remains explicit. Copy controls use the existing notification owner; per-copy path/name/reveal/open actions validate file membership.

The grid uses Ctrl-click, a stable Shift anchor, select-visible and explicit select-all-results. Backend snapshots resolve visible IDs in timeline or search order and carry membership revisions. Changes to visibility, filters, tags, faces, albums and events invalidate snapshots before batch admission. Selection does not require mounting every result. The selection bar occupies the page's toolbar slot, outside the grid's absolute scroller.

Batch favourite, tags, manual album membership, block, Trash/restore and recycle reuse existing services and confirmation/privacy workflows. Results distinguish successful, failed and cancelled items; large outcome lists are paged. Block uses the shared PIN dialog. Trash offers its existing valid Undo.

The shared overlay coordinator assigns paint order when a portalled layer opens. Global confirmation and privacy dialogs can cover the feature dialog or viewer that opened them, regardless of persistent portal DOM order. Reka retains focus and dismissal ownership.

Explicit rename/move operates on one primary copy per selected asset, keeps its extension and stays inside its indexed source. Destination names, links, excluded/blocked folders, disk/index collisions and source revisions are checked. Existing Organise admission, scan holds, sidecar handling, durable progress, crash recovery and Undo retain asset identity. Public move outcomes hide covered paths. Existing XMP dispositions and companion paths are shown in the preview/outcomes.

Original copies preserve the selected source's exact bytes and embedded metadata; they do not copy companion XMP files. Rendered JPEG/PNG copies use upright sRGB pixels and strip source metadata. The user chooses format, long-side limit, JPEG quality and whether to bake presentation adjustments. Outputs never replace an existing destination. Each export writes an adjacent owned temporary file and journals its digest before publication. Cancellation, changed sources and privacy revocation prevent publication and remove temporary copies. Recovery distinguishes committed bytes from unrelated destination files. Completed exports have no Undo.

## Ownership and persistence

Go wire types live in `internal/core`; Wails bindings are regenerated. Components call only the typed bridge. Viewing preferences use the existing queued preference save owner. SQL stays in the store; file writes stay in `internal/fileops`. Export and move execution share the Organise admission/lifetime owner and scan hold. Metadata/render requests are tracked through service shutdown and the existing privacy gate.

Migrations 0028–0030 add presentation adjustments, optional playback positions, export batches/jobs and selection revisions. Existing preference defaults and downgrade-test fixtures are updated. No originals change during viewing or theme changes.

## Verification

- `bash scripts/lint.sh`: passed, including Go lint/deadcode/vulnerability checks, Vue typecheck, ESLint, Prettier, **495 frontend tests across 47 files**, Python lint, four installer regressions and ShellCheck.
- `go test -race -count=1 -timeout 900s . ./internal/... ./services/...`: passed across all packages. Subsequent store, file-operation and service regressions passed with the race detector; the final selection revision-fence change also passed targeted store race tests.
- Production `wails3 build`: passed. The build reports a main JavaScript chunk above Vite's 500 kB warning threshold. No installer or release package was produced for Phase 8.
- Regenerated Wails bindings: deterministic, with identical before/after SHA-256 lists. `git diff --check`: passed.
- Production Windows WebView2 acceptance: **52 checks passed, zero failed, zero uncaught frontend errors**, using 17 isolated fixtures. The [recorded results](phase8-native-acceptance-2026-10-05.json) include EXIF orientation, profile detection, 6000 × 4000 TIFF and HEIC renders; actual-size/zoom/pan/fullscreen; bounded filmstrip, comparison and slideshow; selection and physical batch-confirmation clicks; original/rendered exports; rename identity and Undo; stale selection refusal; MP4/MKV playback, alternate audio and visible text subtitle cues; confirmed frame exports through the UI; opt-in resume and privacy revocation. Viewer and subtitle screenshots were visually inspected.

The accepted executable is `bin/Lumen.exe`, SHA-256 `B63E9209E69F3587D74792263D57F2D2337D9F54416EED40BC3E8BB20536FE9F`. The local acceptance harness is `.task/phase8-native/accept.mjs`, run through `.task/phase8-native/run.sh`; preparation is in `.task/phase8-native/prepare.sh` and `prepare.py`. These scratch scripts are ignored by Git. Fixtures, fresh per-run library profiles, outputs and screenshots are under `D:/tmp/lumen-phase8-2026-10-05`; no normal library was used. The harness launches its own process with an isolated `LUMEN_DATA_DIR` and DevTools port and closes it at the end.

## Explicit limits and remaining acceptance

- There is no tiled full-resolution viewer. HEIC/TIFF viewing is bounded; sources beyond admission limits keep their indexed preview. RAW/DNG work belongs to Phase 9.
- Multi-page/animated sources report their declared page/frame count; this phase does not add page navigation or a new animation decoder.
- Bitmap subtitle rendering and native direct-play alternate-audio switching are not claimed. Audio selection uses conversion.
- JPEG/PNG exports are rendered copies, including an intermediate JPEG viewing render. PNG export does not imply lossless preservation of the source's pixels, bit depth, alpha or metadata.
- File clipboard transfer is not exposed. Windows Open/Open with are implemented through the native shell; manual OS-dialog/app hand-off acceptance remains separate from automated WebView checks.
- Cross-source and cross-volume moves remain outside the explicit move tool. Existing Organise recovery and Undo rules are reused.
- Large real-library timing/memory, touch hardware, codec/hardware matrices, clean-machine installation, signing and release packaging remain separate acceptance work. Nothing has been published.
