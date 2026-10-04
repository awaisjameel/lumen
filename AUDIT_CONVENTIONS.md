# Audit conventions

Every change in the October 2026 hardening pass follows these rules. They
are the patterns the codebase already uses most. Where two patterns
compete, the rule below picks one and says so. The README covers layout and
tooling; this file covers how code is written.

## General

- **Preserve behaviour.** A behaviour change needs a stated reason and a
  regression test that fails without it.
- **Never weaken a check to pass it.** No deleted or loosened tests, no new
  `//nolint`, `eslint-disable`, `@ts-expect-error` or `as` cast, unless
  the line says why and nothing cleaner works. Existing rules and configs
  stay as they are.
- **Comments say why, in plain full sentences and British English** (the
  linter checks UK spelling). Match the density of the surrounding code. No
  commented-out code, no `TODO` without an owner or a plan, no debug output.
- **One source of truth per rule.** Before writing a helper, search for one.
  If two copies exist, keep the one in the lower layer (`core` before
  `store` before `services`; `shared` before a feature folder) and delete
  the other.
- **Scope.** Phase 5 is built as `docs/phase5-design-2026-10-01.md` sets
  out. Phase 7 (the Nuxt UI frontend and design system) is built as
  `docs/phase7-design-2026-10-04.md` records. Phase 6 (the installer, politeness controls, crash reports and
  diagnostics, onboarding and empty states, the performance pass) is built
  as `docs/phase6-design-2026-10-03.md` sets out; its decisions are not
  reopened except by a later explicit product-owner decision. Lumen currently
  makes no network request. The owner's 4 October request authorises the
  planned update-only exception in
  [the automatic update plan](docs/automatic-updates-plan-2026-10-04.md):
  opt-in checking and explicitly requested authenticated downloads. Other
  features must not add network requests or upload library data.

## Go

- **Packages** keep the jobs the README's layout gives them. Only `store`
  writes SQL. `ai` never imports `store`. `services` stay thin: argument
  checks, error codes, calls into `internal/*`.
- **Interfaces** are declared by the package that consumes them, and only
  when there are two implementations or a test needs a fake (for example
  `index.Events`).
- **Errors**
  - Failures the UI explains are sentinel values (`var ErrX = errors.New(...)`)
    in the package that detects them, compared with `errors.Is`.
  - Wrap with `fmt.Errorf("doing what: %w", err)` when the error crosses a
    package boundary, or when the call site adds something the caller can't
    know (which file, which stage). Inside a package's own helper chain, a
    bare `return err` is fine. This is the choice where the code differs today;
    don't wrap the same error twice.
  - Never swallow an error. Best-effort cleanup is discarded explicitly
    (`_ = f.Close()`) with a comment if the reason isn't obvious. Nothing
    panics in normal flow.
- **Context.** Every function that does I/O, SQL or model work takes
  `ctx context.Context` first and passes it on. Use `QueryContext` and
  `ExecContext`, never the plain forms. Goroutines are tied to a context or a
  `Stop`, and `Stop` waits for them.
- **State.** No mutable package-level state. Package-level `var`s are
  constants computed once (`sync.OnceValue`, lookup tables, SQL fragments). The
  exception is native-library state that is process-wide by nature (`ai`'s
  GPU lock, which the models take turns on). Shared fields have a mutex or atomic beside
  them, and a comment names what it guards.
- **SQLite**
  - Write transactions go through `Store.tx`, and multi-query reads go
    through `Store.read`.
  - Parameters are always bound. Concatenation is allowed only for constant
    fragments.
  - Close `rows` with a deferred close and check `rows.Err()`.
  - Batch writes; never write row by row in a loop.
  - Schema changes are new numbered files in `internal/store/migrations`.
    Never edit an applied migration, apart from the whitespace already fixed.
  - Visibility goes through `visible`, `labelBlocked` and `blockVisible`.
    A new input to those needs a trigger that bumps `visible_value`.
  - Names and paths compare with `fold()` and `foldKey`, never `NOCASE`.
- **Logging** uses `log/slog`. Messages are lower case and say what failed
  ("tidy thumbnail cache"), and the error goes in the `"err"` key. Never log
  a PIN, a hash of one, or a file's contents. Background failures are logged.
  User-triggered ones are returned.
- **Versions.** A change to what a stage produces bumps that stage's version
  constant, with an upgrade rule if old output can stand.
- **Tests**
  - Tests are named as a sentence about behaviour
    (`TestTagByNameRefusesABlockedThing`).
  - Use the package's existing helpers (`openTest` in store, and so on) and
    `t.TempDir`. Table tests use `t.Run` only when the cases are independent.
  - Real-model tests use `testenv.Runtime`, which skips cleanly without
    `deps/runtime`.
  - Pass gate: `go test -race -count=1 -timeout 900s` on the named packages
    (never `./...`), and `golangci-lint run` reports 0 issues.

## Go → TypeScript contract

- **Types.** Wire types live in `internal/core`. Wails generates
  `frontend/bindings`, which is never edited by hand. Regenerate with
  `wails3 generate bindings -f '-tags production' -clean=true -ts -i`.
- **The bridge.** `frontend/src/bridge/index.ts` is the only importer of
  bindings and of `@wailsio/runtime` (ESLint enforces it). It re-exports the
  generated types. Where Go sends `null` for an empty list, the bridge
  normalises it and narrows the type with `NonNullLists`. No other file
  declares a type that shadows a generated one.
