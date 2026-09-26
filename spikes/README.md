# Phase 0 spikes

Throwaway programs that answer the plan's Phase 0 questions with numbers from a real machine. They are a separate Go module (`lumen/spikes`) so their dependencies stay out of the app. The findings below are what matters; the code only shows how the numbers were produced.

**Machine:** Intel Core i7-13700H (14 cores, 20 threads), Intel Iris Xe (integrated), 32 GB RAM, Windows 11, on AC power, balanced power plan.

**Test data:** read-only local folders: 204 camera JPEGs of an office event (24 MP, many faces), 1,000+ screenshots (PNG), 7 public HEIC samples including a tiled iPhone photo, and a few AVIF/GIF fixtures. Outputs (contact sheets, databases) go to the git-ignored `spikes/out`.

## Results

### Thumbnails: libvips 8.18 vs the current pure-Go path

1,000 images (785 PNG, 206 JPEG, 7 HEIC, 1 AVIF, 1 GIF), 256 + 1024 px from one decode, warm file cache.

| | 19 workers | 8 workers | 24 MP JPEG (per image, 8 workers) | Screenshot PNG |
| --- | --- | --- | --- | --- |
| Pure Go (today) | 55 images/s | 33 images/s | 709 ms | 138 ms |
| libvips | **134 images/s** | **120 images/s** | **114 ms** | **59 ms** |

- libvips is 2.5 to 3.6 times faster overall and about 6 times faster on camera JPEGs, and it keeps pixel memory in C instead of growing the Go heap (the pure-Go run reached 0.9 to 2.2 GB).
- One decode must be copied to memory (`vips_image_copy_memory`) before deriving the second size, because files are opened for sequential reads.
- libvips also handles AVIF (127 ms). **It cannot decode HEIC on Windows**: the official build ships libheif without an HEVC decoder (patents), so it reads the header and fails on the pixels.

### HEIC

| Decoder | Works | Median | Max (12 MP tiled iPhone photo) | Catch |
| --- | --- | --- | --- | --- |
| Windows Imaging Component (WIC) | 7/7 | 54 ms | 337 ms | Needs Microsoft's HEVC Video Extension (paid, or preinstalled by some OEMs) |
| FFmpeg 9.0 process | 7/7 | 139 ms | 573 ms | Tiled iPhone files need `-filter_complex "[0:g:0]scale=…"` (a stream group) |
| libvips (official Windows build) | 0/7 | | | No HEVC decoder |

**Decision:** HEIC goes WIC first, FFmpeg as the fallback (FFmpeg ships anyway for videos). Everything else goes through libvips. WIC does not apply HEIF rotation for us; the app must read it from metadata.

### Vector search (sqlite-vec vs in memory)

