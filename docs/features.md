# What Lumen does

Lumen is a Windows desktop photo and video gallery. All of its AI runs on your own PC. It has no accounts, uploads or telemetry, and it makes no network requests; the only exception is an opt-in updater, inert until release signing is set up (section 16). The AI models, FFmpeg and the city database ship inside the installer.

---

## 1. Getting started and the library

- **First run:** a welcome screen suggests folders to add, such as Pictures, and lets you pick others. Source folders can't overlap.
- **Scanning:** Lumen walks each folder, skipping hidden and system folders. It identifies every file by a hash of its contents, and guesses a date from the file name when EXIF has none.
- **Watching:** Lumen watches the folders, including subfolders, for changes and rescans after a short delay. It also checks everything once at startup.
- **Exact duplicates** (byte-identical files) appear as one photo with several file paths.
- **Formats:**
  - JPEG, PNG, WebP, GIF, animated PNG, TIFF (including multi-page), AVIF, HEIC, BMP and JPEG XL.
  - Camera RAW: DNG, Canon, Nikon, Sony, Fujifilm, Olympus/OM, Panasonic and Pentax. RAW files are developed in a separate worker process. If a file can't be developed, Lumen shows its embedded preview with a label saying so.
  - Videos are optional: switch them on in Settings. MP4/MOV and other containers play directly. Others, such as MPG, TS, OGV and ASF, are converted while they play.
- You can leave individual formats out of indexing without losing anything already in the library.

## 2. Background indexing

Each photo goes through these stages in order: metadata → thumbnails → AI embedding → faces → text recognition (OCR) → XMP export.

- The work queue is stored in the database, so it picks up where it left off after a crash or restart.
- **Newest photos go first**, and the days currently on screen jump the queue.
- The **sidebar status** says what indexing is doing. Hovering it shows what's being worked on, what comes next, and which files couldn't be read and why (damaged, too slow, or locked by another app).
- **It stays out of your way:**
  - It runs below normal priority on about half the CPU's threads, which you can change.
  - The GPU rests a third of the time.
  - It pauses on battery (a setting allows it), while a game, full-screen video or presentation is in front, and while other programs keep the CPU above 70%.
  - There's a manual pause and an "only while I'm away" mode (no input for 5 minutes).
- **Thumbnail cache:** limited to 2 GB by default. The thumbnails you've looked at least recently are removed first and rebuilt when needed.

## 3. Browsing: the timeline grid

- A grid grouped by day that stays fast with very large libraries, with a timeline scrubber for jumping through years.
- Zoom levels, keyboard navigation, and hover previews for videos (a 4×4 grid of frames).
- Sidebar: **Library** and **Favourites**; the collections **People, Things, Places, Events, Albums**; the tools **Duplicates, Compression, Trash**; and **Settings**. The Library's All / Photos / Videos filter takes the place of a separate Videos page.
- **Multi-selection** works across the whole grid, with a selection bar of batch actions.
- A **details panel** (`I`) shows the date, camera, size, location, people, Things and albums for the selected item.

## 4. Viewer

- Opens instantly from a preview, then loads the full-quality original.
- Fit or actual size, zoom with the wheel or a pinch centred on the pointer, panning and full screen.
- **Filmstrip, slideshow and side-by-side comparison.**
- **Rotate and flip for display only.** The original file never changes.
- Multi-page TIFFs turn page by page. Very large images show a preview limited to 4096 px, and the viewer says what resolution it is showing.
- **Video playback:** configurable controls, audio conversion, text subtitles, looping, optional resume history and saving a frame as an image. Seeking in a converted video starts a new stream at that point.
- Options include the background colour, how photos open, what the wheel does, viewing-copy quality and holding animations.

## 5. Search (AI)

- Press `Ctrl+F` or `/` and type in plain language, e.g. "dog on a beach at sunset". The SigLIP 2 model matches the meaning of the query.
- Results come back best match first. Weak matches are cut off relative to the best one, so a query that matches nothing returns nothing. Searches take about 120–200 ms.
- **People in queries:** "Ali and Sara at the beach" narrows to photos containing both of them. A name on its own gives that person's photos. The search box suggests people as you type.
- **Video results** jump to the best matching moment.
- **Text in images** (off by default) reads text in photos the AI classes as screenshots or documents (English, on the CPU). Photos containing every word of the query come first, ahead of the AI matches.

## 6. Things (automatic tags)

- About **180 built-in labels**, each kept because it performed well on a real library. The AI also sorts every photo into one of ten broad kinds: People, Screenshots, Documents and so on.
- **Custom labels:** type something like "Eiffel Tower" and Lumen finds it across the whole library immediately.
- **Manual tags:** add or remove tags on any photo. Your edits always override the AI.
- A **confidence slider** controls how strict tagging is. It takes effect without rescanning.
- **Pin** Things to keep them at the top, and **rename** built-in ones. A renamed Thing keeps its AI matching, search uses the new name, and "Reset name" brings back the original.

## 7. People (face recognition)

- Faces are detected and recognised on the GPU, with a CPU fallback.
- Faces are **grouped into people automatically** as photos arrive, with some safeguards:
  - Two faces in the same photo are never treated as the same person.
  - Blurry or turned-away faces can join an existing person but can't start a new one.
  - A new person needs clear faces in three photos.
- **Your corrections always win:**
  - Name a person. Using a name that's already taken asks "same person?" and merges them.
  - Remove a face with "Not this person"; it is never put back.
  - Move faces to another person or a new person, which splits a group.
  - Choose a person's picture.
