# Conventions

Every change to Lumen follows these rules. They are the patterns the
codebase already uses most, first written down in the October 2026
hardening pass. Where two patterns compete, the rule below picks one and
says so. The README covers layout and tooling; this file covers how code is
written.

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
- **Commit messages** are Conventional Commits, `type(scope): summary`
  (feat, fix, perf, refactor, docs, test, build, ci, chore, style or
  revert), with a body that says why. No message credits or mentions an AI
  assistant. `scripts/check-commit-msg.sh` checks both, as the commit-msg
  hook and in CI.
- **One source of truth per rule.** Before writing a helper, search for one.
  If two copies exist, keep the one in the lower layer (`core` before
  `store` before `services`; `shared` before a feature folder) and delete
  the other.
- **Scope.** Each phase is built as its record in `docs/history/` sets out
  ([the index](docs/history/README.md) names the record that applies to
  each phase; a later record corrects an earlier one, as the Phase 11 audit
  corrects the Phase 11 record). Phase 6's decisions (the installer,
  politeness controls, crash reports and diagnostics, onboarding and empty
  states) are not reopened except by an explicit product-owner decision.
  Same-format compression replacement (11E) is deferred. Public update
  delivery (Phase 12) stays disabled until compiled trust and signing
  policy are provisioned ([production signing](docs/production-signing.md)).
- **No network.** The one authorised exception is the updater planned in
  [the automatic update plan](docs/history/automatic-updates-plan-2026-10-04.md):
  opt-in checking and explicitly requested authenticated downloads. No
  other feature may add a network request or upload library data.

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
  - The whole-library counts (timeline days, Things, people, job progress)
    are kept in tables by triggers in the transaction that changes them
    (migration 0037); the query each replaced stays as its recount and its
    test's reference. A new input to one needs its trigger, and a statement
    that can change the visibility of an unbounded number of assets calls
    `markKeptCountsForChange` first, so they are recounted once instead.
  - Names and paths compare with `fold()` and `foldKey`, never `NOCASE`. A
    list of names the user reads sorts by `byName` (the collation
    `name_order`, `core.CompareNames`), never by bytes or `NOCASE`; only
    queries name the collation, never the schema.
- **Logging** uses `log/slog`. Messages are lower case and say what failed
  ("tidy thumbnail cache"), and the error goes in the `"err"` key. Never log
  a PIN, a hash of one, or a file's contents. Background failures are logged.
  User-triggered ones are returned.
- **Versions.** A change to what a stage produces bumps that stage's version
  constant, with an upgrade rule if old output can stand.
- **Formats.** An extension is recognised in one place, the catalogue in
  `internal/core/formats.go`. Code that treats a format specially asks for
  its `core.Family`, never compares extensions; which decoder reads a family
  is `internal/media`'s choice (`Decoders.open`). A format is listed only
  with fixtures that prove what Lumen does with it.
- **Tests**
  - Tests are named as a sentence about behaviour
    (`TestTagByNameRefusesABlockedThing`).
  - Use the package's existing helpers (`openTest` in store, and so on) and
    `t.TempDir`. Table tests use `t.Run` only when the cases are independent.
  - Real-model tests use `testenv.Runtime`, which skips cleanly without
    `deps/runtime`.
  - Pass gate: `go test -race -count=1 -timeout 900s` on the named packages
    (never `./...`), and `golangci-lint run` reports 0 issues.
  - Large-library latency is also a gate: `scripts/lint.sh` runs
    `scripts/bench.sh` after the Go static checks and still runs every later
    step if it fails. Run it alone for the 100,000-photo store fixture in three
    block configurations, without the race detector; it sources `scripts/env.sh`
    and takes one to two minutes on an i7-13700H with a warm toolchain.
    Budgets live in `internal/store/testdata/library-budgets.txt`. Raise one
    only by hand, with a reason and at least five repeat measurements in review,
    applying that file's headroom rule. Do not raise one just to pass a check.

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
- **Exported service signatures are a contract.** Changing one changes the
  bindings: regenerate them in the same change and review
  `git diff -- frontend/bindings`.

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
  - Interface preferences (appearance, layout, viewing, playback, shortcuts,
    export presets): `shared/preferences.ts`, saved in the library's settings
    through the bridge (`latestSave` ordering). Browser storage holds only
    the derived first-paint cache. A new preference is a field of
    `core.Preferences` (or `core.ViewerPreferences`) with a default, a rule
    in `Normalised` and a reset category (`Preferences.Reset`); one that
    changes how the page is drawn becomes an attribute on the root
    (`design/appearance.ts`), which the tokens read.
  - Shortcuts: a command declares its own keys; the user's are an overlay
    read through `keysOf`. A command worth a shortcut is listed in
    `shared/shortcuts.ts`, which a test holds to what the views declare.
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
