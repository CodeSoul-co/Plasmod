#!/usr/bin/env python3
"""
End-to-end storage → retrieval test for CogDB (ANDB).

Covers:
  1. Health check
  2. Ingest events (memory / state / artifact types)
  3. Query — verify ingested IDs are returned
  4. Proof trace (/v1/traces/{id}) — canonical object, edges, evidence
  5. Canonical CRUD — /v1/memory, /v1/agents, /v1/sessions
  6. Memory recall (/v1/internal/memory/recall)

Usage:
  # Server must be running first (make dev or make docker-up)
  source .venv/bin/activate
  python scripts/e2e_storage_retrieval_test.py

  # Or point at a remote server:
  PLASMOD_BASE_URL=http://10.0.0.1:8080 python scripts/e2e_storage_retrieval_test.py
"""

from __future__ import annotations

import datetime as dt
import json
import os
import sys
import uuid

import requests

BASE_URL = os.environ.get("PLASMOD_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
TIMEOUT = float(os.environ.get("PLASMOD_HTTP_TIMEOUT", "10"))

PASS = "\033[32mPASS\033[0m"
FAIL = "\033[31mFAIL\033[0m"

_results: list[tuple[str, bool, str]] = []


def _uid() -> str:
    return uuid.uuid4().hex[:10]


def _now() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _post(path: str, body: dict) -> requests.Response:
    return requests.post(f"{BASE_URL}{path}", json=body, timeout=TIMEOUT)


def _get(path: str, params: dict | None = None) -> requests.Response:
    return requests.get(f"{BASE_URL}{path}", params=params, timeout=TIMEOUT)


def check(name: str, ok: bool, detail: str = "") -> bool:
    _results.append((name, ok, detail))
    status = PASS if ok else FAIL
    msg = f"  [{status}] {name}"
    if detail:
        msg += f"  — {detail}"
    print(msg)
    return ok


# ── 1. Health check ────────────────────────────────────────────────────────────

def test_health():
    print("\n── 1. Health check")
    r = _get("/healthz")
    check("GET /healthz → 200", r.status_code == 200, f"status={r.status_code}")
    check("response has status=ok", r.json().get("status") == "ok", str(r.json()))


# ── 2. Ingest events ───────────────────────────────────────────────────────────

def test_ingest() -> dict[str, str]:
    """Ingest several events; return {label: memory_id}."""
    print("\n── 2. Ingest events")
    now = _now()
    ids: dict[str, str] = {}

    cases = [
        {
            "label": "memory_semantic",
            "body": {
                "event_id": f"evt_e2e_sem_{_uid()}",
                "agent_id": "agent_e2e",
                "session_id": "sess_e2e",
                "event_type": "user_message",
                "event_time": now, "ingest_time": now, "visible_time": now,
                "payload": {"text": "The transformer architecture uses self-attention mechanisms"},
                "importance": 0.9,
                "visibility": "private",
                "version": 1,
            },
        },
        {
            "label": "memory_episodic",
            "body": {
                "event_id": f"evt_e2e_epi_{_uid()}",
                "agent_id": "agent_e2e",
                "session_id": "sess_e2e",
                "event_type": "user_message",
                "event_time": now, "ingest_time": now, "visible_time": now,
                "payload": {"text": "Yesterday the agent completed a code review task successfully"},
                "importance": 0.7,
                "visibility": "private",
                "version": 1,
            },
        },
        {
            "label": "memory_procedural",
            "body": {
                "event_id": f"evt_e2e_pro_{_uid()}",
                "agent_id": "agent_e2e",
                "session_id": "sess_e2e",
                "event_type": "tool_call",
                "event_time": now, "ingest_time": now, "visible_time": now,
                "payload": {"text": "To deploy the service: build image, push to registry, apply k8s manifest"},
                "importance": 0.8,
                "visibility": "private",
                "version": 1,
            },
        },
        {
            "label": "unrelated",
            "body": {
                "event_id": f"evt_e2e_unrel_{_uid()}",
                "agent_id": "agent_e2e",
                "session_id": "sess_e2e",
                "event_type": "user_message",
                "event_time": now, "ingest_time": now, "visible_time": now,
                "payload": {"text": "The weather today is sunny with light winds"},
                "importance": 0.2,
                "visibility": "private",
                "version": 1,
            },
        },
    ]

    for case in cases:
        r = _post("/v1/ingest/events", case["body"])
        ok = r.status_code == 200
        data = r.json() if ok else {}
        mid = data.get("memory_id", "")
        check(
            f"ingest {case['label']} → 200 + memory_id",
            ok and bool(mid),
            f"memory_id={mid} lsn={data.get('lsn')} edges={data.get('edges')}",
        )
        if mid:
            ids[case["label"]] = mid

    return ids


# ── 3. Query — verify retrieval ────────────────────────────────────────────────

def test_query(ingested: dict[str, str]):
    print("\n── 3. Query retrieval")

    cases = [
        {
            "label": "attention mechanism query",
            "body": {
                "query_text": "self-attention transformer",
                "query_scope": "global",
                "top_k": 5,
                "agent_id": "agent_e2e",
                "session_id": "sess_e2e",
            },
            "expect_ids": [ingested.get("memory_semantic")],
        },
        {
            "label": "code review episodic query",
            "body": {
                "query_text": "code review task agent",
                "query_scope": "global",
                "top_k": 5,
                "agent_id": "agent_e2e",
            },
            "expect_ids": [ingested.get("memory_episodic")],
        },
        {
            "label": "deployment procedure query",
            "body": {
                "query_text": "deploy service kubernetes registry",
                "query_scope": "global",
                "top_k": 5,
                "agent_id": "agent_e2e",
            },
            "expect_ids": [ingested.get("memory_procedural")],
        },
    ]

    for case in cases:
        r = _post("/v1/query", case["body"])
        ok = r.status_code == 200
        if not ok:
            check(f"query '{case['label']}' → 200", False, f"status={r.status_code} body={r.text[:200]}")
            continue
        data = r.json()
        objects: list[str] = data.get("objects", [])
        expect_ids = [i for i in case["expect_ids"] if i]
        found = all(eid in objects for eid in expect_ids)
        check(
            f"query '{case['label']}' returns expected memory_id",
            found,
            f"returned={objects[:5]}  expected={expect_ids}",
        )
        check(
            f"query '{case['label']}' has proof_trace",
            bool(data.get("proof_trace")),
            f"proof_trace_len={len(data.get('proof_trace', []))}",
        )


# ── 4. Proof trace ─────────────────────────────────────────────────────────────

def test_traces(ingested: dict[str, str]):
    print("\n── 4. Proof trace /v1/traces/{id}")
    mid = ingested.get("memory_semantic")
    if not mid:
        check("traces: memory_id available", False, "no memory_id from ingest")
        return

    r = _get(f"/v1/traces/{mid}")
    ok = r.status_code == 200
    check(f"GET /v1/traces/{mid} → 200", ok, f"status={r.status_code}")
    if not ok:
        return

    data = r.json()
    check("trace.object_id matches", data.get("object_id") == mid, f"got={data.get('object_id')}")
    check("trace.object_type = memory", data.get("object_type") == "memory", f"got={data.get('object_type')}")
    check("trace.proof_steps non-empty", bool(data.get("proof_steps")), f"steps={len(data.get('proof_steps', []))}")

    phases = {s.get("phase") for s in data.get("proof_steps", [])}
    check("trace has 'canonical' phase", "canonical" in phases, f"phases={phases}")
    check("trace has 'fragment' phase", "fragment" in phases, f"phases={phases}")


# ── 5. Canonical CRUD ─────────────────────────────────────────────────────────

def test_canonical_crud():
    print("\n── 5. Canonical CRUD")

    agent_id = f"agent_crud_{_uid()}"
    session_id = f"sess_crud_{_uid()}"
    now = _now()

    r = _post("/v1/agents", {"agent_id": agent_id, "name": "E2E Test Agent", "description": "e2e"})
    check("POST /v1/agents → 200", r.status_code == 200, r.text[:100])

    r = _get("/v1/agents")
    agents = r.json() if r.status_code == 200 else []
    check("GET /v1/agents lists agent", any(a.get("agent_id") == agent_id for a in agents),
          f"total={len(agents)}")

    r = _post("/v1/sessions", {
        "session_id": session_id, "agent_id": agent_id,
        "start_time": now, "status": "active",
    })
    check("POST /v1/sessions → 200", r.status_code == 200, r.text[:100])

    r = _get("/v1/sessions", {"agent_id": agent_id})
    sessions = r.json() if r.status_code == 200 else []
    check("GET /v1/sessions lists session", any(s.get("session_id") == session_id for s in sessions),
          f"total={len(sessions)}")

    mem_id = f"mem_crud_{_uid()}"
    r = _post("/v1/memory", {
        "memory_id": mem_id, "agent_id": agent_id, "session_id": session_id,
        "content": "CRUD test memory content", "memory_type": "semantic",
        "created_at": now, "version": 1,
    })
    check("POST /v1/memory → 200", r.status_code == 200, r.text[:100])

    r = _get("/v1/memory", {"agent_id": agent_id, "session_id": session_id})
    mems = r.json() if r.status_code == 200 else []
    check("GET /v1/memory lists memory", any(m.get("memory_id") == mem_id for m in mems),
          f"total={len(mems)}")


# ── 6. Memory recall ─────────────────────────────────────────────────────────

def test_memory_recall(ingested: dict[str, str]):
    print("\n── 6. Memory recall /v1/internal/memory/recall")
    r = _post("/v1/internal/memory/recall", {
        "query": "transformer attention self-attention",
        "scope": "global",
        "top_k": 5,
        "agent_id": "agent_e2e",
        "session_id": "sess_e2e",
    })
    ok = r.status_code == 200
    check("POST /v1/internal/memory/recall → 200", ok, f"status={r.status_code}")
    if ok:
        data = r.json()
        check("recall response non-empty", bool(data), f"keys={list(data.keys())[:6]}")
        mid = ingested.get("memory_semantic")
        recalled_ids = data.get("memory_ids") or data.get("objects") or []
        if mid and recalled_ids:
            check("recall includes semantic memory", mid in recalled_ids,
                  f"returned={recalled_ids[:5]}")


# ── Summary ───────────────────────────────────────────────────────────────────

def _summary():
    print("\n" + "═" * 55)
    passed = sum(1 for _, ok, _ in _results if ok)
    failed = sum(1 for _, ok, _ in _results if not ok)
    print(f"  Total: {len(_results)}  {PASS}: {passed}  {FAIL}: {failed}")
    if failed:
        print("\nFailed checks:")
        for name, ok, detail in _results:
            if not ok:
                print(f"  ✗ {name}  {detail}")
    print("═" * 55)
    return failed == 0


def main():
    print(f"CogDB E2E Storage → Retrieval Test")
    print(f"Server: {BASE_URL}")

    test_health()
    ingested = test_ingest()
    test_query(ingested)
    test_traces(ingested)
    test_canonical_crud()
    test_memory_recall(ingested)

    ok = _summary()
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    main()
