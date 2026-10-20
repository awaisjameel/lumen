# Phase 7A: inventory of the baseline interface

Date: 4 October 2026. Baseline: `main` at `5525ec2` (Lumen 0.9.1, the beta),
read from the committed code and confirmed in the running baseline build
(`bin/Lumen.exe` built from that commit, kept at `<scratch>\phase7\baseline`).
This is the regression baseline the Phase 7 rebuild is checked against; the
requirement-to-implementation matrix is in
[the Phase 7 record](phase7-design-2026-10-04.md).

Baseline screenshots (every route at 1440×900, and the core screens at
1040, 720 and 520 px wide) are in `<scratch>\phase7\shots\baseline`, taken by
`<scratch>\phase7\harness\tour.mjs` from the production build on the acceptance
library. They are evidence for this record, not repository content.

## 1. Routes

| Path | Name | View | Scope or purpose |
| --- | --- | --- | --- |
| `/` | library | LibraryView | The whole timeline (`FilterAll`). |
| `/favourites` | favourites | LibraryView | Favourites (`FilterFavourites`). |
| `/videos` | videos | LibraryView | Videos (`FilterVideos`); in the sidebar only while videos are indexed. |
| `/trash` | trash | LibraryView + TrashBar | Lumen's Trash (`FilterTrash`). |
| `/people` | people | PeopleView | People cards. |
| `/people/:id` | person | LibraryView + PersonHeader | One person's photos. |
| `/people/:id/faces` | personFaces | PersonFaces | Face review for one person. |
| `/things` | things | ThingsView | Things (labels). |
| `/things/:id` | thing | LibraryView | One Thing's photos. |
| `/albums` | albums | AlbumsView | Manual and smart albums. |
| `/albums/:id` | album | LibraryView | One album's photos. |
| `/places` | places | PlacesView | Places. |
| `/places/:id` | place | LibraryView | One place's photos. |
| `/events` | events | EventsView | Events. |
| `/events/:id` | event | LibraryView | One event's photos. |
| `/duplicates` | duplicates | DuplicatesView | Exact copies and similar photos. |
| `/search?q=` | search | LibraryView | Search results, best first. |
| `/settings` | settings | SettingsView | Settings and About. |
| `/settings/blocked` | blocked | BlockedView | The PIN-gated Blocked screen. |
| `/settings/organise` | organise | OrganiseView | Organise files on disk. |
| anything else | – | – | Redirects to `/`. |

Outside the router: `Welcome` (onboarding) replaces the shell while the
library has no source; a failed start shows the failure in place of both.

## 2. Overlays and floating surfaces

| Surface | Baseline implementation | Owner |
| --- | --- | --- |
| Confirmation | Native `<dialog>` (`ConfirmDialog`), one question at a time, Cancel focused. | `shared/confirm.ts` |
| Block dialog | Native `<dialog>` (`BlockDialog`): mode, face scope, first PIN. | `shared/block.ts` |
| Failure notice | Fixed bar (`Notice`), 6 s. | `shared/errors.ts` |
| Block Undo | Same bar, 8 s, Undo button. | `shared/block.ts` |
| Trash Undo | Second fixed bar (`TrashUndo`), 8 s. | `trash/undo.ts` |
| Crash notice | Fixed card at the top (`CrashNotice`), until dismissed. | `about/store.ts` |
| Viewer | Full-window layer, `aria-modal`, own key handling. | `library/Viewer.vue` |
| Details panel | Inline panel beside the grid or inside the viewer. | `library/DetailsPanel.vue` |
| Index status list | Hover/focus card above the status card; Escape hides it. | `library/IndexStatus.vue` |
| Search suggestions | Listbox under the search field. | `search/SearchBox.vue` |
| Person picker | Listbox under its field (details panel, face review). | `people/PersonPicker.vue` |
| Duplicate review | Native `<dialog>` confirming what is removed. | `duplicates/ReviewDialog.vue` |
| Duplicate preview | Native `<dialog>` with the large preview. | `duplicates/DuplicatesView.vue` |
| Inline editors | Rename forms for a person, a Thing and an event; the album editor. | their views |
| Native dialogs | Folder picker, save-report dialog, Windows sign-in (PIN reset). | engine |

## 3. Actions

