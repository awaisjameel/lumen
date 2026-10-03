# Phase 7A: RAW and rendered-image feasibility record

## Scope and machine

Recorded on 4 October 2026 in `D:\Downloads\Lumen`. This is an offline
feasibility spike for plan sections 7A, 8A, Phase 9 and verification section
8. It adds no product decoder, render endpoint, recognised extension or
capability. All probe files and outputs are under `spikes/phase7/`. Product
code, frontend, dependencies, lockfiles and git state were not changed.

The current path was read end to end: `internal/media/thumbs.go`,
`formats.go`, `metadata.go`, `vips/vips.go`, `wic/wic_windows.go` and
`services/media.go`, with `AUDIT_CONVENTIONS.md` and `spikes/README.md`.
`Decoders.Decode` already accepts a requested long edge for model input;
this does not make a viewer render service exist. The current thumbnail
operation makes 1024 px and 256 px JPEGs plus the hash square. HEIC uses WIC
first, then FFmpeg. Other tested images use libvips. Original serving
currently uses the extension-based `core.WebNative` gate.

Machine evidence, from PowerShell registry reads and native Windows APIs:

```text
ProcessorNameString: 13th Gen Intel(R) Core(TM) i7-13700H
logical_processors=20
physical_bytes=33854128128
DisplayVersion: 25H2
CurrentBuild: 26200
UBR: 9457
Power Scheme GUID: 381b4222-f694-41f0-9685-ff5bb260df2e (Balanced)
```

This is the Windows 11 25H2 machine; the compatibility registry
`ProductName` still reports `Windows 10 Pro`. Usable physical memory is
31.53 GiB. CIM machine, operating-system and battery queries were refused:
`Get-CimInstance : Access denied`, HRESULT `0x80041003`. Native
`GlobalMemoryStatusEx`, `GetActiveProcessorCount` and `GetSystemPowerStatus`
worked instead. No GPU was involved in these CPU decoder probes.

An early run was on battery (`ACLineStatus=0`, charge 53%). Power changed
to AC during the investigation. The final main dataset records power and
timestamp for every run rather than copying the AC assertion from the old
spike README.

Final main table: 165 runs, all `ACLineStatus=1` (AC), battery charge 59%. Run timestamps: `2026-10-04T01:03:30+05:00` to `2026-10-04T01:04:17+05:00`. The power plan remained Balanced. See `inventory-final-start.txt`, `inventory-final-end.txt` and `power-plan.txt`.

## Decoder inventory (evidence)

### Bundled libvips

Commands, executed in PowerShell from the repository root:

```powershell
& deps/vips/bin/vips.exe --version
& deps/vips/bin/vips.exe -l foreign
& deps/vips/bin/vips.exe -l dcrawload
& deps/vips/bin/vips.exe -l jxlload
& deps/vips/bin/vips.exe -l magickload
Get-FileHash deps/vips/bin/libvips-42.dll,deps/runtime/libvips-42.dll
```

Observed output:

```text
vips-8.18.6
vips.exe: 'dcrawload' is not the name of a vips class
vips.exe: 'jxlload' is not the name of a vips class
vips.exe: 'magickload' is not the name of a vips class
SHA256 E6CC51BBC763E7DEDA536C6F56CE96B4C51EA769690CE4F4ED07607527B81DAE
```

Both DLL paths have that SHA-256. The Go probe linked to the shipping
runtime also printed `libvips=8.18.6`. `scripts/deps.sh` pins
`vips-dev-x64-web-8.18.6-static.zip` with archive SHA-256
`42ceea0c2f53d1244f3cb54c059881487b37452ee46c1c98c0d4b47480b20588`.
The archive hash was read from the script, not rechecked against a download.

The complete operation tree is saved in
`spikes/phase7/vips-foreign.txt`. Its loader families are `csvload`,
`matrixload`, `rawload`, `vipsload`, `svgload`, `gifload`, `uhdrload`,
`pngload`, `jpegload`, `webpload`, `tiffload` and `heifload`, with file,
buffer or source variants where listed there. There is no `dcrawload`,
`jxlload` or `magickload`. `rawload` means headerless pixels with explicit
width, height and band count, not a camera RAW demosaicer.

