# Phase 9: camera RAW first, then broader formats

Implemented locally on 7 October 2026 on `phase9/formats`, from merged main
`37da4c5`, following Phase 9 of the
[development plan](next-development-plan-2026-10-04.md). Nothing is
committed. This record says what was built, the decisions taken and why, what
was measured, and what remains unproven.

## Delivered behaviour

### 9A. Camera RAW

Lumen takes these files into the library as photos: Adobe DNG (`.dng`),
Canon (`.cr2`, `.cr3`, `.crw`), Nikon (`.nef`, `.nrw`), Sony (`.arw`, `.sr2`,
`.srf`), Fujifilm (`.raf`), Olympus and OM System (`.orf`), Panasonic (`.rw2`)
and Pentax (`.pef`). Existing libraries find them on the next scan.

They are read by `lumen-raw.exe`, a worker that links LibRaw 0.22.2
statically and runs once per file (`native/lumen-raw/main.cpp`). It answers
three questions on standard output and never writes a file:

| Operation | Answer |
| --- | --- |
| `info` | Make and model, the developed picture's upright size, capture time, ISO, shutter, aperture, focal length, lens, GPS, the largest embedded JPEG's size |
| `develop <side>` | The sensor data as upright 8-bit sRGB pixels, halved by LibRaw while the long edge stays at least `side` |
| `preview` | The largest JPEG the camera embedded, and the orientation still to apply |

The engine starts it below normal priority for the index and at normal
priority for the viewer, puts it in a Windows job with a 1 GiB memory limit,
gives it one minute, and kills it when the request goes away. Exit code 3
means LibRaw cannot read the file or that part of it.

**The developed sensor data is the photo.** Thumbnails, the perceptual
hash, the pixels the AI and face models read, and the viewer's render all
come from the same development, so a face found in one lands on the same
spot of the others. The development policy is fixed and written in the
worker: the camera's white balance (daylight where it recorded none), the
camera's colour matrix, sRGB primaries and tone curve, 8 bits, LibRaw's
automatic brightening, clipped highlights, the camera's orientation
applied. Bayer sensors use LibRaw's default interpolation at full size;
Fujifilm X-Trans uses one pass, not three.

A file whose sensor data LibRaw cannot develop, but whose embedded JPEG it
can read, shows that preview instead. The render carries a mark, the viewer
says "Camera preview from the RAW file … Its sensor data could not be
developed", and `RenderInfo.preview` reports it. A file LibRaw cannot read at
all stays in the library without a preview: its metadata and thumbnail jobs
are skipped with the reason (`media.ErrUnsupported`), not failed and retried.
Without the worker (removed or quarantined) every RAW is skipped the same way.

A RAW is never sent to the WebView as an original (`caps.original` is
false). `caps.render` is true where the render worker exists and the
developed size fits its budget. The inspector shows lens, exposure, aperture
and ISO from LibRaw.

A RAW and the JPEG the camera saved beside it are two assets. The
Duplicates screen never groups a RAW with a picture of another format as
"similar", so it never suggests removing one of the pair; two RAW frames of
one burst still group, and exact byte copies of a RAW are still exact
copies. Among copies the best-copy rule now ranks RAW above the lossless
formats.

### The format catalogue

`internal/core/formats.go` is the one list of what Lumen recognises: each
extension with its kind, its `Family` and the MIME type the WebView is sent
(empty for a file it is never sent as it is). `KindOf`, `WebNative`,
`ContentType` and `PlaybackOf` read it, as do the scanner's transport-stream
check, the decoders' routing, the colour-upgrade rule and the best-copy
rank. No other file compares extensions. Which decoder reads a family
stays `internal/media`'s choice (`Decoders.open`).

### 9B. Animation and pages

- Animated GIF, WebP and AVIF already play in the viewer from their
  originals. `.apng` is now recognised and sent as `image/apng`. The index,
  the hash and the AI use the first frame.
- A TIFF may hold several pages (`caps.paged`). The viewer's toolbar shows
  "Page 2 of 3" with previous and next buttons when the file's inspection
  reports more than one, and each page is rendered and cached on its own
  (`MediaService.RenderPage`, `/media/render/{id}/{side}?v=…&page=n`). The
  grid, the details and the models know the first page. A page the file does
  not hold is refused.
- The inspector says "Pages" for a TIFF and "Animation frames" for an
  animation, and nothing for a still picture.

### 9C. JPEG XL

`.jxl` is indexed and viewed: the bundled FFmpeg's libjxl decodes it, FFprobe
reads its size, and the viewer shows it through the bounded render. The
libvips build has no JPEG XL loader, and the WebView does not show one.