- **Mirrored values** that bindings don't carry (constants, error codes,
  event names) are declared once in the bridge, each with a `// core.X`
  comment, and checked against Go by `services/contract_test.go`. A new
  mirror needs a new line in that test.
- **Errors.** The chain is: a store or service sentinel, then an entry in
  `services.errorCodes`, then `ErrorCode` in the bridge, then `codeText` in
  `shared/errors.ts`, then an i18n key. Every service is registered through
  `main.go`'s `service()`.
- **Events.** Event names are `services.Event*` constants registered with
  `application.RegisterEvent[T]`. The bridge subscribes as
  `'name' satisfies keyof Events.CustomEvents` and exposes one `on*` function
  per event that returns its unsubscribe function.
- **Exported service signatures are frozen for sub-agents.** Changing one
  changes the bindings, so it is the lead's job.

## TypeScript and Vue

- `strict` and `noUncheckedIndexedAccess` are on. No `any`, no non-null `!`
  without a proving check, and no `as` except `as const` and import renames.
  Narrow with type guards instead (`errorCode`, for example).
- Components use `<script setup lang="ts">` with typed `defineProps<…>()`,
  `defineEmits<…>()` and `defineModel<…>()`. The blocks go in this order:
  script, template, style.
- **Feature folders** (`library`, `people` and the rest) own their views
  and their store. Code used by two features moves to `shared/`.
- **Layers.** `design/` (tokens, the icon manifest, how the appearance is
  applied) sits below everything but the bridge's types. `shared/` holds
  the compositions and registries features build on and never imports a
  feature or the shell. Features never import `app/`; the shell (`app/`)
  composes features, which reach it through shared registries (commands,
  notifications, overlays). ESLint enforces each direction.
- **Design system**
  - Controls are Nuxt UI's, imported by name
    (`import UButton from '@nuxt/ui/components/Button.vue'`); auto-import is
    off, so a template uses only what its script imports. Use a library
    control directly when it meets the need; add a Lumen component only for
    shared product behaviour or a recurring composition (`Page`,
    `PageHeader`, `FeatureState`, `CollectionCard`, `MediaTile`,
    `SelectionBar`, `MetaRow`, `PrefRow`, `WorkProgress`). Virtual grids, the
    viewer and timelines stay purpose-built.
  - Colour, spacing, density, motion and layers come from the tokens in
    `design/tokens.css` (Nuxt UI's `--ui-*` and Lumen's `--lumen-*`), by
    utility class or scoped CSS. No literal colours outside the tokens,
    except the viewer's fixed dark palette and photo badges.
  - Icons are names from `design/icons.ts`, shown with `Icon` or passed as
    `icons[name]`. A new icon is a new manifest line; nothing is fetched.
  - Every list has loading, empty and failure states (`FeatureState`); a
    state is never shown by colour, shadow or motion alone, so reduced
    motion and forced colours lose nothing.
- **One owner each**
  - Appearance and layout preferences: `shared/preferences.ts`, saved in the
    library's settings through the bridge (`latestSave` ordering). Browser
    storage holds only the derived first-paint cache.
  - Notifications: `notify()` in `shared/notify.ts`, shown as toasts by the
    shell. No component draws its own notice bar.
  - Overlays: Nuxt UI's modal, slideover, menu and popover (focus trap,
    Escape and focus return are theirs). A full-window layer of Lumen's own
    registers with `useLayer` and handles a key only when it `owns()`; page
    shortcuts check `shortcutsSuspended()`.
  - Commands: an action offered in more than one place (toolbar, menu,
    palette, shortcut) is one `Command` registered with `useCommands`; its
    availability is computed, and the engine still validates every call.
- **Pinia**
  - Only setup stores (`defineStore(id, () => …)`).
  - A list from the engine uses `defineListStore` or `defineCollectionStore`
    from `shared/listStore`. A list one view reads alone uses `useList`.
  - Use the store object (`lib.x`). Destructure it only through
    `storeToRefs`.
  - Don't keep state that can be derived: use `computed`.
  - Async actions expose `loading` and `error`, or report as below.
- **Errors in the UI**
  - The user did something and the view has a place to say why: put
    `errorText(e)` in that view's own error ref.
  - The user did something and there is no such place: `reportError(e)`.
  - Background work: `logError(e)` or `orFallback`.
  - Cancelled reads are dropped with `isCancelled`.
  - No other `console.*` calls.
- **Text** comes only from `t('key', params)`, so placeholders are typed. Add
  keys to `shared/i18n.ts`, and remove the keys you make unused.
- **Reactivity**
  - Use `shallowRef` for large engine payloads.
  - Watchers and listeners are disposed: `onBeforeUnmount`, or the
  unsubscribe function `on*` returns.
  - `v-for` keys are stable ids, never indexes.
  - Large lists are virtualized (TanStack Virtual).
- **Tests** are Vitest with `@vue/test-utils`. They mock the bridge with
  `vi.mock('../bridge', async (importOriginal) => ({ ...await importOriginal(), engine: {...} }))`,
  are named for behaviour, and sit beside the file they test
  (`X.test.ts`). A component built on Nuxt UI is mounted with `mountInApp`
  (`shared/testing.ts`), which supplies the app's providers; overlay
  content is portalled, so it is found in `document.body`. Notifications
  are asserted with `recordNotifications()`. Tests find elements by role,
  label or a deliberate `data-*` hook, not by styling classes.
- **Pass gate:** `npm run check` (vue-tsc, ESLint with zero warnings,
  Prettier) and `npm run test`. `scripts/lint.sh` runs everything.