`deps/vips/versions.json` lists libvips 8.18.6, libheif 1.23.2 and libtiff
4.7.2. It contains no LibRaw, ImageMagick, JPEG XL or HEVC decoder entry.
The operation inventory proves the absence of a usable `dcrawload` path;
the versions list alone would not prove every optional feature absent.

HEIC header reading is available, but pixel reading failed:

```powershell
& deps/vips/bin/vipsheader.exe spikes/.cache/heic/iphone.heic
& deps/vips/bin/vips.exe thumbnail spikes/.cache/heic/autumn_1440x960.heic spikes/phase7/autumn-vips.jpg 1024
```

```text
spikes/.cache/heic/iphone.heic: 3992x2992 uchar, 3 bands, srgb, heifload
heif: Error while loading plugin: Support for this compression format has not been built in (11.6003)
```

The tiled iPhone pixel probe also failed, reporting
`heif: Decoder plugin generated an error: Unspecified (7.0)` and tile/seek
warnings. Logs retain both failures; do not treat header success as render
success. No claim about every HEIF compression variant follows from these
two pixel failures.

### Windows Imaging Component

The probe creates `IWICImagingFactory`, calls
`CreateComponentEnumerator(WICDecoder, WICComponentEnumerateDefault)`,
queries `IWICBitmapDecoderInfo`, and prints friendly names, versions and
registered extensions. It runs as a 64-bit process, like Lumen.

```powershell
$env:PATH='D:\Downloads\Lumen\deps\runtime;'+$env:PATH
& spikes/phase7/probe.exe '-test.run=^TestInventory$' '-test.v' '-test.timeout=120s'
Get-AppxPackage -Name '*RawImageExtension*'
Get-AppxPackage -Name '*HEVC*'
Get-AppxPackage -Name '*HEIF*'
```

Relevant exact enumeration output:

```text
decoder=TIFF Decoder version=1.0.0.0 extensions=.tiff,.tif
decoder=DNG Decoder version=1.0.0.0 extensions=.dng
decoder=Microsoft HEIF Decoder version=1.0.0.0 extensions=.heic,.heif,.hif,.avci,.heics,.heifs,.avcs,.avif,.avifs
decoder=Microsoft Raw Image Decoder version=10.0.26100.1 extensions=.3FR,.ARI,.ARW,.BAY,.CAP,.CR2,.CR3,.CRW,.DCS,.DCR,.DRF,.EIP,.ERF,.FFF,.IIQ,.K25,.KDC,.MEF,.MOS,.MRW,.NEF,.NRW,.ORF,.ORI,.PEF,.PTX,.PXN,.RAF,.RAW,.RW2,.RWL,.SR2,.SRF,.SRW,.X3F,.DNG
decoder=Microsoft JPEG XL Decoder version=10.0.26100.1 extensions=.JXL
Microsoft.RawImageExtension 2.5.35.0
Microsoft.HEVCVideoExtension 2.5.33.0
Microsoft.HEIFImageExtension 1.2.48.0
```

Other enumerated decoders are BMP, GIF, ICO, CUR, JPEG, PNG, WMPhoto, DDS
and Microsoft Webp. Full output is in the inventory logs. Extension
registration does not prove camera-model or compression support. The RAW
and HEVC extensions belong to this installation; neither can be a shipped
guarantee or silently downloaded prerequisite.

The existing WIC wrapper is generic `CreateDecoderFromFilename` plus frame
0, colour conversion and scaling. Product routing invokes it only for
HEIC/HEIF today. It has no RAW development controls, decoder selection,
source-versus-preview distinction, cancellable context or native memory
limit. All seven existing HEICs passed direct calls to this wrapper in the
final repeated probes. This does not prove RAW coverage.

### Bundled FFmpeg

The executable used was `deps/runtime/ffmpeg/ffmpeg.exe`, never the unrelated
FFmpeg on the inherited PATH. Commands:

```powershell
& deps/runtime/ffmpeg/ffmpeg.exe -version
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -decoders
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -demuxers
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -h decoder=tiff
```

Selected exact output:

```text
ffmpeg version n9.0.2-10-g51c4a23d74-20260926
 VF...D tiff                 TIFF image
 V....D mjpeg                MJPEG (Motion JPEG)
 V..... rawvideo             raw video
 D   tiff_pipe       piped tiff sequence
 D   jpeg_pipe       piped jpeg sequence
Decoder tiff [TIFF image]:
  -subimage          <boolean>    .D.V....... decode subimage instead if available (default false)
  -thumbnail         <boolean>    .D.V....... decode embedded thumbnail subimage instead if available (default false)
```