### 9D. Video containers

`.mpg`, `.mpeg`, `.ts`, `.ogv` and `.asf` are indexed (metadata, poster,
sprite, frames) and play through the converter; none plays directly in the
WebView. A TypeScript source named `.ts` is told from a transport stream by
its first packets, as `.mts` already was.

## Decisions

| Decision | Reason |
| --- | --- |
| LibRaw is built from pinned source by `scripts/deps.sh`, with the llvm-mingw clang cgo already needs, into one static executable. | LibRaw's official Win64 binaries import `MSVCP140.dll` and `VCRUNTIME140.dll`, which a clean PC may lack; the build here imports only Windows' own UCRT. It compiles in about 25 s. |
| A worker executable, not a library linked into `Lumen.exe`. | A malformed or enormous RAW gets a process lifetime and a job memory limit, as the plan asked. The engine loads no new native code. |
| LibRaw is used under CDDL-1.0, one of the two licences it offers. | Its source is unmodified and linked into a program of its own. `THIRD_PARTY_NOTICES.txt` names the version, the source URL, the copyright notice and the licence text. |
| Development is canonical; the embedded preview is only a fallback. | Embedded previews measured from 640×424 to full size, and one camera's preview is a crop of its sensor. Mixing sources per file would break the shared geometry faces rely on. |
| No JPEG or zlib library is compiled into LibRaw. | Each is another pinned native build. The cost is stated below: lossy-JPEG and deflate-compressed DNG are not read. |
| JPEG XL goes through FFmpeg. | The bundled build already carries libjxl; nothing new ships. |

The render worker's limits changed to hold a RAW development: its job
memory limit is 1.5 GiB (was 768 MiB) and its deadline 45 s (was 20 s).

## Measurements

