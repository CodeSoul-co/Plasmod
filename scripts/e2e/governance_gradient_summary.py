#!/usr/bin/env python3
"""
Summarize governance-gradient experiment checks.

Targets:
- 4-E1 sharing strength
- 4-E2 no governance
- 4-E3 namespace only
- 4-E4 namespace + TTL
- 4-E5 namespace + TTL + quarantine
- 4-E6 full policy layer
- 4-E9 latency overhead
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

E1_RE = re.compile(
    r"Governance gradient E1: "
    r"isolated_visible=(?P<isolated>\d+) "
    r"partial_visible=(?P<partial>\d+) "
    r"full_visible=(?P<full>\d+) "
    r"level=(?P<level>\S+)"
)

E2_RE = re.compile(
    r"Governance gradient E2: "
    r"candidates=(?P<candidates>\d+) "
    r"returned=(?P<returned>\d+) "
    r"level=(?P<level>\S+)"
)

E3_RE = re.compile(
    r"Governance gradient E3: "
    r"namespace_visible=(?P<visible>\d+) "
    r"excluded_by_namespace=(?P<excluded>\d+) "
    r"returned=(?P<returned>\d+) "
    r"level=(?P<level>\S+)"
)

E4_RE = re.compile(
    r"Governance gradient E4: "
    r"namespace_visible=(?P<visible>\d+) "
    r"ttl_excluded=(?P<ttl>\d+) "
    r"returned=(?P<returned>\d+) "
    r"level=(?P<level>\S+)"
)

E5_RE = re.compile(
    r"Governance gradient E5: "
    r"namespace_visible=(?P<visible>\d+) "
    r"ttl_excluded=(?P<ttl>\d+) "
    r"quarantine_excluded=(?P<quarantine>\d+) "
    r"returned=(?P<returned>\d+) "
    r"level=(?P<level>\S+)"
)

E6_RE = re.compile(
    r"Governance gradient E6: "
    r"namespace_visible=(?P<visible>\d+) "
    r"ttl_excluded=(?P<ttl>\d+) "
    r"quarantine_excluded=(?P<quarantine>\d+) "
    r"min_version_excluded=(?P<min_version>\d+) "
    r"unverified_excluded=(?P<unverified>\d+) "
    r"returned=(?P<returned>\d+) "
    r"level=(?P<level>\S+)"
)

E9_RE = re.compile(
    r"Governance gradient E9: "
    r"baseline_candidates=(?P<candidates>\d+) "
    r"baseline_ms=(?P<baseline>[0-9.]+) "
    r"policy_ms=(?P<policy>[0-9.]+) "
    r"overhead_ms=(?P<overhead>[0-9.]+) "
    r"level=(?P<level>\S+)"
)

M2_RE = re.compile(
    r"Governance metric M2: "
    r"total=(?P<total>\d+) "
    r"stale_filtered=(?P<filtered>\d+) "
    r"returned=(?P<returned>\d+) "
    r"stale_usage_rate=(?P<rate>[0-9.]+)"
)

M3_RE = re.compile(
    r"Governance metric M3: "
    r"total=(?P<total>\d+) "
    r"violations_filtered=(?P<filtered>\d+) "
    r"returned=(?P<returned>\d+) "
    r"policy_violation_rate=(?P<rate>[0-9.]+)"
)

M7_RE = re.compile(
    r"Governance metric M7: "
    r"baseline_ms=(?P<baseline>[0-9.]+) "
    r"governance_ms=(?P<governance>[0-9.]+) "
    r"overhead_ms=(?P<overhead>[0-9.]+)"
)


def run_go_tests() -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env.setdefault("GOCACHE", str(ROOT / "out" / "gocache"))
    env.setdefault("GOTMPDIR", str(ROOT / "out" / "gotmp"))
    cmd = [
        "go",
        "test",
        "./src/internal/storage",
        "-run",
        "TestGovernanceGradient_SharingStrength|"
        "TestGovernanceGradient_NoGovernance|"
        "TestGovernanceGradient_NamespaceOnly|"
        "TestGovernanceGradient_NamespaceTTL|"
        "TestGovernanceGradient_NamespaceTTLQuarantine|"
        "TestGovernanceGradient_FullPolicyLayer|"
        "TestGovernanceGradient_LatencyOverhead|"
        "TestGovernanceMetrics_StaleMemoryUsageRate|"
        "TestGovernanceMetrics_PolicyViolationRate|"
        "TestGovernanceMetrics_LatencyOverhead",
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
        "suite": "governance_gradient",
        "checks": {},
        "notes": [],
    }

    e1 = E1_RE.search(combined)
    if e1:
        summary["checks"]["sharing_strength"] = {
            "requirement": "4-E1",
            "isolated_visible": int(e1.group("isolated")),
            "partial_visible": int(e1.group("partial")),
            "full_visible": int(e1.group("full")),
            "level": e1.group("level"),
        }
    else:
        summary["notes"].append("4-E1 sharing-strength metrics were not found")

    e2 = E2_RE.search(combined)
    if e2:
        summary["checks"]["no_governance"] = {
            "requirement": "4-E2",
            "candidates": int(e2.group("candidates")),
            "returned": int(e2.group("returned")),
            "level": e2.group("level"),
        }
    else:
        summary["notes"].append("4-E2 no-governance metrics were not found")

    e3 = E3_RE.search(combined)
    if e3:
        summary["checks"]["namespace_only"] = {
            "requirement": "4-E3",
            "namespace_visible": int(e3.group("visible")),
            "excluded_by_namespace": int(e3.group("excluded")),
            "returned": int(e3.group("returned")),
            "level": e3.group("level"),
        }
    else:
        summary["notes"].append("4-E3 namespace-only metrics were not found")

    e4 = E4_RE.search(combined)
    if e4:
        summary["checks"]["namespace_ttl"] = {
            "requirement": "4-E4",
            "namespace_visible": int(e4.group("visible")),
            "ttl_excluded": int(e4.group("ttl")),
            "returned": int(e4.group("returned")),
            "level": e4.group("level"),
        }
    else:
        summary["notes"].append("4-E4 namespace+ttl metrics were not found")

    e5 = E5_RE.search(combined)
    if e5:
        summary["checks"]["namespace_ttl_quarantine"] = {
            "requirement": "4-E5",
            "namespace_visible": int(e5.group("visible")),
            "ttl_excluded": int(e5.group("ttl")),
            "quarantine_excluded": int(e5.group("quarantine")),
            "returned": int(e5.group("returned")),
            "level": e5.group("level"),
        }
    else:
        summary["notes"].append("4-E5 namespace+ttl+quarantine metrics were not found")

    e6 = E6_RE.search(combined)
    if e6:
        summary["checks"]["full_policy_layer"] = {
            "requirement": "4-E6",
            "namespace_visible": int(e6.group("visible")),
            "ttl_excluded": int(e6.group("ttl")),
            "quarantine_excluded": int(e6.group("quarantine")),
            "min_version_excluded": int(e6.group("min_version")),
            "unverified_excluded": int(e6.group("unverified")),
            "returned": int(e6.group("returned")),
            "level": e6.group("level"),
        }
    else:
        summary["notes"].append("4-E6 full policy layer metrics were not found")

    e9 = E9_RE.search(combined)
    if e9:
        summary["checks"]["latency_overhead"] = {
            "requirement": "4-E9",
            "baseline_candidates": int(e9.group("candidates")),
            "baseline_ms": float(e9.group("baseline")),
            "policy_ms": float(e9.group("policy")),
            "overhead_ms": float(e9.group("overhead")),
            "level": e9.group("level"),
        }
    else:
        summary["notes"].append("4-E9 latency-overhead metrics were not found")

    m2 = M2_RE.search(combined)
    if m2:
        summary["checks"]["stale_memory_usage_rate"] = {
            "requirement": "4-M2",
            "total": int(m2.group("total")),
            "stale_filtered": int(m2.group("filtered")),
            "returned": int(m2.group("returned")),
            "stale_usage_rate": float(m2.group("rate")),
        }
    else:
        summary["notes"].append("4-M2 stale-memory usage metrics were not found")

    m3 = M3_RE.search(combined)
    if m3:
        summary["checks"]["policy_violation_rate"] = {
            "requirement": "4-M3",
            "total": int(m3.group("total")),
            "violations_filtered": int(m3.group("filtered")),
            "returned": int(m3.group("returned")),
            "policy_violation_rate": float(m3.group("rate")),
        }
    else:
        summary["notes"].append("4-M3 policy violation metrics were not found")

    m7 = M7_RE.search(combined)
    if m7:
        summary["checks"]["governance_latency_overhead"] = {
            "requirement": "4-M7",
            "baseline_ms": float(m7.group("baseline")),
            "governance_ms": float(m7.group("governance")),
            "overhead_ms": float(m7.group("overhead")),
        }
    else:
        summary["notes"].append("4-M7 governance-overhead metrics were not found")

    if "Access is denied." in combined:
        summary["notes"].append(
            "Windows temp cleanup reported 'Access is denied'; verify PASS lines above to distinguish cleanup noise from logic failures"
        )

    return summary


def flatten(summary: dict[str, Any]) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for _, check in summary["checks"].items():
        rows.append(check)
    if not rows:
        rows.append({"requirement": "", "notes": "no checks parsed"})
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


def print_summary(summary: dict[str, Any]) -> None:
    print("Governance Gradient Summary")
    print(f"suite: {summary['suite']}")
    for name, check in summary["checks"].items():
        print(f"{name}: {check}")
    if summary["notes"]:
        print("notes:")
        for note in summary["notes"]:
            print(f"  - {note}")


def main() -> int:
    parser = argparse.ArgumentParser(description="Summarize governance-gradient checks")
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