There is no separately named DNG or LibRaw decoder in this inventory. The
TIFF path nevertheless decoded the synthetic uncompressed CFA DNG below.
`rawvideo` is not camera RAW support. Native software HEVC decoding also
worked on the two HEIC fixtures measured through the CLI.

## RAW/DNG findings

The fixture search was confined to `spikes/.cache` and `spikes/corpus`.
`spikes/corpus` contains helper code, not camera files. A recursive file
search for DNG and the registered camera RAW families returned:

```text
Count
-----
    0
```

There is **no real RAW/DNG fixture** in those locations. Neither the
prohibited photo folder nor any other personal image folder was read.
Existing authorised images are seven HEICs, `camera/JPEG.jpg` (1000×563,
352,469 bytes), `NoExif.jpg`, and small AVIF/GIF fixtures. There was no
existing TIFF or genuinely large camera JPEG. Large-image measurements use
explicitly generated derivatives, not an invented camera corpus.

A separate, clearly synthetic probe was useful to test the TIFF/DNG
boundary. `make_synthetic_dng.py` uses only Python's standard library. It
writes a 64×48, 16-bit, uncompressed RGGB CFA DNG with DNG version tags and
a JPEG-compressed SubIFD containing the existing 1000×563 JPEG. The unusual
preview-versus-CFA dimensions deliberately reveal which representation a
decoder chooses. This file is a structure probe, not a calibrated camera
fixture or an acceptance test.

```powershell
python spikes/phase7/make_synthetic_dng.py
& deps/runtime/ffmpeg/ffprobe.exe -v error -show_entries stream=codec_name,pix_fmt,width,height -of default=noprint_wrappers=1 spikes/phase7/fixtures/synthetic-cfa.dng
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -i spikes/phase7/fixtures/synthetic-cfa.dng -frames:v 1 -y spikes/phase7/synthetic-dng-render.png
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -subimage 1 -i spikes/phase7/fixtures/synthetic-cfa.dng -frames:v 1 -y spikes/phase7/synthetic-dng-subimage.jpg
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -thumbnail 1 -i spikes/phase7/fixtures/synthetic-cfa.dng -frames:v 1 -y spikes/phase7/synthetic-dng-preview.jpg
```

Observed results:

| Path | Measured result | What it proves |
| --- | --- | --- |
| libvips header | `64x48 ushort, 1 band, multiband, tiffload` | Generic TIFF can read this CFA plane. No RAW loader was used. |
| libvips thumbnail with `--size down` | `64x48 uchar, 1 band, b-w, pngload` | It produces a monochrome CFA representation, not a developed colour RAW image. |
| Existing Go `Decoders.Decode` | Successful 64×48 JPEG encoding | Success alone is insufficient evidence of correct RAW colour development. |
| Existing `wic.Decode`, side 4096 | 1000×563 RGBA, 2,252,000 bytes | On this file the wrapper returns the embedded preview rather than CFA dimensions. The wrapper does not report which decoder won. |
| FFmpeg default input | `codec_name=tiff`, `width=64`, `height=48`, `pix_fmt=bayer_rggb16le`; RGB PNG output | The bundled TIFF decoder plus conversion can handle this uncompressed Bayer example. |
| FFmpeg `-subimage 1` | `Video: tiff, yuv444p, 1000x563`; JPEG output | It can decode and re-encode this JPEG SubIFD preview. |
| FFmpeg `-thumbnail 1` | `No embedded thumbnail present`; conversion failed | This option does not select the SubIFD layout used by the probe. |
| FFmpeg `-thumbnail 1 -c:v copy` | Output SHA-256 equals the whole DNG | TIFF stream copy merely writes the TIFF/DNG packet under a `.jpg` name. It does not extract the embedded JPEG bytes. |

The copied packet and source DNG both hash to
`2DDD44A8A3B9E9490D3C7986E6D9140C1142DF7D25F33E4FB927791678B94159`.
The SubIFD output has a different hash from the embedded JPEG because it
was re-encoded. This establishes a limited preview decode path, not
lossless preview extraction.

