# Phase 8 branch audit and repairs, 6 October 2026

Reviewed all seven commits on `update/phase-8` through `f15be77` against
`main` at `4fc704a`. The starting working tree was clean. The branch changed
186 files across viewing, playback, selection, file tools, SQL persistence,
generated bindings, shared controls, branding, packaging and documentation.
The repairs below are additional uncommitted changes.

## Contract and ownership review

Traced the changed Go models and service methods through store migrations,
file operations, generated Wails bindings, the typed bridge, Pinia state and
Vue consumers. Reviewed cancellation, disposal, visibility, revision checks,
recovery, native worker admission, offline assets, command ownership and shared
Nuxt UI controls. Collection consumers include Events, Places, Things,
People, Albums, Trash, Blocked and Duplicates.

Lumen retains its standalone Vue/Vite/Wails architecture. SQL belongs to the
store; filesystem changes belong to fileops; services coordinate admission
and translate store outcomes; the bridge owns the frontend wire contract.
The new batch path reuses the same mutation primitives as single-item edits.
Generated bindings were regenerated without manual edits or drift. No schema
or public wire-type changes were needed for these repairs.

## Repairs

| Finding | Final behaviour |
| --- | --- |
| Previews could replace/prune journal rows while an export was executing; synchronous planning was outside shutdown tracking. | Planning, moving, exporting and recovery share organiser admission. Synchronous operations register with the lifetime owner, cancel on shutdown and finish before the store closes. |
| Batch visibility validation and subsequent writes were separate; every item incurred a separate transaction. | Each submitted part uses one transaction, checking canonical visibility beside each write. Covered/trashed assets are refused; restore checks Trash membership. Cancellation rolls back the part and reports no committed successes. |
| A background OCR enqueue failure could report a committed tag batch as failed. | Tag outcomes describe committed edits. Background queue failures are logged separately. |
| Pending confirmations, validation, planning and export completion could act on a closed or reopened dialog. | FileTools and BatchTools fence asynchronous work by dialog generation and captured inputs. Closed dialogs cannot initiate delayed actions; obsolete results cannot replace the current dialog's state. |
| A pending select-all/Shift request could overwrite a subsequent visible selection or survive store disposal. | Visible selection goes through the store's shared invalidation owner. Disposal invalidates requests and releases the timeline cache. |
| Body portals were outside the browser's visible fullscreen tree. | UApp's shared portal target follows `fullscreenElement`. Tooltips, file tools and confirmations remain visible and usable in native image/video fullscreen, returning to the body on exit. |
| Buffering and PiP events from replaced video elements could change current UI state. | Those events check ownership of the current element, matching the existing guards on playback, position and errors. |
| Export naming could treat invalid/inaccessible destinations as collisions and ignore cancellation while searching. | Validate destination ancestry, propagate filesystem errors, and check cancellation throughout naming. Missing/replaced previews return not-found rather than empty success. |
| Failed temporary-copy cleanup could erase the journal's recovery path. | Preserve writing/ready journal entries when cleanup or post-publication settlement fails. Recovery removes the owned temporary copy and verifies published digests. Hashing honours cancellation; RunExport recovers interrupted work before a retry. |
| IconButton focus relied on an unchecked DOM cast. | Narrow the exposed element before focusing it. |

## Verification

Commands ran under Git Bash after sourcing `scripts/env.sh`, with
`CGO_ENABLED=1` and `CC=clang` for native Go checks.

- `scripts/lint.sh`: passed. Zero Go lint issues, no Go vulnerabilities,
  TypeScript/Vue checks, ESLint, Prettier, Python and ShellCheck passed.
  Frontend: **534 tests across 52 files**. Installer regressions: **4 passed**.
- `go test -race -count=1 -timeout 900s . ./internal/... ./services/...`:
  passed across the root, internal packages and services. After the final
  export recovery/cleanup refinements, reran
  `go test -race -count=1 -timeout 120s ./internal/fileops ./services -run Export`:
  passed. The Windows regression holds a real file handle open, proves failed
  deletion retains the journal, then releases it and proves recovery settles.
- `wails3 generate bindings -f '-tags production' -clean=true -ts -i`:
  passed; before/after binding checksums were identical.
- `wails3 build`: passed. The main JavaScript chunk is 470.92 kB
  (111.60 kB gzip). This is a build measurement, not a library-scale benchmark.
- `git diff --check`: passed.
- `npm audit --omit=dev --json`: **zero production findings**.
  Full `npm audit --json` still reports four high-severity development
  dependency entries for one upstream advisory in the existing
  `@vue/eslint-config-typescript -> fast-glob -> micromatch -> braces` chain:
  [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm).
  The registry reports `braces` 3.0.3 and Vue's configuration 14.9.0 as the
  latest published versions. npm's suggested downgrade to configuration
  14.0.1 requires ESLint 9 and eslint-plugin-vue 9, conflicting with the
  installed version-10 toolchain. Dependencies and checks were preserved;
  this remains an explicit upstream development-tool acceptance issue.
  [Dependency audit evidence](phase8-audit-dependencies-2026-10-06.json).
- Native production WebView2 acceptance: **63/63 checks passed**, no uncaught
  frontend errors and no external requests. Evidence:
  [native acceptance](phase8-audit-native-acceptance-2026-10-06.json).
- Native route tour: **64 screens**, covering 16 routes at 1440, 1040, 720 and
  520 CSS pixels (device scale 1.25), with no horizontal overflow, 261 local
  requests and no external requests. It includes populated Events/Thing
  detail routes; People, Places and Albums used empty-state fixtures.
  Viewer/fullscreen checks used dark surfaces; the route tour used light.
  Screenshots for narrow Events, event detail, Library and Settings were
  visually inspected. Evidence:
  [route tour](phase8-audit-native-tour-2026-10-06.json).
  The tour recorded the runtime's optional `/wails/custom.js` 404 and its
  corresponding resource message; no other console errors were recorded.

Native acceptance used copied writable fixtures and a fresh data profile at
`D:/tmp/lumen-phase8-audit-2026-10-06/data/1791231736761`, with isolated
`LUMEN_DATA_DIR` and DevTools port `9477`. Native runs were sequential.
The tour reused that isolated profile on port `9478`.
The user's normal library was never opened for verification.

The native checks cover EXIF orientation, embedded colour-profile inspection,
TIFF/HEIC renders, honest resolution limits, zoom/pan, display adjustments,
comparison, filmstrip, slideshow, image auto-hide, image fullscreen/tooltips,
bulk selection/action outcomes, stale snapshots, original-byte copies,
rendered exports, rename/Undo, MP4/MKV audio switching, reusable subtitle cues,
video fullscreen with nested frame-export confirmation, saved frames,
opt-in resume history and privacy changes during active playback.
Viewer, video and fullscreen-confirmation screenshots were visually inspected.

The first native attempt stopped at the image auto-hide assertion because the
slideshow's next fixture was a video with a separate control owner. The
harness now opens a known photo before that assertion. The passing run also
correctly recognises Wails' `wails.localhost` host as local traffic.

Production executable SHA-256:
`e0558f67b3b590ff0712ad55479c9fdcddc7b94387789b8234c847422e5a2ac5`.

## Acceptance limits

This review and its passing gates do not prove that every possible input,
device or interleaving is defect-free. No new clean-machine installation,
signing, installer packaging, OS Open With dialog, touch/high-DPI hardware,
unusual codec/hardware acceleration, RAW/DNG quality, downloaded AI-model
quality or very large real-library acceptance was performed. Existing
historical packaging and performance reports remain separate evidence.
No commit, push, merge or publication was performed.
