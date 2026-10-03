# Lumen: lossless image compression

Date: 4 October 2026. Status: planned, not implemented. This is Phase 11
of [the next development plan](next-development-plan-2026-10-04.md).

The product owner requested advanced lossless compression inside Lumen,
for an individual image or a bulk selection. Compression is an explicit
local tool, integrated with the new Nuxt UI design system, media actions,
format catalogue, settings and file-operation ownership. It never uploads
files and is not enabled automatically while indexing.

## 1. What lossless means

Lumen distinguishes three operations in the interface:

| Mode                       | Promise                                                                                                                                                                        | Default destination                                                            |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| Optimise existing format   | Preserve the image data and supported metadata while rewriting its compression structure. Keep the extension and compatibility.                                                | New copy; optional verified same-format replacement with recoverable original. |
| Convert losslessly         | Preserve exact source image samples, precision, alpha and colour interpretation in an eligible output format. The original file's bytes are not recoverable from this promise. | New copy only.                                                                 |
| Reversible JPEG to JPEG XL | Preserve reconstruction data and prove that the complete original JPEG can be recovered byte for byte.                                                                         | New `.jxl` copy with a Restore original JPEG action.                           |

Do not use a quality slider, resizing, chroma subsampling, near-lossless
preprocessing, transparent-pixel rewriting, tone mapping or colour-space
conversion in a lossless operation. Metadata removal is a separate,
explicit export option, labelled as removing information; it is not part
of the default compression promise. Preserve EXIF, ICC, embedded XMP,
capture dates, orientation, copyright and supported auxiliary content.

Compression operates on the original image representation, not the
colour-managed thumbnail or viewer preview. The current sRGB/8-bit render
path cannot be used to certify losslessness for a 16-bit or wide-gamut
source. A rendered RAW image is not the original sensor data.

For same-format JPEG optimisation, retain quantisation tables, sampling
and coefficients; verify structure and decode with the same pinned
decoder. For reversible JPEG XL, additionally reconstruct and compare a
full-file SHA-256 and byte length. The scanner's sampled large-file hash
is not sufficient for archival verification.

For raster conversion, compare full-resolution samples in a source-native,
unassociated-alpha representation at original precision, including RGB
under fully transparent pixels. Compare colour/profile interpretation and
metadata independently. A visual comparison, PSNR or SSIM score alone is
not a lossless check. Unsupported preservation requirements cause a skip
with an explanation, never silent downgrade to a weaker mode.

## 2. Codec strategy

Use mature optimisers plus modern lossless codecs, selected by measured
results on representative files. There is no single encoder that gives
the smallest output for every image. The product goal is the smallest
**verified** eligible candidate within the selected time/resource budget.

| Input / mode                          | Planned engine                                                                                                                  | Conditions                                                                                                                                                                                                   |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| JPEG, keep JPEG                       | Pinned libjpeg-turbo `jpegtran`, coefficient-domain entropy optimisation; compare baseline/progressive output where compatible. | Copy metadata and prove preserved coefficients/parameters. Skip unusual auxiliary-image or metadata structures the engine cannot preserve. No decode/re-encode JPEG quality setting.                         |
| PNG, keep PNG                         | Pinned OxiPNG; benchmark its regular compression and optional Zopfli search for Maximum mode.                                   | Preserve metadata, precision and exact alpha samples. Disable alpha rewriting and metadata stripping. Keep the best verified candidate, not blindly the highest effort level.                                |
| JPEG, reversible modern format        | Pinned libjxl JPEG reconstruction mode.                                                                                         | Reconstruction must reproduce the complete source file. Fail closed when reconstruction data cannot be retained; never silently use a pixels-to-JPEG fallback.                                               |
| Eligible static raster, modern format | Explicit lossless libjxl; compare lossless WebP for compatible 8-bit inputs.                                                    | Preserve precision, colour and metadata; WebP requires exact transparent RGB preservation. Select only candidates passing the complete contract.                                                             |
| Already-lossless WebP/JXL             | Re-optimisation through the corresponding pinned codec.                                                                         | Preserve samples and metadata. A reconstructable JXL must retain its original JPEG reconstruction bytes.                                                                                                     |
| BMP / supported TIFF                  | Lossless conversion or conservative TIFF recompression candidates.                                                              | Preserve every supported sample/page/tag; TIFF implementation is enabled only after camera, page and metadata fixtures pass.                                                                                 |
| GIF / APNG / animated WebP/AVIF       | Later sub-milestone, after static compression.                                                                                  | Compare every composited frame, frame timing, disposal, blend behaviour and loop count. Never flatten animation into one frame.                                                                              |
| RAW / DNG / HEIC / HEIF / AVIF        | Explicitly unsupported for source replacement initially.                                                                        | Compressed sensor data, gain maps, auxiliary images and colour/YUV paths need their own preservation design. A rendered copy is offered through export, not presented as lossless compression of the source. |

