# Application commands

This records the pre-implementation inventory, accepted scope and command contract for
[#46](https://github.com/awaisjameel/lumen/issues/46), against integration parent
`70f186e1`. Implementation and its child Windows acceptance record are in [PR #55](https://github.com/awaisjameel/lumen/pull/55),
merged into the parent at [`e09f966`](https://github.com/awaisjameel/lumen/commit/e09f96639a67011b4743be958dd943d6440fea3c).
The integrated change is under owner review in [PR #56](https://github.com/awaisjameel/lumen/pull/56);
its [audit ledger](pr56-audit.md) distinguishes current validation from historical child results.

## Current inventory and confirmation

The current registry contains 95 literal declarations, two compression-factory
identities and eleven generated navigation identities: 108 static identities in
total. Twenty-four item action families cover albums, Things, People, places,
events, blocks, organise runs and duplicate review. Duplicate review includes
the duplicate kind and first asset ID. Navigation command identities come from
the navigation entries; notification IDs and widget row IDs are not commands.
There are 31 curated shortcut entries and 173 production consumer references.
The source inventory uses TypeScript/Vue ASTs, rather than matching ID-looking
strings indiscriminately.

The isolated before fixture compiles misspelt/unknown declarations and consumers
without error, while an unknown runtime identity resolves to false. Regressions
also confirm that re-registering one binding object lets an older disposer
remove its replacement, and that a pending callback admits duplicate activation.
The same rejected async callback takes different paths through a real selection
button, menu, palette and keyboard. This is confirmation of the contract gap,
not a claim that an existing production ID is misspelt or global error handling
is absent.

## Accepted ownership and execution policy

Keep the existing command registry, shortcut settings/help, preferences,
notifications, overlays and feature action owners. One shared catalogue
owns identities, default presentation, groups, zones, keys and repeat policy.
Types and shortcut/help entries derive from those executable definitions.
Features bind callbacks and reactive availability, reasons and context labels;
they do not repeat default shortcut metadata. Public command reads must not
expose raw callbacks for entry points to bypass the runner.

Static identities remain stable strings. Item actions use closed typed
constructors, validated positive safe IDs and duplicate kinds, with one stable
key codec preserving existing UI identity strings. Raw palette values and
stored overrides are explicit validated boundaries, rather than permissive
execution overloads. Navigation identity derives from #38's vocabulary; the
shell retains navigation presentation and contextual availability.

Each registration has its own ownership token. Disposal removes only that
registration; item menus resolve the current owner at activation. In-flight
state belongs to a registration, so an old completion cannot clear a newer
owner's pending work. Admission checks current availability and enabled state.
Non-repeatable work admits one invocation; explicitly repeatable navigation/
view controls remain responsive. Commands have independent admission, with no
global execution queue. Available commands own their matching keyboard event
while disabled or pending, preventing a browser fallback; unavailable contexts
do not own the event.

The runner handles completion and failures consistently for buttons, menus,
palette and keyboard. Cancelled work is quiet; coded errors use the existing
localised reporter; unexpected errors use the existing unexpected-error owner.
Features retain their intentional form-level/handled outcomes and domain write
queues. Invalid identities are developer errors, distinct from a valid command
whose contextual owner is absent. Diagnostics do not include item names, paths,
queries or PINs.

## Stored shortcuts and compatibility

Go still owns persisted preferences, format validation, the bound and reset/
import semantics. Preserve stored IDs and values. Known static overrides,
including previously imported commands outside the curated editor, remain
active; the fixed viewer Escape remains fixed. Unknown legacy keys round-trip
without becoming executable commands. They count towards Go's storage bound;
Reset all remains the explicit way to clear the entire map.

Shortcut defaults, alternate keys, zones and curated help labels derive from
the catalogue. Reassignment removes alternate keys as before. Conflicts include
known imported overrides outside the editor and use the same chord semantics
as dispatch, including symbol Shift handling. No source-format-dependent
shortcut consistency parser remains in the delivered contract.

## Required verification

Record before/after runtime and compiler evidence. Compile actual valid/invalid
TypeScript/Vue consumers, including malformed item identities and reusable
controls. Cover pending/repeatable actions, each failure path, registration
replacement/disposal, stale item menus, overrides/aliases/conflicts and reset/
import compatibility. Exercise real native controls, reassignment, focus,
overlays, failure/retry and persistence in a scratch library. Preserve all
existing native suites, generated contracts, originals, corrections and PIN/
visibility rules. Run fresh frontend/static, named Go race, production/package
checks and record unchanged latency gates separately from external acceptance.

## Extending the contract

Add a static definition to `shared/commandContract.ts`, then bind its callback
with `useCommands` in the existing feature owner. A `shortcut` definition also
opts it into the curated settings/help list. Keep default labels free of
placeholders; bindings retain selection-dependent labels and reasons. An item
action needs a closed family definition and `itemCommand(name, positiveID)`;
duplicate review uses `duplicateCommand(kind, positiveID)`. Do not persist item
identities or accept arbitrary strings in execution APIs.

Controls use `CommandButton` or `IconButton`'s `command` prop. Existing explicit
labels, toggle tips, slots and styling can remain contextual. Selection docks
and menus read registry snapshots. The palette validates its untrusted row key
with `commandIdFromKey`; no entry point calls a raw binding callback. Current
availability and enabled predicates are checked again at execution. A boolean
runner result means the callback was admitted and resolved, rather than proving
a long job or a feature-handled form outcome succeeded.

Use the registration disposer only for imperative owners; composition owners
use `useCommands` or `useItemCommands`. Reactive item sources retire old bindings
synchronously. Only deliberately repeatable view controls opt into `repeatable`;
other async callbacks remain pending until settled. Feature write queues remain
the authority for ordering durable edits.

## Verification checkpoint

The eight policy regressions fail against the integration parent and pass with
the shared runner. The isolated misspelling fixture compiles against the parent;
normal checks now require 21 invalid TypeScript/Vue examples to be rejected and
valid counterparts to compile. The shared compiler-fixture checker also retains
#38's 18 navigation rejections. No cast, suppression or permissive ID overload
was added. Runtime fixtures reject invalid IDs and check every family codec.

Focused checks cover pending work, repeatable and held keys, synchronous re-entry,
old completion versus a replacement owner, stale menus before the next render,
handled/unexpected/cancelled failures and retry, real controls, late labels,
defaults/alternates/conflicts and imported overrides. Go and TypeScript share the
stored-chord validation fixtures; Go's existing Windows path assumptions prevent
a complete core-package pass on Linux, while the affected shortcut race tests
pass. The native `commands` suite joins all nine existing regression suites in
Windows CI and preserves its reports/screenshots. Native/production/package
results are recorded in [the PR checks](https://github.com/awaisjameel/lumen/pull/55/checks) and must pass before merge; a syntax check supplies
none of those results. Milestone latency and external release acceptance remain
separate gates in the feature audit.

Local checkpoint on 6 November 2026: all 1,167 frontend tests in 121 files pass;
the final migrated callers pass 173 focused tests, and the final album-submit/
control/preferences check passes 56. Type checking, zero-warning ESLint, formatting,
21 command/18 navigation compiler rejections, all three navigation mutation checks,
production frontend build, documentation/screenshot integrity, affected Go shortcut
race tests and core static checks pass. The existing bundle-size advisory remains.
Generated bindings and Go manifests are unchanged. These local results do not establish native or installer acceptance; the current
Windows record is the linked PR checks and its uploaded native reports.

The first Windows run, [72](https://github.com/awaisjameel/lumen/actions/runs/42222154897)
at `c79ba23`, passes production/current bindings, static checks, 1,168 frontend
tests and all three Go race shards. Eight native suites pass; collection recovery
passes 50/51 because a deliberate reload aborts its count/place reads, and the
new suite stops on Puppeteer's unsupported combined-key spelling. These failures
are retained as failed evidence. The harness now sends separate modifier-down/
key/modifier-up events and settles the corrected header and reads before reload;
fetch errors are not suppressed. Installer packaging is skipped by that failed
run. Follow-up native acceptance and packaging are required in the PR checks.

Two additional control regressions confirm and prevent an extra catalogue icon
beside content supplied by a custom default slot. Explicit icon props retain
their meaning. Both controls, Person-header aliases and runner policies pass
29 focused checks after the fix; the current Windows run verifies the final
control implementation with the full suite. Shortcut help acceptance checks the
reassigned action's own key caps, rather than unrelated text elsewhere in help.

Run [73](https://github.com/awaisjameel/lumen/actions/runs/42226724358) at `532f6e2`
passes all 1,170 frontend tests, static/build/contracts and all Go race shards.
Collection recovery now passes 51/51. Native verification remains failed: the
new pointer fixture leaves the palette open, and an Undo fixture selects the
wrong photo while privacy recovery moves virtual rows. Pointer fixtures now
activate the WebView and settle reads before taking coordinates; the new suite
retains click-target diagnostics. No assertion or product error is suppressed.

A separate mounted regression confirms that viewer adjustment bindings used
busy state as availability. The shared feature guard now keeps rotate, flip and
reset available in the photo context while disabling them during the save, with
one reason. This preserves keyboard ownership for a reassigned Ctrl+R while
pending and disables peer adjustments without adding a global queue. The test
fails before the repair; all 67 viewer/policy checks, type checking and focused
lint pass afterwards. Current native/package acceptance is the linked PR record.

The early command report from [run 75](https://github.com/awaisjameel/lumen/actions/runs/42231644706)
confirms thirteen native checks, including palette pointer activation, shortcut
reassignment/conflicts/help, held zoom, independent pending admission, failure/
retry, Escape hierarchy and focus restoration. It stops at the fixture's call
to a nonexistent singular Album method, after the real edit has saved. The
fixture now reads the existing Albums contract; its literal method names and
arities were checked against generated bindings. The planned aborted request
must produce exactly one existing reporter diagnostic. Only that exact message,
phase and source, its one transport resource failure, and the optional Wails
custom script's 404 are expected; all other console/page errors still fail.

The follow-up local browser check confirms that a disposed item owner disappears
from the real palette. It also exposes a retained footer highlight: an empty
search can leave Run enabled for a command excluded by the query. A mounted
regression fails before the repair; the footer now requires the current command
to match the current query. Native acceptance asserts this alongside the exact
open palette's query, empty state and item disposal, with a screenshot and result
diagnostics. Closing portals are excluded from these observations. Current
Windows acceptance remains the linked PR record.
