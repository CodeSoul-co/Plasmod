#!/usr/bin/env python3
"""
Enable and verify vector-only baseline mode.

This runner is intentionally narrow: it validates that the runtime honors
PLASMOD_VECTOR_ONLY_MODE and can optionally export a small machine-readable
report for experiment bookkeeping.
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import pathlib
import subprocess
import sys


ROOT = pathlib.Path(__file__).resolve().parents[2]


def main() -> int:
    parser = argparse.ArgumentParser(description="Enable and verify vector-only baseline mode")
    parser.add_argument("--json", dest="json_path", help="Optional JSON output path")
    parser.add_argument("--csv", dest="csv_path", help="Optional CSV output path")
    args = parser.parse_args()

    cmd = [
        "go",
        "test",
        "./src/internal/worker",
        "-run",
        "TestRuntime_VectorOnlyMode_SkipsGraphAndProvenance",
        "-count=1",
        "-v",
    ]

    env = os.environ.copy()
    env["PLASMOD_VECTOR_ONLY_MODE"] = "true"

    print("Vector-Only Baseline Runner")
    print("vector_only_mode=true")
    print("command:", " ".join(cmd))

    result = subprocess.run(
        cmd,
        cwd=ROOT,
        env=env,
        text=True,
        capture_output=True,
        encoding="utf-8",
        errors="replace",
    )

    combined = "\n".join(part for part in (result.stdout, result.stderr) if part)
    passed = "PASS" in combined and "TestRuntime_VectorOnlyMode_SkipsGraphAndProvenance" in combined
    report = {
        "baseline_mode": "vector_only",
        "env_var": "PLASMOD_VECTOR_ONLY_MODE=true",
        "verification_test": "TestRuntime_VectorOnlyMode_SkipsGraphAndProvenance",
        "status": "enabled" if passed else "failed",
    }

    print(result.stdout, end="")
    if result.stderr:
        print(result.stderr, end="")

    if args.json_path:
        out = pathlib.Path(args.json_path)
        if not out.is_absolute():
            out = ROOT / out
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"json_written: {out}")

    if args.csv_path:
        out = pathlib.Path(args.csv_path)
        if not out.is_absolute():
            out = ROOT / out
        out.parent.mkdir(parents=True, exist_ok=True)
        with out.open("w", encoding="utf-8", newline="") as fh:
            writer = csv.DictWriter(fh, fieldnames=list(report.keys()))
            writer.writeheader()
            writer.writerow(report)
        print(f"csv_written: {out}")

    return 0 if passed else result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