Exactly what the pinned libvips build offers is therefore a **generic TIFF
container path**, including a readable uncompressed CFA plane in this
synthetic DNG. It offers no `dcrawload`/LibRaw camera development path.
Already-developed RGB TIFF-like DNG variants might use ordinary TIFF
loading, but that is inference and remains untested. Neither a header nor
a successful monochrome/preview decode justifies enabling RAW support.
Compressed DNG, camera-specific RAW, white balance, colour matrices,
orientation and full-resolution development remain unaccepted.

## Rendered-image measurements

### Method and reproduction

The Go probe reuses `spikes/go.mod` with `-mod=readonly`; no dependency
files are changed and the network is disabled explicitly:

```powershell
$env:GOCACHE='D:\tmp\phase7\gocache'
$env:CGO_ENABLED='1'
$env:PATH='D:\Downloads\Lumen\deps\runtime;'+$env:PATH
$env:GOPROXY='off'
$env:GOSUMDB='off'
Push-Location spikes
go test -mod=readonly -c -timeout 120s -o phase7/probe.exe ./phase7/probe
Pop-Location
& spikes/phase7/measure.ps1
& spikes/phase7/measure-extra.ps1
```

Fixture generation used the bundled CLI:

```powershell
& deps/vips/bin/vips.exe thumbnail spikes/.cache/camera/JPEG.jpg 'spikes/phase7/fixtures/enlarged-24mp.tif[compression=none]' 6000 --height 4000 --size force
& deps/vips/bin/vips.exe copy spikes/phase7/fixtures/enlarged-24mp.tif 'spikes/phase7/fixtures/enlarged-24mp.jpg[Q=95]'
& deps/vips/bin/vips.exe copy spikes/phase7/fixtures/enlarged-24mp.tif 'spikes/phase7/fixtures/enlarged-24mp-tiled.tif[tile=true,tile-width=256,tile-height=256,compression=lzw]'
```

The generated files are 6000×4000, 8-bit RGB: strip/uncompressed TIFF
72,015,944 bytes, tiled/LZW TIFF 16,030,612 bytes, JPEG 3,825,318 bytes.
They share enlarged pixels from a small photograph. They test allocation
and ordinary decoder work at 24 MP, not genuine camera detail, every TIFF
compression or every large JPEG encoding.

Each case ran five times, sequentially, in fresh processes. Timings are
wall time around the operation, with libvips initialisation and source
metadata inspection before the timer. `thumbnail` is the actual current
two-JPEG-plus-hash operation. `render` calls the existing
`Decoders.Decode` and then `vips.JPEG` at quality 85; it is a disposable
encoded-copy probe, not a new product path. Decode/resize includes native
decoding, orientation, sRGB conversion and transfer of RGB into Go.
Encoding time is recorded separately. The render has a single output and
is not identical work to the multi-output thumbnail baseline.

All output sizes are capped without enlargement. Thus the 4096 request
is full resolution for the HEICs, whose longest edge is at most 3992.
The 6000 request is full resolution for the generated 24 MP files.
`vips.Init` uses the product settings: native concurrency 1 and operation
cache disabled. No worker concurrency experiment was performed.

Peak memory comes from Windows `GetProcessMemoryInfo`: maximum process
working set and maximum process pagefile usage/commit, in MiB. These are
whole-process peaks including Go, DLLs, COM and native allocations, not Go
heap measurements or an incremental decoder allocation. Header work
before the timer is included in the lifetime memory peak. The probe does
not subtract startup memory. Warm operating-system file caches were
allowed; the machine was not reserved for exclusive benchmark use.