JPEG XL is the primary modern archival candidate, promoted from optional
format evaluation to a required decoder/reconstruction capability for this
milestone. Lumen must browse, index and restore its own outputs through the
native decoder before enabling JPEG XL compression. Other apps' support
is explained when the output extension changes; do not rely on WebView2
being able to display JXL directly.

JPEGli's improved lossy encoding is not a substitute for coefficient-
preserving JPEG optimisation. AVIF quality 100 or an encoder's “lossless”
flag alone also does not prove unchanged RGB samples after conversion.
Do not add obscure encoders solely to advertise a larger format list.

Pin tools, hashes, CPU compatibility and licences in the dependency build.
Bundle codecs in the existing runtime/installer inventory and generate
their notices. Use explicit verified flags from the pinned versions, not
unreviewed CLI defaults. Codec updates require preservation regressions
and a recorded benchmark before changing presets.

## 3. Individual and bulk UX

Add **Compress losslessly…** to the media command registry: image viewer,
details panel, thumbnail context menu and selection toolbar all open one
workspace. Provide a dedicated Tools → Compression page for runs/history,
without introducing another asset browser or another file picker model.

The workspace shows selected source paths, mode, output format,
destination, effort preset and metadata policy. Exact duplicate assets
have multiple paths: choose one readable copy by default, explain which
one, and offer explicit selection of other copies. Never silently change
every duplicate path. Reject duplicate jobs targeting the same file.

Workflow:

1. Resolve the selection with the existing visibility rules and catalogue.
   Explain unsupported files, locked files and preservation constraints.
2. Choose Compatible optimisation, Lossless conversion or Reversible JPEG
   XL, then Fast/Balanced/Maximum effort. Default to compatible optimisation,
   Balanced and new copies with metadata preserved.
3. Analyse/encode candidates into owned temporary files without changing
   originals. Show measured input/output sizes, verification status,
   format/compatibility changes and per-file skips. Estimates are labelled;
   a sampled bulk estimate is not a measured total.
4. Review the results and commit the selected verified candidates. The
   confirm action writes exactly the reviewed output, provided the source
   and destination still pass validation. Expired candidates are rebuilt
   and reviewed again if the source changed.
5. Show completed/skipped/failed/cancelled counts, bytes saved, destinations,
   recovery information and appropriate reveal/view/restore actions.

Support pause, resume, cancellation, retry failed items and restart
recovery, including cancellation during candidate analysis. Progress
reports processed files and phase, not invented codec percentages. The UI
remains navigable during a bulk run.

An output is committed only when it is smaller than its source and passes
verification. Otherwise report **Already optimised / no smaller lossless
result** and leave the source alone. A minimum savings preference can
avoid insignificant gains. No fixed compression percentage is promised.

Show output-size reduction separately from actual storage impact:
retaining originals, additional outputs and recovery backups may consume
more space overall. Include temporary/backup space in the preflight disk
budget. Compression must not claim freed disk space while those bytes are
still retained.

## 4. Services, jobs and file safety

Use one `internal/compress` coordinator for candidate generation,
eligibility and verification. It consumes the media catalogue and shares
existing cancellation, resource-budget and subprocess conventions.
Encoders write only in owned temporary directories. `internal/fileops`
alone commits outputs and changes paths in user folders. Only `store`
writes SQL; thin services expose generated core types through the bridge.

Persist compression runs and per-file jobs in new migrations. Record
source identity/revision and full hash, plan/options, tool version,
candidate path/hash, verification result, destination and original backup.
Use states such as planned → encoding → verifying → ready → committing →
done, with paused/skipped/cancelled/failed/recovering states. Reconcile disk
and journal after crashes; do not assume SQL and filesystem changes are
one atomic transaction. Polling/events expose bounded typed progress.

Apply a shared CPU budget, bounded process count, per-file memory/dimension
limits and disk-space reserve. Maximum mode increases search effort, not
pixel loss. Honour user pause and existing politeness holds; foreground
viewing/playback retain priority. Avoid multiplying process concurrency by
each encoder's default “all cores” threading. Pause takes effect between
bounded jobs or checkpointable stages; cancellation terminates and waits
for non-cooperative workers and their children.

Default new copies use exclusive temporary creation and a same-volume,
non-overwriting commit. Validate destinations, link/junction behaviour,
source changes and access permissions. A changed source invalidates the
ready result. Hold a per-file operation lease and coordinate folder
watching and organise/recycle/export jobs to prevent overlapping mutations.
Do not hold a global scan pause for the whole encoding run.

**Optional same-format replacement** ships only after the reversible
transaction sub-milestone passes. The owner must explicitly choose it in
the run preview; it is not a preset's hidden default. Preserve the original
as a verified recoverable file before committing the smaller result, and
journal both paths and hashes. Test Windows replacement/rollback behaviour,
attributes, permissions, timestamps, source locks and failures at every
boundary. Initially skip hard-linked sources, alternate data streams and
other structures that cannot be preserved safely.

