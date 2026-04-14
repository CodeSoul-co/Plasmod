#!/usr/bin/env python3
"""
Layer-5 task suite for 5-T1 ~ 5-T8.

Covers:
- Research Copilot: cross-doc retrieval / long-session summary / provenance / multi-round correction
- Software Engineering Agent: multi-file understanding / version trace / patch artifact / rollback dry-run
"""

from __future__ import annotations

import argparse
import json
import os
import time
import uuid
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


def base_url() -> str:
    return (
        os.environ.get("ANDB_BASE_URL")
        or os.environ.get("PLASMOD_BASE_URL")
        or "http://127.0.0.1:8080"
    ).rstrip("/")


def admin_key() -> str:
    return (
        os.environ.get("PLASMOD_ADMIN_API_KEY")
        or os.environ.get("ANDB_ADMIN_API_KEY")
        or ""
    ).strip()


def req(method: str, url: str, body: dict | None = None, headers: dict[str, str] | None = None, timeout: float = 60.0) -> tuple[int, Any]:
    data = json.dumps(body).encode() if body is not None else None
    r = Request(url, data=data, method=method)
    if data:
        r.add_header("Content-Type", "application/json")
    if headers:
        for k, v in headers.items():
            r.add_header(k, v)
    try:
        with urlopen(r, timeout=timeout) as resp:
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


def ingest(base: str, agent: str, session: str, workspace: str, text: str, *, event_type: str = "user_message", source_file: str = "", dataset: str = "layer5_suite") -> tuple[int, str]:
    eid = f"evt_l5_{uuid.uuid4().hex[:12]}"
    payload: dict[str, Any] = {"text": text, "dataset_name": dataset}
    if source_file:
        payload["source_file_name"] = source_file
    body = {
        "event_id": eid,
        "agent_id": agent,
        "session_id": session,
        "workspace_id": workspace,
        "tenant_id": "t_layer5",
        "event_type": event_type,
        "payload": payload,
    }
    st, resp = req("POST", f"{base}/v1/ingest/events", body, timeout=120.0)
    mid = ""
    if isinstance(resp, dict):
        mid = str(resp.get("memory_id", ""))
    if not mid:
        mid = f"mem_{eid}"
    return st, mid


def query(base: str, workspace: str, text: str) -> tuple[int, Any]:
    body = {
        "query_text": text,
        "workspace_id": workspace,
        "query_scope": "workspace",
        "top_k": 8,
        "include_evidence": True,
    }
    return req("POST", f"{base}/v1/query", body, timeout=240.0)


def list_memories(base: str, agent: str, session: str) -> tuple[int, list]:
    st, resp = req("GET", f"{base}/v1/memory?agent_id={agent}&session_id={session}", None, timeout=60.0)
    if st == 200 and isinstance(resp, list):
        return st, resp
    return st, []


def post_artifact(base: str, session: str, owner: str, patch_text: str) -> tuple[int, str]:
    art_id = f"art_patch_{uuid.uuid4().hex[:10]}"
    body = {
        "artifact_id": art_id,
        "session_id": session,
        "owner_agent_id": owner,
        "artifact_type": "patch",
        "mime_type": "text/x-diff",
        "content_ref": "inline",
        "uri": f"mem://patch/{art_id}",
        "metadata": {"title": "auto patch", "lines": len(patch_text.splitlines())},
        "hash": "",
        "produced_by_event_id": "",
        "version": 1,
    }
    st, resp = req("POST", f"{base}/v1/artifacts", body, timeout=60.0)
    rid = art_id
    if isinstance(resp, dict) and resp.get("artifact_id"):
        rid = str(resp["artifact_id"])
    return st, rid


def ok_row(name: str, ok: bool, detail: str) -> dict[str, Any]:
    return {"task": name, "ok": ok, "detail": detail}


