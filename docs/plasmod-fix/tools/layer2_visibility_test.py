#!/usr/bin/env python3
"""
第二层实验：动态事件流 + Plasmod.md 2.5 指标（write-to-visible、物化滞后、负载下检索等）

依赖：HTTP API（默认 http://127.0.0.1:8080），仅标准库。

用法：
  终端1: make dev   （或 ANDB_HTTP_ADDR=127.0.0.1:8081 make dev）
  终端2: python3 docs/plasmod-fix/tools/layer2_visibility_test.py
        python3 docs/plasmod-fix/tools/layer2_visibility_test.py --full

环境：
  ANDB_BASE_URL / PLASMOD_BASE_URL
  PLASMOD_ADMIN_API_KEY         若启用 admin 鉴权，wipe 时需要

指标与 2.5 对应关系见 --help 与下方 run_* 说明。
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import threading
import time
import uuid
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


def _base_url() -> str:
    return (
        os.environ.get("ANDB_BASE_URL")
        or os.environ.get("PLASMOD_BASE_URL")
        or "http://127.0.0.1:8080"
    ).rstrip("/")


def _admin_key() -> str:
    return (
        os.environ.get("PLASMOD_ADMIN_API_KEY")
        or os.environ.get("ANDB_ADMIN_API_KEY")
        or os.environ.get("PLASMOD_ADMIN_KEY")
        or ""
    ).strip()


def _http_json(
    method: str,
    url: str,
    body: dict | None = None,
    *,
    timeout: float = 60.0,
    extra_headers: dict[str, str] | None = None,
) -> tuple[int, Any]:
    data = json.dumps(body).encode() if body is not None else None
    req = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    if extra_headers:
        for k, v in extra_headers.items():
            req.add_header(k, v)
    try:
        with urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            return resp.status, json.loads(raw) if raw else None
    except HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw.decode(errors="replace")
    except URLError as e:
        return 0, {"error": str(e)}


def health_ok(base: str) -> bool:
    st, _ = _http_json("GET", f"{base}/healthz", None, timeout=5.0)
    return st == 200


def ingest_event(base: str, ev: dict) -> tuple[int, Any]:
    return _http_json("POST", f"{base}/v1/ingest/events", ev, timeout=120.0)


def list_memories(base: str, agent_id: str, session_id: str) -> tuple[int, list]:
    q = f"agent_id={quote(agent_id)}&session_id={quote(session_id)}"
    st, body = _http_json("GET", f"{base}/v1/memory?{q}", None, timeout=30.0)
    if st != 200 or not isinstance(body, list):
        return st, []
    return st, body


def list_states(base: str, agent_id: str, session_id: str) -> tuple[int, list]:
    q = f"agent_id={quote(agent_id)}&session_id={quote(session_id)}"
    st, body = _http_json("GET", f"{base}/v1/states?{q}", None, timeout=30.0)
    if st != 200 or not isinstance(body, list):
        return st, []
    return st, body


def query_search(
    base: str,
    query_text: str,
    agent_id: str,
    session_id: str,
    tenant_id: str,
    workspace_id: str,
) -> tuple[int, Any]:
    body = {
        "query_text": query_text,
        "query_scope": "workspace",
        "session_id": session_id,
        "agent_id": agent_id,
        "tenant_id": tenant_id,
        "workspace_id": workspace_id,
        "top_k": 8,
        "time_window": {"from": "2020-01-01T00:00:00Z", "to": "2099-12-31T23:59:59Z"},
        "object_types": ["memory"],
        "memory_types": ["semantic", "episodic", "procedural"],
        "relation_constraints": [],
        "response_mode": "structured_evidence",
    }
    return _http_json("POST", f"{base}/v1/query", body, timeout=120.0)


def memory_visible(
    base: str, agent_id: str, session_id: str, memory_id: str, timeout_s: float, poll_s: float
) -> tuple[bool, float]:
    deadline = time.monotonic() + timeout_s
    t0 = time.monotonic()
    while time.monotonic() < deadline:
        st, rows = list_memories(base, agent_id, session_id)
        if st == 200:
            for row in rows:
                if isinstance(row, dict) and row.get("memory_id") == memory_id:
                    return True, time.monotonic() - t0
        time.sleep(poll_s)
    return False, time.monotonic() - t0


def state_visible_by_event(
    base: str,
    agent_id: str,
    session_id: str,
    event_id: str,
    timeout_s: float,
    poll_s: float,
) -> tuple[bool, float]:
    """物化后的 State：derived_from_event_id == event_id 或 state_id 以 event_id 结尾。"""
    deadline = time.monotonic() + timeout_s
    t0 = time.monotonic()
    while time.monotonic() < deadline:
        st, rows = list_states(base, agent_id, session_id)
        if st == 200:
            for row in rows:
                if not isinstance(row, dict):
                    continue
                if row.get("derived_from_event_id") == event_id:
                    return True, time.monotonic() - t0
                sid = row.get("state_id") or ""
                if sid.endswith(event_id):
                    return True, time.monotonic() - t0
        time.sleep(poll_s)
    return False, time.monotonic() - t0


def build_event(
    *,
    event_id: str,
    agent_id: str,
    session_id: str,
    tenant_id: str,
    workspace_id: str,
    event_type: str,
    text: str,
    causal_refs: list[str],
) -> dict:
    now = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())
    payload_size = len(text.encode("utf-8"))
    trigger_state_update = event_type == "state_update"
    trigger_relation_provenance_update = len(causal_refs) > 0
    return {
        "event_id": event_id,
        "tenant_id": tenant_id,
        "workspace_id": workspace_id,
        "agent_id": agent_id,
        "session_id": session_id,
        "event_type": event_type,
        "event_time": now,
        "ingest_time": now,
        "visible_time": now,
        "logical_ts": 0,
        "parent_event_id": "",
        "causal_refs": causal_refs,
        "payload": {
            "text": text,
            # 2.3 synthetic-stream fields for experiment bookkeeping.
            "payload_size": payload_size,
            "embedding_present": False,
            "trigger_state_update": trigger_state_update,
            "trigger_relation_provenance_update": trigger_relation_provenance_update,
        },
        "source": "layer2_synthetic",
        "importance": 0.6,
        "visibility": "private",
        "version": 1,
    }


def percentile(sorted_vals: list[float], p: float) -> float:
    if not sorted_vals:
        return 0.0
    s = sorted_vals
    k = (len(s) - 1) * p / 100.0
    f = int(k)
    c = min(f + 1, len(s) - 1)
    return s[f] + (k - f) * (s[c] - s[f]) if f != c else s[f]


def run_sequential(
    base: str,
    n: int,
    agent_id: str,
    session_id: str,
    tenant_id: str,
    workspace_id: str,
    visibility_timeout: float,
    poll: float,
    include_state_mix: bool,
) -> tuple[list[float], list[float], list[float]]:
    """
    返回：
      poll_after_ack_ms — ingest 返回后轮询到可见（与旧版一致）
      e2e_client_ms     — 从发起 ingest POST 前到首次可见（2.5 write-to-visible 更完整口径）
      ingest_http_ms    — HTTP ingest 耗时
    """
    poll_after_ack_ms: list[float] = []
    e2e_client_ms: list[float] = []
    ingest_http_ms: list[float] = []

    for i in range(n):
        token = f"LAYER2_TOKEN_{uuid.uuid4().hex[:12]}"
        eid = f"evt_layer2_{uuid.uuid4().hex[:10]}"
        mem_id = f"mem_{eid}"

        if include_state_mix and i % 4 == 3:
            se = build_event(
                event_id=f"{eid}_state",
                agent_id=agent_id,
                session_id=session_id,
                tenant_id=tenant_id,
                workspace_id=workspace_id,
                event_type="state_update",
                text=f"state after {token}",
                causal_refs=[],
            )
            st, _ = ingest_event(base, se)
            if st != 200:
                print(f"  [warn] state_update ingest status={st}", file=sys.stderr)

        ev = build_event(
            event_id=eid,
            agent_id=agent_id,
            session_id=session_id,
            tenant_id=tenant_id,
            workspace_id=workspace_id,
            event_type="user_message",
            text=f"visibility probe {token}",
            causal_refs=[f"evt_prior_{i}"] if i > 0 else [],
        )
        t_client_start = time.perf_counter()
        st, _ack = ingest_event(base, ev)
        t_after_ack = time.perf_counter()
        ih_ms = (t_after_ack - t_client_start) * 1000
        ingest_http_ms.append(ih_ms)

        if st != 200:
            print(f"  [fail] ingest {eid} status={st}", file=sys.stderr)
            continue

        ok, vis_s = memory_visible(base, agent_id, session_id, mem_id, visibility_timeout, poll)
        poll_ms = vis_s * 1000
        e2e_ms = (time.perf_counter() - t_client_start) * 1000
        if ok:
            poll_after_ack_ms.append(poll_ms)
            e2e_client_ms.append(e2e_ms)
            print(
                f"  ok event={eid} mem={mem_id} "
                f"e2e_write_to_visible_ms={e2e_ms:.2f} "
                f"poll_after_ack_ms={poll_ms:.2f} ingest_http_ms={ih_ms:.2f}"
            )
        else:
            print(
                f"  TIMEOUT waiting for {mem_id} after {visibility_timeout}s ingest_http_ms={ih_ms:.2f}",
                file=sys.stderr,
            )
    return poll_after_ack_ms, e2e_client_ms, ingest_http_ms


def run_state_materialization_lag(
    base: str,
    n: int,
    agent_id: str,
    session_id: str,
    tenant_id: str,
    workspace_id: str,
    timeout: float,
    poll: float,
) -> list[float]:
    """2.5 materialization lag（state）：仅 state_update，直到 GET /v1/states 可见 derived_from_event_id。"""
    lags: list[float] = []
    print(f"[layer2] state materialization lag (n={n}, event_type=state_update)")
    for _ in range(n):
        eid = f"evt_state_{uuid.uuid4().hex[:12]}"
        ev = build_event(
            event_id=eid,
            agent_id=agent_id,
            session_id=session_id,
            tenant_id=tenant_id,
            workspace_id=workspace_id,
            event_type="state_update",
            text=f"state probe {uuid.uuid4().hex[:8]}",
            causal_refs=[],
        )
        t0 = time.perf_counter()
        st, _ = ingest_event(base, ev)
        if st != 200:
            print(f"  [fail] state ingest {eid} status={st}", file=sys.stderr)
            continue
        ok, vis_s = state_visible_by_event(base, agent_id, session_id, eid, timeout, poll)
        lag_ms = (time.perf_counter() - t0) * 1000
        if ok:
            lags.append(lag_ms)
            print(f"  ok event={eid} state_materialization_lag_ms={lag_ms:.2f} (poll window {vis_s*1000:.2f}ms)")
        else:
            print(f"  TIMEOUT state {eid}", file=sys.stderr)
    return lags


def run_retrieval_under_write_load(
    base: str,
    duration_s: float,
    ingest_hz: float,
    query_hz: float,
    agent_id: str,
    session_id: str,
    tenant_id: str,
    workspace_id: str,
) -> tuple[list[float], int, int]:
    """
    2.5 retrieval latency under write load：后台持续 ingest，另一循环 POST /v1/query。
    返回 (query_latency_ms_samples, ingest_count, query_count)
    """
    stop = threading.Event()
    lock = threading.Lock()
    query_lat_ms: list[float] = []
    ingests = 0
    queries = 0

    # 固定锚点文本：先写入一条再开始压测，使 query 有稳定语义锚点
    anchor = f"layer2_anchor_{uuid.uuid4().hex[:8]}"
    aid = f"evt_anchor_{uuid.uuid4().hex[:8]}"
    ev0 = build_event(
        event_id=aid,
        agent_id=agent_id,
        session_id=session_id,
        tenant_id=tenant_id,
        workspace_id=workspace_id,
        event_type="user_message",
        text=f"anchor query text {anchor}",
        causal_refs=[],
    )
    ingest_event(base, ev0)

    def writer() -> None:
        nonlocal ingests
        interval = 1.0 / max(ingest_hz, 0.001)
        while not stop.is_set():
            eid = f"evt_wload_{uuid.uuid4().hex[:10]}"
            ev = build_event(
                event_id=eid,
                agent_id=agent_id,
                session_id=session_id,
                tenant_id=tenant_id,
                workspace_id=workspace_id,
                event_type="user_message",
                text=f"wload {uuid.uuid4().hex[:6]}",
                causal_refs=[],
            )
            st, _ = ingest_event(base, ev)
            if st == 200:
                with lock:
                    ingests += 1
            time.sleep(interval)

    def reader() -> None:
        nonlocal queries
        q_interval = 1.0 / max(query_hz, 0.001)
        qtext = f"anchor query text {anchor}"
        while not stop.is_set():
            tq = time.perf_counter()
            st, _ = query_search(base, qtext, agent_id, session_id, tenant_id, workspace_id)
            dt = (time.perf_counter() - tq) * 1000
            if st == 200:
                with lock:
                    query_lat_ms.append(dt)
                    queries += 1
            time.sleep(q_interval)

    tw = threading.Thread(target=writer, daemon=True)
    tr = threading.Thread(target=reader, daemon=True)
    tw.start()
    tr.start()
    time.sleep(duration_s)
    stop.set()
    tw.join(timeout=2.0)
    tr.join(timeout=2.0)
    return query_lat_ms, ingests, queries


def run_stale_probe(
    base: str,
    samples: int,
    agent_id: str,
    session_id: str,
    tenant_id: str,
    workspace_id: str,
    poll: float,
) -> tuple[int, int]:
    """
    2.5 stale result rate（粗测）：每次 ingest 后立即检查 GET /v1/memory 是否已含 mem_<event_id>。
    同步路径下应接近 0；若异步变长，未出现则计 stale。
    """
    stale = 0
    ok = 0
    for _ in range(samples):
        eid = f"evt_stale_{uuid.uuid4().hex[:10]}"
        mem_id = f"mem_{eid}"
        ev = build_event(
            event_id=eid,
            agent_id=agent_id,
            session_id=session_id,
            tenant_id=tenant_id,
            workspace_id=workspace_id,
            event_type="user_message",
            text=f"stale probe {eid}",
            causal_refs=[],
        )
        st, _ = ingest_event(base, ev)
        if st != 200:
            stale += 1
            continue
        # 极短窗口内第一次可见性（无额外 sleep = 最严）
        visible, _ = memory_visible(base, agent_id, session_id, mem_id, timeout_s=0.25, poll_s=poll)
        if visible:
            ok += 1
        else:
            stale += 1
    return stale, ok


def run_replay_sim_ingest_throughput(base: str, n: int, agent_id: str, session_id: str, tenant_id: str, workspace_id: str) -> float:
    """
    合成「连续 ingest」吞吐（events/s）。不是 WAL 文件 replay，仅作 2.5 replay throughput 的工程近似备注。
    """
    t0 = time.perf_counter()
    for i in range(n):
        eid = f"evt_rps_{i}_{uuid.uuid4().hex[:6]}"
        ev = build_event(
            event_id=eid,
            agent_id=agent_id,
            session_id=session_id,
            tenant_id=tenant_id,
            workspace_id=workspace_id,
            event_type="user_message",
            text=f"rps {i}",
            causal_refs=[],
        )
        st, _ = ingest_event(base, ev)
        if st != 200:
            break
    elapsed = time.perf_counter() - t0
    return n / elapsed if elapsed > 0 else 0.0


def run_admin_replay_preview_throughput(base: str, *, from_lsn: int = 0, limit: int = 1000) -> tuple[float, int, int]:
    """
    通过 /v1/admin/replay dry-run 估算 replay preview 吞吐（events/s）。
    返回: (events_per_s, scanned, http_status)。
    """
    h: dict[str, str] = {}
    k = _admin_key()
    if k:
        h["X-Admin-Key"] = k
    t0 = time.perf_counter()
    st, body = _http_json(
        "POST",
        f"{base}/v1/admin/replay",
        {"from_lsn": int(from_lsn), "limit": int(limit), "dry_run": True, "apply": False},
        timeout=120.0,
        extra_headers=h if h else None,
    )
    elapsed = max(time.perf_counter() - t0, 1e-9)
    scanned = 0
    if isinstance(body, dict):
        # 兼容不同字段命名
        for key in ("scanned", "events_scanned", "preview_count", "total"):
            v = body.get(key)
            if isinstance(v, int):
                scanned = v
                break
    return (scanned / elapsed), scanned, st


def run_recovery_after_wipe(base: str, agent_id: str, session_id: str, tenant_id: str, workspace_id: str) -> tuple[float, bool]:
    """2.5 recovery time：admin wipe 后到 health + 一次 ingest 成功。"""
    headers = {}
    key = _admin_key()
    if key:
        headers["X-Admin-Key"] = key
    t0 = time.perf_counter()
    st, body = _http_json(
        "POST",
        f"{base}/v1/admin/data/wipe",
        {"confirm": "delete_all_data"},
        timeout=120.0,
        extra_headers=headers if headers else None,
    )
    if st != 200:
        print(f"[layer2] wipe failed status={st} body={body!r}", file=sys.stderr)
        return -1.0, False
    # 等到健康且可 ingest
    deadline = time.monotonic() + 60.0
    recovered = False
    while time.monotonic() < deadline:
        if health_ok(base):
            ev = build_event(
                event_id=f"evt_post_wipe_{uuid.uuid4().hex[:8]}",
                agent_id=agent_id,
                session_id=session_id,
                tenant_id=tenant_id,
                workspace_id=workspace_id,
                event_type="user_message",
                text="post wipe probe",
                causal_refs=[],
            )
            st2, _ = ingest_event(base, ev)
            if st2 == 200:
                recovered = True
                break
        time.sleep(0.1)
    elapsed = time.perf_counter() - t0
    return elapsed, recovered


def print_summary(name: str, vals: list[float]) -> None:
    if not vals:
        print(f"[layer2] {name}: (no samples)")
        return
    s = sorted(vals)
    print(
        f"[layer2] {name}: n={len(s)} p50={percentile(s, 50):.2f} "
        f"p95={percentile(s, 95):.2f} max={s[-1]:.2f} ms"
    )


def main() -> int:
    ap = argparse.ArgumentParser(
        description="Layer-2 synthetic stream + Plasmod.md §2.5 metrics",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
指标对照（2.5）:
  write-to-visible     → e2e_write_to_visible_ms / poll_after_ack_ms
  materialization lag    → state_materialization_lag_ms (state_update)
  retrieval under load   → query_latency_ms under write (后台 ingest)
  stale result rate      → stale_probe (立即 list memory 是否可见)
  replay throughput      → admin_replay_preview_events_per_s（/v1/admin/replay dry-run）+ synthetic_ingest_events_per_s
  recovery time          → --wipe-recovery
        """,
    )
    ap.add_argument("--base-url", default=_base_url())
    ap.add_argument("--events", type=int, default=20)
    ap.add_argument("--agent-id", default="agent_layer2")
    ap.add_argument("--session-id", default="sess_layer2_demo")
    ap.add_argument("--tenant-id", default="t_layer2")
    ap.add_argument("--workspace-id", default="w_layer2")
    ap.add_argument("--visibility-timeout", type=float, default=15.0)
    ap.add_argument("--poll", type=float, default=0.02)
    ap.add_argument("--mixed-state", action="store_true")
    ap.add_argument(
        "--full",
        action="store_true",
        help="sequential + state lag + under-write query load + stale + replay sim",
    )
    ap.add_argument(
        "--state-lag-n",
        type=int,
        default=0,
        help="state_update materialization probes (default 0; --full uses 5 if still 0)",
    )
    ap.add_argument("--write-load-seconds", type=float, default=12.0)
    ap.add_argument("--write-load-ingest-hz", type=float, default=8.0)
    ap.add_argument("--write-load-query-hz", type=float, default=4.0)
    ap.add_argument("--stale-samples", type=int, default=30)
    ap.add_argument("--replay-sim-n", type=int, default=400)
    ap.add_argument("--replay-preview-limit", type=int, default=1000)
    ap.add_argument("--wipe-recovery", action="store_true", help="destructive: POST /v1/admin/data/wipe then measure recovery")
    ap.add_argument("--i-understand-wipe", action="store_true", help="required with --wipe-recovery")
    ap.add_argument(
        "--stress",
        action="store_true",
        help="legacy: after main, background ingest + health poll only",
    )
    ap.add_argument("--stress-seconds", type=float, default=10.0)
    ap.add_argument("--stress-health-qps", type=float, default=5.0)
    args = ap.parse_args()

    base = args.base_url.rstrip("/")
    print(f"[layer2] base_url={base}")
    if not health_ok(base):
        print("[layer2] ERROR: /healthz failed.", file=sys.stderr)
        return 1

    ok_main = True

    poll_ms: list[float] = []
    e2e_ms: list[float] = []
    ingest_ms: list[float] = []
    if args.events > 0:
        print("[layer2] §2.5 — sequential memory visibility (GET /v1/memory)")
        poll_ms, e2e_ms, ingest_ms = run_sequential(
            base,
            args.events,
            args.agent_id,
            args.session_id,
            args.tenant_id,
            args.workspace_id,
            args.visibility_timeout,
            args.poll,
            args.mixed_state or args.full,
        )
        print()
        print_summary("poll_after_ack_ms (ingest 返回 → 首次可见)", poll_ms)
        print_summary("e2e_write_to_visible_ms (发起 ingest 前 → 首次可见)", e2e_ms)
        print_summary("ingest_http_ms", ingest_ms)
        if not e2e_ms:
            ok_main = False
    else:
        print("[layer2] skip sequential memory (--events 0)")

    state_lag_n = args.state_lag_n
    if args.full and state_lag_n == 0:
        state_lag_n = 5
    if state_lag_n > 0:
        print()
        sl = run_state_materialization_lag(
            base,
            state_lag_n,
            args.agent_id,
            args.session_id,
            args.tenant_id,
            args.workspace_id,
            args.visibility_timeout,
            args.poll,
        )
        print_summary("state_materialization_lag_ms", sl)

    if args.full:
        print()
        print(
            f"[layer2] §2.5 — retrieval latency under write load "
            f"({args.write_load_seconds}s, ingest≈{args.write_load_ingest_hz}/s, query≈{args.write_load_query_hz}/s)"
        )
        qlats, ic, qc = run_retrieval_under_write_load(
            base,
            args.write_load_seconds,
            args.write_load_ingest_hz,
            args.write_load_query_hz,
            args.agent_id,
            args.session_id,
            args.tenant_id,
            args.workspace_id,
        )
        print_summary("query_latency_ms_under_write_load", qlats)
        print(f"  ingest_events≈{ic} query_requests={qc}")

        print()
        print(f"[layer2] §2.5 — stale probe (immediate list after ingest, n={args.stale_samples})")
        stale, good = run_stale_probe(
            base,
            args.stale_samples,
            args.agent_id,
            args.session_id,
            args.tenant_id,
            args.workspace_id,
            args.poll,
        )
        total = stale + good
        rate = (stale / total) if total else 0.0
        print(f"  stale={stale} ok={good} stale_result_rate={rate:.4f}")

        print()
        print(f"[layer2] §2.5 — admin replay preview throughput (limit={args.replay_preview_limit})")
        rrps, scanned, st = run_admin_replay_preview_throughput(
            base,
            from_lsn=0,
            limit=args.replay_preview_limit,
        )
        if st == 200:
            print(f"  admin_replay_preview_events_per_s≈{rrps:.1f} (scanned={scanned})")
        else:
            print(f"  admin_replay_preview failed status={st} (fallback to synthetic only)")

        print()
        print(f"[layer2] §2.5 — synthetic ingest throughput (n={args.replay_sim_n}, non-replay baseline)")
        rps = run_replay_sim_ingest_throughput(
            base,
            args.replay_sim_n,
            args.agent_id,
            args.session_id,
            args.tenant_id,
            args.workspace_id,
        )
        print(f"  synthetic_ingest_events_per_s≈{rps:.1f}")

    if args.wipe_recovery:
        if not args.i_understand_wipe:
            print("[layer2] refuse wipe: pass --i-understand-wipe", file=sys.stderr)
            return 1
        print()
        print("[layer2] §2.5 — recovery_time after admin data wipe (destructive)")
        rec_s, ok = run_recovery_after_wipe(
            base, args.agent_id, args.session_id, args.tenant_id, args.workspace_id
        )
        if ok:
            print(f"  recovery_time_s≈{rec_s:.3f} (wipe ack → health + successful ingest)")
        else:
            print("  recovery failed within timeout", file=sys.stderr)
            ok_main = False

    if args.stress:
        print()
        print("[layer2] legacy stress: background ingest + /healthz poll")
        run_stress_legacy(
            base,
            args.stress_seconds,
            args.agent_id,
            args.session_id,
            args.tenant_id,
            args.workspace_id,
            args.stress_health_qps,
        )

    print()
    print("[layer2] 提示: 完整 2.5 一键跑: python3 docs/plasmod-fix/tools/layer2_visibility_test.py --full")
    return 0 if ok_main else 1