| Input | Operation / long edge | Output | Decode + resize median ms | Total median ms (min–max) | Peak working set MiB | Peak commit MiB |
| --- | --- | --- | ---: | ---: | ---: | ---: |
| autumn_1440x960.heic | thumbnail / 1024 | 1024×683 | Included in total | 141.1 (137.1–143.8) | 59.34 | 108.64 |
| autumn_1440x960.heic | render / 4096 | 1440×960 | 134.2 | 144.8 (143.3–165.5) | 59.02 | 106.46 |
| iphone.heic | thumbnail / 1024 | 1024×767 | Included in total | 591.8 (562.1–606.7) | 160.98 | 190.50 |
| iphone.heic | render / 4096 | 3992×2992 | 656.2 | 730.9 (717.8–749.0) | 279.75 | 304.72 |
| libheif-example.heic | thumbnail / 1024 | 1024×683 | Included in total | 147.0 (139.3–177.5) | 42.37 | 99.59 |
| libheif-example.heic | render / 4096 | 1280×854 | 138.2 | 147.3 (136.6–156.8) | 46.07 | 102.40 |
| old_bridge_1440x960.heic | thumbnail / 1024 | 1024×683 | Included in total | 138.9 (137.8–167.2) | 59.35 | 108.20 |
| old_bridge_1440x960.heic | render / 4096 | 1440×960 | 135.2 | 149.1 (136.2–151.9) | 58.86 | 105.88 |
| random_collection_1440x960.heic | thumbnail / 1024 | 1024×683 | Included in total | 130.6 (125.0–132.3) | 59.62 | 108.59 |
| random_collection_1440x960.heic | render / 4096 | 1440×960 | 123.3 | 135.7 (127.7–185.4) | 59.11 | 106.14 |
| season_collection_1440x960.heic | thumbnail / 1024 | 1024×683 | Included in total | 164.5 (144.0–177.1) | 59.46 | 108.30 |
| season_collection_1440x960.heic | render / 4096 | 1440×960 | 137.7 | 148.2 (148.1–226.0) | 59.12 | 106.31 |
| winter_1440x960.heic | thumbnail / 1024 | 1024×683 | Included in total | 159.6 (146.6–176.0) | 59.37 | 109.10 |
| winter_1440x960.heic | render / 4096 | 1440×960 | 141.2 | 153.8 (150.9–163.5) | 59.12 | 106.18 |
| enlarged-24mp-tiled.tif | thumbnail / 1024 | 1024×683 | Included in total | 245.9 (238.4–250.2) | 135.69 | 172.23 |
| enlarged-24mp-tiled.tif | render / 1024 | 1024×683 | 233.5 | 238.7 (218.8–244.9) | 136.00 | 172.01 |
| enlarged-24mp-tiled.tif | render / 4096 | 4096×2731 | 352.4 | 405.6 (392.9–418.1) | 161.36 | 203.55 |
| enlarged-24mp-tiled.tif | render / 6000 | 6000×4000 | 388.2 | 495.9 (483.4–504.2) | 260.95 | 299.02 |
| enlarged-24mp.jpg | thumbnail / 1024 | 1024×683 | Included in total | 106.0 (98.7–109.0) | 35.80 | 67.25 |
| enlarged-24mp.jpg | render / 1024 | 1024×683 | 98.3 | 103.8 (90.4–109.7) | 35.87 | 67.25 |
| enlarged-24mp.jpg | render / 4096 | 4096×2731 | 251.9 | 305.2 (298.2–323.4) | 116.65 | 153.64 |
| enlarged-24mp.jpg | render / 6000 | 6000×4000 | 290.3 | 396.4 (395.5–416.0) | 228.32 | 265.92 |
| enlarged-24mp.tif | thumbnail / 1024 | 1024×683 | Included in total | 90.9 (87.5–97.5) | 617.22 | 654.41 |
| enlarged-24mp.tif | render / 1024 | 1024×683 | 81.3 | 86.9 (77.2–99.6) | 617.44 | 654.64 |
| enlarged-24mp.tif | render / 4096 | 4096×2731 | 197.6 | 250.5 (237.6–274.8) | 678.62 | 717.11 |
| enlarged-24mp.tif | render / 6000 | 6000×4000 | 224.7 | 332.8 (329.4–345.8) | 789.18 | 827.52 |

Every main case completed successfully in all five runs. Raw per-run
numbers, output dimensions and bytes are in `measurements.csv` and
`measurements.jsonl`; `summary.csv` contains the table aggregates. Median
decode and encode columns are computed independently and need not sum to
the median total.

For example, this exact line came from the first final iPhone render run
(the CSV/JSONL add repeat number and exit status separately):