def main() -> int:
    ap = argparse.ArgumentParser(description="Layer-5 task suite (5-T1~5-T8)")
    ap.add_argument("--base-url", default=base_url())
    ap.add_argument("--workspace-id", default=f"w_l5_{uuid.uuid4().hex[:6]}")
    ap.add_argument("--session-id", default=f"s_l5_{uuid.uuid4().hex[:6]}")
    ap.add_argument("--agent-id", default="agent_l5_suite")
    ap.add_argument("--out-dir", default="out/layer5_task_suite")
    args = ap.parse_args()

    base = args.base_url.rstrip("/")
    out_dir = Path(args.out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    results: list[dict[str, Any]] = []

    # 5-T1 Research Copilot: cross-document retrieval
    docs = [
        ("doc_a.md", "Refund policy: you can request refund within 7 days."),
        ("doc_b.md", "Billing policy: invoice is generated monthly."),
        ("doc_c.md", "Support guide: escalation path includes L2 and L3."),
    ]
    for f, t in docs:
        ingest(base, args.agent_id, args.session_id, args.workspace_id, t, source_file=f, dataset="research_copilot_docs")
    st, resp = query(base, args.workspace_id, "What is the refund policy window?")
    hits = (resp or {}).get("results", []) if isinstance(resp, dict) else []
    results.append(ok_row("5-T1", st == 200 and isinstance(hits, list) and len(hits) > 0, f"query_status={st} hits={len(hits) if isinstance(hits, list) else 0}"))

    # 5-T2 long-session summary
    mids: list[str] = []
    for i in range(15):
        st_i, mid = ingest(base, args.agent_id, args.session_id, args.workspace_id, f"long session turn {i} about research decisions", event_type="agent_thought")
        if st_i == 200:
            mids.append(mid)
    sum_ok = False
    if mids:
        st_s, _ = req("POST", f"{base}/v1/internal/memory/summarize", {"memory_id": mids[-1], "agent_id": args.agent_id}, timeout=60.0)
        sum_ok = st_s in (200, 201, 204)
    results.append(ok_row("5-T2", sum_ok, f"ingested={len(mids)} summarize={sum_ok}"))

    # 5-T3 provenance trace
    trace_ok = False
    if mids:
        st_t, tr = req("GET", f"{base}/v1/traces/{mids[0]}", None, timeout=60.0)
        trace_ok = st_t == 200 and isinstance(tr, dict) and ("steps" in tr or "versions" in tr)
    results.append(ok_row("5-T3", trace_ok, f"trace_ok={trace_ok}"))

    # 5-T4 multi-round correction
    ingest(base, args.agent_id, args.session_id, args.workspace_id, "Initial claim: Model X was released in 2022.")
    ingest(base, args.agent_id, args.session_id, args.workspace_id, "Correction: Model X was released in 2023.")
    st_c, resp_c = query(base, args.workspace_id, "When was Model X released?")
    txt = json.dumps(resp_c, ensure_ascii=False) if resp_c is not None else ""
    corr_ok = st_c == 200 and ("2023" in txt)
    results.append(ok_row("5-T4", corr_ok, f"query_status={st_c} corrected_found={corr_ok}"))

    # 5-T5 SE multi-file understanding
    code_chunks = [
        ("a.py", "def load_data(): return 'data'"),
        ("b.py", "def transform(x): return x.upper()"),
        ("c.py", "def pipeline(): return transform(load_data())"),
    ]
    for f, c in code_chunks:
        ingest(base, args.agent_id, args.session_id, args.workspace_id, c, source_file=f, dataset="se_repo")
    st_q2, resp_q2 = query(base, args.workspace_id, "How does pipeline use load_data and transform?")
    se_ok = st_q2 == 200 and isinstance((resp_q2 or {}).get("results", []), list)
    results.append(ok_row("5-T5", se_ok, f"query_status={st_q2}"))

    # 5-T6 version management via trace versions field
    ver_ok = False
    if mids:
        st_v, tr_v = req("GET", f"{base}/v1/traces/{mids[-1]}", None, timeout=60.0)
        ver_ok = st_v == 200 and isinstance(tr_v, dict) and "versions" in tr_v
    results.append(ok_row("5-T6", ver_ok, f"version_field_exposed={ver_ok}"))

    # 5-T7 patch generation artifact
    patch = "--- a.py\n+++ a.py\n@@\n-def load_data(): return 'data'\n+def load_data(): return 'DATA'\n"
    st_a, aid = post_artifact(base, args.session_id, args.agent_id, patch)
    st_al, arts = req("GET", f"{base}/v1/artifacts?session_id={args.session_id}", None, timeout=60.0)
    listed = 0
    if st_al == 200 and isinstance(arts, list):
        listed = sum(1 for x in arts if isinstance(x, dict) and str(x.get("artifact_id", "")) == aid)
    results.append(ok_row("5-T7", st_a == 200 and listed > 0, f"post={st_a} listed={listed}"))

    # 5-T8 rollback dry-run
    rollback_ok = False
    if mids:
        headers = {}
        k = admin_key()
        if k:
            headers["X-Admin-Key"] = k
        st_r, _ = req(
            "POST",
            f"{base}/v1/admin/rollback",
            {"memory_id": mids[-1], "action": "deactivate", "dry_run": True, "reason": "layer5_suite"},
            headers=headers if headers else None,
            timeout=60.0,
        )
        rollback_ok = st_r == 200
    results.append(ok_row("5-T8", rollback_ok, f"rollback_dry_run={rollback_ok}"))

    pass_n = sum(1 for r in results if r["ok"])
    out = {
        "base_url": base,
        "workspace_id": args.workspace_id,
        "session_id": args.session_id,
        "agent_id": args.agent_id,
        "pass": pass_n,
        "total": len(results),
        "results": results,
    }
    (out_dir / "layer5_t1_t8.json").write_text(json.dumps(out, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")

    md = [
        "# Layer-5 Task Suite (5-T1~5-T8)",
        "",
        f"- pass: **{pass_n}/{len(results)}**",
        f"- workspace: `{args.workspace_id}`",
        f"- session: `{args.session_id}`",
        "",
        "| Task | Status | Detail |",
        "|---|---|---|",
    ]
    for r in results:
        md.append(f"| {r['task']} | {'✅' if r['ok'] else '❌'} | {r['detail']} |")
    (out_dir / "layer5_t1_t8.md").write_text("\n".join(md) + "\n", encoding="utf-8")

    print(f"[layer5-task-suite] pass={pass_n}/{len(results)}")
    print(f"[layer5-task-suite] outputs: {out_dir / 'layer5_t1_t8.md'}")
    return 0 if pass_n == len(results) else 1


if __name__ == "__main__":
    raise SystemExit(main())

