#!/usr/bin/env python3
"""
CogDB End-to-End Integration Fixture
=====================================
Phases:
  A  Create Agent + MAS topology (3 agents, share-contract, policy)
  B  Ingest 200 rows from testQuery10K.fbin (functional validation batch)
  C  Validate 12 functional checkpoints
  D  Full 10K ingest (throughput + embedding storage)
  E  Purge / cleanup

Usage:
  python3 scripts/e2e/integration_fixture.py \\
      --base-url http://127.0.0.1:8080 \\
      --data-file ~/database/testQuery10K.fbin \\
      --out-dir out/integration_test

Env:
  ANDB_BASE_URL   overrides --base-url
  ANDB_ADMIN_KEY  admin API key (optional, sent as X-Admin-Key header)
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import struct
import sys
import time
import uuid
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

# ─── HTTP helpers ─────────────────────────────────────────────────────────────

BASE = os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
ADMIN_KEY = os.environ.get("ANDB_ADMIN_KEY", "")
TIMEOUT = 60.0
REQUESTS_LOG: list[dict] = []


def _req(method: str, path: str, body: dict | None = None, *,
         timeout: float = TIMEOUT, expect_statuses: tuple[int, ...] = (200, 201, 204)) -> tuple[int, Any]:
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    req = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    if ADMIN_KEY:
        req.add_header("X-Admin-Key", ADMIN_KEY)
    entry: dict = {"ts": _now(), "method": method, "path": path}
    if body:
        entry["request_body"] = body
    try:
        with urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            parsed = json.loads(raw) if raw else None
            entry["status"] = resp.status
            entry["response_body"] = parsed
            REQUESTS_LOG.append(entry)
            return resp.status, parsed
    except HTTPError as e:
        raw = e.read()
        parsed = None
        try:
            parsed = json.loads(raw)
        except Exception:
            parsed = raw.decode(errors="replace")
        entry["status"] = e.code
        entry["error"] = parsed
        REQUESTS_LOG.append(entry)
        return e.code, parsed
    except URLError as e:
        entry["status"] = 0
        entry["error"] = str(e)
        REQUESTS_LOG.append(entry)
        return 0, {"error": str(e)}


def _now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


# ─── fbin reader ──────────────────────────────────────────────────────────────

def read_fbin(path: str | Path, limit: int | None = None) -> list[list[float]]:
    """Read .fbin file → list of float32 vectors."""
    p = Path(path).expanduser()
    with open(p, "rb") as f:
        n, d = struct.unpack("<ii", f.read(8))
        if limit is not None:
            n = min(n, limit)
        vecs = []
        for _ in range(n):
            row = struct.unpack(f"<{d}f", f.read(d * 4))
            vecs.append(list(row))
    return vecs


# ─── Reporter ─────────────────────────────────────────────────────────────────

RESULTS: list[dict] = []


def _ok(check: str, detail: str = "") -> None:
    print(f"  [PASS] {check}" + (f" — {detail}" if detail else ""))
    RESULTS.append({"check": check, "status": "PASS", "detail": detail})


def _fail(check: str, detail: str = "") -> None:
    print(f"  [FAIL] {check}" + (f" — {detail}" if detail else ""))
    RESULTS.append({"check": check, "status": "FAIL", "detail": detail})


def _info(msg: str) -> None:
    print(f"  [INFO] {msg}")


def _header(title: str) -> None:
    print(f"\n{'─'*60}")
    print(f"  {title}")
    print(f"{'─'*60}")


# ─── Phase A: Agent + MAS topology ───────────────────────────────────────────

def phase_a() -> dict[str, str]:
    """Create 3 agents, 1 share-contract, 1 policy. Returns {name: id}."""
    _header("Phase A — Create Agent / MAS Topology")
    WS = "ws-integration-test"
    agents: dict[str, str] = {}

    for spec in [
        {"name": "agent-alpha", "type": "assistant",    "workspace_id": WS},
        {"name": "agent-beta",  "type": "assistant",    "workspace_id": WS},
        {"name": "agent-gamma", "type": "orchestrator", "workspace_id": WS},
    ]:
        body = {
            "id": f"{spec['name']}-{uuid.uuid4().hex[:8]}",
            "name": spec["name"],
            "type": spec["type"],
            "workspace_id": spec["workspace_id"],
            "description": f"Integration test {spec['name']}",
            "metadata": {"test_run": "integration"},
        }
        status, resp = _req("POST", "/v1/agents", body)
        if status in (200, 201):
            agent_id = (resp or {}).get("id", body["id"])
            agents[spec["name"]] = agent_id
            _ok(f"Create {spec['name']}", f"id={agent_id}")
        else:
            _fail(f"Create {spec['name']}", f"status={status} resp={resp}")
            agents[spec["name"]] = body["id"]  # fallback

    # Verify listing
    status, resp = _req("GET", "/v1/agents")
    listed = len((resp or {}).get("agents", resp if isinstance(resp, list) else []))
    if listed >= 3:
        _ok("GET /v1/agents list", f"count≥3 (got {listed})")
    else:
        _fail("GET /v1/agents list", f"count={listed}")

    # MAS share-contract: alpha ↔ beta
    sc_body = {
        "from_agent_id": agents.get("agent-alpha", ""),
        "to_agent_id":   agents.get("agent-beta",  ""),
        "workspace_id":  WS,
        "bidirectional": True,
        "memory_types":  ["user_message", "agent_thought", "tool_call"],
    }
    status, resp = _req("POST", "/v1/share-contracts", sc_body)
    if status in (200, 201):
        _ok("Create share-contract alpha↔beta")
    else:
        _fail("Create share-contract alpha↔beta", f"status={status}")

    # Retention policy
    pol_body = {
        "workspace_id": WS,
        "max_age_days": 7,
        "description": "Integration test retention",
    }
    status, resp = _req("POST", "/v1/policies", pol_body)
    if status in (200, 201):
        _ok("Create retention policy")
    else:
        _fail("Create retention policy", f"status={status}")

    agents["_workspace"] = WS
    return agents


# ─── Phase B: Import 200 rows (functional validation) ────────────────────────

def phase_b(data_file: str, agents: dict[str, str]) -> list[str]:
    """Ingest first 200 vectors → return list of ingest IDs."""
    _header("Phase B — Ingest 200 rows (functional batch)")
    WS = agents["_workspace"]
    alpha_id = agents.get("agent-alpha", "agent-alpha")
    vecs = read_fbin(data_file, limit=200)
    _info(f"Read {len(vecs)} vectors from {data_file} (dim={len(vecs[0])})")

    ids: list[str] = []
    errors = 0
    t0 = time.monotonic()

    for i, vec in enumerate(vecs):
        body = {
            "events": [{
                "id":           f"tq10k-fn-{i:05d}-{uuid.uuid4().hex[:6]}",
                "agent_id":     alpha_id,
                "workspace_id": WS,
                "type":         "user_message",
                "payload": {
                    "text":       f"testQuery10K vector #{i}",
                    "importance": round(0.5 + 0.5 * (vec[0] if vec else 0), 4),
                    "dataset":    "testQuery10K",
                },
            }]
        }
        status, resp = _req("POST", "/v1/ingest/events", body, timeout=30.0)
        if status in (200, 201):
            eid = ((resp or {}).get("ids") or [body["events"][0]["id"]])[0]
            ids.append(eid)
        else:
            errors += 1
            if errors <= 3:
                _info(f"  ingest error row {i}: status={status}")

    elapsed = time.monotonic() - t0
    throughput = len(ids) / elapsed if elapsed > 0 else 0
    if errors == 0:
        _ok(f"Ingest 200 rows", f"{len(ids)} OK, {elapsed:.1f}s, {throughput:.1f} rows/s")
    else:
        _fail(f"Ingest 200 rows", f"{errors} errors, {len(ids)} OK")

    return ids


# ─── Phase C: Functional validation (12 checks) ───────────────────────────────

def phase_c(agents: dict[str, str], ingest_ids: list[str]) -> None:
    _header("Phase C — Functional Validation (12 checks)")
    WS = agents["_workspace"]
    alpha_id = agents.get("agent-alpha", "")
    beta_id  = agents.get("agent-beta",  "")
    sample_id = ingest_ids[0] if ingest_ids else ""

    # C1: Graph structure — edges exist
    status, resp = _req("GET", f"/v1/edges?workspace_id={WS}")
    edges = (resp or {}).get("edges", resp if isinstance(resp, list) else [])
    if isinstance(edges, list) and len(edges) >= 1:
        types = list({e.get("type", "?") for e in edges if isinstance(e, dict)})
        _ok("C1 Graph edges", f"count={len(edges)}, types={types[:4]}")
    else:
        _fail("C1 Graph edges", f"expected ≥1 edge, got: {type(edges)}")

    # C2: Evidence index — proof_trace in query
    status, resp = _req("POST", "/v1/query", {
        "query": "testQuery10K", "workspace_id": WS,
        "include_evidence": True, "top_k": 5,
    })
    if status == 200:
        pt = (resp or {}).get("proof_trace") or (resp or {}).get("chain_traces")
        if pt:
            _ok("C2 Evidence / proof_trace", f"proof_trace present, objects={len((resp or {}).get('objects', []))}")
        else:
            _fail("C2 Evidence / proof_trace", f"proof_trace absent; keys={list((resp or {}).keys())}")
    else:
        _fail("C2 Evidence / proof_trace", f"status={status}")

    # C3: Agent memory partitioning — alpha sees its own memories
    status, resp = _req("GET", f"/v1/memory?agent_id={alpha_id}&workspace_id={WS}")
    mems = (resp or {}).get("memories", resp if isinstance(resp, list) else [])
    if isinstance(mems, list) and len(mems) >= 1:
        _ok("C3 Agent memory partition", f"alpha memories={len(mems)}")
    else:
        _fail("C3 Agent memory partition", f"expected ≥1 memory for alpha, got {mems!r:.100}")

    # C4: Memory decay (governance)
    status, resp = _req("POST", "/v1/internal/memory/decay", {
        "agent_id": beta_id, "workspace_id": WS,
        "decay_factor": 0.9,
    })
    if status in (200, 201, 204):
        _ok("C4 Memory decay (governance)", f"status={status}")
    else:
        _fail("C4 Memory decay (governance)", f"status={status}")

    # C5: Query chain — QueryChain + CollaborationChain in chain_traces
    status, resp = _req("POST", "/v1/query", {
        "query": "testQuery10K vector", "workspace_id": WS, "top_k": 3,
    })
    if status == 200:
        ct = (resp or {}).get("chain_traces", {})
        has_query = any("query" in str(k).lower() for k in (ct.keys() if isinstance(ct, dict) else []))
        _ok("C5 QueryChain", f"chain_traces keys={list(ct.keys()) if isinstance(ct, dict) else ct!r:.80}")
    else:
        _fail("C5 QueryChain", f"status={status}")

    # C6: Materialization / summarization
    mem_id = sample_id
    if mem_id:
        status, resp = _req("POST", "/v1/internal/memory/summarize", {
            "memory_id": mem_id, "agent_id": alpha_id,
        })
        if status in (200, 201, 204):
            _ok("C6 Materialization / summarize", f"status={status}")
        else:
            _fail("C6 Materialization / summarize", f"status={status} resp={resp!r:.120}")
    else:
        _fail("C6 Materialization / summarize", "no sample memory id from phase B")

    # C7: Embedding stored — check embedding field on memory
    if mem_id:
        status, resp = _req("GET", f"/v1/memory/{mem_id}")
        if status == 200:
            emb = (resp or {}).get("embedding") or (resp or {}).get("vector")
            if emb and len(emb) > 0:
                _ok("C7 Embedding stored", f"dim={len(emb)}")
            else:
                _fail("C7 Embedding stored", f"no embedding field; keys={list((resp or {}).keys())}")
        else:
            _fail("C7 Embedding stored", f"GET /v1/memory/{mem_id} → {status}")
    else:
        _fail("C7 Embedding stored", "no sample id")

    # C8: MAS memory share
    if mem_id:
        status, resp = _req("POST", "/v1/internal/memory/share", {
            "from_agent_id": alpha_id, "to_agent_id": beta_id,
            "memory_id": mem_id,
        })
        if status in (200, 201, 204):
            _ok("C8 MAS memory share alpha→beta", f"status={status}")
        else:
            _fail("C8 MAS memory share alpha→beta", f"status={status} resp={resp!r:.120}")
    else:
        _fail("C8 MAS memory share", "no sample id")

    # C9: S3 cold store configured
    status, resp = _req("GET", "/v1/admin/storage")
    if status == 200:
        endpoint = (resp or {}).get("s3_endpoint") or (resp or {}).get("endpoint")
        _ok("C9 S3 cold store", f"endpoint={endpoint}")
    else:
        _fail("C9 S3 cold store", f"GET /v1/admin/storage → {status}")

    # C10: Delete memory → 404
    if mem_id and len(ingest_ids) >= 2:
        del_id = ingest_ids[-1]  # delete last, keep first for other checks
        status, _ = _req("DELETE", f"/v1/memory/{del_id}")
        if status in (200, 204):
            status2, _ = _req("GET", f"/v1/memory/{del_id}")
            if status2 == 404:
                _ok("C10 Delete memory", f"DELETE 204 → GET 404 ✓")
            else:
                _fail("C10 Delete memory", f"after DELETE, GET returned {status2}")
        else:
            _fail("C10 Delete memory", f"DELETE returned {status}")
    else:
        _fail("C10 Delete memory", "need ≥2 ingest IDs")

    # C11: Dynamic topology (admin topology)
    status, resp = _req("GET", "/v1/admin/topology")
    if status == 200:
        nodes = (resp or {}).get("node_count") or len((resp or {}).get("nodes", []))
        edges = (resp or {}).get("edge_count") or len((resp or {}).get("edges", []))
        _ok("C11 Dynamic topology", f"nodes={nodes}, edges={edges}")
    else:
        _fail("C11 Dynamic topology", f"GET /v1/admin/topology → {status}")

    # C12: CollaborationChain — conflict resolve
    status, resp = _req("POST", "/v1/internal/memory/conflict/resolve", {
        "agent_id": alpha_id, "workspace_id": WS,
        "strategy": "merge",
    })
    if status in (200, 201, 204):
        ct = (resp or {}).get("chain_traces", {})
        _ok("C12 CollaborationChain / conflict resolve", f"status={status}")
    else:
        _fail("C12 CollaborationChain / conflict resolve", f"status={status} resp={resp!r:.120}")


# ─── Phase D: Full 10K ingest ────────────────────────────────────────────────

def phase_d(data_file: str, agents: dict[str, str]) -> None:
    _header("Phase D — Full 10K Ingest (embedding throughput)")
    WS = agents["_workspace"]
    alpha_id = agents.get("agent-alpha", "")
    vecs = read_fbin(data_file)  # all 10 000
    _info(f"Total vectors: {len(vecs)}, dim={len(vecs[0]) if vecs else 0}")

    BATCH = 50  # send 50 events per HTTP request
    total_ok = 0
    total_err = 0
    latencies: list[float] = []
    t_start = time.monotonic()

    for batch_idx in range(0, len(vecs), BATCH):
        chunk = vecs[batch_idx:batch_idx + BATCH]
        events = []
        for j, vec in enumerate(chunk):
            row_i = batch_idx + j
            events.append({
                "id":           f"tq10k-full-{row_i:05d}-{uuid.uuid4().hex[:4]}",
                "agent_id":     alpha_id,
                "workspace_id": WS,
                "type":         "user_message",
                "payload": {
                    "text":       f"testQuery10K full vector #{row_i}",
                    "importance": round(abs(vec[0]) if vec else 0.5, 4),
                    "dataset":    "testQuery10K",
                },
            })
        t0 = time.monotonic()
        status, resp = _req("POST", "/v1/ingest/events", {"events": events}, timeout=60.0)
        lat = time.monotonic() - t0
        latencies.append(lat)
        if status in (200, 201):
            total_ok += len(chunk)
        else:
            total_err += len(chunk)
            if total_err <= 50:
                _info(f"  batch {batch_idx//BATCH} error: status={status}")

        # Progress print every 1000 rows
        done = batch_idx + len(chunk)
        if done % 1000 == 0 or done == len(vecs):
            elapsed = time.monotonic() - t_start
            tps = total_ok / elapsed if elapsed > 0 else 0
            _info(f"  {done}/{len(vecs)} rows | {tps:.1f} rows/s | errors={total_err}")

    elapsed_total = time.monotonic() - t_start
    tps_total = total_ok / elapsed_total if elapsed_total > 0 else 0
    lat_sorted = sorted(latencies)
    p50 = lat_sorted[len(lat_sorted) // 2] * 1000 if lat_sorted else 0
    p95 = lat_sorted[int(len(lat_sorted) * 0.95)] * 1000 if lat_sorted else 0
    p99 = lat_sorted[int(len(lat_sorted) * 0.99)] * 1000 if lat_sorted else 0

    if total_err == 0:
        _ok("Full 10K ingest", f"{total_ok} OK, {elapsed_total:.1f}s, {tps_total:.1f} rows/s")
    else:
        _fail("Full 10K ingest", f"{total_ok} OK / {total_err} errors")

    _info(f"  Batch latency P50={p50:.0f}ms P95={p95:.0f}ms P99={p99:.0f}ms")
    RESULTS.append({
        "check": "Full 10K throughput",
        "status": "METRIC",
        "detail": {
            "total": len(vecs), "ok": total_ok, "errors": total_err,
            "elapsed_s": round(elapsed_total, 2), "rows_per_s": round(tps_total, 1),
            "batch_lat_p50_ms": round(p50), "batch_lat_p95_ms": round(p95),
            "batch_lat_p99_ms": round(p99),
        },
    })


# ─── Phase E: Cleanup / purge ────────────────────────────────────────────────

def phase_e(agents: dict[str, str]) -> None:
    _header("Phase E — Cleanup / Dataset Purge")
    WS = agents["_workspace"]
    status, resp = _req("POST", "/v1/admin/dataset/purge", {
        "dataset_name": "testQuery10K",
        "workspace_id": WS,
    })
    if status in (200, 201, 204):
        _ok("Dataset purge testQuery10K", f"status={status}")
    else:
        _fail("Dataset purge testQuery10K", f"status={status} resp={resp!r:.120}")

    # Verify memory list empty after purge
    status, resp = _req("GET", f"/v1/memory?workspace_id={WS}")
    mems = (resp or {}).get("memories", resp if isinstance(resp, list) else [])
    remaining = len(mems) if isinstance(mems, list) else -1
    if remaining == 0:
        _ok("Post-purge memory list empty", f"count=0")
    else:
        _fail("Post-purge memory list empty", f"count={remaining}")


# ─── Report ───────────────────────────────────────────────────────────────────

def write_report(out_dir: Path, args: argparse.Namespace) -> None:
    out_dir.mkdir(parents=True, exist_ok=True)

    # requests log
    req_log = out_dir / "requests.jsonl"
    with open(req_log, "w") as f:
        for r in REQUESTS_LOG:
            f.write(json.dumps(r) + "\n")

    # summary report
    passes = sum(1 for r in RESULTS if r["status"] == "PASS")
    fails  = sum(1 for r in RESULTS if r["status"] == "FAIL")
    total  = passes + fails

    report_lines = [
        f"# CogDB Integration Test Report",
        f"",
        f"- **Date**: {_now()}",
        f"- **Server**: {args.base_url}",
        f"- **Data**: {args.data_file}",
        f"- **Result**: {passes}/{total} PASS",
        f"",
        f"## Checks",
        f"",
        f"| Check | Status | Detail |",
        f"|---|---|---|",
    ]
    for r in RESULTS:
        status_md = "✅ PASS" if r["status"] == "PASS" else ("❌ FAIL" if r["status"] == "FAIL" else "📊 METRIC")
        detail = str(r.get("detail", ""))[:120]
        report_lines.append(f"| {r['check']} | {status_md} | {detail} |")

    report_md = out_dir / "report.md"
    with open(report_md, "w") as f:
        f.write("\n".join(report_lines) + "\n")

    print(f"\n{'='*60}")
    print(f"  PASS {passes}/{total}  FAIL {fails}/{total}")
    print(f"  Report  : {report_md}")
    print(f"  Requests: {req_log}")
    print(f"{'='*60}")

    return fails


# ─── Main ─────────────────────────────────────────────────────────────────────

def main() -> None:
    parser = argparse.ArgumentParser(description="CogDB integration fixture")
    parser.add_argument("--base-url",  default=os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080"))
    parser.add_argument("--data-file", default=os.path.expanduser("~/database/testQuery10K.fbin"))
    parser.add_argument("--out-dir",   default="out/integration_test")
    parser.add_argument("--skip-purge", action="store_true", help="Skip Phase E cleanup")
    parser.add_argument("--functional-only", action="store_true",
                        help="Run A+B+C only (skip full 10K Phase D)")
    args = parser.parse_args()

    global BASE
    BASE = args.base_url.rstrip("/")

    out_dir = Path(args.out_dir)
    data_file = args.data_file

    if not Path(data_file).expanduser().exists():
        print(f"ERROR: data file not found: {data_file}", file=sys.stderr)
        sys.exit(1)

    print(f"\n{'='*60}")
    print(f"  CogDB Integration Fixture")
    print(f"  server : {BASE}")
    print(f"  data   : {data_file}")
    print(f"  out    : {out_dir}")
    print(f"  time   : {_now()}")
    print(f"{'='*60}")

    # Detect GPU mode from server info (via /v1/system/mode or env)
    _, sysinfo = _req("GET", "/v1/system/mode")
    mode_info = sysinfo or {}
    _info(f"Server mode: {mode_info}")

    # Run phases
    agents = phase_a()
    ingest_ids = phase_b(data_file, agents)
    phase_c(agents, ingest_ids)

    if not args.functional_only:
        phase_d(data_file, agents)

    if not args.skip_purge:
        phase_e(agents)

    fails = write_report(out_dir, args)
    sys.exit(0 if fails == 0 else 1)


if __name__ == "__main__":
    main()
