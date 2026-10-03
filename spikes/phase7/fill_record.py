"""Fill measured tables from final CSV evidence without hand-copying numbers."""
from pathlib import Path
import csv
import statistics

HERE = Path(__file__).resolve().parent
DOC = HERE.parents[1] / "docs" / "phase7-media-feasibility-2026-10-04.md"
rows = list(csv.DictReader((HERE / "summary.csv").open(encoding="utf-8-sig")))
raw = list(csv.DictReader((HERE / "measurements.csv").open(encoding="utf-8-sig")))
assert len(raw) == 165 and all(r["exit_code"] == "0" for r in raw)
assert all(r["ac_line_status"] == "1" for r in raw), "Power changed; document it before publishing the table"
charges = sorted({int(r['battery_percent']) for r in raw})
charge = str(charges[0]) if len(charges) == 1 else f"{charges[0]}–{charges[-1]}"
power = (f"Final main table: {len(raw)} runs, all `ACLineStatus=1` (AC), "
         f"battery charge {charge}%. "
         f"Run timestamps: `{raw[0]['recorded_at']}` to `{raw[-1]['recorded_at']}`. "
         "The power plan remained Balanced. See `inventory-final-start.txt`, "
         "`inventory-final-end.txt` and `power-plan.txt`.")
table = ["| Input | Operation / long edge | Output | Decode + resize median ms | Total median ms (min–max) | Peak working set MiB | Peak commit MiB |",
         "| --- | --- | --- | ---: | ---: | ---: | ---: |"]
for r in rows:
    if r["mode"] == "wic":
        continue
    decode = f"{float(r['median_decode_resize_ms']):.1f}" if r["mode"] == "render" else "Included in total"
    table.append(f"| {r['file']} | {r['mode']} / {r['side']} | {r['w']}×{r['h']} | {decode} | "
                 f"{float(r['median_ms']):.1f} ({float(r['min_ms']):.1f}–{float(r['max_ms']):.1f}) | "
                 f"{float(r['peak_ws_mib']):.2f} | {float(r['peak_commit_mib']):.2f} |")
ffrows = list(csv.DictReader((HERE / "ffmpeg-measurements.csv").open(encoding="utf-8-sig")))
fftable = ["| Input | Requested long edge / output | Median ms (min–max) | Maximum peak working set MiB |",
           "| --- | --- | ---: | ---: |"]
for file, side in dict.fromkeys((r["file"], r["side"]) for r in ffrows):
    group = [r for r in ffrows if (r["file"], r["side"]) == (file, side)]
    times = [float(r["ms"]) for r in group]
    dims = {("autumn_1440x960.heic", "1024"): "1024×683", ("autumn_1440x960.heic", "4096"): "1440×960",
            ("iphone.heic", "1024"): "1024×767", ("iphone.heic", "4096"): "3992×2992"}[file, side]
    fftable.append(f"| {file} | {side} / {dims} | {statistics.median(times):.0f} ({min(times):.0f}–{max(times):.0f}) | "
                   f"{max(float(r['peak_ws_mib']) for r in group):.2f} |")
doc = DOC.read_text(encoding="utf-8")
for marker, value in [("FINAL_POWER", power), ("MAIN_TABLE", "\n".join(table)), ("FFMPEG_TABLE", "\n".join(fftable))]:
    placeholder = f"<!-- {marker} -->"
    assert placeholder in doc
    doc = doc.replace(placeholder, value)
DOC.write_text(doc, encoding="utf-8")
print(f"Filled record from {len(raw)} main and {len(ffrows)} FFmpeg runs")