- Anything you've placed or named is never undone by the AI.
- **Face review** shows the least similar faces first, so mistakes are easy to find.
- The details panel asks "Who is this?" for faces not yet recognised.
- A person's page is the timeline filtered to their photos.

## 8. Albums

- **Manual albums** collect photos and videos without moving any files. Membership follows a file when you rename or move it.
- **Smart albums** save a rule (media type, favourites, a date range, a Thing, a person) and update automatically as the library changes.
- Albums have live counts, covers, rename, editing and deletion.

## 9. Places and Events

- **Places** groups GPS-tagged media by the nearest city (within 75 km), using a bundled offline copy of GeoNames with about 70,000 places.
- **Events** start a new group after a time gap (12 hours by default, configurable) or a jump of more than 100 km in location.
  - Names, pins and blocks you set follow the photos when events regroup. If an event splits, both halves keep them.
  - Reset brings back the automatic name.

## 10. Blocking (privacy)

- Almost anything can be **blocked**: people, single faces (optionally with similar faces), Things, places, events, albums, individual photos or videos, and whole folders.
- A label can be hidden on its own, or together with every photo it covers.
- Once blocked, an item disappears everywhere: grids, search, counts, album covers, thumbnails and video streaming.
- **Settings → Privacy and blocked** is protected by a PIN:
  - Wrong attempts are rate-limited, and it locks after 10 idle minutes or when you leave it.
  - It lists every block, where you can change how each one hides content or unblock it. A new block offers a brief Undo.
  - You can browse a block's hidden photos in the viewer. Playback history isn't recorded there.
  - A forgotten PIN is reset by signing in to Windows (Windows Hello or your account password).

## 11. Trash and Duplicates

- **Trash:** "Move to Trash" needs no confirmation and offers Undo. From Trash you can restore items, or send them to the **Windows Recycle Bin** after confirming. Nothing is emptied automatically, and nothing is ever permanently deleted.
- **Duplicates** lists exact copies and visually similar photos, with the best copy pre-selected: highest resolution, then the original camera file and format, file size, metadata and age. Nothing happens until you confirm.

## 12. File tools

- **Rename and move** with a preview first. Moves are journalled so they recover after a crash, and they can be undone.
- **Export** as the original, JPEG or PNG, reporting the outcome for each file. Export presets save your choices.
- Open a file in other Windows apps.
- **Organise on disk** (Settings → Files and organisation → Organise files) moves files into folders built from a template such as `{Year}/{Month}/{Event}/{Name}`. Other fields: Day, MonthName, Place, Camera, Ext.
  - A preview lists every move, skip and renamed collision before anything happens.
  - Nothing is overwritten: a name collision becomes `name (2)`.
  - Runs are journalled, so they can be resumed or undone.
  - Blocked, trashed and missing files are left alone.
- **XMP sidecars** (off by default) write `photo.jpg.xmp` files containing Things, named people with their face areas, and a 5-star rating for favourites. Lumen only rewrites sidecars it wrote itself, and one button moves them all to the Recycle Bin.

## 13. Lossless compression (Tools)

- Analyse first, review the measured savings, then save **new copies**. Originals are never changed.
- Three kinds of conversion:
  - JPEG and PNG optimisation that stays compatible.
  - **JPEG to JPEG XL**, reversible back to the exact original JPEG bytes.
  - PNG to JPEG XL, for supported files.
- Each result goes through independent preservation checks. Runs can be paused, cancelled and recovered after a restart, and history is kept per file.
- Current limits: images over 16 megapixels are skipped, and history keeps the newest 100 runs.

## 14. Interface and customisation

- **Appearance:** light or dark theme, six accent colours, comfortable or compact density, text size from 90% to 130%, how thumbnails fill their squares and which badges they show, and the screen Lumen opens on.
- **Command palette** (`Ctrl+K`), shortcut reference (`?`), sidebar toggle (`Ctrl+B`).
- **All shortcuts can be reassigned**, with collision checks. Escape and Tab can't be changed.
- **Accessibility:** reduced-motion setting, Windows contrast themes, a stronger focus ring.
- **Customisation files** carry your interface settings to another library, with a preview before applying. They never contain folders, history, the PIN or blocks.
- Every settings section has its own **Reset**, and there's a reset for everything.
- All settings are saved in the library database, not in the browser engine.

Settings sections: Appearance · Library and folders · Viewing and playback · AI and search · Indexing and performance · Files and organisation · Accessibility and shortcuts · Privacy and blocked · Backup and presets · Updates and about.

## 15. Reliability and diagnostics

- **Crash reports stay on your PC.** After a crash, the next start says Lumen closed unexpectedly.
- About Lumen opens the log folder and can save a **problem report** (logs, crash reports, a description of the PC and library, no photos) for you to share if you choose.
- Third-party licences are listed in the app.

## 16. Installation and updates

- A **per-user installer** with no administrator prompt. It includes WebView2, so installing needs no internet connection.
- It won't install over or uninstall a running Lumen, and the uninstaller asks before removing your library (the default is to keep it).
- **Updates:** an opt-in updater is built (Settings → Updates and about). It verifies downloads, prepares them in the background, backs up the library and checks the app's health on first launch after an update. It stays inert until release signing is set up, because its trust settings are empty, so builds from this repository make no network request. Until then, a new version installs over the old one with the offline installer.

---

**How far along it is:** see [the status table in the README](../README.md#status), which also lists the known limits.
