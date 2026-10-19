# Phase 11: lossless compression

Date: 9 October 2026. Implements the static-image milestones 11A–D from
[the compression plan](lossless-compression-plan-2026-10-04.md). This record
supersedes that plan's implementation status. Optional replacement with Undo
(11E) remains deferred. The application version is 0.11.0.

## Behaviour and supported inputs

The grid, selection toolbar, viewer and inspector share compression commands.
Tools → Compression accepts individual photos or the selected set. A source
with identical library copies offers an explicit path choice; automatic choice
uses one available copy. Defaults favour compatible output, balanced effort,
one thread and no minimum saving. Settings, customisation export/import and
category reset use the existing preference owner.

Analyse produces measured, verified candidates without changing originals or
publishing files. Review lists original/output bytes, destination, preservation
check and per-file outcome, with bounded result pages. Save selected copies
requires confirmation of exactly the selected ready candidates. Successful
outputs can be revealed; verified reversible JPEG results can prepare a
separately reviewed restoration. History is local to the library.

| Source and mode | Publication | Verification and conservative limits |
| --- | --- | --- |
| JPEG, compatible | New JPEG | Equal quantised coefficients, coding parameters, supported APP/COM payloads and order, plus decoded comparison using the pinned decoder. Baseline/progressive 8-bit sources only. |
| JPEG, reversible | New JPEG XL | Reconstruct the complete original JPEG and compare its full SHA-256 and length. Restoration repeats this comparison before publishing a new JPEG. |
| PNG, compatible | New PNG | Exact native samples, precision, alpha including hidden RGB, IHDR, ancillary payloads/order and their position around IDAT. Greyscale, palette, RGB/RGBA and 16-bit fixtures are covered. |
| PNG, lossless conversion | New JPEG XL | Decode the candidate back to PNG and compare every native sample and precision. Initially only plain 8/16-bit RGB/RGBA without ancillary metadata; only known standard sRGB representations from the reconstruction decoder are accepted. |
| Other or ambiguous inputs | Per-file skipped outcome | No animation flattening, rendered-image substitution or guessed metadata conversion. |

Input verification is bounded to 256 MiB and 16 million pixels. Animated PNG,
multi-picture JPEG, JPEG JUMBF/APP11, metadata after the first JPEG scan and
trailing JPEG payloads are rejected. CRCs and PNG structure are checked.
PNG conversion with ICC, EXIF, text or other ancillary chunks remains
unsupported until a complete mapping is implemented. TIFF/BMP/WebP/AVIF/HEIF,
RAW and animation compression remain unsupported. JPEG XL copies use Lumen's
native decoding and index pipeline; other applications' compatibility
varies.

Copies retained beside originals increase total disk usage. The review shows
both the copy's reduction and bytes added; it makes no net-space-saving promise.
Copies published into an indexed folder receive the original's favourite,
manual tags/album/event membership and manual face corrections, while content
and derived stages belong to the new asset. An existing identical asset keeps
its annotations. Outside-library destinations receive image metadata and
external XMP; manual library annotations remain with the source. Outside
reversible copies can still be restored through their verified journal.
Known automatic face detections also carry over provisionally, so person
blocks apply before the new copy's analysis completes. They retain automatic
ownership and are replaced by the normal faces stage; manual corrections and
rejections remain protected.
Known places and look-alike face-block matches also carry over pending
analysis, keeping the canonical visibility rule effective from registration.
Single-face block rules retain their original scope.

## Ownership, recovery and bounds

`internal/compress` coordinates analysis; `internal/media.RunTool` contains
codec subprocesses; `internal/fileops.CompressionPublisher` owns publication;
`internal/store` owns SQL and annotation reconciliation. Migration 0031 records
runs and per-file checkpoints. `CompressionService` is registered through the
same error-code wrapper as the other services, with generated TypeScript types
and one bridge owner. No codec runs or downloads in the WebView.

