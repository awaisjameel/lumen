"""Fetches CC0 camera RAW samples from raw.pixls.us into a folder, once.

    python fetch_raw.py <out>                all 32 samples (about 1.2 GB), for the Go tests
    python fetch_raw.py --acceptance <out>   the 14 the formats suite uses (about 360 MB)

Each sample is pinned in raw-samples.sha256 by its path in the archive and its
SHA-256; a file already there with the right size is not downloaded again.
"""

import hashlib
import re
import sys
import urllib.parse
import urllib.request
from pathlib import Path

from build import ACCEPTANCE_RAW

BASE = "https://raw.pixls.us/data/"
LIST = Path(__file__).with_name("raw-samples.sha256")


def flat_name(name: str) -> str:
    """The archive's maker/model/file path as one file name."""
    parts = name.split("/")
    return re.sub(r"[^A-Za-z0-9.-]+", "_", f"{parts[0]}_{parts[-2]}_{parts[-1]}")


def main() -> int:
    args = [a for a in sys.argv[1:] if a != "--acceptance"]
    if len(args) != 1:
        sys.exit(__doc__)
    out = Path(args[0])
    out.mkdir(parents=True, exist_ok=True)
    wanted = set(ACCEPTANCE_RAW) if "--acceptance" in sys.argv else None
    failed = 0
    for line in LIST.read_text(encoding="utf-8").splitlines():
        digest, _, name = line.partition(" ")
        name = name.lstrip(" *")
        flat = flat_name(name)
        if wanted is not None and flat not in wanted:
            continue
        target = out / flat
        if target.exists():
            continue
        print(f"fetching {name}", flush=True)
        with urllib.request.urlopen(BASE + urllib.parse.quote(name), timeout=300) as response:
            data = response.read()
        if hashlib.sha256(data).hexdigest() != digest:
            print(f"BAD HASH {name}", file=sys.stderr)
            failed += 1
            continue
        target.write_bytes(data)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
