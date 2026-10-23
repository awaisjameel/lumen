#!/usr/bin/env bash
# Checks the documentation without starting Lumen: the link checker's own
# regressions, every relative link and anchor, and the published screenshots
# against their capture record.
set -euo pipefail
cd "$(dirname "$0")/.."

python scripts/check_links_test.py
python scripts/check_links.py
python e2e/docs/check.py
