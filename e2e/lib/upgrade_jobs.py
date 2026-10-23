"""Read the upgrade suite's durable OCR jobs without changing its library."""

import json
import sqlite3
import sys
from pathlib import Path
from typing import Never


def schema_fold(_value: object) -> Never:
    raise RuntimeError("queue inspection must not evaluate fold()")


def jobs(data_dir: Path) -> list[dict[str, object]]:
    uri = (data_dir / "library.db").resolve().as_uri() + "?mode=ro"
    connection = sqlite3.connect(uri, uri=True, timeout=5)
    try:
        # SQLite validates the expression indexes when opening the schema.
        # This query never evaluates a name or path comparison through fold.
        connection.create_function("fold", 1, schema_fold, deterministic=True)
        connection.row_factory = sqlite3.Row
        return [
            dict(row)
            for row in connection.execute(
                "SELECT asset_id, status, model_version, error FROM asset_jobs WHERE stage = 'ocr' ORDER BY asset_id"
            )
        ]
    finally:
        connection.close()


if __name__ == "__main__":
    print(json.dumps(jobs(Path(sys.argv[1]))))
