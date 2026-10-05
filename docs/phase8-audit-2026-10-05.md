# Phase 8 branch audit and repairs

Reviewed `update/phase-8`, implementation commit `335a2de`, against merged main
`4fc704a` on 5 October 2026. The branch introduced 92 changed files spanning
viewing, playback, inspection, selection, file operations, migrations and
generated bindings. This audit traces those contracts and their existing
consumers; it does not assert exhaustive device or scale acceptance.

## Repairs

- **Image readiness:** presentation and thumbnail refreshes preserve an already
  loaded original when its URL is unchanged. Readiness belongs to the loaded
  source URL, so refreshed metadata cannot leave that original hidden forever.
- **Viewer state:** comparison requests follow navigation and discard obsolete
  results. Current details and the filmstrip refresh with library changes;
  comparison refreshes also carry a request fence. Navigation errors are
  reported, auto-hide responds to preference changes, and presentation edits
  disable conflicting commands until their save settles.
- **File-action admission:** previews bind to their complete inputs, including
  captured frame time and selection revision. Changes during planning or
  confirmation cannot execute an obsolete plan. Closing the owner invalidates
  pending apply/Undo confirmations. Progress polls cannot overwrite settled
  exports or schedule themselves again after their batch ends.
- **Batch lifetime:** a disposed dialog cannot start a newly confirmed batch.
  Batch blocking honours cancellation between assets and settles the current
  item once. Missing-outcome detection uses a set instead of repeated scans.
  Additive Shift selection removes IDs absent from the new membership snapshot.
- **Playback replacement:** events from discarded video elements cannot change
  current playback intent, errors, volume or position. Converted looping starts
  the new source without replaying the old offset. Resume reads cannot overwrite
  an explicit seek/audio change or apply after recording is disabled. Restored
  times use the clamped position, and custom saved speeds remain selectable.
- **Bounded previews:** playback scrubbing and grid hover share the existing
  indexed sprite geometry. Scrubbing no longer decodes and permanently caches
  arbitrary timestamps in the face-frame cache.
- **Subtitle ownership:** one text track per video element is reused. Old cues
  are removed in hidden mode, then the track is disabled; disabled tracks expose
  no cue list. Repeated selection cannot duplicate visible captions or retain
  old cue lists.
- **Native worker admission:** image inspection shares rendering's cancellation
  and admission slot. Inspector/player requests abort when obsolete or disposed.
  Metadata reads cannot multiply concurrent native-worker memory budgets.
- **Capabilities and history:** render admission and the advertised render
  capability use the same source-dimension budget, including unlocked browsing.
  Playback writes check recording opt-in inside their write transaction, so late
  saves cannot recreate history after disabling it. Viewer preference keys have
  one core constant, startup failures use the notification owner, and a contract
  regression compares frontend fallback preferences with engine defaults.

## Verification

The full static gate, Go race suite and production build passed. Final frontend
checks passed with **507 tests across 48 files**. Native WebView2 acceptance
passed **56 checks with zero failures and zero uncaught frontend errors**,
recorded in [the acceptance artifact](phase8-audit-native-acceptance-2026-10-05.json).
Viewer and subtitle screenshots were visually inspected after the final rerun.
The repairs add 12 frontend regressions and four Go regressions.

- `source scripts/env.sh; export CGO_ENABLED=1 CC=clang; bash scripts/lint.sh`:
  Go lint reported zero issues; deadcode, govulncheck, Vue typecheck, ESLint,
  Prettier, frontend tests, Python checks, four installer regressions and
  ShellCheck passed. The Go vulnerability check reported no findings. After
  the final subtitle correction, `npm run check` and the full frontend test
  suite were rerun successfully against the final source.
- `go test -race -count=1 -timeout 900s . ./internal/... ./services/...`:
  all packages passed. The subsequently added default-contract regression and
  final playback-history guard were also checked with the race detector.
- Wails bindings regenerated from Go. The only generated change is the corrected
  render-capability documentation; subsequent generation was byte-for-byte
  deterministic.
- `wails3 build`: production Windows executable built successfully. Its SHA-256
  is `F5524C56E6DEB9FD73D7ED5A3EF74D35A6A06233699045C24FEED4F6E262D94B`.
- Native checks use a fresh library and copied fixtures under
  `D:/tmp/lumen-phase8-audit-2026-10-05`, with a fresh DevTools port per run.
  Harness and logs are under `.task/phase8-audit`. The initial harness needed a
  readiness wait for asynchronous audio-track discovery. Screenshot inspection
  then caught duplicate subtitle cues; the fix and stricter cue-count assertions
  were applied before the final rerun.
- `git diff --check`: passed. These repairs were committed as `470ccc7`.

## Acceptance limits

The production build still reports a 554 kB main JavaScript chunk above Vite's
500 kB advisory threshold (approximately 132 kB gzipped). No threshold was
raised or check weakened. A representative large-library startup/memory/DPI
matrix was not measured in this audit.

RAW/DNG and tiled full-resolution decoding remain later-phase work. JPEG/PNG
exports retain the documented intermediate rendered-copy limitations. Native
Open/Open with OS-dialog hand-off, touch hardware, unusual codecs/GPUs, clean
machine installation, signing and release packaging are separate acceptance
work. No commit, push, merge, installer release or publication was performed.

## Follow-up audit of the branch head

Reviewed `update/phase-8` at `098f16b` (merge base and main `4fc704a`):
`335a2de` implementation, `470ccc7` the repairs above, `098f16b` the new logo
and its derived assets. These repairs are local and uncommitted.

- **Render cache key:** the display-adjustment version was part of the key
  although rendered pixels do not depend on it, so every rotate, flip or reset
  of a HEIC/TIFF ran the native worker again and kept another identical copy.
  The key is now source revision, orientation, size and decoder version.
- **Export encoding:** a rendered export was JPEG-encoded up to three times
  (viewing render, baked adjustment, final) and a video frame twice. Pixels are
  now decoded once, adjusted, and encoded once at the chosen quality.
- **Export names:** one name already in the folder, or shared by two selected
  files, refused the whole plan; saving a second frame of a video to the same
  folder always failed. Such a name now takes the Organise planner's " (2)".
- **Consistency:** Open/Open with accepts an already-initialised COM thread as
  recycling does; the `strconvInt` and `EncodeImage` forwarding wrappers are
  gone; the logo's default size can no longer outrank a caller's size class.
- **Records:** the plan's status line, the render-key and export statements in
  the implementation record, and the conventions' scope now match the code.

Verification of the repaired tree: `scripts/lint.sh` passed (507 frontend
tests across 48 files). The Go race suite passed except
`TestWatchReportsChangesAndReleasesHandle` in the untouched `internal/scanner`,
which timed out once under load and passed three consecutive race reruns.
Bindings regenerated with only the `PlanExport` comment changed. `wails3 build`
passed (`Lumen.exe` SHA-256
`5DF4D5FC796F806A03FC6E5C9373CCFAF12A5D051BB653DFD2264F49446F49A4`) and the
native WebView2 harness passed 56 checks with no uncaught frontend error. Four
Go regressions were added (adjustment geometry, free export names at both
layers, baked export rotation).

Recorded, not changed: batch actions and Shift-range selection make one call
or one full-result query per step, unmeasured on a large library; a rotated
photo's preview is briefly stretched until its details load; the inspector
shows the engine's untranslated "Embedded ICC"; `NewLibraryService` takes its
cache as an optional trailing argument; export journal rows are never pruned.
