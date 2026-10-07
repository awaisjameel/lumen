"""Runs lumen-raw over every camera RAW sample and records time, peak memory and output.

    python raw_matrix.py [name-part ...]   only the samples whose name holds a part

Reads e2e/.cache/raw (fixtures/fetch_raw.py fills it) and writes
e2e/.work/results/raw-matrix/matrix.json, with a developed JPEG and the
embedded preview of each sample beside it, for looking at.
"""

import ctypes
import ctypes.wintypes as wt
import json
import os
import subprocess
import sys
import time
from pathlib import Path

E2E = Path(__file__).resolve().parents[1]
RUNTIME = E2E.parent / "deps" / "runtime"
EXE = RUNTIME / "lumen-raw.exe"
FFMPEG = RUNTIME / "ffmpeg" / "ffmpeg.exe"
RAW = Path(os.environ.get("LUMEN_E2E_CACHE", E2E / ".cache")) / "raw"
OUT = Path(os.environ.get("LUMEN_E2E_WORK", E2E / ".work")) / "results" / "raw-matrix"


class Counters(ctypes.Structure):
    _fields_ = (
        ("cb", wt.DWORD),
        ("PageFaultCount", wt.DWORD),
        ("PeakWorkingSetSize", ctypes.c_size_t),
        ("WorkingSetSize", ctypes.c_size_t),
        ("QuotaPeakPagedPoolUsage", ctypes.c_size_t),
        ("QuotaPagedPoolUsage", ctypes.c_size_t),
        ("QuotaPeakNonPagedPoolUsage", ctypes.c_size_t),
        ("QuotaNonPagedPoolUsage", ctypes.c_size_t),
        ("PagefileUsage", ctypes.c_size_t),
        ("PeakPagefileUsage", ctypes.c_size_t),
    )


def run(*args: str) -> tuple[int, bytes, str, int, int]:
    """One lumen-raw call: exit code, output, error text, milliseconds, peak commit in MB."""
    start = time.perf_counter()
    proc = subprocess.Popen([str(EXE), *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE)  # noqa: S603
    out, err = proc.communicate(timeout=180)
    ms = (time.perf_counter() - start) * 1000
    c = Counters()
    c.cb = ctypes.sizeof(c)
    ctypes.windll.psapi.GetProcessMemoryInfo(wt.HANDLE(int(proc._handle)), ctypes.byref(c), c.cb)
    return proc.returncode, out, err.decode("ascii", "replace").strip(), round(ms), round(c.PeakPagefileUsage / 2**20)


def split(out: bytes) -> tuple[dict, bytes]:
    head, _, body = out.partition(b"\n")
    return json.loads(head), body


def develop(f: Path, side: int) -> dict:
    code, out, err, ms, mem = run("develop", str(f), str(side))
    r = {"exit": code, "ms": ms, "commit_mb": mem}
    if code:
        return r | {"err": err}
    head, body = split(out)
    r.update(head)
    r["ok"] = len(body) == head["width"] * head["height"] * 3
    size = f"{head['width']}x{head['height']}"
    subprocess.run(  # noqa: S603
        [str(FFMPEG), "-v", "error", "-y", "-f", "rawvideo", "-pix_fmt", "rgb24", "-s", size, "-i", "-",
         "-vf", "scale=1024:-1", "-frames:v", "1", "-q:v", "3", str(OUT / f"{f.stem}-{side}.jpg")],
        input=body,
        check=False,
    )  # fmt: skip
    return r


def preview(f: Path) -> dict:
    code, out, err, ms, mem = run("preview", str(f))
    r = {"exit": code, "ms": ms, "commit_mb": mem}
    if code:
        return r | {"err": err}
    head, body = split(out)
    r.update(head)
    r["jpeg_bytes"] = len(body)
    (OUT / f"{f.stem}-preview.jpg").write_bytes(body)
    return r


def main() -> None:
    if not EXE.exists() or not RAW.is_dir():
        sys.exit(f"needs {EXE} (scripts/deps.sh) and {RAW} (e2e/fixtures/fetch_raw.py)")
    OUT.mkdir(parents=True, exist_ok=True)
    rows = []
    only = sys.argv[1:]
    for f in sorted(RAW.iterdir()):
        if only and not any(o.lower() in f.name.lower() for o in only):
            continue
        row = {"file": f.name, "bytes": f.stat().st_size}
        code, out, err, ms, _ = run("info", str(f))
        row["info"] = split(out)[0] if code == 0 else {"exit": code, "err": err}
        row["info_ms"] = ms
        d1 = row["develop1024"] = develop(f, 1024)
        d4 = row["develop4096"] = develop(f, 4096)
        p = row["preview"] = preview(f)
        rows.append(row)
        failed = d1["exit"] or d4["exit"] or p["exit"]
        print(
            f"{f.name[:44]:44} info={row['info'].get('width')}x{row['info'].get('height')}"
            f" d1024={d1['exit']}/{d1['ms']}ms/{d1['commit_mb']}MB/{d1.get('width')}x{d1.get('height')}"
            f" d4096={d4['exit']}/{d4['ms']}ms/{d4['commit_mb']}MB/{d4.get('width')}x{d4.get('height')}"
            f" prev={p['exit']}/{p['ms']}ms/{p.get('width')}x{p.get('height')}/o{p.get('orientation')}"
            + (f" ERR {d1.get('err') or d4.get('err') or p.get('err')}" if failed else ""),
            flush=True,
        )
    (OUT / "matrix.json").write_text(json.dumps(rows, indent=1), encoding="utf-8")


if __name__ == "__main__":
    main()
