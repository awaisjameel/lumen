# Current implementation audit — 28 September 2026

This follow-up covers phases 1–4 and the implemented manual/smart-album part of
phase 5. The working tree was clean before this audit. The earlier
[phase 1–4 report](audit-2026-09-28.md) is historical evidence; verification below
was run again against the current implementation.

## Scope and architectural review

| Boundary | Reviewed paths and checks |
| --- | --- |
| Startup and shutdown | `main.go`, data/runtime paths, service wiring, model lifecycle, indexer cancellation, SQLite closure and logging. |
| Persistence | All six migrations, asset/file identity, visibility predicates, source transactions, durable jobs, embeddings, tags, faces, people and album rules/membership. |
| Background work | Scanning, Windows watching, reconciliation, queue pressure, stage dependencies/versions, battery/pause behavior and native model/decoder ownership. |
| Media and AI | Thumbnail regeneration, video frames/sprites/streaming, HTTP range support, model preparation/metadata, native inference tests, text/image search and user-correction precedence. |
| UI contracts | Generated Wails bindings, the bridge, Pinia stores, route scopes, selection, viewer, Details, People, Things, Albums, Settings and shared loading/error handling. |
| Build and quality | Windows build tasks, dependency setup, Go/TypeScript configuration, linting, tests, generated output and documentation. Other platform files are Wails templates, not accepted Lumen targets. |

The existing ownership boundaries remain appropriate: `core` defines contracts,
`store` owns SQL, services expose typed operations, the bridge owns UI calls, and
feature stores own presentation state. Repairs keep those boundaries and reuse
the existing list store rather than introducing a second state abstraction.

## Confirmed findings and repairs

| Finding | Repair and evidence |
| --- | --- |
| Album kind crossed Go and TypeScript as an unrestricted string, with a frontend assertion maintaining a second contract. | Added `core.AlbumKind`, regenerated the Wails enum and method signature, and updated bridge/view consumers to use it. Strict compilation and enum linting verify the consumers. |
| Albums bypassed the shared list loader and could let an older request replace newer membership counts. A failed shared-list read could also lose a queued refresh. | Albums now use `defineListStore`; the helper publishes only the current response and preserves a queued refresh after an older failure. Regression tests cover both paths. |
| Album counts and covers could come from different database states while indexing or source removal ran. Membership checks also happened outside their write transaction. | Album summaries use one read transaction; membership validation and writes use one write transaction. Existing album integration tests and the race suite pass. |
| Merging a person left smart albums pointing to the deleted identity. Removing an unnamed identity could lose saved rules or user rejections. | Merge the JSON person reference in the same transaction as faces. Keep empty unnamed people while an album or rejection references them. Tests cover merged album counts/timeline, empty referenced identities and last-face rejections. |
| Smart rules could save nonexistent references; deleting/replacing a Thing could leave a rule pointing to a missing or subsequently reused ID. | Validate references under the writer transaction. Reject deletion of a Thing used by a smart album with an actionable error. Vocabulary retirement preserves referenced Things as manual tags. Tests cover create/update validation, deletion and vocabulary retirement. |
| `Person` copied its return value before populating its cover. | Populate the cover before copying the response. A chosen-cover regression verifies the result. |
| Details, viewer and person responses could overwrite a later visit to the same identity. A completed album creation could erase a draft on another photo. | Use request generations and selection revisions, invalidate work on unmount, and guard edit/navigation completion. Component regressions cover A → B → A navigation and delayed album creation. |
| Removing an item from a filtered view could leave an open Details panel or viewer whose actions no longer had a grid item. | Clear stale selections/viewers when their item or entire day disappears; prevent the viewer-close watcher from restoring an invalid selection. Store regressions cover both cases; reproduced during browser acceptance. |
| The 256-entry scan queue dropped work under pressure. Removing the last source could also fail to emit a library-change event. | Coalesce pending scans by source, widening disjoint changes to a full-source scan, and explicitly report source changes. A 300-source regression verifies queue retention; indexing/watching race tests pass. |
| A failed thumbnail rebuild could remove a lock entry while another caller still waited on that lock, allowing later retries to overlap. | Use a fixed set of stable rebuild locks and check cancellation after acquiring a lock. Cache/race tests and a cancelled-rebuild/retry regression pass. |
| The album editor exposed two different controls with the same “Media” accessible name. Album cover links also lacked an accessible name. | Name the album-type selector separately and label cover links with the album name. |

