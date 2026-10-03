"""Exercise the actual NSIS payload and cleanup macros in isolated folders.

    python scripts/installer_payload_test.py

These tiny installers write no registry entries or shortcuts and never touch
Lumen's library. They verify cleanup without packing the full model payload.
"""

from __future__ import annotations

import os
import subprocess
import tempfile
import unittest
from pathlib import Path

from installer_payload import nsis, payload

ROOT = Path(__file__).resolve().parent.parent
MAKENSIS = ROOT / "deps" / "tools" / "nsis" / "makensis.exe"
PROJECT = ROOT / "build" / "windows" / "nsis" / "project.nsi"


def macro(name: str) -> str:
    """Use the production macro verbatim, rather than a copy of its rules."""
    source = PROJECT.read_text(encoding="utf-8")
    start = source.index(f"!macro {name}")
    end = source.index("!macroend", start) + len("!macroend")
    return source[start:end]


@unittest.skipUnless(MAKENSIS.is_file(), "run scripts/deps.sh for NSIS")
class InstallerPayloadTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="lumen-installer-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.runtime = self.root / "runtime"
        self.installed = self.root / "installed"
        self.runtime.mkdir()
        self.installed.mkdir()
        (self.runtime / "models").mkdir()
        (self.runtime / "lib.dll").write_text("library", encoding="utf-8")
        (self.runtime / "models" / "old-é.bin").write_text("model", encoding="utf-8")

    def run_installer(self, install: bool) -> subprocess.CompletedProcess[str]:
        """Compile and run the real macros, directed only at this test's folder."""
        include = self.root / "payload.nsh"
        include.write_text(payload(self.runtime), encoding="utf-8")
        exe = self.root / ("install.exe" if install else "remove.exe")
        script = self.root / "test.nsi"
        action = (
            'SetOutPath "$INSTDIR"\n'
            'FileOpen $R9 "$INSTDIR\\${LUMEN_INVENTORY}" w\n'
            "!insertmacro LUMEN_RUNTIME_FILES\nFileClose $R9"
            if install
            else "Call RemoveRuntime"
        )
        inventory = next(
            line
            for line in PROJECT.read_text(encoding="utf-8").splitlines()
            if line.startswith("!define LUMEN_INVENTORY ")
        )
        script.write_text(
            'Unicode true\nName "Lumen cleanup test"\nRequestExecutionLevel user\nSilentInstall silent\n'
            f'OutFile "{nsis(str(exe))}"\n!include "LogicLib.nsh"\n!include "FileFunc.nsh"\n'
            f"{inventory}\n{macro('LUMEN_PAYLOAD_CHECK')}\n{macro('LUMEN_REMOVE_RUNTIME')}\n"
            f'!insertmacro LUMEN_REMOVE_RUNTIME ""\n!include "{nsis(str(include))}"\n'
            f'Section\nStrCpy $INSTDIR "{nsis(str(self.installed))}"\n{action}\nSectionEnd\n',
            encoding="utf-8",
        )
        # Both executables and all arguments are test-owned paths, never user input.
        subprocess.run([str(MAKENSIS), "-V2", str(script)], check=True, capture_output=True, text=True)  # noqa: S603
        return subprocess.run([str(exe), "/S"], check=False, capture_output=True, text=True, timeout=30)  # noqa: S603

    def test_cleanup_preserves_unrelated_files_and_nonempty_folders(self) -> None:
        self.assertEqual(self.run_installer(install=True).returncode, 0)
        foreign = [self.installed / "foreign.dll", self.installed / "models" / "my-photo.jpg"]
        for path in foreign:
            path.write_text("keep", encoding="utf-8")
        self.assertEqual(self.run_installer(install=False).returncode, 0)
        self.assertFalse((self.installed / "lib.dll").exists())
        self.assertFalse((self.installed / "models" / "old-é.bin").exists())
        for path in foreign:
            self.assertEqual(path.read_text(encoding="utf-8"), "keep")

    def test_upgrade_removes_files_absent_from_the_new_payload(self) -> None:
        self.assertEqual(self.run_installer(install=True).returncode, 0)
        (self.runtime / "models" / "old-é.bin").unlink()
        (self.runtime / "models" / "new.bin").write_text("new model", encoding="utf-8")
        self.assertEqual(self.run_installer(install=False).returncode, 0)
        self.assertEqual(self.run_installer(install=True).returncode, 0)
        self.assertFalse((self.installed / "models" / "old-é.bin").exists())
        self.assertEqual((self.installed / "models" / "new.bin").read_text(encoding="utf-8"), "new model")

    def test_inventory_cannot_delete_outside_the_installation_or_use_wildcards(self) -> None:
        outside = self.root / "outside.txt"
        outside.write_text("keep", encoding="utf-8")
        for entry in ["F..\\outside.txt", "F*.dll", "Fmodels\\..\\..\\outside.txt"]:
            with self.subTest(entry=entry):
                inventory = self.installed / ".lumen-runtime-files.txt"
                inventory.write_text(entry + "\r\n", encoding="utf-16-le")
                self.assertEqual(self.run_installer(install=False).returncode, 4)
                self.assertEqual(outside.read_text(encoding="utf-8"), "keep")
                self.assertTrue(inventory.exists())

    def test_cleanup_refuses_a_junction_to_another_folder(self) -> None:
        self.assertEqual(self.run_installer(install=True).returncode, 0)
        model = self.installed / "models" / "old-é.bin"
        model.unlink()
        model.parent.rmdir()
        outside = self.root / "outside"
        outside.mkdir()
        (outside / "old-é.bin").write_text("keep", encoding="utf-8")
        # mklink is a Windows shell builtin; these are only this test's paths.
        cmd = Path(os.environ["SYSTEMROOT"]) / "System32" / "cmd.exe"
        subprocess.run(  # noqa: S603
            [str(cmd), "/c", "mklink", "/J", str(model.parent), str(outside)],
            check=True,
            capture_output=True,
        )
        self.assertEqual(self.run_installer(install=False).returncode, 4)
        self.assertEqual((outside / "old-é.bin").read_text(encoding="utf-8"), "keep")


if __name__ == "__main__":
    unittest.main()