Undo restores only when the committed output still matches the journal;
subsequent user edits cause a conflict and preserve both versions. Backups
are not disposable thumbnails and are not silently evicted by a cache cap.
The user can explicitly send superseded originals to the Recycle Bin;
when Windows cannot recycle, keep them. No permanent deletion is added.
If this transaction is not ready, only the new-copy mode is available.

Treat source bytes and library identity separately. A recompressed file's
content hash changes even when pixels do not. Update the chosen file row
and asset association explicitly so favourites, names, tags, albums and
user corrections survive. If one duplicate path is recompressed, the other
copies remain valid; refreshing exact-duplicate groups must not erase
those annotations. A format conversion creates a related output asset,
with intentional annotation transfer that does not create false exact
duplicates. Cache/stage reuse follows verified pixel/metadata equivalence;
unknown equivalence causes targeted regeneration.

Preserve source `.xmp` files unchanged during same-format optimisation.
Converted outputs need an explicit filename/sidecar mapping through
`fileops`; never overwrite or silently copy Lumen ownership onto a sidecar
edited by another application. Reversible JPEG XL restores the embedded
JPEG bytes; an external sidecar is separately preserved and mapped.

Recheck visibility at planning and commit. Locked/blocked and trashed
sources cannot be compressed through ordinary library actions. Gate
revocation cancels reads/previews and prevents commit. Logs and run reports
stay local and disclose that they may contain file paths.

## 5. Preferences and acceptance

Add Compression under the existing typed settings owner: default mode,
effort, output preset, minimum savings, resource budget and metadata
preservation. Replacement always needs a run-level explicit choice.
Destination paths and history do not enter portable UI presets. Preset
imports cannot start jobs, enable replacement or discard metadata.

| Milestone | Deliverable                                                                           | Exit evidence                                                                                                                      |
| --------- | ------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 11A       | Preservation contracts, fixtures, codec/effort benchmarks and catalogue capabilities. | Native precision/metadata checks; measured bytes, time and peak memory on photos, screenshots, scans and already-optimised images. |
| 11B       | Individual compatible JPEG/PNG optimisation with verified new copies.                 | Source unchanged, candidate smaller, metadata preserved, cancellation and destination races handled.                               |
| 11C       | Bulk workflow and durable run history.                                                | Large selection, bounded resources, partial failures, pause/resume and crash recovery.                                             |
| 11D       | Reversible JPEG XL and eligible exact raster conversion.                              | JXL viewing/indexing and complete JPEG hash round trip; native sample/alpha/profile equality for conversion.                       |
| 11E       | Optional same-format replacement and Undo; additional formats only when verified.     | Fault injection around every commit/backup/database transition; no annotation drift, source loss or overwriting later edits.       |

Fixtures cover EXIF orientations, ICC/wide gamut, 8/16-bit images,
transparent RGB, palette/greyscale, unknown metadata chunks, embedded XMP,
maker notes, unusual JPEG scans, auxiliary images, large/corrupt inputs,
duplicates and user-edited external sidecars. Animation fixtures compare
all frames/timing before animated support is advertised.

Use independent decode paths on conformance fixtures to avoid a matching
encoder/decoder bug masking data loss. Production verification is mandatory
for every committed output; it is not just a unit-test assertion. Record
tool versions and verification mode in run history. Test low disk space,
read-only/removable/network drives, collisions, links, source edits during
encoding, restart at every journal boundary and privacy revocation.

Benchmarks select defaults and bounded search candidates per format;
publish measured distributions and skipped/no-gain cases, not an absolute
“best compression” claim. Run existing frontend/bridge/Go/race/build gates,
native UI and installer acceptance when this feature is implemented.

## Sources checked

- [JPEG XL standard overview](https://jpeg.org/jpegxl/): lossless coding, legacy JPEG recompression and reconstruction data.
- [libjxl reference implementation](https://github.com/libjxl/libjxl) and [changelog](https://github.com/libjxl/libjxl/blob/main/CHANGELOG.md): explicit encoding/reconstruction modes and decoder integration.
- [libjpeg-turbo usage](https://github.com/libjpeg-turbo/libjpeg-turbo/blob/main/doc/usage.txt): coefficient-domain JPEG transcoding and metadata-copy controls.
- [OxiPNG documentation](https://github.com/oxipng/oxipng): PNG search effort, optional Zopfli, metadata and transparent-pixel cautions.
- [WebP encoder documentation](https://developers.google.com/speed/webp/docs/cwebp) and [lossless FAQ](https://developers.google.com/speed/webp/faq): exact transparent samples, lossless flags and conversion limitations.
- Local ownership evidence: `internal/fileops/fileops.go`, `internal/fileops/runner.go`, `AUDIT_CONVENTIONS.md` and the next development plan.