| Module | Actions |
| --- | --- |
| Onboarding | Pick a folder, add a suggested folder, remove a chosen folder, start. |
| Library, Favourites, Videos | Select, open, favourite, show details, change thumbnail size, scrub the timeline, search. |
| Search | Search by description, pick a suggested person or Thing, clear; open the Blocked screen from a blocked-name notice. |
| Viewer | Previous, next, close, favourite, details, move to Trash; in the Trash: restore, delete from disk; play video (direct, HEVC, converted) and seek. |
| Details | Add or remove a tag, add to or remove from an album, name or reject a face, block (Thing, person, face, place, event, photo, folder), show in folder. |
| People | Open a person, rename, merge on a name clash, set the cover, block, review faces (select, reject, move to another name). |
| Things | Create a label, rename or reset, pin, delete a custom one, block. |
| Places | Open a place, block. |
| Events | Open an event, rename, pin, block. |
| Albums | Create a manual or smart album, edit name and rule, delete. |
| Duplicates | Choose which copy to keep, preview, remove copies (recycle extra files of exact copies; move similar photos to the Trash), Undo. |
| Trash | Restore one, restore all, delete one or all from disk (to the Recycle Bin, after confirming the file count), Undo of "Move to Trash". |
| Blocked | Create a PIN, unlock, lock, reset a forgotten PIN by Windows sign-in, switch a block's mode, unblock, browse covered media, move covered media to the Trash, block a folder. |
| Organise files | Edit the template, choose a source, preview, start, resume, undo a run. |
| Settings | Add, remove and rescan folders; videos; AI on battery; pause for full screen; pause when busy; idle only; CPU threads; tag confidence; event gap; thumbnail cache cap; OCR; XMP (on/off, export now, delete sidecars); theme; open Blocked and Organise. |
| About and diagnostics | Version, releases page link, licences, log folder, save a problem report; crash notice with the same report. |
| Indexing status | Pause, resume, read what is at work, what waits and why files failed. |

## 4. Keyboard shortcuts

| Keys | Where | Does |
| --- | --- | --- |
| Ctrl+F, `/` | Anywhere, unless typing or a modal is open | Focus the search box. |
| Arrow keys | Grid | Move the selection (selects the first photo if none). |
| Enter | Grid | Open the selected photo. |
| Escape | Grid | Clear the selection. |
| F | Grid, viewer | Toggle favourite (not in the Trash). |
| I | Grid, viewer | Show or hide details. |
| ←, → | Viewer | Previous, next (not while a slider or the video has the arrows). |
| Escape | Viewer | Close; a confirmation above it takes the key first. |
| ↑, ↓, Enter, Escape | Search box, person picker | Move through suggestions, choose, close. |
| Enter, Escape | Rename editors | Save, cancel. |
| Arrow keys | Theme choice | Move the choice, as a radio group. |
| Escape | Index status list | Hide the list. |
| Tab, Shift+Tab | Everywhere | Focus order; dialogs trap focus and return it. |

## 5. Preferences and where they lived

| Preference | Type and range | Default | Baseline owner |
| --- | --- | --- | --- |
| Index videos | on/off | off | SQLite `videos_enabled` |
| AI on battery | on/off | off | SQLite `ai_on_battery` |
| Pause for full screen | on/off | on | SQLite `pause_full_screen` |
| Pause when the PC is busy | on/off | on | SQLite `pause_busy` |
| Index only when idle | on/off | off | SQLite `idle_only` |
| CPU threads | 1..machine | half the machine's, at least 2 | SQLite `cpu_threads` |
| Tag confidence | 0.002..0.3 | 0.02 | SQLite `tag_confidence` |
| Event gap | 1..72 h | 12 | SQLite `event_gap_hours` |
| Thumbnail cache cap | 256..102,400 MB | 2,048 | SQLite `thumb_cache_mb` |
| Read text (OCR) | on/off | off | SQLite `ocr_enabled` |
| XMP sidecars | on/off | off | SQLite `xmp_enabled` |
| Theme | system, light, dark | system | WebView localStorage `lumen.theme` |
| Thumbnail size | 96..320 px | 168 | WebView localStorage `lumen.zoom` |
| Details panel shown | on/off | off | Session only |
| Indexing paused | on/off | off | Engine, session only |
| Sources | folders | none | SQLite `sources` |
| Block PIN and blocks | – | none | SQLite (hashed PIN, blocks) |

Phase 7 moves the two WebView-owned preferences into the library's settings
and adds the appearance preferences the new design system needs; see the
record's preference contract. Nothing above is removed.

## 6. Baseline states captured

Per route: populated; and from the code and tests, each view's loading,
empty and failure wording (the `*.empty`, `*.emptyHint`, `error.*` and
`status.*` messages in `shared/i18n.ts`), which the migrated screens keep.
Failure states that need a fault (a failed engine call, a missing file, an
interrupted organise run, a wrong PIN and its cool-down) are covered by the
component tests beside each view rather than by screenshots.
