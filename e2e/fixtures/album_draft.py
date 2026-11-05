"""Refuse one class of album I/O in this suite's scratch library only."""

import argparse
import sqlite3
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["refuse-read", "restore-read", "refuse-save", "restore-save"])
    parser.add_argument("database", type=Path)
    args = parser.parse_args()
    with sqlite3.connect(args.database, timeout=20) as db:
        db.create_function("fold", 1, lambda text: str(text).casefold(), deterministic=True)
        if args.action == "refuse-read":
            db.execute("ALTER TABLE albums RENAME COLUMN identity TO unavailable_identity")
        elif args.action == "restore-read":
            db.execute("ALTER TABLE albums RENAME COLUMN unavailable_identity TO identity")
        elif args.action == "refuse-save":
            db.execute("""CREATE TRIGGER refuse_album_draft BEFORE UPDATE OF name, rule_json ON albums
                BEGIN SELECT RAISE(ABORT, 'album draft write refused'); END""")
        else:
            db.execute("DROP TRIGGER refuse_album_draft")


if __name__ == "__main__":
    main()
