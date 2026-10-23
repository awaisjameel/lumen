# Using Lumen

These examples use a folder such as `D:\Pictures\Weekend`, containing photos
you are comfortable testing. Names and queries are examples you can copy, not
special commands. Your results depend on the photos, completed indexing and
visibility settings. See [installation](../README.md#installing) first.

## Add a folder and let indexing catch up

On first run, continue through Welcome, choose a suggested folder or add your
own, review the selection and choose **Start browsing**. Later use
**Settings → Library and folders** to add or remove sources. Pick a parent
folder once: sources cannot overlap, and subfolders are included automatically.

Adding `D:\Pictures\Weekend` lets Lumen read the files in place. It does not
copy them into the library or reorganise them. Thumbnails appear before AI work
finishes. Select the activity card at the bottom of the sidebar for progress,
files that could not be read and a pause button. On battery, while the PC is
busy or behind a full-screen app, indexing may wait. Check
**Settings → Indexing and performance** if progress seems paused.

New files and changes are watched, and sources are checked at startup. Removing
a source does not delete its originals. Byte-identical copies normally share
one library item with several paths; large files use sampled hashes, so this is
not a forensic full-file comparison tool.

## Browse photos and play a video

Use **Library**, select a day and open a tile. Arrow keys navigate; `I` toggles
details; `Esc` closes Details first, then the viewer. The viewer offers fit and
actual size, zoom, a filmstrip, a slideshow and comparison. Rotating and
flipping change only how Lumen shows a photo, never the file.

Videos are off by default: enable them in **Settings → Library and folders**.
Use the Library's All / Photos / Videos filter. Some codecs play directly;
others are converted locally while playing. The container suffix alone does
not guarantee direct playback. Seek to another point to start a new converted
stream. Video controls include tracks, text subtitles and frame export where
available.

![A photo of a coffee cup on a wooden table open in the viewer, with its date, place and metadata in the details panel](screenshots/viewer.webp)

The viewer loads a preview first. A RAW may use an embedded preview if it cannot
be developed; a viewing copy may be resolution-limited. The label describes
what is actually available. See [known format limits](beta-testing.md#known-limits-of-this-beta).

## Search by meaning, person or text

Press `Ctrl+F` or `/`, then try these queries with suitable photos:

| Query | Expected behaviour | Limit |
| --- | --- | --- |
| `coffee cup` | Semantic matches, strongest first. | AI embeddings must exist; the first query may load a model and take longer. |
| `cat on a sofa` | Matches the scene's meaning rather than filenames alone. | Plausible matches can be wrong, and a weak query may return nothing. |
| `Ali` | A named person's visible photos. | Name them in People first; a name in a filename does not name a face. |
| `Ali and Sara at the beach` | Photos of both named people, ranked for the remaining description. | Both people must be recognised or manually assigned in the photo. |
| `invoice total` | Photos whose recognised text contains every word come before the semantic matches. | Turn on **Find text in screenshots and documents** in **AI and search** first. English only, and only photos the AI sorts as screenshots or documents are read. |

Suggestions in the search box help you select named people. Video search can
open the best matching moment. Hidden/blocked content follows the same visibility
rules as the Library. Search is not a guarantee that a particular object,
person or exact phrase will be recognised.

![Search results for snowy mountains at sunrise, best matches first](screenshots/search.webp)

## Name a person and correct faces

1. Open **People**, then an unnamed person. Choose **Add a name** or click the
   name on their page. Enter `Ali` and choose **Save name**.
2. Open **Review faces** (also the **Faces** tab). The least alike faces appear
   first. Select an incorrect face and choose **Not Ali**. The photo stays in
   the Library; the AI must not put that face back into Ali's group.
3. If you know who it is, select it and choose **Move to someone…**, then enter
   an existing name or create another person. Selecting one face also allows
   **Use as their picture**.
4. To merge two groups of the same person, choose **Merge with someone…** from
   the person's actions, or give one of them the other's name: Lumen shows the
   existing person and asks whether they are the **Same person**. Confirm only
   if they really are. Clearing a name keeps the photos grouped.

![The Faces tab of a person named Sample astronaut, with one face selected and the Not Sample astronaut, Move to someone and Use as their picture actions](screenshots/faces.webp)

New automatic groups need clear faces in at least three photos. Small, blurred
or turned-away faces can remain unassigned. Details can offer **Who is this?**
for those faces. User assignments and names take precedence over clustering.
**Hide from People…** hides the identity while leaving photos visible;
**Block this person…** can hide their photos too. Review the block mode before
confirming. See [privacy](#hide-block-and-unblock).

The person page's **Most photographed with** lists co-occurrence, not inferred
family relationships. A designated **Me** identity and relationship labels are
not implemented in this source; [issue #30](https://github.com/awaisjameel/lumen/issues/30)
tracks that work. Recheck these instructions and the People screenshots after it merges.

## Correct Things and make albums

Open a photo's details to add a manual tag such as `Weekend`, or remove an
incorrect Thing. A manual correction overrides AI tagging. On **Things**, a
custom label such as `red bicycle` searches the existing library immediately;
it is a semantic prompt, not a newly trained model. The confidence control in
**AI and search** changes automatic matching without rescanning files.

To collect a trip, open **Albums → New album**, name it `Weekend`, then select
photos in the Library and use **Add to album…**. A manual album stores membership
without moving files. A **Smart album** saves rules instead and follows the
library as it changes: for example, turn on **Favourites only** and set **Media**
to **Photos** for a live view of your favourite photos, or choose a **Thing**
such as Mountains. Deleting an album does not delete its originals, and
membership follows files renamed or moved through Lumen.

![Three smart albums, By the sea, City walks and Mountain views, each following a Thing](screenshots/albums.webp)

Places need GPS or a location correction; the bundled atlas is an approximate
nearest-city grouping, not an online street map. Events group photos by time
gaps and location changes. Rename or pin these collections without changing
the source files' metadata. See [Places and Events](features.md#9-places-and-events).

## Hide, block and unblock

Use a collection's actions or a photo's block action. Create a PIN when prompted.
Review whether the action hides **only the label/person** or **the photos too**.
Photo-hiding blocks remove matching media from ordinary grids, search, counts,
covers and media access. Label-only hiding leaves photos visible.

Open **Settings → Privacy and blocked**, unlock with the PIN, then review or
unblock an entry. A new block offers a brief Undo. The unlocked gate locks when
you leave or after 10 idle minutes. Wrong attempts are rate-limited. If the PIN
is forgotten, use the Windows sign-in reset offered there.

Blocking is an application visibility feature. It does not encrypt photos,
erase them or prevent access in Explorer or other applications. Avoid sharing
screenshots or problem reports containing names you intended to hide.

## Review duplicates and use Trash safely

Open **Duplicates** to inspect exact copies and visually similar groups. The
best-copy suggestion favours resolution and other quality signals; inspect it
yourself. Similar images can be different moments. Confirm the removal only
after choosing which copies to keep.

**Move to Trash** hides a library item and offers Undo; it does not immediately
remove the source from disk. In **Trash**, restore it or choose **Delete from
disk** and confirm to send it to the **Windows Recycle Bin**. Nothing is emptied
automatically or permanently deleted by Lumen. Do not empty Windows' Recycle
Bin until you are satisfied with your choices.

## Rename, move, export and organise files

From selected media, open **File tools**. Preview a rename or move before
applying it. Journalled moves recover after interruption and can be undone;
name collisions receive a numbered name instead of overwriting another file.
An export writes originals or converted JPEG/PNG copies to a chosen folder,
reporting each outcome. Display rotation does not edit the original.

For a whole-library move, open **Settings → Files and organisation → Organise
files on disk…**. Try this folder scheme in the preview:

```text
{Year}/{Month}/{Event}/{Name}
```

The preview lists planned moves, skips and renamed collisions before anything
happens. Blocked, trashed and missing files are excluded. Review it with a small
test folder first. XMP sidecars are a separate, off-by-default setting: enabling
them writes `photo.jpg.xmp` beside originals. Lumen rewrites only sidecars it
created; its sidecar-removal action sends those files to the Recycle Bin.

## Analyse compression before saving copies

Select a supported JPEG/PNG, choose **Compress losslessly…**, or open
**Compression** under Tools. Pick a mode, analyse, then review measured sizes,
preservation results and skip explanations. Save selected **new copies** only
when the review is satisfactory.

| Mode | Result |
| --- | --- |
| **Optimise existing format** | Smaller JPEG or PNG copies that keep their format. |
| **Reversible JPEG to JPEG XL** | A JPEG XL copy from which Lumen can restore the original JPEG, byte for byte, into a new file. |
| **Convert losslessly to JPEG XL** | A lossless JPEG XL copy of an eligible plain RGB/RGBA PNG without extra metadata. |

Pause/cancel and restart recovery are available. The source is not replaced;
keeping both files consumes more disk space overall. Other programs may not
open JPEG XL. Sources above 16 million pixels or 256 MiB, animations, unsupported
metadata and other ineligible inputs are skipped with a reason. History keeps
the newest 100 runs; save wanted copies before old reviews age out. Same-format
replacement and Undo are deferred.

## Common settings and help

- **Appearance:** light/dark/system, accent, density, text size and thumbnail fit.
- **Library and folders:** sources, video indexing and excluded formats.
- **AI and search:** tag confidence and **Find text in screenshots and documents** (off by default).
- **Indexing and performance:** battery/busy/full-screen pauses, CPU threads,
  manual pause and indexing only while away.
- **Viewing and playback:** fit, wheel behaviour, slideshow, video controls
  and optional playback history.
- **Accessibility and shortcuts:** reduced motion, focus and shortcut changes.
  `Ctrl+K` opens the command palette; `?` opens the shortcut reference.
- **Backup and presets:** preview a customisation file before applying it.
  These files carry interface preferences, not a backup of photos, names or
  albums. Folders, PIN, blocks and history are excluded.
- **Updates and about:** version, licences, local logs and problem reports.
  Public update delivery remains disabled; see [release status](release-status.md).

Settings are saved in the library database. A section's Reset and Reset all
offer confirmation; review what the dialog includes. For a problem, follow
[the problem-report instructions](beta-testing.md#sending-a-problem-report)
and inspect the ZIP for private paths before sharing it.
