"""Check the shipped runtime's DLL imports: python scripts/runtime_imports.py [RUNTIME].

Every executable and DLL under RUNTIME (deps/runtime by default) must find each
DLL it imports, directly or delay-loaded, either in its own folder, which
Windows searches before System32, or among the DLLs Windows 10 and later
include. A developer PC often has more in System32 (the Visual C++ runtime,
for one), so a passing app there does not prove a clean PC has what it needs.
The check reads the files only; it loads none of them.
"""

import struct
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

# Part of every Windows 10 and later installation. API sets (api-ms-win-*) are
# resolved by the loader itself; the Universal CRT behind api-ms-win-crt-* is
# part of Windows 10. The Visual C++ runtime (msvcp140*, vcruntime140*) is not.
WINDOWS = frozenset(
    {
        "advapi32.dll",
        "avicap32.dll",
        "avrt.dll",
        "bcrypt.dll",
        "bcryptprimitives.dll",
        "cfgmgr32.dll",
        "crypt32.dll",
        "d2d1.dll",
        "d3d12.dll",
        "dbghelp.dll",
        "dnsapi.dll",
        "dwrite.dll",
        "dxgi.dll",
        "gdi32.dll",
        "iphlpapi.dll",
        "kernel32.dll",
        "msimg32.dll",
        "msvcrt.dll",
        "ncrypt.dll",
        "ntdll.dll",
        "ole32.dll",
        "oleaut32.dll",
        "rpcrt4.dll",
        "secur32.dll",
        "setupapi.dll",
        "shell32.dll",
        "shlwapi.dll",
        "user32.dll",
        "userenv.dll",
        "usp10.dll",
        "winmm.dll",
        "ws2_32.dll",
    }
)


def imports(binary: Path) -> set[str]:
    """The lower-case DLL names binary imports, directly or delay-loaded."""
    data = binary.read_bytes()
    pe = struct.unpack_from("<I", data, 0x3C)[0]
    if data[:2] != b"MZ" or data[pe : pe + 4] != b"PE\0\0":
        raise ValueError(f"{binary} is not a PE file")
    sections, optional_size = struct.unpack_from("<H12xH", data, pe + 6)
    optional = pe + 24
    directories = optional + (112 if struct.unpack_from("<H", data, optional)[0] == 0x20B else 96)
    table = optional + optional_size

    def offset(rva: int) -> int:
        for i in range(sections):
            size, address, raw_size, raw = struct.unpack_from("<4I", data, table + 40 * i + 8)
            if address <= rva < address + max(size, raw_size):
                return raw + rva - address
        raise ValueError(f"{binary}: RVA {rva:#x} is outside its sections")

    def name(rva: int) -> str:
        start = offset(rva)
        return data[start : data.index(b"\0", start)].decode("ascii").lower()

    found: set[str] = set()
    # Import descriptors are 20 bytes with the name at 12; delay-load ones are
    # 32 bytes with the name at 4. Each table ends with an all-zero entry.
    for index, size, name_at in ((1, 20, 12), (13, 32, 4)):
        rva = struct.unpack_from("<I", data, directories + 8 * index)[0]
        if not rva:
            continue
        entry = offset(rva)
        while any(data[entry : entry + size]):
            found.add(name(struct.unpack_from("<I", data, entry + name_at)[0]))
            entry += size
    return found


def missing(runtime: Path) -> list[str]:
    """Each import Windows would not find on a clean PC, as 'binary: dll'."""
    problems = []
    for binary in sorted(p for p in runtime.rglob("*") if p.suffix.lower() in {".dll", ".exe"}):
        beside = {p.name.lower() for p in binary.parent.iterdir()}
        for dll in sorted(imports(binary)):
            if dll not in beside and dll not in WINDOWS and not dll.startswith("api-ms-win-"):
                problems.append(f"{binary.relative_to(runtime).as_posix()}: {dll}")
    return problems


def main() -> None:
    runtime = Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "deps" / "runtime"
    if not runtime.is_dir():
        raise SystemExit(f"runtime_imports: no runtime at {runtime}; run scripts/deps.sh")
    if problems := missing(runtime):
        print("imports neither shipped beside them nor part of Windows:", *problems, sep="\n  ")
        raise SystemExit(1)


if __name__ == "__main__":
    main()
