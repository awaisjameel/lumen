# End-to-end tests

These suites drive the **built app** (`bin\Lumen.exe`, the production build in its native WebView2) the way a person does: they click, type and press keys through the WebView's DevTools port, call the same bound services the frontend calls, and check the files on disk. They complement the Go tests (engine and services) and the Vitest tests (components in jsdom), which cannot show that a codec plays, a dialog takes the pointer or a file really moved.

Every run uses a scratch library (`LUMEN_DATA_DIR`). The suites never open your own library, and never read a folder outside this repository.

## Running

```bash
wails3 build                      # the suites test bin\Lumen.exe
cd e2e && npm ci                  # once: the locked puppeteer-core dependencies
node run.mjs                      # every suite, then a summary
node run.mjs viewer formats       # only these
node run.mjs --list               # what there is
node run.mjs --clean              # delete generated fixtures, libraries and results
node suites/interface.mjs         # one suite on its own
SECTIONS=themes,video node suites/interface.mjs
```

From the repository root, `wails3 task e2e` runs them all and `wails3 task e2e SUITES="viewer formats"` runs those two.

A suite prints `PASS`/`FAIL` for each check with what it observed, writes `report.json` and its screenshots to `.work/results/<suite>/`, and exits non-zero if a check failed. Suites run one after another: they share the GPU, and timing checks need a quiet PC. Leave the window alone while one runs; a click in it is a click in the test.

Needs: Node (as `package.json` says), Python with Pillow, and `deps/` from `scripts/deps.sh` (the fixtures are made with its FFmpeg). `PYTHON` names another interpreter, `LUMEN_EXE` another build.

## The suites, by use case

| Suite | What a user does | Runs on |
| --- | --- | --- |
| `onboarding` | Opens Lumen for the first time: the Welcome screen, nothing added by itself | an empty library |
| `tour` | Visits every screen at four window widths: no sideways overflow, no errors, no request leaving the PC | base library |
| `interface` | Themes and preferences, command palette, keyboard in grid and viewer, Escape, reduced motion, forced colours, DPI, narrow windows, video playback (direct and converted), PIN-locked blocks | base library + `videos` |
| `collections` | Makes an album, trashes and restores from the viewer, smart albums, custom Things, About | base library |
| `organise` | Organise on disk: preview, move, Undo, checked on the files themselves | base library + two files of its own |
| `viewer` | Zoom, pan, rotate, full screen, filmstrip, compare, slideshow; selection and batch; export and rename with Undo; audio tracks, subtitles, frame export; playback history; blocks | `media` |
| `file-tools` | The File tools dialog and its menu at every width, by keyboard, and inside a full-screen video | `media` |
| `branding` | The Lumen mark on every surface in both themes, and the favicon | one photo |
| `formats` | Camera RAW of every family, JPEG XL, animated PNG, multi-page TIFF, MPEG/TS/Ogg/ASF video, from indexing to the viewer | `formats` |
| `customise` | Changes every preference in Settings and sees it take effect (page, grid, viewer) and survive a restart; reassigns a shortcut and uses it; turns a photo format off and on; holds an animation still; clears the viewing copies; saves an export preset; resets a section and everything | base library + three files of its own |
| `compression` | Reviewed JPEG/PNG copies, JPEG XL native rendering and byte-exact JPEG restoration, partial outcomes, pause/cancel, restart, collisions and compression defaults | Generated scratch JPEG/PNG/BMP files, duplicate JPEG and XMP |
| `showcase` | The component showcase of a development run. Only when named: it starts `wails3 dev`, so it needs the build toolchain in the environment | base library |

## Fixtures

Nothing binary is committed. `fixtures/build.py` generates every file deterministically on first use, into `.work/fixtures/`:

- `library`: dated synthetic JPEGs in year/month folders (600; `LUMEN_E2E_PHOTOS` changes it).
- `videos`: one H.264 MP4 that WebView2 plays directly, two MPEG-4 files it cannot.
- `media`: twelve photos (one turned by EXIF), a 6000x4000 TIFF, a Display P3 JPEG, a HEIC where `spikes/fetch.sh` has downloaded one, and two videos with two audio tracks and subtitles.
- `formats`: the files above the table's last row, with a RAW cut short (it keeps only its preview) and a TypeScript `main.ts` the scanner must not take for a video.

The **base library** is the `library` fixture indexed to the end by the build under test (thumbnails, embeddings, faces: a few minutes, once), then given favourites, albums, a Thing and some Trash. Each suite works on its own fresh copy, so suites cannot spoil one another and can run in any order.

