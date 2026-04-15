#!/usr/bin/env python3
"""
Run cold-tier benchmark tests and summarize the key metrics.

This script is intentionally lightweight:
- it reuses the existing Go benchmark / validation tests
- it extracts experiment-facing metrics from verbose test logs
- it can emit both human-readable text and machine-readable JSON

Typical usage:
  python scripts/e2e/cold_tier_benchmark_summary.py
  python scripts/e2e/cold_tier_benchmark_summary.py --json out/cold_tier_benchmark.json
  python scripts/e2e/cold_tier_benchmark_summary.py --csv out/cold_tier_benchmark.csv
  python scripts/e2e/cold_tier_benchmark_summary.py --include-scaling
  python scripts/e2e/cold_tier_benchmark_summary.py --include-curve --csv out/recall_throughput_curve.csv
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


RUNTIME_METRIC_RE = re.compile(
    r"Runtime include_cold 10K archived: "
    r"recall@10=(?P<recall>[0-9.]+) "
    r"p50=(?P<p50>[0-9.]+)ms "
    r"p95=(?P<p95>[0-9.]+)ms "
    r"p99=(?P<p99>[0-9.]+)ms "
    r"target_p95_lt_500ms=(?P<p95_target>true|false) "
    r"evidence_cache=\{LookedUp:(?P<looked_up>\d+) Hits:(?P<hits>\d+) Misses:(?P<misses>\d+) ColdHits:(?P<cold_hits>\d+) ColdMisses:(?P<cold_misses>\d+)\}"
)

RECALL_COMPARE_RE = re.compile(
    r"Recall@10 comparison: hot_only=(?P<hot>[0-9.]+) include_cold=(?P<cold>[0-9.]+) "
    r"hot_objects=(?P<hot_objects>\d+) cold_objects=(?P<cold_objects>\d+)"
)

SCALING_METRIC_RE = re.compile(
    r"Dataset scaling: size=(?P<size>\d+) "
    r"recall@10=(?P<recall>[0-9.]+) "
    r"p50=(?P<p50>[0-9.]+)ms "
    r"p95=(?P<p95>[0-9.]+)ms "
    r"p99=(?P<p99>[0-9.]+)ms "
    r"cold_hits=(?P<cold_hits>\d+) cold_misses=(?P<cold_misses>\d+)"
)

CURVE_METRIC_RE = re.compile(
    r"Recall-throughput curve: "
    r"dataset_size=(?P<size>\d+) "
    r"topk=(?P<topk>\d+) "
    r"recall@k=(?P<recall>[0-9.]+) "
    r"qps=(?P<qps>[0-9.]+) "
    r"p50=(?P<p50>[0-9.]+)ms "
    r"p95=(?P<p95>[0-9.]+)ms"
)


def run_go_test(include_scaling: bool, include_curve: bool) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env.setdefault("GOCACHE", str(ROOT / "out" / "gocache"))
    env.setdefault("GOTMPDIR", str(ROOT / "out" / "gotmp"))

    test_pattern = (
        "TestRuntime_IncludeCold_10KArchivedCorrectnessAndLatency|"
        "TestRuntime_IncludeCold_10KArchived_RecallVsHotOnly"
    )
    if include_scaling:
        test_pattern += "|TestRuntime_IncludeCold_DatasetScaling"
    if include_curve:
        test_pattern += "|TestRuntime_IncludeCold_RecallThroughputCurve"

    cmd = [
        "go",
        "test",
        "./src/internal/worker",
        "-run",
        test_pattern,
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
        "package": "plasmod/src/internal/worker",
        "benchmark_suite": "cold_tier",
        "tests_ran": [
            "TestRuntime_IncludeCold_10KArchivedCorrectnessAndLatency",
            "TestRuntime_IncludeCold_10KArchived_RecallVsHotOnly",
        ],
        "metrics": {},
        "notes": [],
    }

    runtime_match = RUNTIME_METRIC_RE.search(combined)
    if runtime_match:
        summary["metrics"]["include_cold_10k"] = {
            "recall_at_10": float(runtime_match.group("recall")),
            "p50_ms": float(runtime_match.group("p50")),
            "p95_ms": float(runtime_match.group("p95")),
            "p99_ms": float(runtime_match.group("p99")),
            "target_p95_lt_500ms": runtime_match.group("p95_target") == "true",
            "evidence_cache": {
                "looked_up": int(runtime_match.group("looked_up")),
                "hits": int(runtime_match.group("hits")),
                "misses": int(runtime_match.group("misses")),
                "cold_hits": int(runtime_match.group("cold_hits")),
                "cold_misses": int(runtime_match.group("cold_misses")),
            },
        }
    else:
        summary["notes"].append("include_cold_10k metrics were not found in go test output")

    compare_match = RECALL_COMPARE_RE.search(combined)
    if compare_match:
        summary["metrics"]["recall_comparison"] = {
            "hot_only_recall_at_10": float(compare_match.group("hot")),
            "include_cold_recall_at_10": float(compare_match.group("cold")),
            "hot_only_objects": int(compare_match.group("hot_objects")),
            "include_cold_objects": int(compare_match.group("cold_objects")),
        }
    else:
        summary["notes"].append("recall comparison metrics were not found in go test output")

    scaling_matches = list(SCALING_METRIC_RE.finditer(combined))
    if scaling_matches:
        summary["tests_ran"].append("TestRuntime_IncludeCold_DatasetScaling")
        summary["metrics"]["dataset_scaling"] = [
            {
                "dataset_size": int(match.group("size")),
                "recall_at_10": float(match.group("recall")),
                "p50_ms": float(match.group("p50")),
                "p95_ms": float(match.group("p95")),
                "p99_ms": float(match.group("p99")),
                "cold_hits": int(match.group("cold_hits")),
                "cold_misses": int(match.group("cold_misses")),
            }
            for match in scaling_matches
        ]

    curve_matches = list(CURVE_METRIC_RE.finditer(combined))
    if curve_matches:
        summary["tests_ran"].append("TestRuntime_IncludeCold_RecallThroughputCurve")
        summary["metrics"]["recall_throughput_curve"] = [
            {
                "dataset_size": int(match.group("size")),
                "top_k": int(match.group("topk")),
                "recall_at_k": float(match.group("recall")),
                "qps": float(match.group("qps")),
                "p50_ms": float(match.group("p50")),
                "p95_ms": float(match.group("p95")),
            }
            for match in curve_matches
        ]

    if "Access is denied." in combined:
        summary["notes"].append(
            "Windows temp cleanup reported 'Access is denied'; verify test lines above to distinguish cleanup noise from logic failures"
        )

    return summary


def print_human_summary(summary: dict[str, Any]) -> None:
    print("Cold-Tier Benchmark Summary")
    print(f"package: {summary['package']}")
    print(f"suite:   {summary['benchmark_suite']}")

    include_cold = summary["metrics"].get("include_cold_10k")
    if include_cold:
        cache = include_cold["evidence_cache"]
        print("")
        print("include_cold_10k:")
        print(f"  recall@10: {include_cold['recall_at_10']:.3f}")
        print(f"  p50_ms:    {include_cold['p50_ms']:.3f}")
        print(f"  p95_ms:    {include_cold['p95_ms']:.3f}")
        print(f"  p99_ms:    {include_cold['p99_ms']:.3f}")
        print(f"  p95<500ms: {include_cold['target_p95_lt_500ms']}")
        print(
            "  evidence_cache:"
            f" looked_up={cache['looked_up']}"
            f" hits={cache['hits']}"
            f" misses={cache['misses']}"
            f" cold_hits={cache['cold_hits']}"
            f" cold_misses={cache['cold_misses']}"
        )

    recall_comparison = summary["metrics"].get("recall_comparison")
    if recall_comparison:
        print("")
        print("recall_comparison:")
        print(f"  hot_only_recall@10:    {recall_comparison['hot_only_recall_at_10']:.3f}")
        print(f"  include_cold_recall@10:{recall_comparison['include_cold_recall_at_10']:.3f}")
        print(f"  hot_only_objects:      {recall_comparison['hot_only_objects']}")
        print(f"  include_cold_objects:  {recall_comparison['include_cold_objects']}")

    if summary["notes"]:
        print("")
        print("notes:")
        for note in summary["notes"]:
            print(f"  - {note}")

    scaling_rows = summary["metrics"].get("dataset_scaling", [])
    if scaling_rows:
        print("")
        print("dataset_scaling:")
        for row in scaling_rows:
            print(
                "  "
                f"size={row['dataset_size']} "
                f"recall@10={row['recall_at_10']:.3f} "
                f"p50={row['p50_ms']:.3f}ms "
                f"p95={row['p95_ms']:.3f}ms "
                f"p99={row['p99_ms']:.3f}ms "
                f"cold_hits={row['cold_hits']} "
                f"cold_misses={row['cold_misses']}"
            )

    curve_rows = summary["metrics"].get("recall_throughput_curve", [])
    if curve_rows:
        print("")
        print("recall_throughput_curve:")
        for row in curve_rows:
            print(
                "  "
                f"dataset_size={row['dataset_size']} "
                f"top_k={row['top_k']} "
                f"recall@k={row['recall_at_k']:.3f} "
                f"qps={row['qps']:.2f} "
                f"p50={row['p50_ms']:.3f}ms "
                f"p95={row['p95_ms']:.3f}ms"
            )


def flatten_summary(summary: dict[str, Any]) -> dict[str, Any]:
    row: dict[str, Any] = {
        "benchmark_suite": summary.get("benchmark_suite", ""),
        "package": summary.get("package", ""),
        "go_test_exit_code": summary.get("go_test_exit_code", ""),
    }

    include_cold = summary.get("metrics", {}).get("include_cold_10k", {})
    if include_cold:
        row["include_cold_recall_at_10"] = include_cold.get("recall_at_10", "")
        row["include_cold_p50_ms"] = include_cold.get("p50_ms", "")
        row["include_cold_p95_ms"] = include_cold.get("p95_ms", "")
        row["include_cold_p99_ms"] = include_cold.get("p99_ms", "")
        row["include_cold_target_p95_lt_500ms"] = include_cold.get("target_p95_lt_500ms", "")
        cache = include_cold.get("evidence_cache", {})
        row["evidence_cache_looked_up"] = cache.get("looked_up", "")
        row["evidence_cache_hits"] = cache.get("hits", "")
        row["evidence_cache_misses"] = cache.get("misses", "")
        row["evidence_cache_cold_hits"] = cache.get("cold_hits", "")
        row["evidence_cache_cold_misses"] = cache.get("cold_misses", "")

    recall_comparison = summary.get("metrics", {}).get("recall_comparison", {})
    if recall_comparison:
        row["hot_only_recall_at_10"] = recall_comparison.get("hot_only_recall_at_10", "")
        row["comparison_include_cold_recall_at_10"] = recall_comparison.get("include_cold_recall_at_10", "")
        row["hot_only_objects"] = recall_comparison.get("hot_only_objects", "")
        row["include_cold_objects"] = recall_comparison.get("include_cold_objects", "")

    row["notes"] = " | ".join(summary.get("notes", []))
    return row


def write_csv(summary: dict[str, Any], csv_path: pathlib.Path) -> None:
    csv_path.parent.mkdir(parents=True, exist_ok=True)
    scaling_rows = summary.get("metrics", {}).get("dataset_scaling", [])
    curve_rows = summary.get("metrics", {}).get("recall_throughput_curve", [])
    if curve_rows:
        rows = []
        base_row = flatten_summary(summary)
        for curve_row in curve_rows:
            row = dict(base_row)
            row["curve_dataset_size"] = curve_row.get("dataset_size", "")
            row["curve_top_k"] = curve_row.get("top_k", "")
            row["curve_recall_at_k"] = curve_row.get("recall_at_k", "")
            row["curve_qps"] = curve_row.get("qps", "")
            row["curve_p50_ms"] = curve_row.get("p50_ms", "")
            row["curve_p95_ms"] = curve_row.get("p95_ms", "")
            rows.append(row)
    elif scaling_rows:
        rows = []
        base_row = flatten_summary(summary)
        for scaling_row in scaling_rows:
            row = dict(base_row)
            row["dataset_size"] = scaling_row.get("dataset_size", "")
            row["scaling_recall_at_10"] = scaling_row.get("recall_at_10", "")
            row["scaling_p50_ms"] = scaling_row.get("p50_ms", "")
            row["scaling_p95_ms"] = scaling_row.get("p95_ms", "")
            row["scaling_p99_ms"] = scaling_row.get("p99_ms", "")
            row["scaling_cold_hits"] = scaling_row.get("cold_hits", "")
            row["scaling_cold_misses"] = scaling_row.get("cold_misses", "")
            rows.append(row)
    else:
        rows = [flatten_summary(summary)]

    with csv_path.open("w", newline="", encoding="utf-8") as fh:
        writer = csv.DictWriter(fh, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description="Summarize cold-tier benchmark metrics")
    parser.add_argument("--json", dest="json_path", help="Optional path to write JSON summary")
    parser.add_argument("--csv", dest="csv_path", help="Optional path to write flat CSV summary")
    parser.add_argument("--include-scaling", action="store_true", help="Also run dataset-size scaling validation")
    parser.add_argument("--include-curve", action="store_true", help="Also run recall-throughput curve validation")
    args = parser.parse_args()

    result = run_go_test(args.include_scaling, args.include_curve)
    summary = parse_output(result.stdout, result.stderr)
    summary["go_test_exit_code"] = result.returncode

    print_human_summary(summary)

    if args.json_path:
        out_path = pathlib.Path(args.json_path)
        if not out_path.is_absolute():
            out_path = ROOT / out_path
        out_path.parent.mkdir(parents=True, exist_ok=True)
        out_path.write_text(json.dumps(summary, indent=2, ensure_ascii=False), encoding="utf-8")
        print("")
        print(f"json_written: {out_path}")

    if args.csv_path:
        csv_path = pathlib.Path(args.csv_path)
        if not csv_path.is_absolute():
            csv_path = ROOT / csv_path
        write_csv(summary, csv_path)
        print("")
        print(f"csv_written: {csv_path}")

    # Treat actual test failures as failures, but allow the caller to inspect the
    # JSON/text summary first. If the known Windows cleanup noise is the only
    # issue, users can still use the parsed metrics.
    if result.returncode != 0:
        combined = "\n".join(part for part in (result.stdout, result.stderr) if part)
        if "FAIL" in combined and "ok\tplasmod/src/internal/worker" not in combined:
            return result.returncode
    return 0


if __name__ == "__main__":
    sys.exit(main())
