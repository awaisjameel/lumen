# Documentation refresh, 24 October 2026

Record of [issue #29](https://github.com/awaisjameel/lumen/issues/29), on
branch `docs/refresh-29` from `main` at `820ebed` (after the dependency
upgrade merged). [The documentation map](../README.md) is the content
inventory: the topic each living guide owns, and what this refresh did to it.
Claims were checked against the source, the manifests, the Taskfiles, the CI
workflows and GitHub's release metadata, not copied from phase records.

## What changed

- **README** follows a new user's path: what Lumen is, installing with a
  checksum, a first library, screenshots, privacy and the network, status and
  known limits, and help. Contributor material moved to
  [Development](../development.md), [Architecture](../architecture.md) and
  [Releasing](../releasing.md); the README keeps one section of links in its
  place, under the old "Building from source (Windows)" heading.
- **[Using Lumen](../using-lumen.md)** is new: worked examples for adding
  folders, search by description, name and text, naming and correcting people,
  Things and albums, blocking, duplicates and Trash, file tools and organising,
  compression and settings, each with what to expect and its limits.
- **[Release status](../release-status.md)** is new. It keeps apart the
  published v0.12.0 (unsigned, and not marked as a pre-release although the
  workflow drafts unsigned releases as one; asset hash checked against
  GitHub's digest), the earlier
  accepted 0.11.0 beta, the current source, which still says 0.12.0 but has
  changed since, and the gates still open.
- **AGENTS.md** keeps its command quick reference, since agents act on it
  directly, and points to Development for setup; CONVENTIONS points to the
  new owners of layout and commands. The original plan, the history index and
  the branding guide link to the living guides instead of restating them; the
  branding guide's dated results moved to
  [their own record](branding-verification-2026-10-05.md).
- **Corrections found by checking the interface's own text**
  (`frontend/src/shared/i18n.ts`) and the views: the compression modes are
  **Optimise existing format**, **Reversible JPEG to JPEG XL** and **Convert
  losslessly to JPEG XL**; the text-recognition switch is **Find text in
  screenshots and documents**; the settings link is **Organise files on
  disk…**; the activity card opens when selected, not on hover (also wrong in
  the feature tour and the beta guide); People has **Merge with someone…**;
  smart-album rules are **Favourites only**, **Media** and **Thing**.
- **One interface bug** found on the way: the Library's caption read "1
  videos" and "1 photos". The counts now have singular forms, with a
  regression test that fails without the fix (`LibraryView.test.ts`).
- **Stale pointers** to README sections in the CI set-up action, the CI
  workflow, `scripts/version.sh`, `spikes/env.sh`, the feature tour and the
  architecture layout now name the guides that own those topics.

## Screenshots

The owner chose to keep the seven README screenshots captured on 21 October
(commit `44edd34`) and to add only a face-review image, which the set lacked.
Recaptures of the other views were discarded, unpublished: one set from four
scikit-image samples repeated the same images, and a second from the Unsplash
library was not wanted over the existing images.

- The earlier images' photos came from a Lorem Picsum list kept outside the
  repository. [e2e/docs/media.json](../../e2e/docs/media.json) now pins
  992 of them by SHA-256 with photographer and Unsplash page. The 993rd has no
  Unsplash page, so no known licence; it was checked to appear in none of the
  images and is left out. Picsum's files were confirmed to download
  byte-identical to the pinned ones.
- `e2e/docs/fixtures.py` arranges those photos with invented dates and cities
  and adds five framings of the public-domain NASA portrait from scikit-image
  0.25.2. `e2e/docs/capture.mjs` indexes the library in the built app, lets the
  faces stage group the portrait (it did, without help), names the group and
  captures `faces.webp` at the earlier images' framing, emulated: 1440 × 900 CSS
  pixels at 125 %, 1800 × 1125 pixels. No request left the PC and the page
  raised no error.
- [capture.json](../screenshots/capture.json) pins all eight images, the new
  one with the executable and source it came from, the kept ones with the
  commit that added them. `e2e/docs/check.py` checks hashes, one framing,
  sizes, alt text and the media manifest; it failed as intended on a stray
  image and on a changed kept image.

## Tooling

- `scripts/check_links.py` also checks HTML image sources, ignores headings
  inside code fences, gives repeated headings GitHub's numbered anchors,
  decodes encoded fragments and reports an unclosed fence, with six
  regressions in `scripts/check_links_test.py`.
- `scripts/check-docs.sh` runs those regressions, the link check and the
  screenshot check; `scripts/lint.sh` runs it in place of the link check
  alone. Markdown is not run through Prettier: outside `frontend/` it never
  was, and doing so had rewritten historical records and the e2e examples.

## Verification

On Windows 11 with Go 1.27.3, Node 24.22.1, npm 12.2.1, Python 3.15.1,
llvm-mingw 20261020 and Wails beta.30, with cgo set and `scripts/env.sh`
sourced. This is a developer machine, not a clean-environment setup or an
installed acceptance run.

| Check | Result |
| --- | --- |
| `npm --prefix frontend run check` | Passed. |
| Focused Vitest, `src/library/LibraryView.test.ts` | 17 of 17 passed; the new test failed against the previous code ("1 photos · 1 videos"). |
| `wails3 build` | Passed, with no drift in the bindings, `go.mod` or the npm lockfile. The first attempt stopped on npm 11 as the engine check intends. |
| `node e2e/docs/capture.mjs` | 998 sample files indexed in under two minutes; all seven checks passed. |
| `scripts/check-docs.sh` | Six link-checker regressions, every repository link and anchor, and eight screenshots passed. |
| `scripts/lint.sh` | Every step passed (Go lint, deadcode, govulncheck with no reachable vulnerability, 110 files and 946 frontend tests, Ruff, installer and runtime regressions, the documentation gate, ShellCheck) except the latency gate. Five of its timings ran 10 to 95 % over budget, with no Go source changed. |
| `scripts/bench.sh`, run again on its own | Passed: every timing within budget. No budget was changed. |

Not repeated for this change: packaging and the installer suites, the full Go
race suite (no Go source changed) and the native e2e suites (one caption
string changed in the frontend). The clean-machine, hardware, real-face,
signing and feed gates stay as [release status](../release-status.md) lists
them. When the People relationships of [issue #30](https://github.com/awaisjameel/lumen/issues/30)
land, the walkthrough, the feature tour and the face-review screenshot need
reviewing again; nothing of that feature is described as present.
