# Phase 10: customisation and preference UX

Implemented locally on 8 October 2026 on `phase10/customisation`, from merged
main `9ed0330`, following Phase 10 of the
[development plan](next-development-plan-2026-10-04.md). Nothing is
committed. This record says what was built, the decisions taken and why, how
it was verified, and what remains unproven.

The [9 October staged audit and local repairs](phase10-audit-2026-10-09.md)
record later regression findings and verification separately from these
8 October results.

## Delivered behaviour

Settings now has eleven sections: Appearance, Library and folders, Image
viewing, Video playback, Indexing and performance, Search and AI,
Organisation and files, Accessibility and shortcuts, Privacy, Reset and
presets, and About. Every change applies at once and is saved in the
library; one "Saving…" note for the page says when a save is on its way.
Search finds each new row by its name and description.

### Preferences added

| Section | Preference | Default | Choices or range | Takes effect |
| --- | --- | --- | --- | --- |
| Appearance | Motion | Match Windows | Match Windows, Reduced, Full | At once: `data-motion` on the root; Reduced holds every animation and transition still, Full lets them run whatever Windows says |
| Appearance | Viewer background | Dark | Dark, Black, Grey | At once: the viewer's `--lumen-viewer-bg` token |
| Library | Open Lumen on | Library | Library, Favourites, Videos, People, Things, Places, Events, Albums | At the next start, if Lumen is still on the screen it opened on |
| Library | Thumbnails | Fill the square | Fill the square, Show the whole picture | At once, in every grid of tiles that does not fix its own fit |
| Library | Thumbnail badges | All shown | Favourite heart, Video icon, Video length, each on or off | At once; a badge hidden changes nothing about the photo |
| Image viewing | Open photos | Fitted | Fitted to the window, At actual size | The next photo opened |
| Image viewing | Mouse wheel | Zooms | Zooms, Moves between photos (Ctrl and the wheel then zoom) | At once |
| Image viewing | Play animations automatically | On | On, Off | The next animated image opened; off, it waits on its first frame with a Play button |
| Image viewing | Viewing copy quality | High (4096 px) | Standard (2048 px), High (4096 px) | Photos opened from now on; existing copies stay until cleared or evicted |
| Image viewing | Loop slideshows | Off | On, Off | The running slideshow |
| Video playback | Start muted | Off | On, Off | The next video opened |
| Video playback | Loop videos | Off | On, Off | The next video opened |
| Video playback | Show subtitles | Off | On, Off | The next video with text subtitles |
| Video playback | Preferred subtitle language | none | a language tag of up to 16 letters, digits and hyphens | With Show subtitles |
| Accessibility | Keyboard focus | Standard | Standard, Strong | At once: a 3 px ring in the text colour over a halo |
| Accessibility | Keyboard shortcuts | each action's own | see below | At once |
| Indexing | Photo formats to index | all on | JPEG, PNG, GIF, WebP, BMP, TIFF, HEIC/HEIF, AVIF, JPEG XL, camera RAW | The next check of the folders |
| Organisation | Export presets | none | up to 12, each a name, format, size, quality and whether display turns are baked in | Offered in the export dialog |

The preferences that already existed keep their keys, defaults and ranges
(theme, accent, density, text size, thumbnail size, panel widths, filmstrip,
auto-hide, slideshow interval, autoplay, volume, speed, skip interval, resume,
and every indexing, search and organisation setting). "Loop" used to start
both slideshows and videos looping; they are two preferences now, and a
library that had the old one on keeps videos looping until the user chooses.

**Persistence owner.** The interface preferences are `core.Preferences`, in
the library's SQLite `settings` table, saved whole in one statement
(`Store.SetPreferences`) and read under the same lock, so a read never
combines two saves. `core.Preferences.Normalised` holds every value to
what the UI can apply: a choice that is not offered becomes its default and
a number out of range the nearest end, so a value from a newer or older
Lumen never breaks the interface. The frontend's store
(`shared/preferences.ts`) holds the session's copy, applies it and saves in
`latestSave` order with rollback on failure. Browser storage still holds
only the first-frame appearance cache (theme, accent, density, text size);
none of the new preferences is cached there. Engine settings stay
`core.Settings`, each with its own setter.

