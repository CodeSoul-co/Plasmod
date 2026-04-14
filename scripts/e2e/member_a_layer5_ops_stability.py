#!/usr/bin/env python3
"""Member-A Layer5 ops stability checks: replay/rollback/wipe/cold-purge."""

from __future__ import annotations

import json
import os
import time
import uuid
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def http_json(method: str, url: str, body: dict | None, key: str) -> tuple[int, object]:
    data = json.dumps(body).encode() if body is not None else None
    req = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    if key:
        req.add_header("X-Admin-Key", key)
    try:
        with urlopen(req, timeout=120.0) as resp:
            raw = resp.read()
            return resp.status, json.loads(raw) if raw else {}
    except HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw.decode(errors="replace")


def ingest_event(base: str, key: str, body: dict) -> tuple[int, object]:
    return http_json("POST", f"{base}/v1/ingest/events", body, key)


def list_states(base: str, key: str, agent_id: str, session_id: str) -> tuple[int, object]:
    return http_json("GET", f"{base}/v1/states?agent_id={agent_id}&session_id={session_id}", None, key)


def state_visible(base: str, key: str, agent_id: str, session_id: str, event_id: str, timeout_s: float = 8.0) -> tuple[bool, int]:
    deadline = time.monotonic() + timeout_s
    last_status = 0
    while time.monotonic() < deadline:
        st, rows = list_states(base, key, agent_id, session_id)
        last_status = st
        if st == 200 and isinstance(rows, list):
            for row in rows:
                if isinstance(row, dict) and row.get("derived_from_event_id") == event_id:
                    return True, st
        time.sleep(0.1)
    return False, last_status


def main() -> int:
    base = os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
    key = (
        os.environ.get("PLASMOD_ADMIN_API_KEY")
        or os.environ.get("ANDB_ADMIN_API_KEY")
        or ""
    ).strip()
    destructive = os.environ.get("MEMBER_A_RUN_DESTRUCTIVE", "false").lower() == "true"

    report: dict[str, object] = {"base_url": base, "destructive": destructive}
    checks: dict[str, dict[str, object]] = {}

    st, body = http_json("GET", f"{base}/healthz", None, key)
    checks["healthz"] = {"status": st, "ok": st == 200, "body": body}

    st, body = http_json("POST", f"{base}/v1/admin/replay", {"from_lsn": 0, "limit": 50, "dry_run": True}, key)
    checks["replay_preview"] = {"status": st, "ok": st == 200, "body": body}

    st, body = http_json("POST", f"{base}/v1/admin/s3/cold-purge", {"confirm": "purge_cold_tier", "dry_run": True}, key)
    checks["cold_purge_dry_run"] = {"status": st, "ok": st == 200, "body": body}

    # 5-T11: tool_call -> state_update -> /v1/states materialized visibility
    agent_id = f"agent_t11_{uuid.uuid4().hex[:6]}"
    session_id = f"sess_t11_{uuid.uuid4().hex[:6]}"
    workspace_id = f"w_t11_{uuid.uuid4().hex[:6]}"
    tool_eid = f"evt_tool_{uuid.uuid4().hex[:8]}"
    state_eid = f"evt_state_{uuid.uuid4().hex[:8]}"
    st_tool, body_tool = ingest_event(
        base,
        key,
        {
            "event_id": tool_eid,
            "agent_id": agent_id,
            "session_id": session_id,
            "tenant_id": "t_member_a",
            "workspace_id": workspace_id,
            "event_type": "tool_call",
            "payload": {"text": "tool_call: update env with latest weather"},
        },
    )
    st_state, body_state = ingest_event(
        base,
        key,
        {
            "event_id": state_eid,
            "agent_id": agent_id,
            "session_id": session_id,
            "tenant_id": "t_member_a",
            "workspace_id": workspace_id,
            "event_type": "state_update",
            "payload": {"text": "env_state: weather=sunny", "triggered_by_tool_event": tool_eid},
            "causal_refs": [tool_eid],
        },
    )
    vis_ok, vis_status = state_visible(base, key, agent_id, session_id, state_eid)
    st_query, body_query = http_json(
        "POST",
        f"{base}/v1/query",
        {
            "query_text": "current env weather",
            "workspace_id": workspace_id,
            "query_scope": "workspace",
            "top_k": 3,
            "include_evidence": True,
        },
        key,
    )
    checks["tool_call_env_update"] = {
        "status_tool_call": st_tool,
        "status_state_update": st_state,
        "state_visible_status": vis_status,
        "query_status": st_query,
        "ok": (st_tool == 200 and st_state == 200 and vis_ok and st_query == 200),
        "agent_id": agent_id,
        "session_id": session_id,
        "workspace_id": workspace_id,
        "tool_event_id": tool_eid,
        "state_event_id": state_eid,
        "tool_body": body_tool,
        "state_body": body_state,
        "query_body": body_query,
    }

    st_mem, mems = http_json("GET", f"{base}/v1/memory", None, key)
    mem_id = ""
    if st_mem == 200 and isinstance(mems, list) and mems:
        first = mems[0]
        if isinstance(first, dict):
            mem_id = str(first.get("memory_id", ""))
    if mem_id:
        st, body = http_json(
            "POST",
            f"{base}/v1/admin/rollback",
            {"memory_id": mem_id, "action": "deactivate", "dry_run": True, "reason": "layer5_ops"},
            key,
        )
        checks["rollback_dry_run"] = {"status": st, "ok": st == 200, "memory_id": mem_id, "body": body}
    else:
        checks["rollback_dry_run"] = {"status": 0, "ok": True, "note": "no memory rows, skip"}

    if destructive:
        t0 = time.perf_counter()
        st, body = http_json("POST", f"{base}/v1/admin/data/wipe", {"confirm": "delete_all_data"}, key)
        checks["wipe"] = {
            "status": st,
            "ok": st == 200,
            "elapsed_s": round(time.perf_counter() - t0, 3),
            "body": body,
        }
    else:
        checks["wipe"] = {"status": 0, "ok": True, "note": "skipped (MEMBER_A_RUN_DESTRUCTIVE=false)"}

    report["checks"] = checks
    report["ok"] = all(v.get("ok", False) for v in checks.values())
    print(json.dumps(report, ensure_ascii=False))
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
