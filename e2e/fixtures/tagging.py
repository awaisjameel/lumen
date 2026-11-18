"""A synthetic document and SQLite fault fixtures for the tagging service boundary."""

import argparse
import json
import sqlite3
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont


def document(folder: Path) -> None:
    folder.mkdir(parents=True, exist_ok=True)
    image = Image.new("RGB", (1200, 700), "white")
    draw = ImageDraw.Draw(image)
    font_file = Path("C:/Windows/Fonts/arial.ttf")
    font = ImageFont.truetype(str(font_file), 72) if font_file.exists() else ImageFont.load_default(size=72)
    draw.text((80, 120), "INVOICE LUMEN", fill="black", font=font)
    draw.text((80, 250), "Payment received", fill="black", font=font)
    for number in (1, 2):
        copy = image.copy()
        ImageDraw.Draw(copy).text((80, 400), f"Reference {number}", fill="black", font=font)
        copy.save(folder / f"document-{number}.png")


def library(path: Path, action: str, asset: int) -> None:
    with sqlite3.connect(path, timeout=20) as db:
        db.create_function("fold", 1, lambda text: str(text).casefold(), deterministic=True)
        if action == "prepare":
            # Only the fixture's AI guesses are removed, before manual tagging.
            db.execute("DELETE FROM asset_tags WHERE source = 'ai'")
        elif action == "refuse-tag":
            db.execute("""CREATE TRIGGER refuse_tag_write BEFORE INSERT ON asset_tags
                WHEN NEW.source = 'user' BEGIN SELECT RAISE(ABORT, 'tag write unavailable'); END""")
        elif action == "allow-tag":
            db.execute("DROP TRIGGER refuse_tag_write")
        elif action == "refuse-queue":
            db.execute("""CREATE TRIGGER refuse_ocr_queue BEFORE INSERT ON asset_jobs
                WHEN NEW.stage = 'ocr' BEGIN SELECT RAISE(ABORT, 'queue unavailable'); END""")
        elif action == "allow-queue":
            db.execute("DROP TRIGGER refuse_ocr_queue")
        elif action == "state":
            tags = db.execute(
                """SELECT t.name, x.source FROM asset_tags x
                JOIN tags t ON t.id = x.tag_id WHERE x.asset_id = ? ORDER BY t.id""",
                (asset,),
            ).fetchall()
            jobs = db.execute("SELECT status FROM asset_jobs WHERE asset_id = ? AND stage = 'ocr'", (asset,)).fetchall()
            text = db.execute("SELECT text FROM ocr_text WHERE asset_id = ?", (asset,)).fetchone()
            print(json.dumps({"tags": tags, "ocr": [row[0] for row in jobs], "text": text[0] if text else ""}))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "action", choices=["document", "prepare", "refuse-tag", "allow-tag", "refuse-queue", "allow-queue", "state"]
    )
    parser.add_argument("path", type=Path)
    parser.add_argument("asset", type=int, nargs="?", default=0)
    args = parser.parse_args()
    if args.action == "document":
        document(args.path)
    else:
        library(args.path, args.action, args.asset)


if __name__ == "__main__":
    main()
