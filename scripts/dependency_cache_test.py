"""Check warm dependency extraction without downloading or changing deps/."""

import os
import shutil
import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path

SCRIPT = Path(__file__).with_name("dependency-cache.sh").resolve().as_posix()


class DependencyCacheTests(unittest.TestCase):
    def setUp(self) -> None:
        self.scratch = tempfile.TemporaryDirectory()
        self.addCleanup(self.scratch.cleanup)
        self.root = Path(self.scratch.name)
        self.archive = self.root / "dependency.zip"
        self.destination = self.root / "extracted"

    def archive_with(self, version: str) -> None:
        with zipfile.ZipFile(self.archive, "w") as archive:
            archive.writestr("upstream/bin/library.dll", version)
            archive.writestr("upstream/LICENSE", "licence " + version)

    def unpack(self, fingerprint: str) -> subprocess.CompletedProcess[bytes]:
        return subprocess.run(  # noqa: S603 - Only the repository test script and scratch paths are passed to Git Bash.
            [
                shutil.which("bash") or "bash",
                "-c",
                'source "$CACHE_SCRIPT"; unpack_dependency_zip "$ARCHIVE" "$DESTINATION" upstream "$FINGERPRINT"',
            ],
            env={
                **os.environ,
                "CACHE_SCRIPT": SCRIPT,
                "ARCHIVE": self.archive.as_posix(),
                "DESTINATION": self.destination.as_posix(),
                "FINGERPRINT": fingerprint,
            },
            capture_output=True,
            check=False,
        )

    def test_a_new_pin_replaces_an_existing_library_and_its_notices(self) -> None:
        self.archive_with("old")
        self.assertEqual(self.unpack("first-hash").returncode, 0)
        (self.destination / "obsolete.dll").write_bytes(b"old")
        self.archive_with("new")
        self.assertEqual(self.unpack("second-hash").returncode, 0)
        self.assertEqual((self.destination / "bin/library.dll").read_bytes(), b"new")
        self.assertEqual((self.destination / "LICENSE").read_bytes(), b"licence new")
        self.assertFalse((self.destination / "obsolete.dll").exists())
        self.assertEqual((self.destination / ".lumen-archive").read_bytes(), b"second-hash\n")

    def test_a_matching_pin_keeps_the_existing_extraction(self) -> None:
        self.archive_with("original")
        self.assertEqual(self.unpack("same-hash").returncode, 0)
        self.archive.unlink()
        self.assertEqual(self.unpack("same-hash").returncode, 0)
        self.assertEqual((self.destination / "bin/library.dll").read_bytes(), b"original")

    def test_a_failed_extraction_preserves_the_previous_build_and_retries(self) -> None:
        self.archive_with("old")
        self.assertEqual(self.unpack("first-hash").returncode, 0)
        self.archive.write_bytes(b"damaged archive")
        self.assertNotEqual(self.unpack("new-hash").returncode, 0)
        self.assertEqual((self.destination / "bin/library.dll").read_bytes(), b"old")
        self.assertEqual((self.destination / ".lumen-archive").read_bytes(), b"first-hash\n")
        self.assertEqual(list(self.root.glob("extracted.unpack.*")), [])
        self.archive_with("repaired")
        self.assertEqual(self.unpack("new-hash").returncode, 0)
        self.assertEqual((self.destination / "bin/library.dll").read_bytes(), b"repaired")

    def test_an_unstamped_extraction_is_replaced(self) -> None:
        self.destination.mkdir()
        (self.destination / "old.dll").write_bytes(b"old")
        self.archive_with("current")
        self.assertEqual(self.unpack("current-hash").returncode, 0)
        self.assertFalse((self.destination / "old.dll").exists())


if __name__ == "__main__":
    unittest.main()