The camera RAW files are real CC0 samples from [raw.pixls.us](https://raw.pixls.us), pinned by SHA-256 in `fixtures/raw-samples.sha256` and downloaded once into `.cache/raw/` (`.cache` survives `--clean`):

```bash
python fixtures/fetch_raw.py --acceptance .cache/raw   # the 14 the formats suite uses, about 360 MB (it fetches them itself)
python fixtures/fetch_raw.py .cache/raw                # all 32, about 1.2 GB, for the Go tests below
```

## Tests on real files, in Go

Some Go tests run on real files and skip without them (from the repository root, after `source scripts/env.sh`, with `CGO_ENABLED=1` and `CC` set as for a build):

```bash
LUMEN_RAW_SAMPLES="$PWD/e2e/.cache/raw" go test -race -timeout 900s ./internal/media ./internal/index
LUMEN_FACE_PHOTOS="<a folder of photos of people>" go test -race -timeout 900s ./internal/ai ./internal/index
python e2e/perf/raw_matrix.py     # lumen-raw on every sample: time, peak memory, output size
python e2e/perf/compression.py    # pinned encoder time, peak private commit and sizes on generated fixtures
```

Three Go benchmarks measure what chose the scanner's worker count and the search index's loading, and run only when named. Scans read a folder of real photos; use your own, never one holding documents:

```bash
LUMEN_SCAN_BENCH_ROOT="<photos>" go test -run '^$' -bench '^BenchmarkPhotoScanWorkers$' -benchtime=5x -timeout 1800s ./internal/scanner
python e2e/perf/cold_scan_copies.py "<photos>"   # 25 uncached copies in .work/cold-scan; each run reads them once
LUMEN_COLD_SCAN_BENCH_ROOT="$PWD/e2e/.work/cold-scan" go test -run '^$' -bench '^BenchmarkColdPhotoScanWorkers$' -benchtime=1x -timeout 1800s ./internal/scanner
LUMEN_SEARCH_BENCH_DIR="$PWD/e2e/.work/search-bench" go test -run '^$' -bench '^Benchmark(SearchStartupLoad|EmbeddingStartupComparison)$' -benchtime=1x -timeout 1800s ./internal/search
```

The search benchmarks build 100,000- and 300,000-vector libraries; `LUMEN_SEARCH_BENCH_DIR` keeps them between runs (about 600 MB), and without it each run builds them again.

## Performance

Not part of `run.mjs`: the figures need a quiet PC on mains power, and take minutes.

```bash
node perf/perf.mjs [label]                               # the build under test, on a copy of the base library
node perf/index.mjs <exe> <dataDir>                      # index the library fixture with any build, timed
node perf/perf.mjs <label> <exe> <dataDir>               # measure that build on that library
node perf/compare.mjs <exeA> <dataA> <exeB> <dataB>      # two builds, launches alternated
```

`perf.mjs` measures cold start to a usable grid, idle memory, reload time and bundle size, scroll frame times, first and warm search, viewer open and next, and memory after use; results go to `.work/results/perf/`. An older build cannot open a library a newer one has migrated, so a comparison gives each build its own library. `LUMEN_E2E_PHOTOS=3000` makes the library the size the published figures used.

`node perf/thumbs.mjs [photos=3600]` measures thumbnail revalidation in the native WebView after full indexing: first timeline entry, ten-screen scrolling in both directions, a fifty-screen fling and return, leaving and returning, the viewer, scrolling during a separate 1,800-photo import, and scrolling with one folder block. It records every thumbnail and sprite request, raw conditional-response statuses, memory/disk cache flags, bytes, timings, decoded visible tiles without a request, animation-frame gaps over 50 ms and matching DevTools traces. Before scrolling, after the fifty-screen fling, and after a further thirty seconds at rest it sums Windows private bytes and working sets for every WebView process descended from this scratch app; working sets may count shared pages more than once. It also probes ordinary and cache-only access after blocking a previously fetched thumbnail, retained decoded images and blobs, and exact cached JPEG bytes after shutdown. The tile is scrolled out before blocking and the grid is reopened afterwards: the script fails if the blocked thumbnail is painted again or any URL access succeeds. Run the probe helpers' tests with `node --test perf/thumbs-*.test.mjs` from `e2e`. Build first, install the e2e dependencies as above, run on a quiet PC, and leave the window alone. Each invocation generates its own fixtures and library under `.work/thumbs-measure/<timestamp>/`, writes `report.json` and scenario traces there, closes only its own process with `taskkill /PID` without `/F`, and retains all scratch data; it never clears other runs. To reuse generated photos while indexing a fresh library, set `LUMEN_THUMBS_FIXTURES` to an earlier run's absolute path under `.work/thumbs-measure/` and pass the same photo count.

## Writing a suite

A suite is one file in `suites/`, registered with its own port in `lib/suite.mjs`:

```js
const s = suite('name')                 // fresh s.run and s.out folders, s.port, s.check, s.must, s.guard
const app = await launch({ exe: s.exe, dataDir: join(s.run, 'data'), port: s.port })
s.watch(app.page)                       // records uncaught errors
s.check('what must be true', observed === expected, observed)
await close(app)                        // a graceful close, as the user's
s.finish()                              // report.json and the exit code
```

- `lib(page, 'Method', ...args)` calls a bound `LibraryService` method (also `media`, `search`, `people`, `system`).
- `fixture(name)` returns a generated folder to read; `fixtureCopy` one to change; `libraryCopy(dir)` a copy of the base library.
- Reload off the Welcome screen as soon as a source is added, so nothing on it can be clicked.
- A new library has videos off and waits for an idle PC: call `SetVideos(true)` and `unthrottle(page)` before expecting an index.
- Say what was observed in a check's third argument: a failure should explain itself from the log.
