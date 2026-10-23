"""Prepare the public documentation library, never a user's own photos.

python e2e/docs/fixtures.py --fetch                    download the pinned inputs once
python e2e/docs/fixtures.py <new-output-directory>     arrange them into a library

The inputs are the photos and the portrait pinned in media.json. Only --fetch
downloads, into the ignored e2e/.cache/docs-media; arranging a library, and so
the capture, needs no network. Every input is checked against its SHA-256.
"""

import hashlib
import json
import os
import random
import subprocess
import sys
from datetime import datetime, timedelta
from fractions import Fraction
from pathlib import Path
from urllib.request import urlopen

from PIL import Image, ImageOps

ROOT = Path(__file__).resolve().parents[2]
CACHE = ROOT / "e2e" / ".cache" / "docs-media"
MANIFEST = json.loads(Path(__file__).with_name("media.json").read_text(encoding="utf-8"))
FFMPEG = ROOT / "deps" / "runtime" / "ffmpeg" / "ffmpeg.exe"
# Trips go to real cities so Places has something to show; the dates, the
# cities and which photo went where are all invented.
CITIES = [
    ("Lisbon", 38.7223, -9.1393),
    ("Kyoto", 35.0116, 135.7681),
    ("Reykjavik", 64.1466, -21.9426),
    ("New York", 40.7128, -74.0060),
    ("Cape Town", -33.9249, 18.4241),
    ("Vancouver", 49.2827, -123.1207),
    ("Edinburgh", 55.9533, -3.1883),
    ("Istanbul", 41.0082, 28.9784),
    ("Sydney", -33.8688, 151.2093),
    ("Florence", 43.7696, 11.2558),
    ("Banff", 51.1784, -115.5708),
    ("Marrakesh", 31.6295, -7.9811),
    ("Lahore", 31.5204, 74.3587),
    ("Copenhagen", 55.6761, 12.5683),
]
HOME = ("London", 51.5072, -0.1276)
# The pan video is made from this photo, on the newest day of the library.
VIDEO_PHOTO = "1015"


def cached(name: str, url: str, digest: str, *, fetch: bool) -> Path:
    """The verified cache copy of one pinned input, downloaded only with --fetch."""
    path = CACHE / name
    if not path.exists():
        if not fetch:
            raise SystemExit(f"missing {name}: run fixtures.py --fetch first")
        path.parent.mkdir(parents=True, exist_ok=True)
        # The URL is built exclusively from the checked-in source manifest.
        with urlopen(url, timeout=60) as response:
            path.write_bytes(response.read())
    if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
        raise SystemExit(f"checksum mismatch: {path} (the source changed; review it before re-pinning)")
    return path


def dms(value: float) -> tuple[Fraction, Fraction, Fraction]:
    value = abs(value)
    degrees = int(value)
    minutes = int((value - degrees) * 60)
    seconds = round(((value - degrees) * 60 - minutes) * 60, 2)
    return Fraction(degrees), Fraction(minutes), Fraction(seconds).limit_denominator(100)


def save(image: Image.Image, path: Path, taken: datetime, place: tuple[str, float, float] | None) -> None:
    """Writes a JPEG with only an invented date and place: a new Exif carries nothing of the source's."""
    exif = Image.Exif()
    stamp = taken.strftime("%Y:%m:%d %H:%M:%S")
    exif[0x0132] = stamp
    exif.get_ifd(0x8769)[0x9003] = stamp
    if place:
        _, lat, lon = place
        exif[0x8825] = {1: "N" if lat >= 0 else "S", 2: dms(lat), 3: "E" if lon >= 0 else "W", 4: dms(lon)}
    path.parent.mkdir(parents=True, exist_ok=True)
    image.convert("RGB").save(path, quality=92, exif=exif.tobytes())
    os.utime(path, (taken.timestamp(), taken.timestamp()))


