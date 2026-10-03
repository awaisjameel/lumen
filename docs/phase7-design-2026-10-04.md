# Phase 7: the Nuxt UI frontend and design system

Date: 4 October 2026. Branch `phase7/frontend`, from `main` at `5525ec2`.
This is the record of what Phase 7 of
[the development plan](next-development-plan-2026-10-04.md) built: 7A
(baseline, contracts, integration), 7B (design system and app foundations)
and 7C (every existing screen migrated). It describes the implementation as
it is, the decisions taken, what was verified and how, and what remains
unverified. The baseline it is measured against is in
[the inventory](phase7-inventory-2026-10-04.md).

Phases 8–12 are not started. Phase 7 establishes their contracts and ships
no placeholder control for them.

## 1. Versions integrated

| Package | Version | Published | Note |
| --- | --- | --- | --- |
| `@nuxt/ui` | 4.11.2 | 22 Sept 2026 | The newest stable release that passes `min-release-age=7` on 4 October. 4.11.3 (1 Oct, 13:37 UTC) becomes eligible on 8 October 13:37 UTC; moving to it is a lockfile change to make then. |
| `tailwindcss` | 4.3.3 | – | Through Nuxt UI's Vite plugin (`@tailwindcss/vite` 4.3.3). |
| `@iconify-json/lucide` | 1.2.137 | 26 Sept 2026 | The local icon collection (dev dependency: only bundled icons ship). |
| `@iconify/vue` | 5.0.3 | – | Direct dependency, to switch the icon API off. |
| `reka-ui` | 2.10.4 | – | Nuxt UI's primitives (transitive). |

Unchanged: Vue 3.5, Vite 8.3, Vue Router 5.3, Pinia 4, TanStack Virtual
3.13, TypeScript 6.0, ESLint 10, Vitest 5, Wails 3.0.0-beta.26, Go 1.27.
`frontend/.npmrc` keeps `min-release-age=7`; the lockfile is updated.

**Integration** (`frontend/nuxt-ui.config.ts`, used by both `vite.config.ts`
and `vitest.config.ts`): Nuxt UI's Vite plugin beside the Vue and Wails
plugins; the Vue plugin and one root `UApp` (`App.vue`); `#app` is
`isolate`d (`index.html`). Component and composable auto-import are off:
components are imported by name, so the strict ESLint rules
(`vue/no-undef-components`, the bridge-only import boundary) hold unchanged
and no generated declaration file is needed. Nuxt UI's colour-mode
integration is off: Lumen's preference store is the one theme owner.

## 2. Contracts

### Preferences

`core.Preferences` (`internal/core/preferences.go`) is the typed contract:
theme (system, light, dark), accent (blue, teal, green, violet, rose,
amber), density (comfortable, compact), text scale (90–130 %), thumbnail
size (96–320 px), sidebar collapsed, sidebar width (200–360 px), inspector
width (280–480 px). Defaults and ranges live in `core`; the bridge mirrors
the numeric limits and `services/contract_test.go` checks them.

- **Durable owner:** the library's SQLite settings (`ui_*` keys),
  through `LibraryService.Preferences`, `SetPreferences` and
  `MigratePreferences` (`internal/store/preferences.go`). A save writes all
  the keys in one statement. Stored values are normalised on read and on
  write, so a value from another version never breaks the interface.
- **Session owner:** `shared/preferences.ts` (Pinia). A change shows at
  once, then saves through `latestSave`: one save at a time, ending with the
  newest; a failed newest save rolls back to what the library holds and
  says why.
- **First-paint cache:** `localStorage["lumen.appearance"]`, rewritten
  whenever an appearance is applied. It is derived, never read as the
  user's choice once the engine has answered.
- **Migration:** the old `lumen.theme` and `lumen.zoom` are read (never
  written) and offered once per library. `MigrateLegacyPreferences` stores a
  legacy value only where the library has none and sets the marker
  `ui_legacy_migrated` in the same statement, so a repeat, an interrupted
  first call or a second library behave correctly (tests in `store`).
- Appearance changes start no indexing and touch no file.

Later phases add their preferences to this contract with the features that
use them. Updater preferences are application-scoped and are not here.

### Media capabilities