### Keyboard shortcuts

The shortcut editor lists the 29 actions a shortcut makes sense for, by
where its keys work: anywhere, in the photo grid, in the viewer. Change
records the next key press (with Ctrl, Shift and Alt); Remove leaves an
action without a shortcut; Reset gives one back its own; Reset all
shortcuts clears every change. Sixteen actions have a shortcut to begin
with, and thirteen more (rotate, flip, slideshow, compare, full screen, the
filmstrip, File tools and others) can be given one.

- A press is refused, with the name of the action that has it, when another
  action would hear the same keys: in the same place, or when either works
  anywhere. The grid and the viewer may share a key, as they do today (F, I).
- Escape and Tab are never assigned, nor is Alt+F4, and "+" cannot be the
  key (it joins a shortcut's parts). The viewer's Close is fixed on Escape.
  Escape cancels a recording; so does moving on with Tab or the pointer.
- The shortcut reference (`?`), the command palette and the tooltips of the
  buttons show the keys in force.
- The engine stores at most 64 reassignments and drops any whose command id
  or keys are not well formed (`core.ValidShortcut`), so a hand-edited
  library or file cannot plant one.

Commands still declare their own keys where they are registered; the
reassignments are an overlay the command registry consults
(`keysOf`). The editor's list (`shared/shortcuts.ts`) repeats each command's
id, group, keys and zone, and a test reads the views' source to fail when
one drifts from the other. The video player's own keys (space, K, the
arrows while a video has focus) are not commands and cannot be changed.

### Format inclusion

Turning a photo format off stops new indexing of it: its files are no
longer added, and a file of it that changes is not read again. **What the
library already holds of that format stays**, with its tags, albums, people
and thumbnails, and stays browsable and searchable exactly as before; nothing
on disk is touched. A file of the format that is deleted from disk still
leaves the library. Turning the format on again has the folders checked, so
the files left out are found. The setting says all this where it is set, and
a note says how many formats are off. Videos keep their own switch, whose
older behaviour (turning it off removes videos from the library) is
unchanged.

### Caches

Settings shows the thumbnail cache and the viewing copies apart, each with
what it holds now. **Clear viewing copies** deletes the bounded copies made
to show photos the window cannot display itself (HEIC, TIFF, RAW, a held
animation). Each is made again when its photo is next opened. It takes the
renderer's turn, so no copy is being written meanwhile, and it cannot reach
originals, thumbnails or the library, which are outside its folder. The
thumbnail cache keeps its size limit and its eviction.

### Reset

Each of eight sections has a Reset button that asks first, then puts that
section back to its defaults: Appearance, Library, Image viewing, Video
playback, Accessibility and shortcuts (interface preferences); Indexing,
Search (engine settings); Organisation and files (the event gap, an engine
setting, and the export presets). **Reset all settings** does all eight
after a confirmation that lists what goes back and what does not.

A reset never changes the folders, photos, tags, albums, people, the PIN or
the blocks, and it leaves alone the three settings that decide what the
library holds or writes: whether videos are included, whether text is read,
and whether XMP sidecars are written. Resetting Indexing takes every format
in again and checks the folders.

### Customisation files

**Save my customisation** writes the interface preferences to a JSON file
the user names: `{"kind": "lumen-customisation", "version": 1,
"preferences": {…}}`. **Use a customisation file** reads one and shows a
preview: the file's name and the settings that would change. Only Apply
changes anything, through the ordinary preference save.

- The file's type has no field for a folder, playback history, the PIN, a
  block or an engine setting, and a test checks a saved file names no library
  folder. Export presets hold no destination, so they travel too.
