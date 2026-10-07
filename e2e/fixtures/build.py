"""Generates the files the end-to-end suites run on. No personal photo is involved.

    python build.py library <out> <count>   dated synthetic JPEGs, in year/month folders
    python build.py videos  <out>           one video WebView2 plays directly, two it cannot
    python build.py media   <out>           photos, a large TIFF, a HEIC, videos with tracks
    python build.py formats <out> <raw>     RAW, JPEG XL, APNG, paged TIFF, older video containers

Everything is deterministic, so two builds of Lumen index exactly the same
library. Needs Pillow, and the FFmpeg that scripts/deps.sh fetches.
"""

import io
import os
import random
import shutil
import struct
import subprocess
import sys
from datetime import datetime, timedelta
from pathlib import Path

from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[2]
FFMPEG = ROOT / "deps" / "runtime" / "ffmpeg" / "ffmpeg.exe"

# The camera files of the formats fixture: one of each family Lumen reads,
# and a lossy-JPEG DNG, which it must keep without a preview.
ACCEPTANCE_RAW = [
    "Canon_EOS_10D_crw_2127.crw",
    "Nikon_D70_20170902_0047.NEF",
    "Sony_ILCE-7M3_UNCOMPRESSED_14bit.ARW",
    "Fujifilm_X-T3_AFXT2720.RAF",
    "Apple_iPhone_12_Pro_IMG_1361.DNG",
    "Panasonic_DC-GH5_T012010.RW2",
    "Canon_Canon_EOS_R10_IMG_6310.CR3",
    "OM_Digital_Solutions_OM-3_P5048182.ORF",
    "Pentax_K-5_II_IMGP2033.PEF",
    "Canon_EOS_5D_Mark_II_IMG_0051.CR2",
    "Nikon_Coolpix_A1000_DSCN0417.NRW",
    "Sony_DSC-R1_DSC1477.SR2",
    "Sony_DSC-F828_DSC06227.SRF",
    "Adobe_DNG_Converter_Canon_EOS_5D_Mark_III_5G4A9394-compressed-lossy.DNG",
]


def ffmpeg(*args: str) -> None:
    subprocess.run([str(FFMPEG), "-v", "error", "-y", *args], check=True)  # noqa: S603


# --- library ----------------------------------------------------------------

W, H = 2048, 1536


