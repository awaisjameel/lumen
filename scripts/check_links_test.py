"""Regressions for real documentation links, screenshots and code examples."""

import tempfile
import unittest
from pathlib import Path

from check_links import anchors, broken


class DocumentationLinks(unittest.TestCase):
    def test_code_examples_cannot_supply_real_heading_anchors(self) -> None:
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "guide.md"
            path.write_bytes(b"# Guide\n~~~sh\n# example\n[ignored](missing.md)\n~~~\n[link](#example)\n")
            self.assertEqual(broken(path), ["6: no heading for #example"])

    def test_repeated_headings_have_github_suffixes(self) -> None:
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "guide.md"
            path.write_bytes(b"# Setup\n## Setup\n[second](#setup-1)\n")
            self.assertEqual(anchors(path), {"setup", "setup-1"})
            self.assertEqual(broken(path), [])

    def test_html_images_are_checked_alongside_markdown_images(self) -> None:
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "guide.md"
            (Path(folder) / "photo.webp").write_bytes(b"fixture")
            path.write_bytes(b'<img src="gone.png" alt="Missing" />\n![Present](photo.webp)\n')
            self.assertEqual(broken(path), ["1: missing gone.png"])

    def test_long_fences_can_contain_shorter_example_fences(self) -> None:
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "guide.md"
            path.write_bytes(b"````md\n```sh\n[ignored](missing.md)\n```\n````\n[real](missing.md)\n")
            self.assertEqual(broken(path), ["6: missing missing.md"])

    def test_unclosed_examples_are_reported(self) -> None:
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "guide.md"
            path.write_bytes(b"# Guide\n```sh\necho example\n")
            self.assertEqual(broken(path), ["2: unclosed code fence"])

    def test_encoded_paths_and_external_links_are_supported(self) -> None:
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "guide.md"
            (Path(folder) / "other guide.md").write_bytes(b"# Getting started\n")
            path.write_bytes(
                b"[local](other%20guide.md#getting%2Dstarted)\n[external](https://example.com/path#anchor)\n"
            )
            self.assertEqual(broken(path), [])


if __name__ == "__main__":
    unittest.main()