- Reading refuses any field the type does not list (`DisallowUnknownFields`),
  a file over 256 KiB, a file that is not Lumen's, and a file of a later
  version, each with its own explanation. A file therefore cannot turn on
  XMP sidecars, start an organise run or change what is indexed.
- Values are held to what the UI can apply, as any saved preference is. A
  preference the file leaves out keeps the library's value; a list it holds
  (shortcuts, presets) replaces the library's whole.
- The file is published in one rename, so a failed save leaves an existing
  file as it was.

## Decisions

All taken from the plan and the code; no product question was asked.

- **One preference owner, extended.** The new preferences are fields of
  `core.Preferences` and `core.ViewerPreferences`, not a second store. Where
  a zero value would have been a wrong default, the field is named for the
  exception (`hideFavouriteBadge`, `stillAnimations`), so a set written by an
  older Lumen reads correctly.
- **Reset categories are the settings sections.** `core.PrefCategory` names
  the same eight sections the screen shows, so "Reset" on a section means
  exactly what is under it. A test checks every interface preference belongs
  to exactly one category.
- **Thumbnail fit, badges, motion, focus and the viewer background are
  attributes on the root** (`design/appearance.ts` sets them, `tokens.css`
  and `MediaTile` read them), like the theme. No tile reads a store and no
  prop is threaded through the grids.
- **Motion has one switch.** The reduced-motion rules moved from the media
  query to `:root[data-motion='reduced']`; "Match Windows" sets it from the
  media query and follows it while Lumen runs. Until the preference is
  applied, the media query itself still stands.
- **Excluding a format keeps what is there.** The plan's rule, and the
  opposite of the videos switch. In the scanner an excluded file that is on
  disk is neither read nor counted as gone.
- **Viewing copy quality is a viewer preference, not an engine setting.**
  The viewer asks the renderer for a side; the cache is already keyed by
  it. Changing it rebuilds nothing.
- **An animation is detected by reading the file** (libvips' page count,
  which the inspection already returns), only when animations are held. With
  the default on, nothing extra is read.
- **Import applies through the ordinary save.** The engine's part ends at
  the preview; there is no second write path to keep in step.
- **The viewer's backgrounds are all dark.** Its controls are light in
  either theme; a light surround would need a second palette for them.

### Not offered, and why

- **Default date grouping and sort.** The plan asks for them "where the
  backend supports it". Timelines are grouped by day, newest first, by the
  one timeline query every view shares, and search keeps relevance order.
  Another order would be a change to that engine, not a preference; the
  Library section says so in place of a control that would do nothing.
- **Preview (thumbnail) quality.** Thumbnails are part of the index: their
  size and encoding are a stage version, and changing them is a rebuild of
  every thumbnail. Only the viewing copy's quality is offered.
- **Application updates.** They belong to Phase 12, which owns their
  policy and their application-scoped store.
- **Reassigning the video player's keys**, as above.
- **The RAW look.** Whether a developed RAW or the camera's embedded
  preview is shown is a rendering decision with consequences for thumbnails
  and comparisons (see the Phase 9 record); it was not made a preference
  here.

## Verification

- `scripts/lint.sh`: all checks passed (golangci-lint 0 issues, deadcode,
  govulncheck, vue-tsc, ESLint with no warnings, Prettier, ruff, shellcheck,
  the installer payload tests).
- Go: the race suite on the named packages. New tests cover normalisation
  of every new preference, shortcut validity, one reset category per
  preference, surviving a reopen, the older shared loop, an unreadable list,
  format exclusion in the store and in the scanner on real files (neither
  added nor forgotten, forgotten when deleted, found when on again), the
  customisation file (round trip between two libraries, names no folder,
  refuses unknown fields, a newer version and an oversized file, holds values
  in range, keeps what it leaves out, a failed save leaves nothing behind),
  reset (restores engine settings, keeps videos, text reading, XMP, the PIN
  and the folders) and the viewing cache (clears only itself).
