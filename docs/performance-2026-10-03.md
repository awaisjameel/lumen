# Performance pass — 3 October 2026

Phase 6's performance pass measures the plan's targets (§6) in the real
app, on this laptop and on a low-end profile of it, and checks that indexing
gives way to the user as §6 asks. Each target is a goal to validate, not a
contract (plan §9, "Performance targets").

**Machine:** Intel i7-13700H (20 threads), Intel UHD graphics, 32 GB, Windows
11 build 26200, NVMe SSD. **Low-end profile:** the same laptop with Lumen held
to 4 logical processors (`start /affinity F`), which also sets its default
indexing budget to 2 threads.

**Library:** 3,000 JPEGs from the benchmark corpus (`D:\tmp\lumen-bench\corpus`,
3.7 GB, about 1.2 MB each), indexed by the build under test into a scratch
library. **Harness:** `D:\tmp\phase6\perf` (`index.mjs`, `perf.mjs`,
`mem.mjs`) drives the production build over the WebView's DevTools port.

## Targets

| Metric | Target | Measured | |
| --- | --- | --- | --- |
| Cold start to a usable grid | < 2 s | 1.0–1.1 s; low-end 0.7–0.8 s | met |
| Grid scroll | 60 fps | 60 fps, p95 frame 16.9 ms, 1 frame over 33 ms in 5 s; low-end 57 fps, p95 18.3 ms, none over 33 ms | met |
| Search | < 300 ms | 120–200 ms once the text model is loaded; the first search of a session loads it (1.2–1.8 s); a repeated query 6–9 ms | met after the first |
| Idle memory | < 500 MB | 454 MB (Lumen 87 MB, WebView2 366 MB); 1,135 MB while the text model is loaded, for 5 minutes after a search; back to 475 MB once it unloads | met at idle |
| Browsable library (metadata and thumbnails) | ~minutes for 100k | 3,000 in 55 s on mains: about 30 minutes per 100k | close |
| Full AI index, 100k photos | a few hours | 3,000 in 264 s on the integrated GPU: about 2.5 hours per 100k | met |

"Cold start" is the time from starting Lumen.exe to the first thumbnail
decoded in the grid, the median of three starts with the library's files in
the OS cache. Memory is the working set of Lumen.exe and its WebView2
processes.

## Indexing throughput

A fresh scratch library indexing the 3,000 photos, with indexing's default
share of the processor (half the threads, below normal priority, FFmpeg
and the GPU rested as the 2 October politeness fix set them):

| Run | Scan | Browsable | Full AI index |
| --- | --- | --- | --- |
| On mains power, files in the OS cache | 2 s | 55 s (55 photos/s) | 264 s |
| On battery, first read of the files | 27 s | 334 s | 612 s (with 1,301 more photos added during the run) |

On battery the laptop's power plan slows the processor several times over,
and the first run read 3.7 GB from disk. On mains, 100,000 such photos would
be browsable in about half an hour and fully understood in about two and a
half hours, while the user keeps half the processor. "Minutes for 100k" would
need the whole processor: the user can give indexing more threads in
Settings → Background work. The thumbnail stage shares the processor
first come, first served with the AI's decoding (`index.cpuBudget`), so the
AI does not wait for every thumbnail; the grid fills newest first and shows
what is on screen first either way. Giving thumbnails strict priority was
not changed in this pass: the faces stage reuses the thumbnail stage's
decoded pixels (`index.decoded`), which a long delay would throw away.

## Giving way to the user

Checked in the real app while it indexed (`D:\tmp\phase6\accept\politeness.mjs`):

- **A busy PC.** Another program loading every core at normal priority:
  indexing waited 46 s after the load began (the 30-second average, plus the
  scan's start), the status said "Waiting while your PC is busy", and indexing
  went on 19 s after the load ended. Lumen's own work never counted: the
  laptop at 100 % from indexing alone never triggered it.
- **Full screen.** A borderless window covering the screen: indexing waited
  within 5 s and went on 4 s after it closed. A real full-screen app the
  user opened during the test runs did the same (the log shows it).
- **While waiting**, the models are freed (their memory goes back), and a
  batch already in hand finishes.
- Under a load at normal priority on every core, Lumen's engine runs below
  normal and answers the UI slowly; the window itself stays at normal
  priority. That is the trade-off decided in the 2 October politeness fix.

## Installer

The installer is 872 MB (zlib) and installs in 56 s on this laptop. LZMA
made an 840 MB installer that took 27 minutes to build and 4 minutes to
install: Setup's LZMA decoder runs at about 4 MB/s, and the model weights
that make most of the payload barely compress either way (zlib keeps 92 %
of them).