`core.MediaCaps` on `AssetDetail.caps` replaces the extension flag
`webNative`: `preview`, `original`, `render`, `directPlay`, `convertPlay`
(`core.CapsOf`). The viewer shows the original when `caps.original` says so.
`render` is always false: no full-resolution render path exists before
Phase 8, and an embedded preview never counts. The wording follows
[the media feasibility record](phase7-media-feasibility-2026-10-04.md),
which also proposes the render budgets and cache key for Phase 8/9.

### Updater

No updater code, setting, control or network request was added. The trust,
installer and shutdown contracts are in
[the updater feasibility record](phase7-updater-feasibility-2026-10-04.md).
Lumen still makes no network request.

## 3. Design system

| Piece | Where | What it is |
| --- | --- | --- |
| Tokens | `design/tokens.css` | Nuxt UI's semantic colour variables set for Lumen's neutral surfaces (light and dark), plus `--lumen-*` layout, density, motion, layer and viewer tokens. System fonts. Reduced motion, forced colours and coarse-pointer rules. |
| Appearance | `design/appearance.ts` | Applies a preference set: `.dark` on the root (so portalled overlays follow), `data-accent`, `data-density`, `--lumen-text-scale`, and Nuxt UI's accent and control size through its reactive config. Follows Windows for "system". |
| Icons | `design/icons.ts` | The manifest: every icon by Lumen's name. The build bundles exactly these from the local Lucide collection (`icon.clientBundle`); an unresolved name fails the build. `main.ts` replaces Iconify's API module, so no icon is ever requested. |
| Layout modes | `shared/layout.ts` | Wide (≥ 1200 px): resizable, collapsible sidebar and inline inspector. Medium (≥ 900 px): icon rail. Narrow: navigation drawer, folded toolbar controls, inspector sheet. Pages also respond to their own width by container queries. |
| Compositions | `shared/` | `Page`, `PageHeader`, `FeatureState` (loading, empty, failure), `CollectionCard` and `CardGrid`, `MediaTile`, `SelectionBar`, `MetaRow`, `PrefRow`, `WorkProgress`, `ResizeHandle`, `Icon`, `ConfirmDialog`. |
| Commands | `shared/commands.ts` | One registry for toolbars, menus, the command palette (Ctrl+K) and shortcuts; availability and enabled state are computed; zones scope a shortcut to the grid or the viewer. The shortcut reference (`?`) lists what the registry holds. |
| Overlays | `shared/overlay.ts` | Lumen's full-window layers register in a stack (`useLayer`); Nuxt UI's portalled overlays are recognised in the document. `shortcutsSuspended()` replaces the old `dialog[open]` check. |
| Notifications | `shared/notify.ts`, `app/AppNotifications.vue` | One channel, shown as toasts: failures, Undo offers (Trash, blocks), notices, the crash notice. The three separate bars of the baseline are gone. |
| Shell | `app/` | `AppShell`, `AppNav`, `AppOverlays`, `CommandPalette`, `ShortcutHelp`; `nav.ts` is the one list of destinations. |
| Showcase | `app/showcase/Showcase.vue` | Development only: its route exists only when `import.meta.env.DEV` is true, so a production build emits nothing of it. |

Layering (ESLint-enforced): `design` → `shared` → features → `app`; only the
bridge imports bindings or the Wails runtime.

## 4. Decisions

1. **Nuxt UI 4.11.2 now, 4.11.3 after 8 October.** The seven-day rule is
   kept rather than bypassed.
2. **Explicit imports, no auto-import.** Keeps every existing lint rule
   and the strict type checks as they were, at the cost of import lines.
3. **Preferences are one struct saved whole.** A save is all of a request's
   values or none; ordering is `latestSave`'s. The thumbnail size and panel
   widths join the appearance values because they follow the library, not
   the WebView.
4. **Legacy browser values are read, not deleted.** A second library opened
   by the same WebView profile still receives them once; the library's
   marker, not the browser, records that the offer was made.
5. **Light theme accents use a deeper shade** (600; 700 for teal, green and
   amber) so white text on the accent keeps 4.5:1.
6. **Density changes control size through Nuxt UI's own size variants**
   and spacing through tokens; coarse pointers get 44 px targets by rule.
7. **The index status list and the search suggestions stay purpose-built**
   (hover card with described-by wiring; combobox), restyled on the tokens.
8. **Toasts carry Undo.** An Undo offer is a notification action with a
   stable id; a newer one replaces the older.