def run_stress_legacy(
    base: str,
    duration_s: float,
    agent_id: str,
    session_id: str,
    tenant_id: str,
    workspace_id: str,
    qps: float,
) -> None:
    stop = threading.Event()
    errs: list[str] = []

    def writer() -> None:
        n = 0
        while not stop.is_set():
            eid = f"evt_stress_{uuid.uuid4().hex[:12]}"
            ev = build_event(
                event_id=eid,
                agent_id=agent_id,
                session_id=session_id,
                tenant_id=tenant_id,
                workspace_id=workspace_id,
                event_type="user_message",
                text=f"stress {n}",
                causal_refs=[],
            )
            st, _ = ingest_event(base, ev)
            if st != 200:
                errs.append(f"ingest {st}")
            n += 1
            time.sleep(0.05)

    th = threading.Thread(target=writer, daemon=True)
    th.start()
    t0 = time.monotonic()
    queries = 0
    while time.monotonic() - t0 < duration_s:
        st, _ = _http_json("GET", f"{base}/healthz", None, timeout=5.0)
        if st != 200:
            errs.append(f"health {st}")
        queries += 1
        time.sleep(max(0.001, 1.0 / qps))
    stop.set()
    th.join(timeout=2.0)
    print(f"  stress done: health_queries={queries} writer_errors={len(errs)}")
    if errs[:5]:
        print(f"  sample errors: {errs[:5]}")


if __name__ == "__main__":
    raise SystemExit(main())