Full source/candidate hashes, lengths and source timestamps are checked before
publication. Source read leases refuse concurrent writes/deletes; destination
ancestor leases refuse rename/junction substitution. Outputs use an exclusive
adjacent temporary file, flush and a no-overwrite publication. Source sidecar
changes, destination collisions, blocked sources/destination folders and
indexed-path conflicts stop that item without overwriting later work.
External XMP is copied unchanged with the image's new filename.
Image and sidecar read leases remain open through verification, copying and
database registration. A regression holds SQLite's writer occupied and checks
that neither verified output can be edited while registration is pending.

The durable queue checkpoints encoding, verification and publication.
Startup discards only owned interrupted encoder work, preserves ready reviews
and reconciles published files by exact hashes. A matching publication is
settled idempotently; an unrelated collision remains untouched. Cleanup refuses
to follow a substituted temporary-directory ancestor. Cancellation closes the
codec job tree and waits for the worker; shutdown waits before closing shared
owners. One active compression worker uses the existing indexing politeness
and CPU budget. Publication shares file-operation ownership and scan holds.
Blocking is checked throughout analysis and again at commit.

JPEG XL viewing and thumbnails now use the same pinned decoder, carrying a
bounded in-memory PNG with straight alpha and its ICC profile into the common
libvips conversion. EXIF extraction handles compressed boxes and feeds capture
date, camera/GPS and inspector fields. JPEG and JPEG XL share the same primary
EXIF parser and fallback, including preferred camera-name interpretation.
Native decoding applies orientation
once. The decoder retains no temporary pixel files; PNG output is capped at
64 MiB and embedded profiles at 4 MiB.
Native decoding uses the existing 1.5 GiB render memory budget; compression
encoding and reconstruction keep their separate 512 MiB budget.
Updated metadata, thumbnail and render versions invalidate older JPEG XL
results. Upgrade rules keep other formats' results. A changed thumbnail
requeues its embedding, faces and OCR through the
existing durable dependency rules; manual face assignments remain protected.

Runs contain at most 10,000 inputs, history returns the latest 100 runs and
results are paged at 100. Encoders have a 512 MiB process-tree memory limit,
90-second ordinary timeout and five-minute Maximum timeout. Available disk
space is checked before analysis and publication, including a reserve.
Actual thread grants are bounded by the shared CPU budget. Effort changes
search time; every preset has the same preservation checks.

## Dependency and benchmark decisions

The installer carries libjpeg-turbo 3.1.2, OxiPNG 9.1.5 and libjxl 0.12.0.
Downloads have fixed SHA-256 checksums. OxiPNG is compiled from the published
crate's locked source using portable Rust 1.99.0 GNU and llvm-mingw clang,
avoiding a separate Visual C++ runtime installation. Cargo checks dependency
checksums from Cargo.lock; static dependency and standard-library licence texts
join the installer notices. None of this tooling is downloaded at runtime.
The locked libdeflate version uses an obsolete clang AVX-512 target attribute;
its supported build flags disable those dispatch variants, retaining portable
SSE/AVX2 implementations without editing the dependency sources.

`python e2e/perf/compression.py` produces an ignored report containing input
and output sizes, exact arguments, elapsed process time and Windows peak
private commit. The initial matrix used generated screenshot, greyscale scan,
already-optimised PNG and metadata-rich JPEG fixtures on the development PC;
concurrent development checks make these indicative measurements, not a
general photo-library performance claim. Preservation has separate tests.

