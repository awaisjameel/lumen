"""Measure pinned encoders on generated fixtures; conformance lives in Go tests.

Run from the repository root: python e2e/perf/compression.py.
Use --no-zopfli to recheck the shipped presets without the rejected long search.
Reports complete process time and Windows peak private commit, including exit,
input/output size and the exact arguments. No source file is modified.
"""

from __future__ import annotations

import ctypes
import ctypes.wintypes as wt
import hashlib
import json
import subprocess
import sys
import time
from pathlib import Path

from raw_matrix import Counters

ROOT = Path(__file__).resolve().parents[2]
OUT = ROOT / "e2e" / ".work" / "results" / "compression-benchmark"
TOOLS = ROOT / "deps" / "runtime" / "compress"


def run(source: Path, tool: str, label: str, args: list[str], output: Path) -> dict:
    start = time.perf_counter()
    proc = subprocess.Popen(  # noqa: S603
        [str(TOOLS / f"{tool}.exe"), *args], stdout=subprocess.DEVNULL, stderr=subprocess.PIPE
    )
    timed_out = False
    try:
        _, diagnostics = proc.communicate(timeout=300)
    except subprocess.TimeoutExpired:
        timed_out = True
        proc.kill()
        _, diagnostics = proc.communicate()
    counters = Counters()
    counters.cb = ctypes.sizeof(counters)
    ctypes.windll.psapi.GetProcessMemoryInfo(wt.HANDLE(int(proc._handle)), ctypes.byref(counters), counters.cb)
    row = {
        "source": source.name,
        "tool": tool,
        "toolSHA256": hashlib.sha256((TOOLS / f"{tool}.exe").read_bytes()).hexdigest(),
        "search": label,
        "arguments": args,
        "inputBytes": source.stat().st_size,
        "outputBytes": output.stat().st_size if output.exists() else 0,
        "milliseconds": round((time.perf_counter() - start) * 1000),
        "peakPrivateMB": round(counters.PeakPagefileUsage / 2**20, 1),
        "exit": proc.returncode,
        "timeout": timed_out,
    }
    if proc.returncode:
        row["diagnostics"] = diagnostics.decode("utf-8", "replace")[-500:]
    print(json.dumps(row), flush=True)
    return row


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    fixtures = OUT / "inputs"
    subprocess.run([sys.executable, str(ROOT / "e2e" / "fixtures" / "compression.py"), str(fixtures)], check=True)  # noqa: S603
    rows = []
    for name in ["screenshot.png", "scan.png", "already-optimised.png"]:
        source = fixtures / name
        levels = ["1", "2", "6"] if "--no-zopfli" in sys.argv else ["1", "2", "6", "zopfli"]
        for level in levels:
            output = OUT / f"{source.stem}-{level}.png"
            output.unlink(missing_ok=True)
            args = ["--nx", "--interlace", "keep", "--threads", "1", "--opt", "6" if level == "zopfli" else level]
            if level == "zopfli":
                args.append("--zopfli")
            rows.append(run(source, "oxipng", level, [*args, "--out", str(output), str(source)], output))
    source = fixtures / "photo.jpg"
    for progressive in [False, True]:
        output = OUT / f"photo-{progressive}.jpg"
        args = ["-copy", "all", "-optimize", "-outfile", str(output)]
        if progressive:
            args.append("-progressive")
        rows.append(run(source, "jpegtran", "progressive" if progressive else "baseline", [*args, str(source)], output))
    for name in ["photo.jpg", "screenshot.png"]:
        source = fixtures / name
        for effort in ["3", "7", "9"]:
            output = OUT / f"{source.stem}-{effort}.jxl"
            args = [str(source), str(output), "--distance=0", "--num_threads=1", f"--effort={effort}", "--container=1"]
            if source.suffix == ".jpg":
                args.extend(["--lossless_jpeg=1", "--allow_jpeg_reconstruction=1"])
            else:
                args.extend(["--alpha_distance=0", "--keep_invisible=1", "--premultiply=0"])
            rows.append(run(source, "cjxl", effort, args, output))
    (OUT / "matrix.json").write_bytes((json.dumps(rows, indent=2) + "\n").encode())


if __name__ == "__main__":
    main()