```text
MEASURE {"ac_line_status":1,"battery_percent":59,"decode_resize_ms":656.31,"file":"iphone.heic","jpeg_encode_ms":82.282,"mode":"render","ms":738.593,"orientation":1,"output_bytes":2768441,"output_h":2992,"output_w":3992,"peak_commit_bytes":311791616,"peak_ws_bytes":284995584,"recorded_at":"2026-10-04T01:03:36+05:00","side":4096,"source_h":2992,"source_w":3992}
```

Direct WIC probes also passed on all seven HEICs. They are included in the
CSV, separately from the complete render path. Their peaks cover the
wrapper's RGBA copies without the later libvips RGB conversion/encoding.

### Separate FFmpeg CLI check

The extra script used the bundled software decoder with `-threads 1`
decoder and encoder options, JPEG `-q:v 2`, no enlargement and `-benchmark`.
The tiled group can still involve multiple tile decoders; those options
are not proof of a single thread for the whole process. The tiled
iPhone requires `[0:g:0]` as the input to `-filter_complex`; selecting a
single tile stream would not represent the photo. Example equivalent
commands are:

```powershell
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -nostdin -benchmark -threads 1 -i spikes/.cache/heic/autumn_1440x960.heic -frames:v 1 -vf "scale=w='min(iw,4096)':h='min(ih,4096)':force_original_aspect_ratio=decrease" -threads 1 -q:v 2 -y spikes/phase7/ffmpeg-autumn_1440x960.heic-4096.jpg
& deps/runtime/ffmpeg/ffmpeg.exe -hide_banner -nostdin -benchmark -threads 1 -i spikes/.cache/heic/iphone.heic -frames:v 1 -filter_complex "[0:g:0]scale=w='min(iw,4096)':h='min(ih,4096)':force_original_aspect_ratio=decrease" -threads 1 -q:v 2 -y spikes/phase7/ffmpeg-iphone.heic-4096.jpg
```

| Input | Requested long edge / output | Median ms (min–max) | Maximum peak working set MiB |
| --- | --- | ---: | ---: |
| autumn_1440x960.heic | 1024 / 1024×683 | 53 (50–59) | 27.79 |
| autumn_1440x960.heic | 4096 / 1440×960 | 55 (53–60) | 35.54 |
| iphone.heic | 1024 / 1024×767 | 82 (78–90) | 83.72 |
| iphone.heic | 4096 / 3992×2992 | 146 (136–173) | 187.17 |

These are FFmpeg's own `rtime` and `maxrss` values, converted from seconds
and KiB. They omit some startup work and do not include a Go parent, pixel
pipe, the product colour-profile conversion or its JPEG policy. The CLI
JPEG quality is also different. They are evidence that this bundled
fallback can render these inputs, not a valid claim that changing the
product route would produce the same timing or colours. Logs for all 20
runs and `ffmpeg-measurements.csv` preserve the original output.

The first final full-resolution iPhone CLI run printed:

```text
bench: utime=0.531s stime=0.188s rtime=0.173s
bench: maxrss=191660KiB
```

The extra tiled-TIFF rerun is retained in `tiled-measurements.csv` as
replication evidence. Its samples are separate from the five-run main
table rather than silently merged into it.

## Recommended budgets and cache key

These are proposed initial Phase 8/9 budgets, **not measured safe maxima**
or existing enforcement. They are deliberately bounded by the small
corpus and must be revised after real camera and adverse-input tests.