9. **No Phase 8 feature in the viewer.** Zoom, filmstrip, slideshow, custom
   video controls, context menus and multi-selection are Phase 8.
10. **Tests mount inside the app's providers** (`mountInApp`) and assert by
    role, label and deliberate `data-*` hooks.

## 5. Requirement matrix

How each Phase 7 requirement is met and what shows it. "Tests" are the
Vitest files beside the code; "native" is the acceptance run on the
production build in WebView2 (section 6).

### 7A

| Requirement | Implementation | Verification |
| --- | --- | --- |
| Inventory of routes, overlays, actions, shortcuts, preferences; baseline screens | [The inventory](phase7-inventory-2026-10-04.md); 37 baseline screenshots | Document; `tour.mjs` on the baseline build |
| Media capability contract | `core.MediaCaps`, `core.CapsOf`, `AssetDetail.caps` | `internal/core/preferences_test.go`; viewer tests; bindings regenerated |
| Preference contract and migration | `core.Preferences`, `store/preferences.go`, `LibraryService` methods, `shared/preferences.ts` | `internal/store/preferences_test.go` (defaults, reopen, repeat, interrupted, two libraries); `shared/preferences.test.ts`; `services/contract_test.go`; native theme checks |
| RAW and rendered-image feasibility | [Record](phase7-media-feasibility-2026-10-04.md), probes in `spikes/phase7` | Measured on this machine; real-camera RAW untested (no fixture) |
| Nuxt UI, Tailwind 4, local icons; Vite plugin beside Wails; one `UApp`; root isolation | `nuxt-ui.config.ts`, `vite.config.ts`, `main.ts`, `App.vue`, `index.html` | `wails3 build`; `npm run check`; 41 test files |
| Local icons, fonts, assets; dynamic icons bundled; no remote fallback | `design/icons.ts`, `icon.clientBundle`, API module replaced in `main.ts`; system fonts only | `design/icons.test.ts`; native tour with a fresh WebView cache and every non-local request refused: none attempted |
| One theme owner, including portal content | `shared/preferences.ts` → `design/appearance.ts`; `.dark` on the root | Native: dark, light and "system" checks; the palette's dialog in dark |
| Updater trust, packaging and shutdown feasibility | [Record](phase7-updater-feasibility-2026-10-04.md) | Source inspection with citations; no certificate or release host exists to test against |
| Exit: production app renders page, dialog, menu, tooltip, form, toast with theme, focus, strict types, offline assets | The shell and Library | Native acceptance; type and lint gate |

### 7B

| Requirement | Implementation | Verification |
| --- | --- | --- |
| Neutral surfaces, restrained accent, system fonts, curated accents, status colours keep meaning | `design/tokens.css`; six accents | Screenshots; native accent check |
| Semantic colour, type, spacing, radii, borders, elevation, focus, control sizes, disabled, layers | Tokens; Nuxt UI variants | Showcase; screenshots |
| Comfortable and compact density; coarse-pointer targets; text scaling without truncating required actions | `data-density`, size variants, `(pointer: coarse)` rule, `--lumen-text-scale` on the root font size | Native: compact + 120 % text; headers wrap their tools rather than cut them |
| Wide, medium and narrow layouts (1200 and 900 px) | `shared/layout.ts`, `AppShell`, container queries | Native at 1440, 1040, 720 and 520 px: no sideways overflow on any toured screen; drawer and inspector sheet checks; `app/shell.test.ts` |
| One page header, toolbar, filter, action menu, confirmation, toast/Undo, empty, loading, error, background work | `Page`, `PageHeader`, `FeatureState`, `SelectionBar`, `ConfirmDialog`, `notify`, `WorkProgress` | Used by every migrated screen; tests |
| Shared command registry | `shared/commands.ts`; palette and shortcut reference | `shared/commands.test.ts`; native palette checks |
| One overlay coordinator | `shared/overlay.ts`; Nuxt UI overlays everywhere | `shared/overlay.test.ts`; native Escape-ownership and focus-return checks |
| Motion tokens; reduced motion; forced colours | Tokens and media rules | Native: reduced motion stops animation and transitions; forced-colours screenshots |
| Development-only showcase | `app/showcase/Showcase.vue`, dev-only route | Production bundle contains no showcase text or route |
| Exit: shell works narrow, high DPI, keyboard-only | Shell | Native at 125, 150 and 200 % and keyboard checks |

