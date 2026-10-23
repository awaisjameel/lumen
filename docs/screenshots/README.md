# Screenshots

Every image here is the **built Windows application** in its own WebView2, on
a scratch library of public sample photos: no one's own library or private
photos. All share one framing, 1440 × 900 CSS pixels at 125 % display scaling
(1800 × 1125 pixels). [capture.json](capture.json) pins each image by SHA-256
and says where it comes from.

- **Seven images were captured on 21 October 2026** (commit `44edd34`), from
  the build of that day: the Library, search, the viewer, Places, Things,
  albums and the dark theme. They are kept as they were.
- **The face review was captured on 24 October 2026** by
  [the capture script](#reproducing-the-face-review), from the build its record
  names, and is the one image the script reproduces.

The dates, the cities each photo is placed in, the albums, the favourites and
the person's name are invented for the screenshots, set through the same
services the interface uses. An image is evidence of the interface on its
capture date, not acceptance of any published installer.

## Library and search

![The Lumen Library: a timeline of photos grouped by day, with the place of each day](library.webp)

The Library groups photos and videos by day, newest first, with the place of
each day and a year rail on the right.

![Search results for snowy mountains at sunrise, best matches first](search.webp)

`snowy mountains at sunrise`, answered by the on-device search model: the best
matches first, then more results.

![Search results for city lights at night in the dark theme](dark.webp)

`city lights at night`, in the dark theme.

## People and face review

![The Faces tab of Sample astronaut with one face selected and the actions for it](faces.webp)

The sample library has one public-domain portrait, framed five ways on five
days. The face model found the faces and grouped them into one person by
itself; the capture then named that person, as a user would. It shows the
flow, not recognition quality: the five frames are one photo of one person.
Selecting a face offers **Not Sample astronaut**, **Move to someone…** and
**Use as their picture**.

## Viewer and collections

![A photo of a coffee cup on a wooden table in the viewer, beside its details panel](viewer.webp)

The best match for `coffee on a wooden table`, with Details open (press `I`).

![Things such as Nature, Mountains and Cities, each with a mosaic of its photos](things.webp)

Things exactly as the AI tagged the library; none was edited.

![Places grouped by city and country on a stylised world atlas](places.webp)

The cities come from the invented locations, not from where the photographers
took the photos.

![Three smart albums, By the sea, City walks and Mountain views, each following a Thing](albums.webp)

Each smart album follows a Thing as the library changes; none moves a file.

## Where the photos come from

[e2e/docs/media.json](../../e2e/docs/media.json) pins the sample photos by
SHA-256:

- **992 photos from [Unsplash](https://unsplash.com/license)**, at 1600 px on
  the long side, as served by [Lorem Picsum](https://picsum.photos). Each entry
  names its photographer and its Unsplash page
  (`https://unsplash.com/photos/<unsplash>`). The Unsplash Licence allows this
  use without attribution; the manifest keeps the credit anyway. Every photo
  visible in these screenshots is one of them. The 21 October library also
  held one more photo from Picsum's list, which has no Unsplash page and so no
  known licence; it appears in none of the images and is left out of the
  manifest.
- **One portrait**, NASA's photograph of astronaut Eileen Collins, public
  domain, from the [scikit-image 0.25.2 data set](https://scikit-image.org/docs/0.25.x/api/skimage.data.html#skimage.data.astronaut).
  It appears as **Sample astronaut**; it implies no endorsement.

`e2e/docs/fixtures.py` arranges the photos into year/month folders and gives
each a new EXIF block with only an invented date and, for trips, a city's GPS
position: nothing of the source files' metadata is kept. It also makes the five
portrait frames and a short video. The inputs stay in the git-ignored
`e2e/.cache/docs-media`; only the screenshots are committed.

## Reproducing the face review

Complete [the development setup](../development.md#setup) first, including the
e2e npm dependencies and Pillow. Then, from the repository root in **Git Bash**:

```sh
export CGO_ENABLED=1 CC=clang CXX=clang++
source scripts/env.sh
wails3 build
# Once: download and verify the pinned photos (about 190 MB) into e2e/.cache/docs-media.
python e2e/docs/fixtures.py --fetch
# The capture itself downloads nothing; it makes a new scratch library each run.
node e2e/docs/capture.mjs
scripts/check-docs.sh
```

`--fetch` is the only step that uses the network, and it is developer tooling:
Lumen itself downloads nothing. It skips inputs already in the cache and
verifies all of them. A source whose bytes no longer match its pinned hash
stops the run; review the change before pinning it again.

The capture uses `bin/Lumen.exe` (or `LUMEN_EXE`), DevTools port **9446** and
its own library under `e2e/.work/docs-capture/<timestamp>/`, indexes it to the
end (a few minutes with a GPU), names the person and closes only its own
process, gracefully. It emulates the gallery's framing whatever the display,
saves `faces.webp` at WebP quality 82 without editing it, and rewrites
`capture.json`, carrying the seven kept images' record over unchanged. A
failed run keeps a `failure.png` and `report.json` in its scratch folder.

`e2e/docs/check.py` (run by `scripts/check-docs.sh` and `scripts/lint.sh`)
checks every image against `capture.json`: its hash, one framing for all, a
size under 350 kB, descriptive alt text on this page, and the media manifest
the capture used. Replacing a kept image means capturing it with a reviewed
script and recording it here, not editing the hash.

Before committing a new capture, look at it for legible controls, loaded
thumbnails and no private path or name, and read its captions here, in
[the README](../../README.md#screenshots) and in [the walkthrough](../using-lumen.md)
against it. Capture again after a change to People or its interface.
