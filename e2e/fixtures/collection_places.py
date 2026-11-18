"""Temporarily refuse place-name reads in this suite's scratch library only."""

import argparse
import sqlite3
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["refuse", "restore"])
    parser.add_argument("database", type=Path)
    args = parser.parse_args()
    with sqlite3.connect(args.database, timeout=20) as db:
        db.create_function("fold", 1, lambda text: str(text).casefold(), deterministic=True)
        if args.action == "refuse":
            db.execute("ALTER TABLE places RENAME COLUMN name TO unavailable_name")
        else:
            db.execute("ALTER TABLE places RENAME COLUMN unavailable_name TO name")


if __name__ == "__main__":
    main()
