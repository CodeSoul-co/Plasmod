#!/usr/bin/env python3
"""Member-A Layer1 E2E helper: import/query/delete/purge loop."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
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


def run_import(repo: Path, args: argparse.Namespace, limit: int) -> float:
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
        "1",
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
    ap.add_argument("--json-out", default="")
    args = ap.parse_args()

    repo = Path(__file__).resolve().parents[2]
    limits = [int(x.strip()) for x in args.limits.split(",") if x.strip()]
    result: list[dict[str, object]] = []

    for limit in limits:
        sec = run_import(repo, args, limit)
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

    if args.json_out:
        Path(args.json_out).write_text(json.dumps(result, indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
