"""Build the small, bundled offline city index from a pinned GeoNames snapshot.

Run after placing the three files in deps/cache. The output is committed so
installing and using Lumen never downloads location data.
"""

from __future__ import annotations

import csv
import gzip
import hashlib
import io
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
CACHE = ROOT / "deps" / "cache"
EXPECTED = {
    "cities5000.zip": "bee719c05a5b532408c98b2a9c67131185d4007339a7b1cf5ad12e9a9706e043",
    "admin1CodesASCII.txt": "1da92a6323a5fec3176f3f743bf4cf4040fd56a876da55e46fbca23c863aa60a",
    "countryInfo.txt": "93bafc525813f22e4711ff9ed6d626343094ce48c26388dc7c49189b3d7d5512",
}


def checked(name: str) -> Path:
    path = CACHE / name
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != EXPECTED[name]:
        raise ValueError(f"{name}: SHA-256 {digest}, expected {EXPECTED[name]}")
    return path


def main() -> None:
    for name in EXPECTED:
        checked(name)
    admin = {}
    with checked("admin1CodesASCII.txt").open(encoding="utf-8") as source:
        for code, name, *_ in csv.reader(source, delimiter="\t"):
            admin[code] = name
    countries = {}
    with checked("countryInfo.txt").open(encoding="utf-8") as source:
        for row in csv.reader((line for line in source if not line.startswith("#")), delimiter="\t"):
            if len(row) > 4:
                countries[row[0]] = row[4]

    output = ROOT / "internal" / "places" / "cities.tsv.gz"
    output.parent.mkdir(parents=True, exist_ok=True)
    with (
        zipfile.ZipFile(checked("cities5000.zip")) as archive,
        archive.open("cities5000.txt") as raw,
        output.open("wb") as target,
        gzip.GzipFile(filename="", mode="wb", fileobj=target, mtime=0, compresslevel=9) as compressed,
    ):
        text = io.TextIOWrapper(compressed, encoding="utf-8", newline="")
        writer = csv.writer(text, delimiter="\t", lineterminator="\n")
        for row in csv.reader(io.TextIOWrapper(raw, encoding="utf-8"), delimiter="\t"):
            # GeoNames: id, name, ASCII name, alternate names, lat,
            # lon, feature class/code, country, cc2, admin1, ...
            if len(row) < 11 or row[6] != "P":
                continue
            country = countries.get(row[8], row[8])
            region = admin.get(f"{row[8]}.{row[10]}", "")
            writer.writerow((row[0], row[1], row[4], row[5], region, country))
        text.flush()
    print(f"wrote {output} ({output.stat().st_size} bytes)")


if __name__ == "__main__":
    main()
