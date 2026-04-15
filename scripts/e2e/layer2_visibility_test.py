#!/usr/bin/env python3
"""Compatibility wrapper for Member-A Layer2 E2E script."""

from __future__ import annotations

import runpy
from pathlib import Path


def main() -> int:
    target = (
        Path(__file__).resolve().parents[2]
        / "docs"
        / "plasmod-fix"
        / "tools"
        / "layer2_visibility_test.py"
    )
    runpy.run_path(str(target), run_name="__main__")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
