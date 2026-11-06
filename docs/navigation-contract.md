# Application navigation

This guide records the contract implemented for [#38](https://github.com/awaisjameel/lumen/issues/38),
inventoried against integration head `557a1693`. The verified code snapshot is
recorded below; [PR #54](https://github.com/awaisjameel/lumen/pull/54) carries the
final head checks and integration record.

## Destinations and input

| Names | Paths | Supported query |
| --- | --- | --- |
| library, favourites, videos, trash | `/`, `/favourites`, `/videos`, `/trash` | media: photos or videos; omitted means the route default |
| people, things, events, duplicates, compression | `/people`, `/things`, `/events`, `/duplicates`, `/compression` | none |
| person, thing, album, place, event | `/people/:id`, `/things/:id`, `/albums/:id`, `/places/:id`, `/events/:id` | media |
| personFaces | `/people/:id/faces` | picture: 1 |
| albums | `/albums` | edit: positive collection ID, consumed once |
| places | `/places` | place: positive place ID, consumed once |
| search | `/search` | q, media, favourites: true, from/to: calendar days, place: positive ID |
| settings | `/settings` | section: appearance, library, viewing, search, indexing, files, accessibility, privacy, presets, updates |
| blocked, organise | `/settings/blocked`, `/settings/organise` | none |
| showcase (development only) | `/__showcase` | none |
| unknown-location fallback | `/:rest(.*)*` | returns to Library |

Settings aliases video, compression, about and ai mean viewing, files, updates
and search respectively. They remain accepted incoming URLs; generated links
use canonical sections. The videos route defaults to Videos; explicitly
choosing another media filter replaces it with Library and retains allowed
query state. New searches push and clear refinements; refinements replace.
Empty search remains Search. Unicode search text is encoded by Vue Router.
Album edit and map-preview query cleanup replaces without reopening consumed
state. Person face-picture hints remain local to their existing owner.

Before this refactor, the digit-only matcher accepted zero and unsafe integers;
the grid used `Number(id) || 0`, so `/people/0` could select the unrelated whole
library. Malformed identifiers now show a generic unavailable-location state
before mounting the feature. Positive safe integer IDs retain engine-owned
missing/blocked checks. Repeated values, unknown query keys, invalid filters,
invalid calendar dates and unsupported sections are invalid incoming input.
Empty optional values are absent. Supported aliases normalise without adding
history. Unknown paths retain the intentional Library fallback. Diagnostics
must never include raw URLs, queries, entity names or PINs.

## Ownership and migration inventory

The shared contract belongs below features and the shell. It owns names, paths,
parameter/query codecs, view identities, parent sections and timeline metadata.
Production registration is derived from it; the root binds views exhaustively,
keeping Library eager and the remaining views lazy. Showcase stays absent from
production assets. Vue Router remains the provider; the explicit pinned
forward-props dependency is explained below.

Typed navigation exposes push, replace, resolve, history and validated reads.
Shared link/button/menu adapters narrow internal targets; external System
OpenLink enum actions keep their bridge owner. Raw incoming locations are an
explicit infrastructure boundary. Invalid input never mounts a feature using
partially parsed parameters. A shared memory factory uses the same records and
isolated histories with stub views, rather than test-owned destination tables.
Focused AST lint and compiled positive/negative TypeScript and Vue fixtures
enforce the boundary, including shared component props.

| Entry-point family | Migrated owners |
| --- | --- |
| Shell, highlighting, drawer, commands | app/nav, AppNav, AppShell, AppOverlays, ShortcutHelp |
| Startup and onboarding | App, shared/preferences, SettingsView start-view choices |
| Grid and collection navigation | LibraryView, CollectionHead, PersonHeader, PeopleView, PersonFaces, AlbumsView, ThingsView, PlacesView, EventsView, EventCard |
| Shared route-bearing controls | CollectionCard, Chip, PageHeader, IconButton, links, buttons and menu items |
| Search and refinement | SearchBox, SearchRefine, SearchSummary |
| Recovery and handoffs | three Manage blocked actions, empty/error states, compression handoff, UpdateNotice, notification callbacks |
| Infrastructure and tests | router, main, production SPA reload handling, shared/people/details testing helpers, component memory routers, e2e helpers |

No native menu or bridge event currently supplies an application route. Native
external links use the fixed System OpenLink enum. Persisted startView values
already have a generated Go contract; validate them against eligible static
destinations without changing the wire type. Sidebar order/presentation and
contextual availability stay with the shell; route parent groups come from
the contract. Search and Videos currently have no sidebar highlight.

Preserve push/replace distinctions, back/forward, selection and scroll owners,
keyboard focus, drawer dismissal, PIN relocking and blocked-media protection.
Viewer/dialog state remains with overlays. #45 draft admission, its consumed
edit latch, focus restoration and guarded writes must survive this migration.

## Required verification

Use real production records for exact resolution and malformed-ID regressions.
Independently assert all three recovery controls match Blocked at the literal
`/settings/blocked`. Compile invalid and valid navigation examples, including
Vue props, and demonstrate route removal/rename, missing view binding and the
original bad link fail required checks. Exercise every production destination
and relevant entry-point family in a native scratch library, including history,
startup, aliases, themes/layouts, keyboard focus and PIN/privacy regressions.
Run fresh frontend/static/contracts, Windows production/package and named Go
race checks. Keep unchanged latency results and external acceptance explicit.
Record the verified head, run and retained reports; a historical result does
not establish acceptance for a later code change.


## Implemented contract and enforcement

The contract owns 21 production destinations plus the development-only Showcase.
`destination(...)` accepts destination-specific params/queries with excess-key
checks; the provider exposes push, replace, resolve, history and validated
reads. No raw-string overload is offered. Shared Link, Button and menu adapters
narrow Nuxt UI's permissive route props without replacing its rendering, focus,
slot or keyboard owners. `reka-ui` 2.11.0 was already in the lockfile through
Nuxt UI; it is now a direct pinned dependency because these adapters use its
`useForwardProps` to preserve optional Boolean props rather than silently
forwarding absent defaults. No library is downloaded at runtime.

Production registration derives from the contract and the root exhaustively
binds views. Settings presentation is exhaustive over the central canonical
sections. Startup choices are checked against eligible static destinations and
the generated preference vocabulary. The shared memory factory uses the same
records, independent histories and optional stub views. Every migrated component
test now uses it; no feature test owns a destination table. Incoming URL strings
in native helpers are deliberate browser-input fixtures and exact independent
expectations, rather than a second production route registry.

AST lint covers raw Vue Router imports, singleton access, native History and
Location writes, raw route-bearing component props, relative/opaque anchor
targets and raw menu imports. Only explicit external href literals bypass the
internal adapters; dynamic targets require the typed navigation boundary.
Positive and negative compiler fixtures include templates,
reusable control props, extra keys in variables and wrong destination queries.
A scratch copy proves route rename, missing root view/example, and the original
`/blocked` link fail mandatory compiler checks. Fixtures are excluded only from
the normal compiler/formatter: their own compiler asserts every intended error
and rejects unexpected errors; valid examples compile in the same project.

The SPA server consumes Vite's contract-derived `navigation.json` to serve known
route shapes containing dotted/escaped invalid IDs to the frontend. It preserves
asset and development-module handling. Runtime rejection never mounts a feature
from partly parsed state. No diagnostics include the incoming path or query.

## Migration coverage

All entry-point families in the inventory above are migrated, including
notification callbacks, startup/onboarding, route-bearing shared controls,
settings aliases and the album edit query's consumed latch. Search pushes and
refinement/media replacements retain existing history semantics; detail routes
inherit their declared sidebar parent. Overlays and original media are unchanged.
No Go-to-TypeScript service or wire type changes are needed.

The verification snapshot below covers the migrated application and enforcement.
Existing feature owners and external acceptance remain distinct from this
refactor's native coverage.

The input boundary awaits PIN-session closure before app
health/library initialisation. A browser reload does not run Vue's old Blocked
unmount hook; a new frontend lifetime therefore closes that prior session using
the existing bridge queue and service gate. A mounted regression fails without
the close and verifies no initial page load starts until it settles. Native
acceptance unlocks media, hard-loads another destination and checks
both service denial and revocation of the previously served thumbnail URL.
Malformed-input tracing observes Timeline, Days, Selection and search reads,
so an empty DOM alone cannot mask an unrelated whole-library request.


## Verification snapshot

Code head `91f54d3b` passes [Windows run 42133839534](https://github.com/awaisjameel/lumen/actions/runs/42133839534):
production build, current bindings/modules, full static checks, 1,147 frontend
tests in 118 files, every named-package Go race shard and installer packaging.
The installer is unsigned under the existing build policy; no release,
clean-machine installation or production signing is inferred.

Artifact `13466405989` retains all 267 native checks: navigation 74/74,
collection recovery 51/51, drafts 26/26, places 22/22, previews 27/27, tagging
18/18, Undo recovery 20/20, history 7/7 and favourite Undo 22/22. Every one of
the 21 production destinations is visited independently through real controls.
Onboarding/startup, history, aliases, malformed/stale input, themes/drawer,
PIN unlock/unblocking, navigation-away and hard-reload service/media relocking
pass. There are no page exceptions, unexpected console errors, unclassified
runtime refusals or external requests.

The reports classify expected HTTP responses by method and structured metadata:
cancelled Timeline/Search reads, the deliberate missing-person fixture and
explicit locked-service probes. Optional Wails-script and protected-thumbnail
responses have exact resource/status expectations. Arguments and PINs are never
recorded. Fixture-driven reloads await the rendered page and settled requests;
all error assertions and interaction deadlines remain intact. Late coded
visibility refusals are cancellation only within Wails' cancelled-read wrapper;
active failures and arbitrary late failures remain reportable.

Enforcement covers 22 AST cases, 18 compiled negative TypeScript/Vue examples
with valid counterparts and three required compiler mutations. Before-repair
regressions reproduce whole-library fallback, date-entry failure, missing
frontend-lifetime PIN closure, late cancelled visibility refusal and opaque/
relative href bypasses. Native narrow-layout and startup screenshots were
inspected in addition to the automated assertions.

The hosted latency job reports combined-block SetEventPinned 15.374/5 ms and
unblocked SaveEmbeddings64 127.695/70 ms. Budgets are unchanged; configured-machine
performance acceptance remains open in #31. This code snapshot does not certify
a later change: the final PR head must pass its own required checks before merge.
