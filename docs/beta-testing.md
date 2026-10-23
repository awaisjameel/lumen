# Testing the Lumen beta

Thank you for testing the Lumen beta. Lumen is a photo gallery for Windows
that organises your photos and videos with AI that runs on your own PC.
Nothing is uploaded, and Lumen makes no network request: the one exception, an
opt-in updater, stays switched off until releases are signed.

The beta is meant to be used on your real photos, so that we learn how it
copes with years of camera rolls, WhatsApp images, screenshots and phone
backups. Settings → Updates and about → About Lumen shows which version you
have.

This page is the testers' checklist: what to try, what Lumen does to your
files, and how to report a problem. [The walkthrough](using-lumen.md) shows how
to do each thing, and [release status](release-status.md) says what each
published version has and has not been tested for.

## Installing

Download the setup executable and `SHA256SUMS.txt` from the same
[release](https://github.com/awaisjameel/lumen/releases). Compare its hash before
running it: `Get-FileHash .\Lumen-0.12.0-x64-setup.exe` in PowerShell, or
`sha256sum -c SHA256SUMS.txt --ignore-missing` in Git Bash. This guide follows
the current source; the release notes link the guide as it was for that version.

1. Run `Lumen-<version>-x64-setup.exe`. It installs Lumen for you alone, in
   `%LOCALAPPDATA%\Programs\Lumen`, without asking for administrator rights.
2. Windows may say it protected your PC, because this beta is not signed
   yet. Choose **More info**, then **Run anyway**.
3. If your PC lacks the Microsoft Edge WebView2 Runtime (some older Windows
   10 PCs do), Setup installs it from inside its own file. It does not
   download anything.

If you already have Lumen, close it and run the new installer over the
existing installation. You do not need to uninstall first; your library,
names, albums and settings are kept.

Use Windows 10 or 11 (x64); at least 8 GB of memory is recommended. A graphics
card is used if there is one; Lumen works without it, more slowly.

## What Lumen does to your files

Nothing, unless you ask, and it never edits a photo itself. Lumen reads your
photos and keeps what it learns in its own library, `%LOCALAPPDATA%\Lumen`. It
only changes files in your folders when you use one of these, each of which
shows you what it will do first ([the walkthrough](using-lumen.md#rename-move-export-and-organise-files)
has examples):

- **File tools** (on selected photos) renames or moves files, or exports
  copies to a folder you choose. Moves are journalled, survive a crash and
  can be undone; a name already taken gets a number instead of being
  overwritten.

- **Delete from disk** (in the Trash and Duplicates) moves files to the
  Windows Recycle Bin, never further.
- **Organise files on disk** (Settings) shows every move before it makes
  any, and can undo them.
- **XMP sidecar files** (Settings, off by default) writes small `.xmp` files
  beside your photos. **Delete Lumen's sidecar files**, beside it, moves
  every one Lumen wrote to the Recycle Bin.
- **Compress losslessly…** analyses selected JPEG or PNG photos, then asks
  before saving the new copies you choose. The originals stay unchanged.
  Restoring a reversible JPEG XL also writes a new JPEG after your review.
  Keeping a copy beside its original uses more space overall, even when the
  copy is smaller.

## What to try

- **Start**: choose your photo folders. Photos appear within seconds; the AI
  then works through them in the background. The status at the bottom left
  says what it is doing; select it for more.
- **Search** (Ctrl+F): try what is in a photo ("beach at sunset", "birthday
  cake", "receipt") and people's names once you have named them.
- **People**: name a few people, merge duplicates, and correct mistakes.
- **Things, Places, Events, Albums, Duplicates**: browse and correct them.
- **Videos** are off by default: turn them on in Settings → Library and folders
  if you like.
- **Keep working as usual.** Lumen should not slow your PC down. It pauses
  while you play a game or watch something in full screen, and while other
  programs keep the processor busy. Settings → Indexing and performance has these
  switches, how many processor threads Lumen may use, and an option to index
  only while you are away.
- **Customise**: try the appearance, image viewing, video and shortcut settings.
  Settings → Backup and presets lets you preview a customisation file before
  applying it, and reset sections while keeping your folders, photos and PIN.
- **Camera RAW**: try photos from your camera. Lumen can use an embedded
  preview or develop the sensor data locally. Tell us the camera model and
  whether a photo opens, has the right orientation and shows useful detail.
  A developed RAW may look darker or flatter than the camera's JPEG.
- **Lossless compression**: select a JPEG or PNG and choose **Compress
  losslessly…**, or open **Tools → Compression**. Analyse first, review the
  measured sizes and preservation checks, then save selected copies. Try a
  bulk selection, pause/cancel and reopening Lumen with an unfinished review.
  **Optimise existing format** keeps JPEG and PNG; **Reversible JPEG to
  JPEG XL** makes a JPEG XL from which the original JPEG can be restored byte
  for byte; **Convert losslessly to JPEG XL** does the same for eligible PNG
  photos. Other programs may not open JPEG XL.

## What to tell us

- Anything confusing, slow or wrong: a person's photos mixed up with
  someone else's, a search that finds nothing, a photo shown on the wrong
  date or the wrong way round.
- How long indexing took, roughly, and how many photos you have.
- Whether your PC stayed responsive while Lumen worked.
- Any crash. If Lumen closes unexpectedly, it says so the next time it
  starts.
- A camera RAW that cannot be read, or an unexpected compression skip,
  incorrect copy or restoration failure. Include the format, dimensions,
  chosen mode and the explanation Lumen shows. Do not send private photos
  unless you choose to share them separately.

### Sending a problem report

Settings → Updates and about → **About Lumen** → **Save a problem report…** saves a zip file
wherever you choose. It holds Lumen's log, any crash reports, and a
description of your PC (Windows version, processor, memory, graphics card)
and of your library's state (how many photos, what is left to do, your
settings). It holds no photos or thumbnails.

The log does name files and folders, for example a photo Lumen could not
read. Open the zip and look it over before you send it to us. Nothing is
sent by Lumen itself.

**Open log folder** in the same place shows the log and crash reports
directly.

## Uninstalling

Use **Settings → Apps** in Windows, or the uninstaller in the install
folder. It asks whether to remove your Lumen library too. Keep it if you
might install Lumen again: your names, albums and everything Lumen learned
come back. Your photos themselves are never touched either way.

## Known limits of this beta

- It is not signed yet, so Windows warns before installing it.
- In-app updates are switched off until releases are signed: Settings →
  Updates and about says so. Install a new version over the old one. New
  versions are published at <https://github.com/awaisjameel/lumen/releases>.
- English only.
- Camera RAW support varies by camera and compression mode. DNG with
  lossy-JPEG sensor data or deflate compression is not read by this build.
  Developed RAW viewing copies are limited to 4096 pixels on the long side;
  the viewer reports the available resolution. RAW originals are not
  compressed or changed.
- Compression accepts supported static JPEG/PNG sources up to 16 million
  pixels and 256 MiB. Larger photos, animations, other formats and ambiguous
  metadata are skipped with an explanation. PNG-to-JPEG-XL conversion is
  limited to plain RGB/RGBA images without ancillary metadata.
- Compression history keeps the newest 100 runs. Older review candidates
  are removed at start-up; keep any copies you want before their runs age
  out. A publication still awaiting recovery is retained.
- Very large JPEG XL viewing copies may be shown without their colour
  profile. Same-format replacement and Undo for compression are not included;
  this beta saves new copies only.
