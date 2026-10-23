"""Check published screenshot integrity without needing Pillow or a browser."""

import hashlib
import json
import re
import struct
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SHOTS = ROOT / "docs" / "screenshots"


def dimensions(data: bytes) -> tuple[int, int]:
    """Read lossy, lossless and extended WebP headers used by capture tools."""
    if data[:4] != b"RIFF" or data[8:12] != b"WEBP":
        raise ValueError("not a WebP image")
    chunk = data[12:16]
    if chunk == b"VP8 ":
        width, height = struct.unpack_from("<HH", data, 26)
        return width & 0x3FFF, height & 0x3FFF
    if chunk == b"VP8X":
        return int.from_bytes(data[24:27], "little") + 1, int.from_bytes(data[27:30], "little") + 1
    if chunk == b"VP8L" and data[20] == 0x2F:
        bits = int.from_bytes(data[21:25], "little")
        return (bits & 0x3FFF) + 1, ((bits >> 14) & 0x3FFF) + 1
    raise ValueError(f"unsupported WebP header: {chunk!r}")


def main() -> None:
    record = json.loads((SHOTS / "capture.json").read_text(encoding="utf-8"))
    media = ROOT / "e2e" / "docs" / "media.json"
    if record["mediaManifestSHA256"] != hashlib.sha256(media.read_bytes()).hexdigest():
        raise SystemExit("screenshot media manifest changed: recapture or review its provenance")
    # Each image is either captured by capture.mjs or kept from an earlier,
    # recorded capture; both are pinned by hash.
    entries = record["screenshots"] + record["kept"]
    listed = [entry["file"] for entry in entries]
    if len(set(listed)) != len(listed) or set(listed) != {path.name for path in SHOTS.glob("*.webp")}:
        raise SystemExit("screenshot inventory does not match capture.json")
    gallery = (SHOTS / "README.md").read_text(encoding="utf-8")
    sizes = set()
    for entry in entries:
        data = (SHOTS / entry["file"]).read_bytes()
        if hashlib.sha256(data).hexdigest() != entry["sha256"]:
            raise SystemExit(f"screenshot bytes changed: {entry['file']}")
        sizes.add(dimensions(data))
        if len(data) > 350_000:
            raise SystemExit(f"optimise screenshot before publishing: {entry['file']}")
        if not re.search(r"!\[[^\]]+\]\(" + re.escape(entry["file"]) + r"\)", gallery):
            raise SystemExit(f"missing gallery reference or descriptive alt text: {entry['file']}")
    if len(sizes) != 1:
        raise SystemExit(f"screenshots differ in framing: {sorted(sizes)}")
    print(f"{len(listed)} screenshots: hashes, framing, sizes, gallery and media provenance agree")


if __name__ == "__main__":
    main()