### 7C

| Module | Migrated files | Behaviour retained (evidence) |
| --- | --- | --- |
| Onboarding | `onboarding/Welcome.vue` | Non-overlapping sources, canonical path comparison, incremental browsing (`Welcome.test.ts`) |
| Library, Favourites, Videos | `library/LibraryView.vue`, `IndexStatus.vue`, `TimelineScrubber.vue`, `MediaTile` | Virtualisation, on-screen priority, scoped timeline, live changes (`LibraryView.test.ts`, `store.test.ts`, `IndexStatus.test.ts`; native keyboard, refresh and removal checks, identical on the baseline) |
| Search | `search/SearchBox.vue`, the Library's search states | Relevance order, person constraints, blocked-name notice (`store.test.ts`; native search) |
| Viewer and details | `library/Viewer.vue`, `DetailsPanel.vue` | Preview then original by capability, navigation, Trash actions, direct and converted video, stale-detail guards (`Viewer.test.ts`, `DetailsPanel.test.ts`; native video and viewer checks) |
| People | `people/*` | Corrections win; no automatic merge of named or blocked identities (`PersonHeader.test.ts`, `PersonFaces.test.ts`, `naming.test.ts`) |
| Things | `things/*` | Prompt identity, built-in versus custom, pin, rename, reset (`ThingsView.test.ts`) |
| Places | `places/PlacesView.vue` | Offline names, live visibility-aware counts (`PlacesView.test.ts`) |
| Events | `events/EventsView.vue` | Names and pins follow photos (`EventsView.test.ts`) |
| Albums | `albums/AlbumsView.vue` | Typed rules and their error codes, live counts (`AlbumsView.test.ts`) |
| Duplicates | `duplicates/*` | Exact copies recycle extra files, similar photos go to the Trash, kept-file notice, Undo (`DuplicatesView.test.ts`, `store.test.ts`) |
| Trash | `trash/*`, the Library's Trash scope | No automatic emptying, Recycle Bin only, confirmed file count, Undo (`trash.test.ts`; native confirmation check) |
| Blocked | `settings/BlockedView.vue`, `CoveredBrowser.vue`, `covered.ts`, `shared/BlockDialog.vue`, `PinFields.vue` | PIN limits and cool-down, Windows recovery, idle lock, stale-response guards (`BlockedView.test.ts`, `covered.test.ts`, `BlockDialog.test.ts`; native: lock removes covered media at once, the engine refuses it, leaving locks) |
| Organise files | `organise/*` | Journalled moves, no overwrite, bounded preview, resume and undo (`OrganiseView.test.ts`, `store.test.ts`, `template.test.ts`) |
| Settings | `settings/SettingsView.vue`, `OcrSetting.vue`, `XmpSetting.vue` | Every option and its save ordering, clamping and rollback (`SettingsView.test.ts`, `OcrSetting.test.ts`, `XmpSetting.test.ts`) |
| About, diagnostics, notices | `about/*` | Local logs, cancellable report, nothing sent (`about.test.ts`) |
| Cleanup | Removed: `shared/styles.css` (the old tokens and control classes), `EmptyState.vue`, `GroupCard.vue`, `Notice.vue`, `TrashUndo.vue`, `shared/theme.ts`, the hand-drawn icon paths, every native `<dialog>` | A scan of `frontend/src` for the old classes, variables and native dialogs finds none; no i18n key is unused |

## 6. Verification

All on 4 October 2026, on the development laptop (Intel i7-13700H, 20
threads, Intel UHD graphics, 32 GB, Windows 11 build 26200, mains power,
display at 125 %).

### Gates