300,000 × 768-dim vectors (the plan's target library), k = 50, clustered synthetic data, 20 queries.

| Method | Median | p95 | Recall@50 |
| --- | --- | --- | --- |
| sqlite-vec float32 (brute force) | 518 ms | 532 ms | 1.00 |
| sqlite-vec int8 | 351 ms | 419 ms | 0.97 |
| sqlite-vec int8 + year filter (metadata column) | 132 ms | 141 ms | |
| Go in-memory scan, float32 (920 MB) | 28 ms | 34 ms | 1.00 |
| **Go in-memory scan, int8 (230 MB)** | **17 ms** | **22 ms** | **0.96** |

- sqlite-vec works from Go (it needs a one-line `sqlite3.h` shim, see `env.sh`), inserts 21k vectors/s, and the database with both tables is 1.2 GB.
- **sqlite-vec alone misses the 300 ms search target at 300k items.** Keep vectors in SQLite as the source of truth, but search a compact int8 copy in memory (230 MB for 300k; loaded at startup, updated as the indexer writes). This also fits the idle-memory target better than float32. The plan already had this fallback (in-memory index); the numbers say use it from the start, and a plain scan is enough, no HNSW needed.

### SigLIP 2 base (search and tags)

ONNX Runtime 1.24.4 (the last DirectML build; `onnxruntime_go` v1.27.0 matches it). 1,000 images, batch 16.

| Model | Provider | Images/s | Agreement with CPU fp32 (mean / min cosine) |
| --- | --- | --- | --- |
| **fp16, fixed 16×3×224×224** | **DirectML (Iris Xe)** | **21.6** | **0.9996 / 0.997** |
| fp32, dynamic shape | DirectML | 12.5 | 1.0000 / 1.0000 |
| fp16, dynamic shape | DirectML | 9.2 | |
| fp32, dynamic shape | CPU | 10.0 | reference |
| fp16, fixed shape | CPU | 8.4 | 1.0000 / 0.9999 |
| int8 (the published dynamic quantization) | CPU | 14.3 | **0.80 / 0.58: unusable** |

Text tower, 12 queries against the 1,000 images:

| Text model | Size | Query latency (tokenize + encode on CPU + scan) | vs fp16 |
| --- | --- | --- | --- |
| **fp16** | 565 MB | **57 ms median, 86 ms max** | reference |
| int8 | 283 MB | 14 ms | mean cosine 0.968; **same top result for only 6 of 12 queries** |

- Preprocessing (libvips decode + squash to 224 px) runs at about 290 images/s on 20 threads, so it is never the bottleneck. In the app it can read the 1024 px preview instead of the original.
- **Pick fp16 with a fixed input shape on DirectML.** DirectML only compiles the graph when every dimension is known; pad the last batch. That is about 80 minutes per 100,000 photos on an integrated GPU, inside the plan's "a few hours".
- CPU-only machines should run fp32 or fixed-shape fp16 (8 to 10 images/s here, about 3 hours per 100k). The published int8 models lose too much quality (both towers); if a smaller model is needed, quantize it ourselves (static calibration, or only the embedding table) and check agreement the same way.
- The fp16 export trips a bug in ONNX Runtime 1.24's layer-norm fusion. `ORT_ENABLE_EXTENDED` optimization avoids it.
- The Gemma tokenizer is 90 lines of Go (`siglip/tokenizer.go`) and matches Hugging Face `tokenizers` exactly (tested). Its 34 MB JSON loads in 0.4 s; the app should ship a compact binary form.
- Search results look right by eye (`out/siglip/q*.jpg` contact sheets): "a group photo" returns eight real group shots, "a bar chart" returns charts, "a screenshot of source code" returns code, a login-form query finds sign-in pages. Queries with no matching photo ("a beach at sunset") score low (about 0.05 vs 0.15), so a threshold can hide junk results.
- **Model size is a packaging problem:** the text tower carries a 256k-token vocabulary (197M of its 282M parameters are the token embedding table), so it is 565 MB in fp16. With the 186 MB fp16 vision tower that is 750 MB for search alone. Quantizing only the embedding table to int8 (a lookup, so little quality risk) would bring the text tower to roughly 370 MB; worth trying before Phase 2 ships.

### Faces: YuNet + SFace

204 office photos (24 MP, decoded at 1024 px as the app's preview), CPU.

- 1,009 faces detected (score ≥ 0.8); 332 were smaller than 36 px and skipped; 677 embedded.
- **Speed:** detection 5.5 ms per image, align + embed 7.9 ms per face. Faces are cheap.
- **Clustering** (average linkage on cosine similarity): 21 / 29 / 33 / 40 clusters at thresholds 0.30 / 0.36 / 0.42 / 0.50. At 0.42 every contact sheet checked by eye held one person, on a hard set (many similar-looking men, same lighting and background). Splits (one person in two clusters) are likely and are what the merge UI is for.
- Small faces are the main loss: a third of detected faces were under 36 px at 1024 px. Decoding group photos at a larger size for the face stage would recover many.
- Licences checked: YuNet MIT, SFace Apache-2.0, both from OpenCV Zoo.

### OCR: PP-OCRv5

60 screenshots, CPU, English recognizer (7.8 MB), results checked by eye (accurate, including small UI text).

| Detector | Detect | Recognize | Total per screenshot |
| --- | --- | --- | --- |
| **Mobile (4.8 MB)** | 80 ms | 450 ms (batches of 4 lines) | **about 0.55 s** |
| Server (84 MB) | 3,180 ms | same | about 4.3 s, same lines found |
| Mobile on DirectML | 630 ms | 3,120 ms | slower: shapes change every image |

- Mobile detector on CPU. Recognition dominates, so batch lines of similar width (4 per batch was best).
- About 36 lines and 570 characters per screenshot. OCR everything would be slow (a 100k library at 0.55 s per image is 15 hours), so run it only on images SigLIP classifies as screenshots or documents.

### Cross-cutting findings

- **Turn off ONNX Runtime thread spinning** (`session.intra_op.allow_spinning = 0`). With it on, idle worker threads busy-wait after each run; with two sessions plus libvips in one process everything ran 10 to 25 times slower (face detection 134 ms instead of 5.5 ms). A background indexer must never spin.
- **Load our DirectML.dll by full path before ONNX Runtime.** Windows has an older DirectML.dll in System32 that the normal DLL search would find first.
- **cgo with llvm-mingw links MSVC-style `.lib` import libraries** only when named explicitly (`-l:libvips.lib`).
- **Installer size estimate:** ONNX Runtime + DirectML 35 MB, libvips (web build, no ImageMagick/poppler) about 11 MB zipped, SigLIP 750 MB (fp16 towers), faces 39 MB, OCR 13 MB, plus FFmpeg. That is close to 1 GB, mostly the SigLIP text tower. **Decided (2026-09-26): everything ships in the one installer; the app never downloads anything.** So shrinking the text tower (int8 embedding table only) is worth doing before Phase 2 ships, and the installer should be LZMA-compressed. NSIS handles this size (its limit is 2 GB).

## What this changes in the plan

1. Thumbnails: libvips for everything except HEIC; HEIC through WIC then FFmpeg.
2. Search: vectors stored in SQLite, searched from an in-memory int8 copy. sqlite-vec is optional (useful for filtered queries), not the search path.
3. SigLIP: fp16 towers; vision with a fixed batch shape on DirectML, CPU as the fallback; no published int8 models. The text tower runs on the CPU per query.
4. OCR only for screenshots and documents, mobile detector, CPU.
5. ONNX Runtime stays on 1.24.x while we use DirectML (no newer DirectML builds exist); Windows ML is the path forward for newer runtimes.

## Reproduce

```sh
./fetch.sh          # ~2 GB into .cache (runtimes, models, HEIC samples)
source env.sh       # PATH for DLLs, cgo flags for sqlite-vec
go build -o out/ ./cmd/...
./out/thumbs.exe -n 1000
./out/vec.exe -n 300000
ORT_OPT=extended ./out/siglip.exe -n 1000 -vision fp16s,fp32 -ep dml,cpu -text fp16 -textcmp int8
./out/faces.exe -dir "D:\path\to\photos"
./out/ocr.exe -dir "D:\path\to\screenshots" 
```

Test folders come from `LUMEN_CORPUS` (semicolon-separated) or a git-ignored `corpus.local` (one folder per line), plus the downloaded samples. Programs must run from the `spikes` folder (they find models in `.cache`).
