"""Generated conformance inputs, with no downloaded or personal photographs."""

from __future__ import annotations

import shutil
import sys
from pathlib import Path

from PIL import Image, ImageCms, ImageDraw


def create(folder: Path) -> None:
    folder.mkdir(parents=True, exist_ok=True)
    screen = Image.new("RGBA", (1600, 1000), (13, 26, 51, 255))
    draw = ImageDraw.Draw(screen)
    for y in range(0, 1000, 40):
        draw.rectangle((100, y, 1500, y + 20), fill=(y % 256, 180, 120, 255))
    draw.rectangle((0, 0, 99, 999), fill=(125, 33, 171, 0))
    screen.save(folder / "screenshot.png", compress_level=0)
    screen.save(folder / "already-optimised.png", optimize=True)
    screen.save(folder / "unsupported.bmp")
    profile = ImageCms.ImageCmsProfile(ImageCms.createProfile("sRGB")).tobytes()
    screen.save(folder / "profile.png", compress_level=0, icc_profile=profile)
    photo = screen.convert("RGB")
    exif = Image.Exif()
    exif[274] = 6
    exif[315] = "Conformance fixture author"
    exif[33432] = "Conformance fixture copyright"
    exif[271] = "Fixture camera"
    exif[34665] = {36867: "2024:03:05 12:34:56", 42036: "Fixture lens"}
    photo.save(folder / "photo.jpg", quality=95, exif=exif, icc_profile=profile)
    scan = Image.new("L", (1600, 1000), 245)
    draw = ImageDraw.Draw(scan)
    for y in range(80, 930, 30):
        draw.text((80, y), "Generated scan: exact greyscale samples must remain unchanged.", fill=20)
    scan.save(folder / "scan.png", compress_level=0)
    shutil.copyfile(folder / "photo.jpg", folder / "duplicate.jpg")
    (folder / "photo.jpg.xmp").write_bytes(b"external XMP fixture: keep these exact bytes")


if __name__ == "__main__":
    create(Path(sys.argv[1]))
