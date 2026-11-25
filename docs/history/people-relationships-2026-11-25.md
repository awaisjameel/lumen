# People: Me and relationships (25 November 2026)

This records the design, parity review and acceptance evidence for
[issue #30](https://github.com/awaisjameel/lumen/issues/30): an optional **Me**
identity and relationship labels in People, searchable as `me` and `my mum`.
How to use it is in [the walkthrough](../using-lumen.md#me-and-relationships);
this record describes the code on its date.

## Google Photos parity

The reference was re-read on 25 November 2026: [Ask Photos onboarding](https://support.google.com/photos/answer/15318661?co=GENIE.Platform%3DAndroid&hl=en),
[face groups](https://support.google.com/photos/answer/6128838?hl=en) and
[labelling your face group](https://support.google.com/photos/answer/7378566?hl=en).
The pages name example relationships ("Mom, Friend, Spouse, or Sibling"), allow
your own word ("More options"), say one face group is "labeled as Me", and keep
labels private to the account. They give no full list, cardinality rule or
search grammar; Lumen's choices for those are below.

| Google Photos flow | Lumen | Difference and reason |
| --- | --- | --- |
| Confirm which face group is yours (onboarding), or **Choose** under "No face labeled as Me" | **Which one is you?** on People offers the six most photographed people; **This is me** in any person's actions | Desktop flow without a wizard. |
| Skip the confirmation | **Not now** hides the prompt until Lumen restarts; **This is me** stays available | Skipping never blocks anything. |
| Change the Me face group | **This is me** on another person asks first, naming the current Me and any relationship that goes | The confirmation is explicit because Me has no relationship. |
| **Remove label** for Me | **This isn't me** | Relationships stay: they are relative to you, not to a face group. |
| Optional relationship (Mom, Friend, Spouse, Sibling) beside a name | **Add a relationship** beside the name: 23 built-in relationships, searchable, or your own word up to 40 characters | British English labels (Mum, Grandad). Name and relationship are separate fields. |
| Review and confirm labels before saving | Each change is saved with **Save relationship** or by choosing an option; **Cancel** and `Escape` discard it; a notification confirms it | One person at a time rather than a batch review. |
| Edit and remove names | Unchanged; removing a name keeps the relationship | |
| Search "me" | `me`, `myself` | |
| Ask Photos understands relationships | `my mum`, `my parents`, `my friends`, `my coach`: a relationship needs `my` | `mum` alone may be a description or someone's name. |
| Merge face groups (Same, Different) | **Same person** as before; if their relationships differ, Lumen asks which to keep | No silent loss. |
| Labels private; not shared with photos | Kept in the local library only; never uploaded, and not written to XMP sidecars | Sidecars carry names only. |
| Hide or block face groups | Existing Hide/Block; a hidden person can't be chosen, and `me` or `my …` never finds or names them | |
| Turning off face groups deletes labels | Not offered | Lumen keeps the user's corrections. |

## Model

- `people.is_me`, `people.relation` and `people.relation_label` (migration
  `0041_relationships.sql`). A unique partial index allows one Me; a partial
  index over Me and related people keeps the search lookup to those few rows.
  Metadata lives on the person, so it is deleted with them and follows merges;
  it never depends on face IDs, which re-analysis replaces.
- `core.Relation` lists the built-in relationships and `RelationCustom`.
  `core.CleanRelation` is the one validation: a label is for a custom
  relationship only, whitespace is collapsed, a leading "my " is dropped, 1 to
  40 characters remain, and a built-in word (`mom`, `Mummy`) becomes that
  relationship. `me`, `myself`, `my`, `i` and group words such as `family` are
  refused.
- Cardinality: one Me; any number of people may share a relationship; Me has
  none.

### Changes to who is Me and what someone is

| Event | Behaviour |
| --- | --- |
| Set Me while someone else is Me, or on a person with a relationship | `relation_conflict`; the UI names the current Me and the relationship that goes, then retries with `replace`. A hidden Me is replaced without being named. |
| Remove Me | Relationships stay. |
| Relationship on Me | Refused (`relation_conflict`); the UI disables it with a reason. |
| Rename, remove a name, split faces, new face indexing, restart | Unchanged: nothing of these touches the columns. A person with Me or a relationship and no face or name is kept, as a named one is. |
| Merge (user) | Me moves to the merged person. A shared or one-sided relationship stays. Different relationships, or Me with a relationship, are `relation_conflict` until the user chooses (`core.MergeKeep`). |
| Clustering | A person with Me or a relationship counts as confirmed: never merged away automatically. |
| Block or hide | The person can't be chosen or labelled (`not_found`); search treats them as blocked. |

## Search

`store.SearchNames` adds, only when the query has the word `me`, `myself` or
`my`, the phrases of the user and related people (`core.RelatedPerson.Targets`),
read through `people_related` in the same snapshot as the names.
`people.ParseQuery` matches whole words, longest first, and groups targets of
the same phrase:

- A part names a **group**: a photo matches when anyone of it is in the photo,
  and every part must match (`store.PeopleAssets`).
- A name or singular relationship that names several people is **ambiguous**:
  photos of any of them are shown, the summary names them and offers
  **Only Sara** for named ones. A person named `My Mum` beside someone set as
  Mum is ambiguous the same way. Nothing is chosen for the user.
- A phrase no one has (`my dad` before anyone is Dad) stays in the description
  and the summary says **No one is set as “my dad” yet**.
- A blocked name blocks the search, as before. A relationship blocks it only
  when everyone it names is blocked; otherwise blocked members are left out
  without a trace. Recent searches use the same rule.

## Interface

- People cards show **Me** or the relationship under the name; the filter
  matches relationships as well as names.
- A person's page shows **Me** or the relationship beside the name as a button
  that opens the editor; the actions menu and the command palette carry
  **This is me** / **This isn't me** and **Add / Change the relationship**.
- The editor is an ARIA combobox with a listbox (`aria-activedescendant`,
  arrow keys, `Enter`, `Escape`), keeps focus and returns it to the control
  that opened it, shows validation and engine errors inline, and needs no
  motion. Forced colours keep its borders.
- A merge or Me conflict uses the app's one modal question, extended with a
  second choice for "Keep Friend" or "Keep Coach".

## Acceptance evidence

Measurements and native runs are listed in [Verification](#verification).

## Verification

Measured on one Windows 11 desktop on 25 November 2026: `main` at `562ba2f`
against this branch, each build in its own scratch library.

### Large-library latency

The 100,000-photo store benchmark: five clean runs of each build's prebuilt
test binary, interleaved, without the race detector. Medians in
milliseconds, with the range across runs.

| Call | Blocks | `main` | Branch |
| --- | --- | --- | --- |
| People | none | 6.76 (5.88–10.00) | 6.14 (5.35–22.04) |
| People | folder | 6.29 (5.80–6.65) | 6.25 (5.81–6.78) |
| People | folder, tag, person | 5.62 (5.19–7.95) | 5.59 (5.20–6.00) |
| PersonFacts | none | 0.56 (0.48–1.09) | 0.64 (0.51–1.95) |
| PersonFacts | folder, tag, person | 0.55 (0.52–0.79) | 0.54 (0.51–0.67) |
| TopPlacesPerson | none | 0.79 (0.65–1.30) | 0.97 (0.60–2.32) |
| TopPlacesPerson | folder, tag, person | 0.74 (0.70–0.97) | 0.72 (0.69–0.79) |
| ClusterData | none | 138.63 (129.49–583.48) | 138.81 (128.77–402.02) |
| Ingest128 | none | 17.01 (15.85–28.86) | 17.85 (15.47–25.55) |
| SaveFaces64 | none | 4.45 (3.34–8.21) | 4.06 (3.71–17.66) |
| SearchNamesRelated (`my friend`) | none / folder / all three | | 0.41 / 0.32 / 0.25 |
| PeopleAssetsRelated (Me with any of 40 friends) | none / folder / all three | | 9.81 / 10.64 / 9.92 |
| SetRelation | none / folder / all three | | 0.65 / 0.60 / 0.51 |

Every existing call stays within its run-to-run range. The seed gains a Me,
40 friends, a Mum and 8 people with a custom label; the three new calls have
budgets in `library-budgets.txt`, and no existing budget was raised. On this
machine some existing budgets (Things among them) are exceeded by `main` as
well as by this branch: that is the machine, not this change.

### The app

| Measurement | `main` | Branch |
| --- | --- | --- |
| Indexing the 600-item library fixture with AI, three alternating runs | 55, 57, 50 s | 52, 51, 52 s |
| Cold start to a usable grid, median of 8 alternating launches | 1,189 ms (940–1,348) | 1,101 ms (953–1,352) |
| Idle working set, median of 3 | 497 MB (496–497) | 507 MB (496–527) |
| Idle private memory, median of 3 | 292 MB (290–295) | 299 MB (289–321) |

Start-up reads nothing new: relationships are read only when a search says
`me`, `myself` or `my`. The memory ranges overlap.

### Native end to end

`node e2e/run.mjs people-relationships` against the production build, in
WebView2, passed 26 of 26 checks. The suite:

- sets Me from the People prompt;
- adds Mum by keyboard alone, checking focus and `aria-activedescendant`, and
  adds a custom **Coach** by typing;
- opens the editor in an 800-pixel window with the dark theme, forced colours
  and reduced motion;
- searches `my mum`, `me`, `my friends`, an ambiguous `my friend` (then
  **Only Omar**) and an unassigned `my dad at the beach`;
- merges a Friend into a Coach, keeping Friend, and declines replacing Me;
- restarts and finds every choice kept;
- hides Mum, then checks that `my mum` is refused without naming her and that
  a direct call can't relabel her.

It made no external request and raised no unexpected frontend error.
`node e2e/docs/capture.mjs` recaptured `faces.webp` and captured
`relationships.webp`.

### Checks

- `go test -race -p 1 -count=1 -timeout 1800s . ./internal/... ./services/... ./cmd/... ./scripts/...`
  passes. The upgrade tests' version-18 fixture now drops migration 0041's
  indexes and columns too.
- `scripts/lint.sh --no-bench` is clean: golangci-lint, deadcode,
  govulncheck, `npm audit`, vue-tsc, ESLint, Prettier, Vitest (123 files,
  1,202 tests), the documentation check, shellcheck, actionlint and zizmor.
- `wails3 generate bindings` was rerun after the service changes.
  `wails3 build` produces the production build that the native suite and the
  screenshots ran against.
- The latency gate: in every run, the three new calls stayed far inside
  their budgets, and no existing call moved outside its range on `main`. The
  gate as a whole fails on this machine for `main` as well, as
  [Large-library latency](#large-library-latency) explains.
