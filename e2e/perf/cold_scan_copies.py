"""Writes the uncached copies of a photo folder that BenchmarkColdPhotoScanWorkers reads.

    python cold_scan_copies.py <photos> [out]

The benchmark (internal/scanner) scans each folder once, so each read must come
from the disk rather than Windows' file cache. This writes every file of
<photos> into one folder per worker count and sample the benchmark runs, named
<workers>-<sample>, with FILE_FLAG_NO_BUFFERING and FILE_FLAG_WRITE_THROUGH: the
writes bypass the cache, so nothing of them is kept there. out defaults to
e2e/.work/cold-scan, must not exist yet, and needs 25 times the folder's size.
Then, from the repository root, after `source scripts/env.sh`:

    LUMEN_COLD_SCAN_BENCH_ROOT=<out> go test -run '^$' -bench '^BenchmarkColdPhotoScanWorkers$' \\
        -benchtime=1x -timeout 1800s ./internal/scanner

Each run reads the copies once, so write new ones before the next run.
"""

import ctypes
import ctypes.wintypes as wt
import os
import sys
from pathlib import Path

E2E = Path(__file__).resolve().parents[1]
OUT = Path(os.environ.get("LUMEN_E2E_WORK", E2E / ".work")) / "cold-scan"

# As BenchmarkColdPhotoScanWorkers runs them; change both together.
WORKERS = (2, 4, 8, 16, 20)
SAMPLES = 5

GENERIC_WRITE = 0x40000000
CREATE_NEW = 1
FILE_FLAG_WRITE_THROUGH = 0x80000000
FILE_FLAG_NO_BUFFERING = 0x20000000
MEM_COMMIT_RESERVE = 0x3000
MEM_RELEASE = 0x8000
PAGE_READWRITE = 0x04
# Unbuffered writes must be whole sectors from sector-aligned memory. A page
# (VirtualAlloc's alignment) covers every sector size Windows uses.
ALIGN = 4096
INVALID_HANDLE = ctypes.c_void_p(-1).value

kernel = ctypes.WinDLL("kernel32", use_last_error=True)
kernel.VirtualAlloc.argtypes = (ctypes.c_void_p, ctypes.c_size_t, wt.DWORD, wt.DWORD)
kernel.VirtualAlloc.restype = ctypes.c_void_p
kernel.VirtualFree.argtypes = (ctypes.c_void_p, ctypes.c_size_t, wt.DWORD)
kernel.VirtualFree.restype = wt.BOOL
kernel.CreateFileW.argtypes = (wt.LPCWSTR, wt.DWORD, wt.DWORD, ctypes.c_void_p, wt.DWORD, wt.DWORD, wt.HANDLE)
kernel.CreateFileW.restype = wt.HANDLE
kernel.WriteFile.argtypes = (wt.HANDLE, ctypes.c_void_p, wt.DWORD, ctypes.POINTER(wt.DWORD), ctypes.c_void_p)
kernel.WriteFile.restype = wt.BOOL
kernel.CloseHandle.argtypes = (wt.HANDLE,)
kernel.CloseHandle.restype = wt.BOOL


def write_uncached(path: Path, data: bytes) -> None:
    """Writes data to a new file without the file cache, then trims the sector padding."""
    size = max(ALIGN, -(-len(data) // ALIGN) * ALIGN)
    memory = kernel.VirtualAlloc(None, size, MEM_COMMIT_RESERVE, PAGE_READWRITE)
    if not memory:
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        ctypes.memmove(memory, data, len(data))
        handle = kernel.CreateFileW(
            str(path), GENERIC_WRITE, 0, None, CREATE_NEW, FILE_FLAG_NO_BUFFERING | FILE_FLAG_WRITE_THROUGH, None
        )
        if handle == INVALID_HANDLE:
            raise ctypes.WinError(ctypes.get_last_error())
        try:
            written = wt.DWORD()
            if not kernel.WriteFile(handle, memory, size, ctypes.byref(written), None):
                raise ctypes.WinError(ctypes.get_last_error())
            if written.value != size:
                raise OSError(f"{path}: wrote {written.value} of {size} bytes")
        finally:
            if not kernel.CloseHandle(handle):
                raise ctypes.WinError(ctypes.get_last_error())
    finally:
        kernel.VirtualFree(memory, 0, MEM_RELEASE)
    # Truncating changes only the file's length, so the data stays uncached.
    os.truncate(path, len(data))


def main() -> None:
    if len(sys.argv) not in (2, 3):
        sys.exit(__doc__)
    photos = Path(sys.argv[1]).resolve()
    out = Path(sys.argv[2]).resolve() if len(sys.argv) == 3 else OUT
    if not photos.is_dir():
        sys.exit(f"{photos} is not a folder")
    if out.exists():
        sys.exit(f"{out} exists: the copies are read once, so write them to a new folder")
    if out.is_relative_to(photos):
        sys.exit(f"{out} is inside {photos}")
    files = sorted(p for p in photos.rglob("*") if p.is_file())
    if not files:
        sys.exit(f"{photos} holds no files")
    total = sum(p.stat().st_size for p in files)
    for sample in range(1, SAMPLES + 1):
        for workers in WORKERS:
            folder = out / f"{workers}-{sample}"
            folder.mkdir(parents=True)
            # A number keeps names from subfolders apart; the extension keeps the format.
            for number, original in enumerate(files):
                write_uncached(folder / f"{number:05d}{original.suffix.lower()}", original.read_bytes())
            print(f"{folder.name}: {len(files)} files, {total:,} bytes", flush=True)


if __name__ == "__main__":
    main()