- The bridge's new constants, lists and error codes are checked against Go
  by `services/contract_test.go`, as is the viewer fallback.
- Frontend: Vitest. New tests cover the shortcut catalogue against the
  views' source, overrides in the command registry, recording, conflicts,
  reserved keys, remove and reset in the editor, the customisation preview
  and apply, refusals, reset with and without confirmation, format switches
  with a failed save, export presets, the new appearance and viewing
  controls, the page attributes, opening at actual size, the wheel stepping,
  the viewing copy size, and holding and playing an animation.
- Native: `e2e/suites/customise.mjs` on `bin\Lumen.exe`, on a copy of the
  base library with three files of its own.

Results on 8 October 2026, on the final build:

| Gate | Result |
| --- | --- |
| `go test -race -count=1` on `.`, `./internal/...`, `./services/...` | every package passed |
| Vitest | 567 tests in 54 files passed (539 before this phase) |
| `wails3 build` | passed |
| `wails3 task package` | passed: the unsigned installer was built (874 MB), not installed |
| `e2e/suites/customise.mjs` | 45 of 45 checks ([report](phase10-native-acceptance-2026-10-08.json)) |
| Every other native suite (`node e2e/run.mjs`) | onboarding 7/7, tour 5/5, interface 44/44, collections 8/8, organise 7/7, viewer 63/63, file-tools 14/14, branding 18/18, formats 55/55 |

The native suite changed each preference in Settings and checked it where
it takes effect: a transition's computed duration under Reduced and Full
motion; the viewer's computed background; the focused control's outline;
a grid thumbnail's `object-fit` and a hidden badge's `display`; the screen
Lumen opened on after a restart, with every preference kept; a recorded key
turning a photo in the viewer, a taken key refused by name, Escape still
closing; a photo opening at 100% and the wheel stepping to the next; a PNG
neither added nor forgotten while PNG was off and found when it was on
again; a GIF waiting on a still copy until played; the viewing copies
cleared with the thumbnail cache unchanged to the byte; a preset saved; one
section reset and then everything, with videos, text reading, XMP, the
folders and the photo count as they were. No uncaught frontend error and no
request off the PC.

Two things outside this phase were fixed because the full run showed them:

- **The grid's selection after the viewer.** After stepping to another
  photo in the viewer and closing it, the grid's keys acted on the photo last
  viewed but the cell drawn as selected was still the one the viewer was
  opened on. Closing the viewer now selects the photo last viewed (a
  selection of several is left as it is). The interface suite's check for
  this had been failing; it passes.
- **The tour's first reload.** The tour reloaded the page the moment the
  window opened; on a busy PC that aborted the app's first calls, which it
  logged, and the tour counted the log as an error. The tour now waits for
  the shell before reloading, and its error report names the screen and the
  script line.

## Limits and what is not proven

- **The file dialogs.** Saving and choosing a customisation file go through
  Windows' own dialogs, which the native suite cannot drive. What happens
  before and after them is tested in Go on real files and in Vitest; the
  dialogs themselves were not exercised.
- **Animated PNG is not held.** libvips does not report an APNG's frames, so
  with animations off an APNG still plays. GIF and animated WebP are held;
  animated AVIF was not tried. The setting says so.
- **Subtitle language matching is by prefix.** Files name languages their
  own way ("eng", "ger"); "en" finds "eng", but "de" does not find "ger". The
  first text track is shown when nothing matches.
- **Start view** applies only when Lumen starts on the library; a start on
  the Welcome screen (no folders) ignores it.
- **Shortcut conflicts are checked among the listed actions.** A key the
  video player or a focused control uses itself is not known to the editor.
- **Not done:** a library of thousands of files with a format turned off,
  an installed upgrade from a build without these preferences (the reading
  of older saved values is covered by store tests only), screen-reader
  testing of the shortcut editor, and forced-colours review of the strong
  focus ring.
- No commit, push, merge or publication was performed.