Read transactions deliberately use SQLite's
[WAL snapshot isolation](https://www.sqlite.org/isolation.html). Reference
protection also avoids depending on ordinary integer primary keys never being
[reused after deletion](https://www.sqlite.org/autoinc.html).

## Compatibility and user-visible effects

- No schema migration or reindexing is required by these repairs. Existing
  album-kind strings remain unchanged on disk and on the wire.
- A Thing used in a smart album must first be removed from that album's rule
  before it can be deleted. Retired vocabulary entries used by saved rules
  become manual Things.
- Person references follow future merges. Already-lost identity/rejection
  information from older runs cannot be reconstructed by this repair.
- Original media is never modified. Browser acceptance used synthetic media
  and an isolated data directory.

## Verification

| Check | Result |
| --- | --- |
| Go race suite | `go test -race -json ./internal/... ./services/...`: 91 tests passed across ten tested packages. Two optional real-person corpus tests skipped. |
| Static quality gate | `scripts/lint.sh`: Go format/import/static/security checks, deadcode, govulncheck, strict Vue/TypeScript checks, ESLint, Prettier, frontend tests, Ruff and ShellCheck passed. |
| Frontend regressions | 20 tests passed in seven files, including eight new regressions. |
| Dependencies | govulncheck reported no vulnerabilities; the production build's npm install audited 305 packages and reported zero vulnerabilities. No dependency upgrades were introduced. |
| Generated contracts | Regenerated with Wails. The reviewed differences are the new album-kind enum, its export and its service/response uses. Repeating generation produced identical binding hashes. |
| Windows production build | `wails3 build` passed, compiling the frontend before Go embeds it. |
| Live browser | Real Wails services in server mode: scanned/indexed three synthetic assets; created a manual album; added a photo from Details; checked its live count; created a favourites-only smart album and observed membership change after unfavouriting; checked direct MP4 and converted AVI playback. Rebuilt and confirmed the final smart-album removal clears stale Details selection; visually inspected the resulting empty state. |
| Diff hygiene | `git diff --check` passed. |

Evidence logs are under ignored `bin/audit-20260928-review/`. The original
`bin/audit-20260928/` artifacts belong to the earlier audit and were not modified.
The isolated audit server was stopped and its browser tab closed after acceptance.
Changes remain local and uncommitted. No push, deployment or remote CI run was
requested or performed.

## Acceptance limits and phase boundaries

- This is evidence from static review, tests, builds and targeted UI acceptance,
  not a guarantee of zero defects or zero technical debt.
- Native WebView2 window behavior, DPI, installers, shell integration, drive
  disconnection/reconnection and every codec/GPU combination were not accepted
  by this browser test. Server-mode browser behavior is not proof of those paths.
- `TestFacesOnRealPhotos` and `TestIndexerFindsPeople` require
  `LUMEN_FACE_PHOTOS`; no real-person corpus was supplied. Native model tests ran,
  but visual identity/clustering accuracy on the user's library remains open.
- Full-library latency/memory benchmarks and long-running soak tests were not
  rerun. The scan-pressure regression is a correctness test, not a throughput
  benchmark.
- The browser console recorded one asynchronous listener/message-channel error.
  Its origin was not established; the exercised UI flows and media playback
  completed, with no video-element errors observed.
- A smart rule already referencing an identity lost before this repair needs
  its person/Thing reselected. The change prevents new invalid saves; it does
  not guess the intended identity in old data.
- Duplicate review, events, places, trash, organise-on-disk, cache-cap enforcement
  and OCR remain planned work. Installer and non-Windows templates are outside
  the currently implemented Windows application acceptance scope.

The implemented scope passes the checks above. Keep these acceptance limits
visible when planning the next phase.
