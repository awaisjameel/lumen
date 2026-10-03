"""Check that the final feasibility record matches the retained measurements."""
from pathlib import Path
import csv
import json

HERE = Path(__file__).resolve().parent
doc = (HERE.parents[1] / "docs" / "phase7-media-feasibility-2026-10-04.md").read_text(encoding="utf-8")
for heading in ["Scope and machine", "Decoder inventory (evidence)", "RAW/DNG findings",
                "Rendered-image measurements", "Recommended budgets and cache key",
                "Capability contract review", "Untested items and blockers"]:
    assert f"## {heading}" in doc
assert "<!--" not in doc and "\ufffd" not in doc and "â€“" not in doc
rows = list(csv.DictReader((HERE / "measurements.csv").open(encoding="utf-8-sig")))
assert len(rows) == 165
groups = {}
for row in rows:
    assert row["exit_code"] == "0" and row["ac_line_status"] == "1"
    assert int(row["peak_ws_bytes"]) > 0 and int(row["peak_commit_bytes"]) > 0
    groups.setdefault((row["file"], row["mode"], row["side"]), []).append(row)
assert len(groups) == 33 and all(len(group) == 5 for group in groups.values())
summaries = list(csv.DictReader((HERE / "summary.csv").open(encoding="utf-8-sig")))
for row in summaries:
    if row["mode"] == "wic":
        continue
    evidence = (f"{float(row['median_ms']):.1f} ({float(row['min_ms']):.1f}–{float(row['max_ms']):.1f}) | "
                f"{float(row['peak_ws_mib']):.2f} | {float(row['peak_commit_mib']):.2f}")
    assert evidence in doc, row
ffrows = list(csv.DictReader((HERE / "ffmpeg-measurements.csv").open(encoding="utf-8-sig")))
assert len(ffrows) == 20
raw = [json.loads(line) for line in (HERE / "measurements.jsonl").read_text(encoding="utf-8-sig").splitlines() if line.strip()]
assert len(raw) == len(rows)
print("Record verified: 7 sections, 33 cases x 5 successful AC runs, 20 FFmpeg runs, table values match CSV")
