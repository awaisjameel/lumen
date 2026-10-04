"""Write a tiny uncompressed CFA DNG with a JPEG SubIFD, never a camera fixture."""
from pathlib import Path
import struct

ROOT = Path(__file__).resolve().parents[2]
OUT = Path(__file__).resolve().parent / "fixtures" / "synthetic-cfa.dng"
JPEG = (ROOT / "spikes" / ".cache" / "camera" / "JPEG.jpg").read_bytes()
buf = bytearray(b"II\x2a\x00\x08\x00\x00\x00")
sizes = {1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 10: 8}


def ifd(tags):
    """Append a TIFF IFD and return inline field addresses for later patching."""
    start = len(buf)
    tags = sorted(tags)
    buf.extend(struct.pack("<H", len(tags)))
    buf.extend(bytes(12 * len(tags) + 4))
    fields = {}
    for i, (tag, kind, count, data) in enumerate(tags):
        pos = start + 2 + 12 * i
        if len(data) != sizes[kind] * count:
            raise ValueError("Incorrect tag size")
        if len(data) <= 4:
            value = data.ljust(4, b"\x00")
        else:
            if len(buf) % 2:
                buf.append(0)
            value = struct.pack("<I", len(buf))
            buf.extend(data)
        buf[pos:pos + 12] = struct.pack("<HHI", tag, kind, count) + value
        fields[tag] = pos + 8
    return start, fields


def short(tag, value):
    return tag, 3, 1, struct.pack("<H", value)


def long(tag, value):
    return tag, 4, 1, struct.pack("<I", value)


width, height = 64, 48
tags = [long(254, 0), long(256, width), long(257, height), short(258, 16),
        short(259, 1), short(262, 32803), long(273, 0), short(274, 1),
        short(277, 1), long(278, height), long(279, width * height * 2),
        short(284, 1), long(330, 0), (33421, 3, 2, struct.pack("<HH", 2, 2)),
        (33422, 1, 4, bytes([0, 1, 1, 2])),
        (50706, 1, 4, bytes([1, 4, 0, 0])), (50707, 1, 4, bytes([1, 1, 0, 0])),
        (50708, 2, 21, b"Phase7 synthetic CFA\x00"),
        (50710, 1, 3, bytes([0, 1, 2])), short(50711, 1), long(50717, 65535)]
_, main = ifd(tags)
preview_tags = [long(254, 1), long(256, 1000), long(257, 563),
                (258, 3, 3, struct.pack("<HHH", 8, 8, 8)), short(259, 7),
                short(262, 6), long(273, 0), short(277, 3), long(278, 563),
                long(279, len(JPEG)), short(284, 1)]
preview_offset, preview = ifd(preview_tags)
struct.pack_into("<I", buf, main[330], preview_offset)
struct.pack_into("<I", buf, main[273], len(buf))
for y in range(height):
    for x in range(width):
        buf.extend(struct.pack("<H", 1000 + x * 500 + y * 300))
struct.pack_into("<I", buf, preview[273], len(buf))
buf.extend(JPEG)
OUT.write_bytes(buf)
print(f"synthetic-cfa.dng bytes={len(buf)} CFA={width}x{height} JPEG_SubIFD=1000x563")