def arrange(ids: list[str]) -> list[tuple[str, datetime, tuple[str, float, float] | None]]:
    """Trips of a few days in one city, with quieter days at home between, newest first."""
    rng = random.Random(7)  # noqa: S311 - a fixed seed is the point
    day = datetime(2026, 10, 18, 9, 0)
    plan = []
    i = 0
    while i < len(ids):
        if rng.random() < 0.45:
            city, count, days = rng.choice(CITIES), rng.randint(18, 45), rng.randint(2, 5)
        else:
            city, count, days = (HOME if rng.random() < 0.6 else None), rng.randint(4, 14), 1
        chunk = ids[i : i + count]
        i += count
        start = day - timedelta(days=days - 1)
        for k, pid in enumerate(chunk):
            taken = start + timedelta(days=(k * days) // max(1, len(chunk)))
            taken = taken.replace(hour=rng.randint(8, 19), minute=rng.randint(0, 59), second=rng.randint(0, 59))
            place = None
            if city:
                name, lat, lon = city
                place = (name, lat + rng.uniform(-0.03, 0.03), lon + rng.uniform(-0.03, 0.03))
            plan.append((pid, taken, place))
        day = start - timedelta(days=rng.randint(3, 16))
    return plan


def inputs(*, fetch: bool) -> Path:
    """Verifies every pinned input, fetching missing ones only when allowed; returns the portrait."""
    for photo in MANIFEST["photos"]:
        cached(f"photos/{photo['id']}.jpg", MANIFEST["download"].format(**photo), photo["sha256"], fetch=fetch)
    portrait = MANIFEST["portrait"]
    return cached(portrait["name"], portrait["download"], portrait["sha256"], fetch=fetch)


def prepare(out: Path) -> None:
    if out.exists():
        raise SystemExit(f"refusing to reuse an output directory: {out}")
    photos = MANIFEST["photos"]
    portrait_path = inputs(fetch=False)

    library = out / "library"
    plan = arrange([photo["id"] for photo in photos])
    for pid, taken, place in plan:
        with Image.open(CACHE / "photos" / f"{pid}.jpg") as image:
            save(image, library / f"{taken:%Y}" / f"{taken:%m}" / f"IMG_{taken:%Y%m%d_%H%M%S}_{pid}.jpg", taken, place)

    # One public-domain portrait, framed five ways on five days, so People has
    # a group to name. It is one photo of one person, not a recognition test.
    with Image.open(portrait_path) as source:
        image = source.convert("RGB")
    frames = [(0, 0, 512, 512), (40, 0, 360, 320), (70, 10, 330, 270), (20, 0, 420, 400), (90, 20, 310, 240)]
    for k, box in enumerate(frames):
        frame = image.crop(box).resize((900, 900), Image.Resampling.LANCZOS)
        if k % 2:
            frame = ImageOps.mirror(frame)
        taken = datetime(2026, 7, 4, 10, 0) + timedelta(days=k * 9, minutes=k * 13)
        save(frame, library / f"{taken:%Y}" / f"{taken:%m}" / f"IMG_{taken:%Y%m%d_%H%M%S}_portrait{k}.jpg", taken, HOME)

    # A short pan across one of the photos, as a phone video would be. A video
    # records UTC, so noon keeps it on the same day in any time zone near it.
    taken = max(when for _, when, _ in plan).replace(hour=12, minute=0, second=0)
    video = library / f"{taken:%Y}" / f"{taken:%m}" / f"VID_{taken:%Y%m%d_%H%M%S}.mp4"
    subprocess.run(  # noqa: S603
        [
            str(FFMPEG),
            "-v",
            "error",
            "-loop",
            "1",
            "-i",
            str(CACHE / "photos" / f"{VIDEO_PHOTO}.jpg"),
            "-vf",
            "scale=1920:-2,crop=1280:720:'(in_w-1280)*t/8':'(in_h-720)/2',format=yuv420p",
            "-t",
            "8",
            "-r",
            "30",
            "-c:v",
            "libopenh264",
            "-b:v",
            "4M",
            "-metadata",
            f"creation_time={taken:%Y-%m-%dT%H:%M:%S}Z",
            str(video),
        ],
        check=True,
    )
    os.utime(video, (taken.timestamp(), taken.timestamp()))
    print(f"{len(plan)} photos, {len(frames)} portrait frames and 1 video prepared in {library}")


if __name__ == "__main__":
    if len(sys.argv) != 2:
        raise SystemExit(__doc__)
    if sys.argv[1] == "--fetch":
        inputs(fetch=True)
        print(f"{len(MANIFEST['photos'])} photos and the portrait verified in {CACHE}")
    else:
        prepare(Path(sys.argv[1]))
