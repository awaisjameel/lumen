"""Scratch-only faces, a scan barrier and a 100-day preview transport fixture."""

import argparse
import json
import shutil
import sqlite3
from pathlib import Path

from PIL import Image, ImageDraw

PREVIEW_SIZE = 3
LARGE_SIZE = 100_000


def marker(folder: Path, label: str) -> None:
    image = Image.new("RGB", (300, 200), "#d4e4fb")
    ImageDraw.Draw(image).text((20, 20), f"Preview scan barrier {label}", fill="black")
    image.save(folder / "preview-scan-barrier.png")


def faces(db: sqlite3.Connection, ids: list[int]) -> None:
    found = []
    for asset in ids:
        row = db.execute(
            """INSERT INTO faces(asset_id, x, y, w, h, score, sharpness, yaw, model, vec)
            VALUES (?, 0.1, 0.1, 0.2, 0.2, 0.9, 20, 0, 'preview-fixture', zeroblob(512))""",
            (asset,),
        )
        found.append(row.lastrowid)
    print(json.dumps(found))


def large(db: sqlite3.Connection, data: Path) -> None:
    # The app has finished scanning a real additional image before injection.
    # Its live watcher sees no media-file changes during this database fixture.
    originals = {
        kind: db.execute(
            """SELECT id FROM assets WHERE kind = ? AND thumb_version > 0 ORDER BY id LIMIT ?""",
            (kind, PREVIEW_SIZE),
        ).fetchall()
        for kind in ("photo", "video")
    }
    if any(len(rows) < PREVIEW_SIZE for rows in originals.values()):
        msg = "the fixture needs three indexed photos and videos"
        raise RuntimeError(msg)
    source, root = db.execute("SELECT id, path FROM sources ORDER BY id LIMIT 1").fetchone()
    db.execute("DELETE FROM assets")
    db.execute(
        """WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x < ?)
        INSERT INTO assets(id, content_hash, kind, ext, taken_at, added_at, width, height,
            thumb_version, favourite, duration_ms, date_from)
        SELECT x, printf('preview-%016x', x), CASE WHEN x % 5 = 0 THEN 'video' ELSE 'photo' END,
            CASE WHEN x % 5 = 0 THEN '.mp4' ELSE '.jpg' END,
            1748736000 + ((x-1)/1000)*86400 + x%1000, 0, 400, 300, 1, x%3 = 0,
            CASE WHEN x % 5 = 0 THEN 12000 ELSE 0 END, 'exif' FROM n""",
        (LARGE_SIZE,),
    )
    db.execute(
        """INSERT INTO files(asset_id, source_id, path, size, mtime)
        SELECT id, ?, ? || '\\preview-metadata-' || id || '.fixture', 1, 0 FROM assets""",
        (source, root),
    )
    # Only samples need actual media/cache files. Other rows measure indexed
    # metadata transport, not decoding, scanning or a real user's library.
    for kind, favourites in (("photo", False), ("video", False), ("video", True)):
        samples = db.execute(
            """SELECT id FROM assets WHERE kind = ? AND (? = 0 OR favourite = 1)
            ORDER BY taken_at DESC, id DESC LIMIT ?""",
            (kind, favourites, PREVIEW_SIZE),
        ).fetchall()
        for (target,), (original,) in zip(samples, originals[kind], strict=True):
            for size in (256, 1024):
                cache = data / "thumbs" / str(size)
                old = cache / f"{original % 256:02x}" / f"{original}.jpg"
                new = cache / f"{target % 256:02x}" / f"{target}.jpg"
                new.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(old, new)
    print(json.dumps({"total": LARGE_SIZE, "photos": 80_000, "favouriteVideos": 6_666, "days": 100}))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["marker", "faces", "large"])
    parser.add_argument("path", type=Path)
    parser.add_argument("argument", nargs="?", default="")
    args = parser.parse_args()
    if args.action == "marker":
        marker(args.path, args.argument)
        return
    with sqlite3.connect(args.path, timeout=30) as db:
        db.execute("PRAGMA foreign_keys = ON")
        db.create_function("fold", 1, lambda value: str(value).casefold(), deterministic=True)
        if args.action == "faces":
            faces(db, json.loads(args.argument))
        else:
            large(db, args.path.parent)


if __name__ == "__main__":
    main()
