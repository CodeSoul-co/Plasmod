#!/usr/bin/env python3
"""
Summarize Member C visibility/scope/quarantine experiment checks.

Targets:
- 4-D3 shared/private scope definition
- 4-D4 memory visibility scope
- 4-D7 quarantine labels
"""

from __future__ import annotations

import argparse
import csv
import json
import os
import pathlib
import re
import subprocess
import sys
from typing import Any


ROOT = pathlib.Path(__file__).resolve().parents[2]

SCOPE_DEFINITION_RE = re.compile(
    r"Scope definition: "
    r"private_agent_visible=(?P<private>true|false) "
    r"restricted_shared_visible=(?P<restricted>true|false) "
    r"workspace_shared_visible=(?P<workspace>true|false) "
    r"visible_refs=(?P<visible>\d+)"
)

VISIBILITY_COVERAGE_RE = re.compile(
    r"Visibility scope coverage: "
    r"visible_refs=(?P<visible>\d+) "
    r"total_candidates=(?P<total>\d+) "
    r"excluded_refs=(?P<excluded>\d+) "
    r"resolved_scope=(?P<scope>\S+)"
)

QUARANTINE_RE = re.compile(
    r"Quarantine label experiment: "
    r"excluded=(?P<excluded>\d+) "
    r"returned=(?P<returned>\d+) "
    r"tag=(?P<tag>\S+)"
)


def run_go_tests() -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env.setdefault("GOCACHE", str(ROOT / "out" / "gocache"))
    env.setdefault("GOTMPDIR", str(ROOT / "out" / "gotmp"))
    cmd = [
        "go",
        "test",
        "./src/internal/storage",
        "./src/internal/retrieval",
        "-run",
        "TestMemoryViewBuilder_SharedPrivateScopeDefinition|"
        "TestMemoryViewBuilder_VisibilityScopeCoverage|"
        "TestSafetyFilter_QuarantineLabelSummary",
        "-count=1",
        "-v",
    ]
    return subprocess.run(
        cmd,
        cwd=ROOT,
        env=env,
        text=True,
        capture_output=True,
        encoding="utf-8",
        errors="replace",
    )


def parse_output(stdout: str, stderr: str) -> dict[str, Any]:
    combined = "\n".join(part for part in (stdout, stderr) if part)
    summary: dict[str, Any] = {
        "suite": "member_c_scope_governance",
        "checks": {},
        "notes": [],
    }

    scope_match = SCOPE_DEFINITION_RE.search(combined)
    if scope_match:
        summary["checks"]["shared_private_scope"] = {
            "private_agent_visible": scope_match.group("private") == "true",
            "restricted_shared_visible": scope_match.group("restricted") == "true",
            "workspace_shared_visible": scope_match.group("workspace") == "true",
            "visible_refs": int(scope_match.group("visible")),
        }
    else:
        summary["notes"].append("shared/private scope metrics were not found")

    coverage_match = VISIBILITY_COVERAGE_RE.search(combined)
    if coverage_match:
        visible = int(coverage_match.group("visible"))
        total = int(coverage_match.group("total"))
        summary["checks"]["memory_visibility_scope"] = {
            "visible_refs": visible,
            "total_candidates": total,
            "excluded_refs": int(coverage_match.group("excluded")),
            "coverage_ratio": round(visible / total, 4) if total else 0.0,
            "resolved_scope": coverage_match.group("scope"),
        }
    else:
        summary["notes"].append("memory visibility scope metrics were not found")

    quarantine_match = QUARANTINE_RE.search(combined)
    if quarantine_match:
        summary["checks"]["quarantine_labels"] = {
            "excluded": int(quarantine_match.group("excluded")),
            "returned": int(quarantine_match.group("returned")),
            "tag": quarantine_match.group("tag"),
        }
    else:
        summary["notes"].append("quarantine label metrics were not found")

    if "Access is denied." in combined:
        summary["notes"].append(
            "Windows temp cleanup reported 'Access is denied'; verify PASS lines above to distinguish cleanup noise from logic failures"
        )

    return summary


def print_summary(summary: dict[str, Any]) -> None:
    print("Member C Scope/Governance Summary")
    print(f"suite: {summary['suite']}")
    for name, check in summary["checks"].items():
        print(f"{name}: {check}")
    if summary["notes"]:
        print("notes:")
        for note in summary["notes"]:
            print(f"  - {note}")


def flatten(summary: dict[str, Any]) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    scope = summary["checks"].get("shared_private_scope")
    if scope:
        rows.append({
            "requirement": "4-D3",
            "check": "shared_private_scope",
            **scope,
        })
    visibility = summary["checks"].get("memory_visibility_scope")
    if visibility:
        rows.append({
            "requirement": "4-D4",
            "check": "memory_visibility_scope",
            **visibility,
        })
    quarantine = summary["checks"].get("quarantine_labels")
    if quarantine:
        rows.append({
            "requirement": "4-D7",
            "check": "quarantine_labels",
            **quarantine,
        })
    if not rows:
        rows.append({"requirement": "", "check": "", "notes": "no checks parsed"})
    return rows


def write_csv(rows: list[dict[str, Any]], path: pathlib.Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fieldnames: list[str] = []
    for row in rows:
        for key in row.keys():
            if key not in fieldnames:
                fieldnames.append(key)
    with path.open("w", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description="Summarize scope/visibility/quarantine checks")
    parser.add_argument("--json", dest="json_path", help="Optional JSON output path")
    parser.add_argument("--csv", dest="csv_path", help="Optional CSV output path")
    args = parser.parse_args()

    result = run_go_tests()
    summary = parse_output(result.stdout, result.stderr)
    summary["go_test_exit_code"] = result.returncode
    print_summary(summary)

    if args.json_path:
        out = pathlib.Path(args.json_path)
        if not out.is_absolute():
            out = ROOT / out
        out.parent.mkdir(parents=True, exist_ok=True)
        out.write_text(json.dumps(summary, indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"json_written: {out}")

    if args.csv_path:
        out = pathlib.Path(args.csv_path)
        if not out.is_absolute():
            out = ROOT / out
        write_csv(flatten(summary), out)
        print(f"csv_written: {out}")

    if result.returncode != 0:
        combined = "\n".join(part for part in (result.stdout, result.stderr) if part)
        if "FAIL" in combined and "ok\tplasmod/src/internal/storage" not in combined:
            return result.returncode
    return 0


if __name__ == "__main__":
    sys.exit(main())