Thirty-two camera files from [raw.pixls.us](https://raw.pixls.us/) (CC0),
fetched and checked against that site's SHA-256 list by
`D:\tmp\phase9\fetch_raw.py`. One process per run on the i7-13700H laptop
described in the Phase 7 feasibility record, on mains power, while other
work was running: these are indications, not benchmarks. Time is wall time
for the whole worker process; memory is its peak commit.

| Camera file | Developed size | `develop 1024` (thumbnails) | `develop 4096` (viewer) | Embedded preview |
| --- | --- | --- | --- | --- |
| Adobe DNG Converter/Canon EOS 5D Mark III/5G4A9394-compressed-lossless.DNG | 5796×3870 | 2898×1935, 617 ms, 107 MB | 5796×3870, 2324 ms, 283 MB | 5760×3840 |
| Adobe DNG Converter/Canon EOS 5D Mark III/5G4A9394-compressed-lossy.DNG | not read | not read | not read | none read |
| Adobe DNG Converter/Canon EOS 5D Mark III/5G4A9394-uncompressed.DNG | 5796×3870 | 2898×1935, 280 ms, 106 MB | 5796×3870, 2124 ms, 283 MB | 5760×3840 |
| Apple/iPhone 12 Pro/IMG_1361.DNG | 3024×4032 | 3024×4032, 1004 ms, 224 MB | 3024×4032, 1004 ms, 224 MB | 4032×3024 |
| Canon/Canon EOS R10/IMG_6310.CR3 | 6000×4000 | 3000×2000, 453 ms, 114 MB | 6000×4000, 2571 ms, 303 MB | 1620×1080 |
| Canon/Canon EOS R5m2/CRAW.CR3 | 8222×5488 | 4111×2744, 1049 ms, 213 MB | 4111×2744, 1055 ms, 213 MB | none read |
| Canon/Canon PowerShot S3IS/CRW_8760.CRW | 2840×2128 | 1420×1064, 193 ms, 30 MB | 2840×2128, 662 ms, 78 MB | none read |
| Canon/EOS 10D/crw_2127.crw | 3088×2056 | 1544×1028, 200 ms, 32 MB | 3088×2056, 678 ms, 82 MB | 3072×2048 |
| Canon/EOS 5D Mark II/09.canon.sraw1.cr2 | 3866×2574 | 3866×2574, 763 ms, 184 MB | 3866×2574, 855 ms, 184 MB | 5616×3744 |
| Canon/EOS 5D Mark II/IMG_0051.CR2 | 5634×3752 | 2817×1876, 781 ms, 101 MB | 5634×3752, 2733 ms, 267 MB | 5616×3744 |
| Canon/EOS 80D/IMG_0096.CR2 | 3000×2000 | 3000×2000, 548 ms, 112 MB | 3000×2000, 550 ms, 112 MB | 6000×4000 |
| DJI/FC6310/DJI_0220.DNG | 5472×3648 | 2736×1824, 288 ms, 93 MB | 5472×3648, 2076 ms, 251 MB | 960×640 |
| Fujifilm/FinePix E550/RAW_FUJI_E550.RAF | 2894×2177 | 1448×1088, 165 ms, 51 MB | 2894×2177, 563 ms, 161 MB | 1600×1200 |
| Fujifilm/X-T30 II/compressed_X-T30II.RAF | 6246×4170 | 3123×2085, 1828 ms, 123 MB | 6246×4170, 9076 ms, 328 MB | 4416×2944 |
| Fujifilm/X-T3/AFXT2720.RAF | 6246×4170 | 3123×2085, 270 ms, 123 MB | 6246×4170, 8840 ms, 328 MB | 4416×2944 |
| Google/Pixel 2 XL/IMG_20180109_080629.dng | 4032×3024 | 2016×1512, 146 ms, 58 MB | 4032×3024, 987 ms, 154 MB | none read |
| Leica/M10/f5381888.dng | 5984×3992 | 2992×1996, 585 ms, 111 MB | 5984×3992, 2304 ms, 300 MB | 5952×3968 |
| Nikon/Coolpix A1000/DSCN0417.NRW | 4608×3456 | 2304×1728, 163 ms, 75 MB | 4608×3456, 1451 ms, 201 MB | 4608×3456 |
| Nikon/D70/20170902_0047.NEF | 3039×2014 | 1520×1007, 187 ms, 31 MB | 3039×2014, 627 ms, 79 MB | 3008×2000 |
| Nikon/D750/compressed_12_bit.NEF | 6032×4032 | 3016×2016, 685 ms, 113 MB | 6032×4032, 2631 ms, 305 MB | 6016×4016 |
| Nikon/Z 6/DSC_0750.NEF | 6064×4040 | 3032×2020, 307 ms, 114 MB | 6064×4040, 2497 ms, 307 MB | 6048×4024 |
| Olympus/E-M5 Mark II/CB252215.ORF | 9274×6926 | 4637×3463, 1090 ms, 295 MB | 4637×3463, 1085 ms, 295 MB | 3200×2400 |
| OM Digital Solutions/OM-3/P5048182.ORF | 5220×3912 | 2610×1956, 807 ms, 95 MB | 5220×3912, 2397 ms, 257 MB | 3200×2400 |
| Panasonic/DC-GH5/_T012010.RW2 | 5208×3904 | 2604×1952, 499 ms, 96 MB | 5208×3904, 2054 ms, 256 MB | 1920×1440 |
| Panasonic/DMC-LX100/Lumix_DMC-LX100_1.RW2 | 4128×3104 | 2064×1552, 292 ms, 61 MB | 4128×3104, 1371 ms, 162 MB | 1920×1440 |
| Pentax/645D/645D2839.DNG | 7328×5501 | 3664×2751, 1317 ms, 188 MB | 7328×5501, 4090 ms, 506 MB | 7264×5440 |
| Pentax/K-1 Mark II/_IMG2388.PEF | 7376×4932 | 3688×2466, 725 ms, 168 MB | 7376×4932, 3366 ms, 455 MB | 7360×4912 |
| Pentax/K-5 II/IMGP2033.PEF | 4950×3284 | 2475×1642, 371 ms, 77 MB | 4950×3284, 1523 ms, 205 MB | 4928×3264 |
| Sony/DSC-F828/DSC06227.SRF | 3287×2460 | 1644×1230, 134 ms, 40 MB | 3287×2460, 1049 ms, 104 MB | 640×480 |
| Sony/DSC-R1/_DSC1477.SR2 | 3925×2608 | 1963×1304, 106 ms, 49 MB | 3925×2608, 810 ms, 130 MB | 640×424 |
| Sony/ILCE-7M3/UNCOMPRESSED_14bit.ARW | 6024×4024 | 3012×2012, 215 ms, 113 MB | 6024×4024, 1825 ms, 304 MB | 1616×1080 |
| Sony/ILCE-7RM4/DSC00395.ARW | 9568×6376 | 4784×3188, 551 ms, 280 MB | 4784×3188, 522 ms, 280 MB | 1616×1080 |

Thirty-one of the thirty-two develop. The one that does not is a DNG whose
sensor data is lossy JPEG. Thumbnail-size development took 0.1–1.8 s and
30–295 MB; viewer-size development took 0.5–9.1 s and 78–506 MB, the slowest
being 26-megapixel X-Trans files. Three files have no embedded JPEG LibRaw
reads (a Canon R5 Mark II CRAW, a PowerShot CRW, a Pixel 2 XL DNG), so they
have no fallback.

## Verification

- `scripts/lint.sh`: passed. Zero Go lint issues, no dead code, no known
  vulnerabilities, vue-tsc, ESLint, Prettier, Ruff and ShellCheck clean;
  **539 frontend tests in 52 files**; 4 installer regressions.
- `go test -race -count=1 -timeout 900s . ./internal/... ./services/...`
  with `LUMEN_RAW_SAMPLES` set to the 32 files: passed in every package.
  `TestRealCameraFiles` checks, for each file, that metadata, the thumbnail
  and decoded pixels agree in shape and that the pixels are developed, not a
  preview; the lossy DNG must be `ErrUnsupported`.
- New Go tests build their fixtures in code or with the bundled FFmpeg: a
  synthetic DNG (turned, cut short, unreadable, cancelled, without the
  worker), an indexer run over RAW files, the five video containers, JPEG XL,
  APNG, a three-page TIFF, per-page render caching and serving, the
  catalogue, and the RAW-and-JPEG duplicate rule.
- `wails3 generate bindings -f '-tags production' -clean=true -ts -i`:
  regenerated (`RenderInfo.preview`, `RenderInfo.page`, `MediaCaps.paged`,
  `MediaService.RenderPage`).
- `wails3 build`: passed.
- Native acceptance in the production build and WebView2, on a fresh scratch
  library: **55 of 55 checks passed**
  ([evidence](phase9-native-acceptance-2026-10-07.json)). Thirteen real
  camera files, one of each extension, were indexed; for each, the recorded
  size, the 1024-pixel thumbnail and the developed render agreed in shape,
  the original endpoint answered 404, and the viewer displayed the render at
  the expected size. The cut-short DNG showed its preview with the label.
  The lossy DNG stayed without a preview and counted as skipped, with no
  errors. JPEG XL, APNG and each page of a three-page TIFF displayed; the
  toolbar turned the TIFF's page. All five new video containers played
  through the converter. A TypeScript file named `main.ts` was ignored. No
  uncaught frontend error and no external request.
- `wails3 task package`: passed. The unsigned installer (874 MB) carries `lumen-raw.exe` in its payload and inventory, and `THIRD_PARTY_NOTICES.txt` lists LibRaw 0.22.2 with its copyright notice and the CDDL text. The installer was built, not installed.

The user's own library was never opened. Scratch data, the corpus, the
scripts and screenshots are in `D:\tmp\phase9`.

## Limits and what is not proven

- **The look.** A developed RAW is a neutral rendering. It is usually
  darker and flatter than the JPEG the camera made, because camera makers'
  tone curves and picture styles are not applied. This is a product choice
  the owner may want to revisit (for example, preferring a full-size
  embedded preview for display only).
- **Not read:** DNG with lossy-JPEG sensor data and DNG compressed with
  deflate (including floating-point HDR merges), because LibRaw is built
  without a JPEG library and zlib. Recognising a family is not a promise for
  every model: the matrix above is what was tested, one or two files per
  camera, not every compression mode of each.
- **Colour.** White balance, colour matrices and highlight handling are
  LibRaw's. No colour comparison against reference renders was made.
  HDR and wide-gamut sources are rendered to sRGB; nothing is labelled HDR.
  JPEG XL pixels are taken for sRGB, so a wide-gamut JPEG XL shows shifted
  colours, and its EXIF (date, camera, GPS) is not read.
- **Size.** The viewer's render stays bounded at 4096 pixels; larger RAW
  files say "Available resolution". There is still no tiled path.
- **Phase 11's needs.** JPEG XL is viewed and indexed. Reconstructing an
  original JPEG from a JPEG XL, which lossless compression requires, needs
  libjxl's own tools and is not part of this phase.
- **Animation.** Animated images play from their originals in the WebView.
  There are no frame-by-frame controls, and an animated JPEG XL shows its
  first frame.
- **Videos.** Each container was tested with one codec pair generated by
  FFmpeg, not with camera or broadcast recordings: interlaced material,
  DVD menus, multi-programme streams and damaged files were not tried.
- **Not done:** a clean-machine offline installation, low-memory hardware,
  running on battery, a library of thousands of RAW files, and AI or face
  quality on developed RAW pixels (the acceptance library ran without the
  AI stages being checked).
- No commit, push, merge or publication was performed.