def picture(rng: random.Random, n: int) -> Image.Image:
    # A small random image scaled up gives smooth, photo-like gradients that
    # compress like a photo; shapes on top make every file's content distinct.
    small = Image.new("RGB", (8, 6))
    small.putdata([(rng.randrange(256), rng.randrange(256), rng.randrange(256)) for _ in range(48)])
    img = small.resize((W, H), Image.Resampling.BICUBIC)
    d = ImageDraw.Draw(img)
    for _ in range(rng.randrange(3, 9)):
        x, y = rng.randrange(W), rng.randrange(H)
        r = rng.randrange(60, 420)
        colour = (rng.randrange(256), rng.randrange(256), rng.randrange(256))
        if rng.random() < 0.5:
            d.ellipse((x - r, y - r, x + r, y + r), fill=colour)
        else:
            d.rectangle((x - r, y - r // 2, x + r, y + r // 2), fill=colour)
    d.text((40, 40), f"#{n:05d}", fill=(255, 255, 255))
    if rng.random() < 0.25:  # some portraits
        img = img.transpose(Image.Transpose.ROTATE_90)
    return img


def library(out: Path, count: int) -> None:
    rng = random.Random(7)  # noqa: S311 - a fixed seed is the point
    start = datetime(2021, 1, 1, 9, 0, 0)
    for n in range(count):
        # Bursts of a few dozen photos on a day, then a gap: days of very
        # different sizes, as real libraries have.
        day = start + timedelta(days=(n // 24) * 5 + rng.randrange(3), minutes=n % 24 * 7)
        folder = out / f"{day.year}" / f"{day.month:02d}"
        folder.mkdir(parents=True, exist_ok=True)
        img = picture(rng, n)
        exif = Image.Exif()
        stamp = day.strftime("%Y:%m:%d %H:%M:%S")
        exif[0x0132] = stamp
        exif[0x010F] = "Lumen"
        exif[0x0110] = "Synthetic"
        exif.get_ifd(0x8769)[0x9003] = stamp
        img.save(folder / f"IMG_{n:05d}.jpg", "JPEG", quality=86, exif=exif)
    print(f"{count} photos in {out}")


# --- videos -----------------------------------------------------------------


def videos(out: Path) -> None:
    tone = ["-f", "lavfi", "-i", "sine=frequency=440:duration=12"]
    ffmpeg("-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30:duration=12", *tone,
           "-c:v", "libopenh264", "-b:v", "2M", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest",
           str(out / "direct-h264.mp4"))  # fmt: skip
    ffmpeg("-f", "lavfi", "-i", "smptebars=size=640x480:rate=25:duration=12", *tone,
           "-c:v", "mpeg4", "-q:v", "5", "-c:a", "mp3", "-shortest", str(out / "convert-mpeg4.avi"))  # fmt: skip
    ffmpeg("-f", "lavfi", "-i", "testsrc=size=640x360:rate=24:duration=10",
           "-c:v", "mpeg4", "-q:v", "5", "-an", str(out / "convert-silent.mkv"))  # fmt: skip
    print(f"3 videos in {out}")


# --- media ------------------------------------------------------------------


def media(out: Path) -> None:
    (out / "Moved").mkdir()
    for i in range(12):
        image = Image.new("RGB", (1800, 1200), (25 + i * 16, 75, 140))
        draw = ImageDraw.Draw(image)
        draw.rectangle((0, 0, 450, 350), fill=(240, 80, 45))
        draw.rectangle((1350, 850, 1800, 1200), fill=(50, 220, 95))
        draw.text((650, 560), f"LUMEN / {i}", fill="white", font_size=60)
        exif = Image.Exif()
        exif[274] = 6 if i == 0 else 1  # photo-00 was taken with the camera turned
        image.save(out / f"photo-{i:02}.jpg", quality=92, exif=exif)
    large = Image.new("RGB", (6000, 4000), (30, 115, 180))
    ImageDraw.Draw(large).text((2000, 1800), "6000 x 4000 TIFF", fill="white", font_size=140)
    large.save(out / "large.tiff", compression="tiff_lzw")
    shutil.copy(ROOT / "internal" / "media" / "testdata" / "red-p3.jpg", out / "red-p3.jpg")
    # A real HEIC, where the Phase 0 spikes downloaded one (spikes/fetch.sh).
    heics = sorted((ROOT / "spikes" / ".cache" / "heic").glob("*.heic"))
    if heics:
        shutil.copy(heics[0], out / "sample.heic")
    subtitle = out / "captions.srt"
    subtitle.write_text(
        "1\n00:00:00,000 --> 00:00:03,000\nFirst caption\n\n2\n00:00:04,000 --> 00:00:07,000\nSecond caption\n",
        encoding="utf-8",
    )
    for name, subtitle_codec in [("tracks.mp4", "mov_text"), ("tracks.mkv", "subrip")]:
        ffmpeg("-f", "lavfi", "-i", "testsrc2=size=640x360:rate=12",
               "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
               "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-i", str(subtitle),
               "-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s",
               "-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=fra", "-metadata:s:s:0", "language=eng",
               "-t", "8", "-c:v", "libopenh264", "-b:v", "500k", "-c:a", "aac", "-c:s", subtitle_codec,
               str(out / name))  # fmt: skip
    print(f"media fixture in {out}" + ("" if heics else " (no HEIC sample: run spikes/fetch.sh for one)"))


# --- formats ----------------------------------------------------------------


def ifd(buf: bytearray, tags: list[tuple[int, int, int, bytes]]) -> tuple[int, dict[int, int]]:
    """Appends tags (id, type, count, value; in ascending order) as a TIFF directory.

    Returns where the directory starts and where each tag's value field is,
    for offsets that are only known later.
    """
    start = len(buf)
    buf += struct.pack("<H", len(tags)) + bytes(12 * len(tags) + 4)
    fields = {}
    for i, (tag, kind, count, data) in enumerate(tags):
        at = start + 2 + 12 * i
        struct.pack_into("<HHI", buf, at, tag, kind, count)
        if len(data) <= 4:
            buf[at + 8 : at + 8 + len(data)] = data
        else:
            if len(buf) % 2:
                buf += b"\0"
            struct.pack_into("<I", buf, at + 8, len(buf))
            buf += data
        fields[tag] = at + 8
    return start, fields


def short(tag: int, value: int) -> tuple[int, int, int, bytes]:
    return tag, 3, 1, struct.pack("<H", value)


def long(tag: int, value: int) -> tuple[int, int, int, bytes]:
    return tag, 4, 1, struct.pack("<I", value)


def ascii_tag(tag: int, text: str) -> tuple[int, int, int, bytes]:
    return tag, 2, len(text) + 1, text.encode() + b"\0"


def paged_tiff(*pages: tuple[int, int, int]) -> bytes:
    """A TIFF with one 40x30 page of each colour, in order (as internal/testenv's TIFF)."""
    w, h = 40, 30
    buf = bytearray(b"II\x2a\x00\x08\x00\x00\x00")
    following = 4  # where the offset of the next directory goes
    for colour in pages:
        if len(buf) % 2:
            buf += b"\0"
        struct.pack_into("<I", buf, following, len(buf))
        tags = [
            long(256, w), long(257, h), (258, 3, 3, struct.pack("<HHH", 8, 8, 8)), short(259, 1),
            short(262, 2), long(273, 0), short(277, 3), long(278, h), long(279, w * h * 3),
        ]  # fmt: skip
        start, fields = ifd(buf, tags)
        following = start + 2 + 12 * len(tags)
        struct.pack_into("<I", buf, fields[273], len(buf))
        buf += bytes(colour) * (w * h)
    return bytes(buf)


def cut_dng() -> bytes:
    """A small camera RAW (Adobe DNG) that ends halfway through its sensor data.

    It cannot be developed, but its embedded green JPEG preview is whole (as
    internal/testenv's DNG{Cut: true}).
    """
    w, h = 64, 48
    preview = io.BytesIO()
    Image.new("RGB", (160, 120), (0, 200, 0)).save(preview, "JPEG")
    jpeg = preview.getvalue()
    identity = b"".join(struct.pack("<II", 1 if i % 4 == 0 else 0, 1) for i in range(9))  # the sensor's colours are XYZ
    neutral = struct.pack("<II", 1, 1) * 3
    buf = bytearray(b"II\x2a\x00\x08\x00\x00\x00")
    _, main = ifd(buf, [
        long(254, 0), long(256, w), long(257, h), short(258, 16),
        short(259, 1), short(262, 32803),  # uncompressed, a colour filter array
        ascii_tag(271, "Lumen"), ascii_tag(272, "Test Camera"),
        long(273, 0), short(274, 1), short(277, 1), long(278, h),
        long(279, w * h * 2), short(284, 1), ascii_tag(306, "2024:03:05 10:20:30"), long(330, 0),
        (33421, 3, 2, bytes([2, 0, 2, 0])), (33422, 1, 4, bytes([0, 1, 1, 2])),  # the 2x2 pattern: R G / G B
        (50706, 1, 4, bytes([1, 4, 0, 0])), (50707, 1, 4, bytes([1, 1, 0, 0])),
        ascii_tag(50708, "Lumen Test Camera"), long(50717, 65535),
        (50721, 10, 9, identity), (50728, 5, 3, neutral), short(50778, 21),
    ])  # fmt: skip
    sub_start, sub = ifd(buf, [
        long(254, 1), long(256, 160), long(257, 120), (258, 3, 3, struct.pack("<HHH", 8, 8, 8)),
        short(259, 7), short(262, 6), long(273, 0), short(277, 3), long(278, 120),
        long(279, len(jpeg)), short(284, 1),
    ])  # fmt: skip
    struct.pack_into("<I", buf, main[330], sub_start)
    # The preview goes before the sensor data, as cameras write it, so a file
    # cut short keeps it.
    struct.pack_into("<I", buf, sub[273], len(buf))
    buf += jpeg
    struct.pack_into("<I", buf, main[273], len(buf))
    for y in range(h):
        for x in range(w):
            red, blue = y % 2 == 0 and x % 2 == 0, y % 2 == 1 and x % 2 == 1
            lit = (x < w // 2 and red) or (x >= w // 2 and blue)
            buf += struct.pack("<H", 50000 if lit else 3000)
    return bytes(buf[: len(buf) - w * h])


def formats(out: Path, raw: Path) -> None:
    for name in ACCEPTANCE_RAW:
        source = raw / name
        if not source.exists():
            sys.exit(f"{source} is missing: run e2e/fixtures/fetch_raw.py --acceptance")
        try:
            os.link(source, out / name)  # tens of megabytes each: no second copy
        except OSError:
            shutil.copy(source, out / name)
    clip = ["-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25",
            "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "6"]  # fmt: skip
    ffmpeg(*clip, "-c:v", "mpeg1video", "-c:a", "mp2", str(out / "film.mpg"))
    ffmpeg(*clip, "-c:v", "mpeg2video", "-c:a", "mp2", "-f", "vob", str(out / "dvd.mpeg"))
    ffmpeg(*clip, "-c:v", "mpeg2video", "-c:a", "mp2", "-f", "mpegts", str(out / "recording.ts"))
    ffmpeg(*clip, "-c:v", "libtheora", "-q:v", "6", "-c:a", "libvorbis", str(out / "clip.ogv"))
    ffmpeg(*clip, "-c:v", "wmv2", "-c:a", "wmav2", str(out / "old.asf"))
    ffmpeg("-f", "lavfi", "-i", "color=red:size=150x200,format=rgb24",
           "-f", "lavfi", "-i", "color=blue:size=150x200,format=rgb24",
           "-filter_complex", "hstack", "-frames:v", "1", "-c:v", "libjxl", "-distance", "0",
           str(out / "picture.jxl"))  # fmt: skip
    ffmpeg("-f", "lavfi", "-i", "testsrc2=size=240x160:rate=5", "-t", "2", "-plays", "0", "-f", "apng",
           str(out / "spinner.apng"))  # fmt: skip
    ffmpeg("-f", "lavfi", "-i", "testsrc2=size=800x600:rate=1", "-frames:v", "1", str(out / "photo.jpg"))
    # A TypeScript source is not a transport stream: the scanner must leave it out.
    (out / "main.ts").write_bytes(b"export const answer: number = 42;\n")
    (out / "cut.dng").write_bytes(cut_dng())
    (out / "scan.tiff").write_bytes(paged_tiff((220, 0, 0), (0, 220, 0), (0, 0, 220)))
    print(f"formats fixture in {out}")


def main() -> None:
    if len(sys.argv) < 3 or not FFMPEG.exists():
        sys.exit(__doc__ if len(sys.argv) < 3 else f"{FFMPEG} is missing: run scripts/deps.sh")
    kind, out = sys.argv[1], Path(sys.argv[2])
    out.mkdir(parents=True, exist_ok=True)
    if kind == "library":
        library(out, int(sys.argv[3]))
    elif kind == "videos":
        videos(out)
    elif kind == "media":
        media(out)
    elif kind == "formats":
        formats(out, Path(sys.argv[3]))
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