| Budget | Initial recommendation | Evidence or rationale |
| --- | --- | --- |
| Whole-image source decode | At most 24,000,000 pixels; each dimension at most 16,384; validate positive dimensions and multiplication before allocating | The largest measured source is 24 MP. There is no evidence for unrestricted 45–100 MP files. The side limit also rejects pathological narrow dimensions; it was not itself benchmarked. |
| Normal viewer render | At most 4096 px long edge, no enlargement; at most 16,777,216 output pixels | The measured renders substantially exceed 1024 px without always materialising a full-size viewer surface. The square output limit is derived from 4096². |
| Explicit actual-size render | Only within the same 24 MP source budget, after full-source decoding is proven; otherwise report available resolution and defer to a tile path | 6000×4000 was measured, but 4096 cannot honestly mean actual size for that source. Embedded RAW previews must remain labelled previews. |
| Active render concurrency | One foreground render globally; prefetch only after it finishes; queue at most one adjacent item | The runs are serial. Strip TIFF already consumes much of a GiB; multi-worker safety is unmeasured. Comparison can fetch its two items serially. |
| Per-render native worker memory | Hard 1 GiB process commit ceiling, one worker; global active-render reservation also 1 GiB | The largest observed full-resolution strip-TIFF commit remains below 1 GiB. This provides headroom, but is not proof that all 24 MP inputs fit. A Windows Job Object must enforce the ceiling; a semaphore alone cannot bound native allocations. |
| Lifetime | Cancel/kill obsolete work on navigation, gate lock or shutdown; initial 30 s hard worker wall deadline | Deadline is a proposed escape hatch for malformed/slow inputs, not a measured RAW latency target. Native in-process WIC/libvips calls do not honour the Go request context today. |
| Encoded disk render cache | Separate 2 GiB byte-capped LRU; reject a single cache entry above 32 MiB; atomic publish after source-revision recheck | Measured outputs are much smaller than this entry ceiling, but the corpus is too small to predict a typical library hit rate. The cap is a policy choice, not a measured optimum. |
| In-memory render cache | At most 128 MiB of encoded entries, preferably stream disk entries; bound active browser surfaces separately | Decoder memory and WebView texture memory do not belong to this encoded-cache allowance. A 4096² RGBA surface alone is 64 MiB before browser overhead, a derived lower-bound calculation. |

The strip-TIFF result is the reason to reject the assumption that a 1024
or 4096 output automatically guarantees low decoder memory. Tiled TIFF
used much less memory but took longer here. A rendered TIFF cannot be
assumed tiled merely because libvips streams. Do not mutate originals to
improve their layout. A process ceiling and a truthful resolution limit
are useful for opaque TIFF and WIC rendering as well as RAW.
Apply the memory ceiling to the worker's entire Windows Job Object as
well as its process, so descendant FFmpeg processes cannot escape the
global render reservation. Worker termination and child cleanup need
their own acceptance tests.

Use a versioned structured render key, hashed into the filename:

```text
render-pipeline-version
asset identity + selected source identity + verified source content revision
requested dimensions/long edge + fit/no-upscale policy + page/frame selection
actual decoder identity/version + relevant codec build/environment version
orientation/colour/RAW development policy version + output format/quality
adjustment version + adjustment values
```

The source revision should be the authoritative scan/content revision;
size and high-resolution modification time are useful validators, but
cannot alone detect an edit preserving both. Revalidate the selected
source before committing a result, including when a duplicate copy was
chosen. Decoder identity must distinguish libvips 8.18.6 plus its codec
inventory, the exact FFmpeg build, and the WIC decoder identity and
installed codec versions. A generic string `wic` is insufficient when
extensions can change. RAW settings must include white balance,
demosaicing, colour-matrix/profile and SDR policy when those exist.
Adjustment changes must not reuse an unadjusted render. Stage/output
versions stay separate from labels, albums and source identity.

Cache keys do not grant permission. Apply the existing admission/gate
checks before every cached open and publication; cancel pending work and
bound responses on lock, as the media service already does for existing
paths. Prefetch and cache eviction must respect blocked/trash visibility.

Keep the plan's **LibRaw-based worker-process evaluation** for Phase 9.
Neither the shipping libvips web build nor optional WIC registration is a
portable full RAW implementation. FFmpeg's synthetic TIFF/DNG success is
too narrow to replace a camera-aware decoder and colour policy. A worker
provides a lifetime, crash boundary, native memory cap and cancellation
that the current wrappers lack. LibRaw itself is absent from this checkout
and was not measured. The plan identifies dual LGPL/CDDL modes; select and
record the redistribution mode, pinned source/binary and optional library
licences before packaging. No licence selection or legal acceptance was
made by this offline spike, and no runtime download is proposed.

## Capability contract review

The proposed five-field `MediaCaps` shape is suitable for the Phase 7
availability UI **with stricter evidence rules**. Keep `Render=false` until
the Phase 8 service, cancellation, cache admission and resolution policy
exist. A probe calling the model-input decoder does not turn it on.

