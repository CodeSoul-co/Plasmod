#!/usr/bin/env python3
"""
Compare full-system and baseline benchmark exports using a shared tabular format.

This script does not implement the baseline itself. Instead, it standardizes how
two already-exported result files are compared for experiment reporting.

Accepted inputs:
- JSON exported by member_c_benchmark_summary.py
- CSV exported by member_c_benchmark_summary.py
- Any CSV/JSON file that exposes matching metric columns
"""

from __future__ import annotations

import argparse
import csv
import json
import pathlib
from typing import Any


ROOT = pathlib.Path(__file__).resolve().parents[2]


def load_json(path: pathlib.Path) -> list[dict[str, Any]]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if isinstance(data, list):
        return data
    if isinstance(data, dict):
        metrics = data.get("metrics", {})
        rows: list[dict[str, Any]] = []

        include_cold = metrics.get("include_cold_10k")
        if include_cold:
            row = {"experiment": "include_cold_10k"}
            row.update(include_cold)
            rows.append(row)

        for scaling in metrics.get("dataset_scaling", []):
            row = {"experiment": f"dataset_scaling_{scaling.get('dataset_size', '')}"}
            row.update(scaling)
            rows.append(row)

        for curve in metrics.get("recall_throughput_curve", []):
            row = {
                "experiment": f"curve_topk_{curve.get('top_k', '')}",
            }
            row.update(curve)
            rows.append(row)

        if rows:
            return rows
        return [data]
    raise ValueError(f"Unsupported JSON structure in {path}")


def load_csv(path: pathlib.Path) -> list[dict[str, Any]]:
    with path.open("r", encoding="utf-8", newline="") as fh:
        return list(csv.DictReader(fh))


def load_rows(path_str: str) -> list[dict[str, Any]]:
    path = pathlib.Path(path_str)
    if not path.is_absolute():
        path = ROOT / path
    if path.suffix.lower() == ".json":
        return load_json(path)
    if path.suffix.lower() == ".csv":
        return load_csv(path)
    raise ValueError(f"Unsupported file type: {path}")


def normalize_value(value: Any) -> Any:
    if isinstance(value, (int, float, bool)):
        return value
    if value is None:
        return ""
    text = str(value).strip()
    if text == "":
        return ""
    lowered = text.lower()
    if lowered in {"true", "false"}:
        return lowered == "true"
    try:
        if "." in text:
            return float(text)
        return int(text)
    except ValueError:
        return text


def index_rows(rows: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    indexed: dict[str, dict[str, Any]] = {}
    for i, row in enumerate(rows):
        if "experiment" in row and str(row["experiment"]).strip():
            key = str(row["experiment"]).strip()
        elif "curve_top_k" in row and str(row["curve_top_k"]).strip():
            key = f"curve_topk_{row['curve_top_k']}"
        elif "dataset_size" in row and str(row["dataset_size"]).strip():
            key = f"dataset_scaling_{row['dataset_size']}"
        else:
            key = f"row_{i}"
        indexed[key] = {k: normalize_value(v) for k, v in row.items()}
    return indexed


def build_comparison(full_rows: list[dict[str, Any]], baseline_rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    full_index = index_rows(full_rows)
    baseline_index = index_rows(baseline_rows)
    keys = sorted(set(full_index.keys()) | set(baseline_index.keys()))

    comparisons: list[dict[str, Any]] = []
    metric_fields = [
        "recall_at_10",
        "recall_at_k",
        "p50_ms",
        "p95_ms",
        "p99_ms",
        "qps",
        "cold_hits",
        "cold_misses",
    ]

    for key in keys:
        full_row = full_index.get(key, {})
        baseline_row = baseline_index.get(key, {})
        out: dict[str, Any] = {"experiment": key}
        for field in metric_fields:
            full_value = full_row.get(field, "")
            baseline_value = baseline_row.get(field, "")
            out[f"full_{field}"] = full_value
            out[f"baseline_{field}"] = baseline_value
            if isinstance(full_value, (int, float)) and isinstance(baseline_value, (int, float)):
                out[f"delta_{field}"] = full_value - baseline_value
            else:
                out[f"delta_{field}"] = ""
        comparisons.append(out)
    return comparisons


def write_csv(rows: list[dict[str, Any]], path: pathlib.Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=list(rows[0].keys()))
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description="Compare full-system and baseline benchmark exports")
    parser.add_argument("--full", required=True, help="Full-system CSV or JSON export")
    parser.add_argument("--baseline", required=True, help="Baseline CSV or JSON export")
    parser.add_argument("--csv", dest="csv_path", help="Optional CSV output path")
    parser.add_argument("--json", dest="json_path", help="Optional JSON output path")
    args = parser.parse_args()

    full_rows = load_rows(args.full)
    baseline_rows = load_rows(args.baseline)
    comparisons = build_comparison(full_rows, baseline_rows)

    print("Member C Baseline Comparison")
    print(f"full_rows: {len(full_rows)}")
    print(f"baseline_rows: {len(baseline_rows)}")
    for row in comparisons:
        print(f"experiment={row['experiment']}")

    if args.csv_path:
        csv_path = pathlib.Path(args.csv_path)
        if not csv_path.is_absolute():
            csv_path = ROOT / csv_path
        write_csv(comparisons, csv_path)
        print(f"csv_written: {csv_path}")

    if args.json_path:
        json_path = pathlib.Path(args.json_path)
        if not json_path.is_absolute():
            json_path = ROOT / json_path
        json_path.parent.mkdir(parents=True, exist_ok=True)
        json_path.write_text(json.dumps(comparisons, indent=2, ensure_ascii=False), encoding="utf-8")
        print(f"json_written: {json_path}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
