#!/usr/bin/env python3
"""Member-A Layer1 E2E helper: import/query/delete/purge loop."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from urllib.request import Request, urlopen


def http_json(method: str, url: str, body: dict | None = None) -> tuple[int, object]:
    data = json.dumps(body).encode() if body is not None else None
    req = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    with urlopen(req, timeout=120.0) as resp:
        raw = resp.read()
        return resp.status, json.loads(raw) if raw else {}


def run_import(repo: Path, args: argparse.Namespace, limit: int, concurrency: int = 1) -> float:
    cmd = [
        sys.executable,
        str(repo / "scripts" / "e2e" / "import_dataset.py"),
        "--file",
        args.file,
        "--dataset",
        args.dataset,
        "--workspace-id",
        args.workspace_id,
        "--base-url",
        args.base_url,
        "--limit",
        str(limit),
        "--concurrency",
        str(max(1, concurrency)),
        "--source",
        "dataset_loader",
        "--ingest-mode",
        "bulk_dataset",
    ]
    t0 = time.perf_counter()
    subprocess.run(cmd, check=True)
    return time.perf_counter() - t0


def run_query_samples(base_url: str, workspace: str, dataset: str, n: int) -> tuple[float, float, list[float]]:
    lat = []
    body = {
        "query_text": f"dataset={dataset}",
        "query_scope": "workspace",
        "tenant_id": "t_member_a",
        "workspace_id": workspace,
        "session_id": f"s_{dataset}",
        "agent_id": "a_member_a",
        "top_k": 10,
        "time_window": {"from": "2020-01-01T00:00:00Z", "to": "2035-01-01T00:00:00Z"},
        "relation_constraints": [],
        "response_mode": "structured_evidence",
        "dataset_name": dataset,
    }
    for _ in range(n):
        t0 = time.perf_counter()
        st, _ = http_json("POST", f"{base_url}/v1/query", body)
        if st != 200:
            raise RuntimeError(f"query failed status={st}")
        lat.append((time.perf_counter() - t0) * 1000)
    s = sorted(lat)
    p50 = s[len(s) // 2]
    p95 = s[min(len(s) - 1, int(len(s) * 0.95))]
    return p50, p95, lat


def run_query_samples_parallel(
    base_url: str,
    workspace: str,
    dataset: str,
    *,
    workers: int,
    per_worker_samples: int,
) -> dict[str, float]:
    if workers <= 0 or per_worker_samples <= 0:
        raise ValueError("workers/per_worker_samples must be > 0")

    def one_worker() -> list[float]:
        _, _, lat = run_query_samples(base_url, workspace, dataset, per_worker_samples)
        return lat

    t0 = time.perf_counter()
    all_lat: list[float] = []
    with ThreadPoolExecutor(max_workers=workers) as ex:
        futs = [ex.submit(one_worker) for _ in range(workers)]
        for fut in as_completed(futs):
            all_lat.extend(fut.result())
    elapsed = time.perf_counter() - t0
    total = len(all_lat)
    if total == 0:
        raise RuntimeError("parallel query produced no samples")
    s = sorted(all_lat)
    p50 = s[len(s) // 2]
    p95 = s[min(len(s) - 1, int(len(s) * 0.95))]
    qps = total / elapsed if elapsed > 0 else 0.0
    return {
        "query_total": float(total),
        "query_elapsed_s": float(elapsed),
        "query_qps": float(qps),
        "query_p50_ms": float(p50),
        "query_p95_ms": float(p95),
    }


def call_cleanup(repo: Path, args: argparse.Namespace, purge: bool, dry_run: bool) -> None:
    cmd = [
        sys.executable,
        str(repo / "scripts" / "e2e" / "import_dataset.py"),
        "--dataset",
        args.dataset,
        "--workspace-id",
        args.workspace_id,
        "--base-url",
        args.base_url,
    ]
    if args.file:
        cmd.extend(["--file", args.file])
    if purge:
        cmd.append("--purge")
        if dry_run:
            cmd.append("--purge-dry-run")
    else:
        cmd.append("--delete")
        if dry_run:
            cmd.append("--delete-dry-run")
    subprocess.run(cmd, check=True)


def main() -> int:
    ap = argparse.ArgumentParser(description="Member-A Layer1 E2E import/query/cleanup helper")
    ap.add_argument("--base-url", default=os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080"))
    ap.add_argument("--workspace-id", default="w_member_a_l1")
    ap.add_argument("--dataset", default="member_a_l1")
    ap.add_argument("--file", required=True)
    ap.add_argument("--limits", default="200,500,1000")
    ap.add_argument("--query-samples", type=int, default=20)
    ap.add_argument(
        "--node-counts",
        default="1,2,4",
        help="Node-scaling simulation via concurrent import/query workers",
    )
    ap.add_argument(
        "--with-node-scaling",
        action="store_true",
        help="Run 1-E3 node scaling experiment in addition to dataset scaling",
    )
    ap.add_argument("--json-out", default="")
    args = ap.parse_args()

    repo = Path(__file__).resolve().parents[2]
    limits = [int(x.strip()) for x in args.limits.split(",") if x.strip()]
    node_counts = [int(x.strip()) for x in args.node_counts.split(",") if x.strip()]
    result: list[dict[str, object]] = []

    for limit in limits:
        sec = run_import(repo, args, limit, concurrency=1)
        p50, p95, _ = run_query_samples(args.base_url.rstrip("/"), args.workspace_id, args.dataset, args.query_samples)
        call_cleanup(repo, args, purge=False, dry_run=True)
        call_cleanup(repo, args, purge=True, dry_run=True)
        result.append(
            {
                "limit": limit,
                "import_elapsed_s": sec,
                "query_p50_ms": p50,
                "query_p95_ms": p95,
                "note": "delete/purge dry-run passed",
            }
        )
        print(json.dumps(result[-1], ensure_ascii=False))

    if args.with_node_scaling:
        base_url = args.base_url.rstrip("/")
        node_runs: list[dict[str, object]] = []
        base_qps = 0.0
        base_idx_time = 0.0
        fixed_limit = limits[-1] if limits else 1000
        for n in node_counts:
            import_elapsed = run_import(repo, args, fixed_limit, concurrency=n)
            q = run_query_samples_parallel(
                base_url,
                args.workspace_id,
                args.dataset,
                workers=n,
                per_worker_samples=args.query_samples,
            )
            # cleanup probes keep same behavior and safety (dry-run only)
            call_cleanup(repo, args, purge=False, dry_run=True)
            call_cleanup(repo, args, purge=True, dry_run=True)

            if n == 1:
                base_qps = q["query_qps"]
                base_idx_time = import_elapsed
            qps_eff = (q["query_qps"] / (base_qps * n)) if base_qps > 0 else 0.0
            build_eff = (base_idx_time / import_elapsed) / n if import_elapsed > 0 and base_idx_time > 0 else 0.0
            row = {
                "experiment": "1-E3_node_scaling",
                "node_count": n,
                "import_elapsed_s": import_elapsed,
                "query_qps": q["query_qps"],
                "query_p50_ms": q["query_p50_ms"],
                "query_p95_ms": q["query_p95_ms"],
                "scale_out_efficiency_qps": qps_eff,
                "scale_out_efficiency_build": build_eff,
                "note": "node scaling simulated by concurrency workers",
            }
            node_runs.append(row)
            print(json.dumps(row, ensure_ascii=False))
        result.append({"node_scaling": node_runs})

    if args.json_out:
        Path(args.json_out).write_text(json.dumps(result, indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