| Field | Evidence required and implication |
| --- | --- |
| `Preview` | A valid thumbnail for the current source revision exists, or the applicable decoder has actually produced a static preview. Installed extensions and header success alone are insufficient. A cached preview can remain usable even when its optional decoder is unavailable. |
| `Original` | The actual original variant is displayable through the WebView and admitted original endpoint. A backend decoder inventory cannot prove browser support; the current extension gate is a candidate rule, not per-file proof. HEIC, TIFF and RAW must not gain this merely because native decoding works. |
| `Render` | A complete source-resolution rendered copy can be made by the product path within its budget, with developed colour pixels. A 4096-only limit for a larger file, a CFA monochrome output, or an embedded JPEG does not satisfy the supplied full-resolution comment. |
| `DirectPlay` | A native WebView playback path is verified for this container/codec variant. FFmpeg decode availability alone does not prove it. No video/browser acceptance was performed in this spike. |
| `ConvertPlay` | The actual installed conversion service can produce the variant and serve it under its lifetime/gate rules. This requires more than a video extension or decoder-list entry. |

One comment needs clarification: **“as proven by its decoders” is too
narrow for `Original` and `DirectPlay`**, which depend on the WebView and
served path. Recommend “as proven by file inspection and the available
decoder, viewer and playback paths”. No contract file was edited.

Before Phase 8/9, add a companion resolution/provenance result to describe
the available rendered dimensions and whether the representation is an
embedded preview or developed source. A boolean cannot tell the viewer
that only 4096 px is available from a 6000 px original. Keeping the strict
`Render` meaning is preferable to silently redefining it as any larger
preview. Pending/unsupported/failed reasons should remain explicit in the
job/result state rather than making every `false` mean permanently
unsupported. WIC's synthetic-DNG preview selection is concrete evidence
for this distinction, not a hypothetical edge case.

## Untested items and blockers

- Real-camera RAW/DNG acceptance is untested: zero fixtures were found in
  the two authorised search roots. No downloads or other photo folders
  were used. Synthetic CFA/SubIFD success must not appear in a camera
  support matrix as acceptance.
- LibRaw is not bundled or present under the prepared source directories.
  Its throughput, peak memory, camera/compression matrix, preview policy,
  licensing selection and Windows worker packaging remain unmeasured.
- Lossless-JPEG/compressed DNG, CR2/CR3, NEF/NRW, ARW, RAF, ORF, RW2, PEF,
  corrupted/truncated RAW, real colour matrices, white balance, orientation
  and camera metadata require fixtures. A RAW+JPEG pair remains two assets.
- WIC decoder registration and installed Appx versions are this user's
  environment, not clean-install support. Generic WIC does not identify
  source-versus-preview output in its current Go API. WIC JPEG XL was only
  enumerated; no JXL pixels or reconstruction were tested.
- libvips HEIC headers work but both selected pixel probes failed. The
  shipping build has no usable `dcrawload`, `magickload` or `jxlload` path.
- Large TIFF/JPEG evidence comes from generated 8-bit 24 MP derivatives.
  Genuine high-detail camera JPEG, 16/32-bit and CMYK TIFF, multi-page
  handling, HDR/wide-gamut visual correctness, alpha, unusual compression,
  larger files and a tile implementation were not accepted.
- Native WebView2 viewing, actual-size zoom, browser memory, export fidelity,
  colour comparisons, live RAW camera models, clean offline installation,
  low-RAM hardware and sustained/concurrent-library load remain separate
  acceptance gates. These measurements do not exercise a render endpoint.
- Process memory limits, cancellation, worker crash/retry, source-change
  races, blocked-cache invalidation and cache hit/eviction behaviour are
  recommendations, not implemented or tested safeguards.
- CIM reads were denied as quoted above. Native APIs supplied machine and
  power data instead. Initial timings crossed a power transition and were
  superseded by the final per-run-power dataset. Five samples do not
  establish a p95 or a production latency guarantee.

All evidence is local and offline. The record supports starting Phase 8
with bounded rendering and Phase 9 with a camera-aware worker evaluation;
it does not declare either phase implemented or accepted.

`python spikes/phase7/verify_record.py` checks the seven required sections,
the 33 groups of five successful AC measurements, the 20 FFmpeg samples,
and the table values against the retained CSV. The named probe package
also passes `go test -mod=readonly -count=1 -timeout 120s ./phase7/probe`
from `spikes`; an unset `PHASE7_FILE` deliberately skips the per-file test,
whose actual measured invocations are recorded above. No product suite or
native browser test was required or claimed for this record-only spike.