| Gate | Result |
| --- | --- |
| `scripts/lint.sh` | All checks passed: golangci-lint 0 issues, deadcode, govulncheck, `vue-tsc`, ESLint (0 warnings), Prettier, 445 frontend tests in 42 files, Ruff, installer payload tests, ShellCheck. |
| `go test -race -count=1 -timeout 900s . ./internal/... ./services/...` | Every package passes. (A first run made while five agents and an indexing app shared the machine timed out in `scanner`'s file-watch test; the quiet rerun passed. That test and package are unchanged by Phase 7.) |
| `wails3 generate bindings -f '-tags production' -clean=true -ts -i` | Regenerated for `Preferences`, `LegacyPreferences`, `MediaCaps` and the three service methods; `services/contract_test.go` passes. |
| `wails3 build` | Production build succeeds. The UI is 49 files: a 443 kB entry script, shared and per-screen chunks, one 214 kB stylesheet. No showcase text or route is in the bundle. |
| `wails3 task package` | `Lumen-0.9.1-x64-setup.exe` built (unsigned: no certificate is configured). `THIRD_PARTY_NOTICES.txt` lists the 30 bundled UI packages with their licences, and the Lucide icon set (ISC; the Feather-derived icons MIT), which `scripts/notices.py` now adds because bundled icon data is not in Vite's licence list. |

The version was not changed and nothing was installed, published, pushed
or committed.

### Native acceptance (WebView2, production build)

Driven over the WebView's DevTools port with a scratch library
(`LUMEN_DATA_DIR`); the user's own library was never opened. Scripts and
output are in `D:\tmp\phase7` (`harness/`, `shots/`).

| Check | Result |
| --- | --- |
| Offline assets | Fresh WebView data folder, every request that is not to the app itself refused and recorded: 37 screens toured, 357 local requests, **0** external requests attempted. Icons, fonts and styles all rendered. |
| Screens | Every route at 1440×900, the core screens at 1040, 720 and 520 px: no sideways overflow on any. |
| Themes and preferences | Dark, light and "system" (following an emulated Windows change while running); accent, compact density and 120 % text survive a reload; a portalled dialog is dark with the page. |
| Overlays and focus | Ctrl+K opens the palette with focus in its field, filters, runs a command and closes; Escape closes it and returns focus to the grid; `?` opens the shortcut reference; a toolbar tooltip shows its name and key. Page shortcuts do not fire over the viewer. |
| Escape ownership | In the Trash, the viewer's "Delete from disk…" asks first with Cancel focused; Escape closes only the question, the viewer stays and nothing is deleted; a second Escape closes the viewer. |
| Keyboard | Arrows select in the grid, the grid names its active descendant, Enter opens the viewer, → moves on, Escape closes it and returns focus and selection to the photo last viewed. |
| State through change | Unfavouriting in Favourites removes the photo and leaves no selection on a missing cell; unfavouriting in the viewer closes it rather than keep a removed photo; a rescan keeps the selection. The baseline build gives the identical results on these checks. |
| Narrow and medium windows | Narrow: no sidebar, a navigation drawer that holds focus and returns it, the inspector as a sheet. Medium: an icon rail. |
| DPI | Emulated 125, 150 and 200 %: the grid and Settings render without overflow. |
| Reduced motion | Animation and transition durations collapse; loading states still say what is happening. |
| Forced colours | Focus is an outline; radios, switches, sliders, progress and button edges are redrawn in system colours (reviewed in screenshots; the first pass showed unselected radios and sliders vanishing, which the token rules now correct). |
| Video | A direct-play H.264/AAC MP4 and two videos that need conversion (MPEG-4 in AVI and in MKV) play in the viewer through the loopback server. |
| Privacy | A blocked photo's media is refused while locked; the Blocked screen starts locked; the PIN opens the list and the covered-media browser; "Lock now" removes covered media from the screen in the same moment and the engine refuses it; leaving the screen locks the engine. |
| Onboarding | On an empty scratch library the Welcome screen renders outside the shell, offers the suggested folders, and fits a 560 px window. Nothing was added. |
| Showcase | In `wails3 dev` the showcase route renders every section and all 99 manifest icons, and its theme control does not save. |

Final run: 43 of 43 behaviour checks passed.

One defect found by native testing and fixed: in development the app's
asset handler answered Vite's extension-less module addresses
(`/@id/virtual:…`, which Nuxt UI adds) with the page, so the UI could not
start under `wails3 dev`. `spa` in `main.go` now leaves paths under `/@`
alone (`TestSPAServesRoutesAsThePageAndLeavesFilesAndDevModulesAlone`).
Production was never affected.

### Performance against the baseline

Same machine, same 3,000-photo synthetic library (2048×1536 JPEGs,
382 MB, indexed once by the baseline build in 273 s on mains; each build ran
on its own copy), production builds, measured through the DevTools port.

| Metric | Goal | Baseline 0.9.1 | Phase 7 |
| --- | --- | --- | --- |
| Cold start to a usable grid (median of 8, builds alternated) | < 2 s | 697 ms | 819 ms |
| Reload to a usable grid in a running app (median of 12) | – | 105 ms | 177 ms |
| Scrolling, 10 s of continuous steps | 60 fps | median 16.7 ms, p99 16.9, max 18.2, no frame over 33 ms | median 16.7 ms, p99 17.3, max 18.0, no frame over 33 ms |
| Warm search (median of 5) | < 300 ms | 96 ms | 101–112 ms |
| First search (loads the text model) | recorded apart | 0.93 s | 1.0–1.1 s |
| Viewer: open; next photo (median of 10) | – | 26 ms; 21 ms | 20–28 ms; 22–23 ms |
| Idle memory, all processes, 45 s on the grid (median of 3, alternated) | < 500 MB | 476 MB (463–485) | 478 MB (477–533) |

The goals are met. The start is about 120 ms slower cold and 70 ms slower
warm: the UI now loads 835 kB of script and 210 kB of styles at start
against 331 kB and 37 kB before (the component library and its theme).
Screens other than the Library, the command palette, the shortcut
reference and onboarding load on first use to hold that down. Scrolling,
search, the viewer and memory are unchanged within run-to-run noise; one
of three idle-memory samples of the new build was above 500 MB.

## 7. Limits and what is not verified

- **Real libraries and scale.** Measured on a 3,000-photo synthetic
  library. A real library, 100,000 items and low-end hardware are not
  measured in Phase 7; the Phase 6 figures for those were extrapolations and
  remain so.
- **Hardware and codecs.** One laptop, one GPU. HEVC playback, GPU
  fallback and removable storage were not re-tested; the playback paths
  exercised are H.264 direct and MPEG-4 converted.
- **People and Places with content.** The synthetic library has no faces
  and no GPS, so People, face review and Places were toured empty; their
  populated behaviour rests on the component tests.
- **DPI, forced colours and reduced motion** were emulated through the
  DevTools protocol on a 125 % display, not by changing Windows settings; a
  real contrast theme and a real 200 % display are not confirmed. Touch
  input was not tested (the coarse-pointer rule is by construction).
- **Fault states natively.** Failed engine calls, an interrupted organise
  run, the wrong-PIN cool-down, Windows sign-in for a forgotten PIN and the
  crash notice are covered by tests, not by native runs. No organise run
  moved files during acceptance.
- **Installer.** The installer was built, not installed: install, upgrade
  and uninstall were not repeated, and it is unsigned.
- **RAW.** No real camera RAW or DNG fixture exists on this machine; see the
  feasibility record. No RAW support is claimed.
- **Updater.** Nothing can be verified without a signing certificate, a
  release host and installed prior versions; see its record.
- **Nuxt UI 4.11.3** is not integrated: it fails the release-age rule until
  8 October 2026. The plan asks for the latest eligible stable release
  before the migration milestone closes; today that is 4.11.2.
- **Nuxt UI's own words** (the few labels inside its components, such as a
  dialog's close button) come from its English locale, not from
  `shared/i18n.ts`.
- **Selection by Ctrl-click or Shift-range, context menus on tiles, zoom and
  the other viewing tools** are Phase 8 and absent by design.

## 8. How the work was done

One lead built the contracts, the design system, the shell and the Library
slice, then integrated and verified. Seven Codex sub-agents (`gpt-6.1-sol`,
high reasoning effort) worked in parallel on bounded tasks with exclusive
file ownership: the two feasibility records, and five migration slices
(viewer and details; collections; Duplicates and Trash; Blocked and
Organise; Settings, onboarding and About). The agents could not run Vitest
in their sandbox, so every test was run centrally. Integration fixes worth
recording:

- One agent had the covered-media browser close on every library change,
  which would have made it unusable while indexing. Removed: the baseline's
  revocation (the screen's own lock, the idle timer, and the engine's
  refusal) is kept and was verified natively.
- An inline rename opened from a menu lost the focus to the menu's focus
  trap. `shared/rename.ts` now waits for the overlay to close.
- Tooltips counted as keyboard-owning overlays and suspended shortcuts.
  They no longer do.
- A replaced notification was removed by the older one's delayed clean-up.
  The presenter now updates in place.
- The viewer's two Trash actions had become icon-only; they are labelled
  again.