| Case | Input bytes | Output bytes | Approximate time / peak private commit |
| --- | ---: | ---: | --- |
| Screenshot PNG, OxiPNG level 1 / 2 / 6 | 6,403,082 | 9,510 / 9,454 / 8,650 | 0.57 / 1.03 / 8.34 s; 48–53 MiB |
| Screenshot PNG, Zopfli | 6,403,082 | 8,164 | 252 s; 80 MiB |
| Greyscale scan, level 1 / 2 / 6 | 1,601,591 | 31,271 / 30,052 / 28,472 | 0.14 / 0.27 / 4.36 s; 19–21 MiB |
| Greyscale scan, Zopfli | 1,601,591 | 26,103 | 144 s; 48 MiB |
| Already-optimised PNG, levels 1 / 2 / 6 | 8,527 | 8,527 | 0.32 / 0.64 / 7.37 s |
| JPEG, optimised baseline / progressive | 178,827 | 101,119 / 95,819 | 0.16 / 0.09 s; about 5 MiB |
| Reversible JPEG XL, effort 3 / 7 / 9 | 178,827 | 45,113 / 45,113 / 34,963 | 1.40 cold / 0.13 / 0.46 s; about 90 MiB |
| PNG to JPEG XL, effort 3 / 7 / 9 | 6,403,082 | 15,499 / 3,375 / 2,860 | 0.52 / 0.72 / 2.12 s; 79–115 MiB |

Compatible PNG tries levels 1, then 2, then 6 for Fast, Balanced and Maximum,
retaining the smallest verified result. Zopfli is excluded: its modest extra
gain on these fixtures cost several minutes. JPEG tests both baseline and
progressive optimisation. JPEG XL uses efforts 3/7/9. A candidate below the
chosen minimum saving is discarded with a no-gain outcome.

A second 17-case run with the final GNU-built OxiPNG (`--no-zopfli`) produced
the same PNG byte counts. Screenshot levels 1/2/6 took 0.29/0.48/4.84 seconds,
with 48–53 MiB peak private commit. Each final report row includes the actual
encoder binary's SHA-256. These figures are from the same generated fixtures
and retain the measurement limits above.

## Acceptance evidence

Regression coverage includes exact metadata/sample preservation,
full JPEG restoration, visibility, duplicate paths, related-copy annotations,
result pagination, source changes/collisions, interrupted publication, selection
changes during asynchronous confirmation and worker cancellation/shutdown.
The camera-name and publication-lease regressions were also run with their
fixes temporarily removed; both failed at the intended assertion.

- Full project Go race suites passed (`-p 1 -count=1 -timeout 900s`), followed
  by complete race runs of fileops, compression and store after the final
  publication lease repair.
- The complete `scripts/lint.sh` gate passed: Go formatting/lint/deadcode,
  govulncheck with no reported Go vulnerability, frontend type checking,
  ESLint/Prettier, all 582 Vitest tests across 56 files, Python, ShellCheck
  and four installer payload regressions. The final Go lint run also reports
  zero issues after the publication lease repair.
- `wails3 task package` passed and produced `bin/Lumen.exe` and the unsigned
  `bin/Lumen-0.11.0-x64-setup.exe` (879,013,377 bytes). Frontend production
  output finished before Go embedded it. Generated service bindings and
  platform version assets are regenerated for 0.11.0.
- The installer was extracted into an ignored scratch directory. Its
  executable, third-party notices and all six compression codec files match
  the staged build by SHA-256. With the runtime override removed, native
  acceptance ran against that extracted executable in sequential isolated
  libraries: compression **41/41**, customisation **45/45**. Both reported
  no uncaught frontend errors and no off-device request.
- Native compression acceptance covered grid/viewer entry points without
  automatic jobs, reviewed publication and related favourites, restart and
  cancellation, complete JPEG restoration, JPEG XL capture metadata/lens,
  upright dimensions and rendering, PNG conversion/rendering, partial skips,
  no gain, destination collisions, indexing pause, defaults/reset and layouts
  at 1000/1440 pixels. The completed-copy screenshot was visually checked.
- `git diff --check` passed. The index remains empty; implementation and
  regression changes are unstaged.

Local gate logs are ignored under `deps/cache/phase11-*`. Native reports and
screenshots are in `e2e/.work/results/compression/` and
`e2e/.work/results/customise/`; the benchmark report is in
`e2e/.work/results/compression-benchmark/matrix.json`.

Clean-VM installer execution, signing and a representative camera-photo
compression matrix remain external acceptance limits. A pre-existing npm
production advisory affects source-map-js 1.2.1's indexed-source-map parsing;
the feature adds no dependency or external source-map input. This is recorded
separately from the preservation and native workflow results.
