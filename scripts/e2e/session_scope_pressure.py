#!/usr/bin/env python3
"""
Pressure-test session/scope filtered query paths via the live /v1/query API.

This runner is intentionally generic so it can be reused for:
- 1-T4 session/scope conditional filtering
- visibility-chain smoke checks under concurrency
- quick latency snapshots before larger experiments
"""

from __future__ import annotations

import argparse
import concurrent.futures
import csv
import json
import os
import pathlib
import statistics
import time
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


ROOT = pathlib.Path(__file__).resolve().parents[2]
DEFAULT_BASE = os.environ.get("PLASMOD_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
DEFAULT_ADMIN_KEY = os.environ.get("PLASMOD_ADMIN_KEY", "")


def http_json(
    base_url: str,
    method: str,
    path: str,
    body: dict[str, Any] | None,
    *,
    timeout: float,
    admin_key: str,
) -> tuple[int, Any]:
    url = base_url.rstrip("/") + path
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    if admin_key:
        req.add_header("X-Admin-Key", admin_key)
    try:
        with urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            parsed = json.loads(raw) if raw else None
            return resp.status, parsed
    except HTTPError as e:
        raw = e.read()
        try:
            parsed = json.loads(raw)
        except Exception:
            parsed = raw.decode("utf-8", errors="replace")
        return e.code, parsed
    except URLError as e:
        return 0, {"error": str(e)}


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    if len(values) == 1:
        return values[0]
    ordered = sorted(values)
    idx = (len(ordered) - 1) * p
    lo = int(idx)
    hi = min(lo + 1, len(ordered) - 1)
    frac = idx - lo
    return ordered[lo] * (1 - frac) + ordered[hi] * frac


def run_one_query(
    *,
    base_url: str,
    admin_key: str,
    timeout: float,
    payload: dict[str, Any],
) -> dict[str, Any]:
    start = time.perf_counter()
    status, resp = http_json(base_url, "POST", "/v1/query", payload, timeout=timeout, admin_key=admin_key)
    latency_ms = (time.perf_counter() - start) * 1000.0

    objects = []
    query_status = ""
    if isinstance(resp, dict):
        objects = resp.get("objects") or []
        query_status = str(resp.get("query_status") or "")

    return {
        "status_code": status,
        "ok": status == 200,
        "latency_ms": latency_ms,
        "result_count": len(objects) if isinstance(objects, list) else 0,
        "query_status": query_status,
    }


def build_payload(args: argparse.Namespace) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "query_text": args.query_text,
        "query_scope": args.query_scope,
        "agent_id": args.agent_id,
        "session_id": args.session_id,
        "workspace_id": args.workspace_id,
        "top_k": args.top_k,
    }
    if args.include_cold:
        payload["include_cold"] = True
    if args.object_types:
        payload["object_types"] = [item.strip() for item in args.object_types.split(",") if item.strip()]
    if args.memory_types:
        payload["memory_types"] = [item.strip() for item in args.memory_types.split(",") if item.strip()]
    return payload


def summarize(results: list[dict[str, Any]], args: argparse.Namespace) -> dict[str, Any]:
    latencies = [row["latency_ms"] for row in results]
    successes = [row for row in results if row["ok"]]
    result_counts = [row["result_count"] for row in results]
    query_statuses = sorted({row["query_status"] for row in results if row["query_status"]})

    return {
        "suite": "session_scope_pressure",
        "query_scope": args.query_scope,
        "requests": len(results),
        "concurrency": args.concurrency,
        "success_rate": round(len(successes) / len(results), 4) if results else 0.0,
        "p50_ms": round(percentile(latencies, 0.50), 3),
        "p95_ms": round(percentile(latencies, 0.95), 3),
        "p99_ms": round(percentile(latencies, 0.99), 3),
        "avg_result_count": round(statistics.mean(result_counts), 3) if result_counts else 0.0,
        "max_result_count": max(result_counts) if result_counts else 0,
        "query_statuses": query_statuses,
    }


def write_csv(rows: list[dict[str, Any]], path: pathlib.Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fieldnames = list(rows[0].keys()) if rows else ["suite"]
    with path.open("w", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)


def main() -> int:
    parser = argparse.ArgumentParser(description="Pressure-test session/scope filtered query paths")
    parser.add_argument("--base-url", default=DEFAULT_BASE, help="CogDB base URL")
    parser.add_argument("--admin-key", default=DEFAULT_ADMIN_KEY, help="Optional admin API key")
    parser.add_argument("--query-text", required=True, help="Query text to issue repeatedly")
    parser.add_argument("--query-scope", default="session", help="Query scope, e.g. session/workspace/agent")
    parser.add_argument("--agent-id", required=True, help="Agent ID for the query")
    parser.add_argument("--session-id", required=True, help="Session ID for the query")
    parser.add_argument("--workspace-id", required=True, help="Workspace ID for the query")
    parser.add_argument("--top-k", type=int, default=10, help="TopK for /v1/query")
    parser.add_argument("--requests", type=int, default=100, help="Total number of requests to send")
    parser.add_argument("--concurrency", type=int, default=8, help="Concurrent workers")
    parser.add_argument("--timeout", type=float, default=60.0, help="Per-request timeout in seconds")
    parser.add_argument("--include-cold", action="store_true", help="Set include_cold=true on requests")
    parser.add_argument("--object-types", default="", help="Comma-separated object_types")
    parser.add_argument("--memory-types", default="", help="Comma-separated memory_types")
    parser.add_argument("--json", dest="json_path", help="Optional JSON summary output path")
    parser.add_argument("--csv", dest="csv_path", help="Optional CSV summary output path")
    args = parser.parse_args()

    payload = build_payload(args)

    results: list[dict[str, Any]] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, args.concurrency)) as pool:
        futures = [
            pool.submit(
                run_one_query,
                base_url=args.base_url,
                admin_key=args.admin_key,
                timeout=args.timeout,
                payload=payload,
            )
            for _ in range(max(1, args.requests))
        ]
        for future in concurrent.futures.as_completed(futures):
            results.append(future.result())

    summary = summarize(results, args)
    print("Session/Scope Pressure Summary")
    for key, value in summary.items():
        print(f"{key}: {value}")

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
        write_csv([summary], out)
        print(f"csv_written: {out}")

    return 0 if summary["success_rate"] > 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
