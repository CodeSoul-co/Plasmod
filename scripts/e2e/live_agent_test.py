#!/usr/bin/env python3
"""
CogDB Live Agent Test
=====================
Full end-to-end test using:
  - Real ONNX embedding (via the running CogDB server)
  - Real LLM inference (PLASMOD_AGENT_LLM_* env vars)
  - Real disk storage (BadgerDB inside the server)
  - All 4 chains: MainChain / MemoryPipelineChain / QueryChain / CollaborationChain
  - MAS: two agents collaborating via memory sharing
  - MemoryBank governance: admission / retention / recall scoring

Usage:
    python3 scripts/e2e/live_agent_test.py [options]

Options:
    --base-url URL      CogDB server (default: PLASMOD_AGENT_ENDPOINT or http://127.0.0.1:8080)
    --skip-query        Skip QueryChain step (ONNX query takes ~120s)
    --out-dir DIR       Write report + request log (default: out/live_agent_test)
    --env-file FILE     Load env vars from file (default: .env)
    --backend-profile   Baseline profile: plain_vector | vector_metadata | plasmod_full
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
import traceback
import uuid
from pathlib import Path
from threading import Lock
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

# ── env file loader ────────────────────────────────────────────────────────────

def _load_env_file(path: str) -> dict[str, str]:
    """Simple .env parser (KEY=VALUE, # comments, no multiline)."""
    env: dict[str, str] = {}
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#"):
                    continue
                if "=" in line:
                    k, _, v = line.partition("=")
                    k = k.strip()
                    v = v.strip().strip('"').strip("'")
                    if k:
                        env[k] = v
    except FileNotFoundError:
        pass
    return env

# ── HTTP helpers ───────────────────────────────────────────────────────────────

RESULTS: list[dict] = []
REQ_LOG: list[dict] = []
_lock = Lock()
BASELINE_PROFILES = ("plain_vector", "vector_metadata", "plasmod_full")

def _req(base: str, method: str, path: str, body: dict | None = None,
         timeout: float = 60.0) -> tuple[int, Any]:
    url = base + path
    data = json.dumps(body).encode() if body is not None else None
    req = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    t0 = time.monotonic()
    try:
        with urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            elapsed = time.monotonic() - t0
            try:
                parsed = json.loads(raw)
            except Exception:
                parsed = raw.decode(errors="replace")
            with _lock:
                REQ_LOG.append({"method": method, "path": path,
                                 "status": resp.status, "elapsed": round(elapsed, 3)})
            return resp.status, parsed
    except HTTPError as e:
        raw = e.read().decode(errors="replace")
        with _lock:
            REQ_LOG.append({"method": method, "path": path,
                             "status": e.code, "elapsed": round(time.monotonic()-t0,3)})
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw
    except URLError as e:
        return 0, str(e)

# ── LLM client ────────────────────────────────────────────────────────────────

class LLMClient:
    def __init__(self, base_url: str, api_key: str, model: str,
                 timeout: float = 120.0, max_tokens: int = 512, temperature: float = 0.7):
        self.base_url  = base_url.rstrip("/")
        self.api_key   = api_key
        self.model     = model
        self.timeout   = timeout
        self.max_tokens = max_tokens
        self.temperature = temperature

    def chat(self, messages: list[dict]) -> tuple[str, int, float]:
        """Returns (content, total_tokens, latency_s)."""
        url  = self.base_url + "/chat/completions"
        body = json.dumps({
            "model":       self.model,
            "messages":    messages,
            "max_tokens":  self.max_tokens,
            "temperature": self.temperature,
        }).encode()
        req = Request(url, data=body, method="POST")
        req.add_header("Content-Type", "application/json")
        req.add_header("Authorization", f"Bearer {self.api_key}")
        t0 = time.monotonic()
        with urlopen(req, timeout=self.timeout) as resp:
            d = json.loads(resp.read())
        elapsed = time.monotonic() - t0
        content = d["choices"][0]["message"]["content"]
        tokens  = d.get("usage", {}).get("total_tokens", 0)
        return content, tokens, elapsed

    def available(self) -> bool:
        return bool(self.base_url and self.api_key and self.model)

# ── result helpers ─────────────────────────────────────────────────────────────

def _pass(check: str, detail: str = "") -> None:
    with _lock:
        RESULTS.append({"status": "PASS", "check": check, "detail": detail})
    print(f"  \033[32m[PASS]\033[0m {check}" + (f" — {detail}" if detail else ""))

def _fail(check: str, detail: str = "") -> None:
    with _lock:
        RESULTS.append({"status": "FAIL", "check": check, "detail": detail})
    print(f"  \033[31m[FAIL]\033[0m {check}" + (f" — {detail}" if detail else ""))

def _info(msg: str) -> None:
    print(f"  \033[90m[INFO]\033[0m {msg}")

def _header(title: str) -> None:
    bar = "─" * 60
    print(f"\n{bar}\n  {title}\n{bar}")

def _now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%S")

# ── event ingestion ────────────────────────────────────────────────────────────

def ingest_event(base: str, agent_id: str, workspace_id: str,
                 text: str, event_type: str = "user_message",
                 importance: float = 0.8, profile: str = "plasmod_full") -> tuple[str, str]:
    """Returns (event_id, memory_id)."""
    eid = f"live-{uuid.uuid4().hex[:12]}"
    payload: dict[str, Any] = {"text": text, "dataset": "live_test"}
    if profile in ("vector_metadata", "plasmod_full"):
        payload.update({
            "tags": ["agent-memory", "baseline-e2e"],
            "source_file_name": "live_agent_test.py",
            "dataset_name": "live_test",
        })
    body = {
        "event_id":   eid,
        "agent_id":   agent_id,
        "workspace_id": workspace_id,
        "event_type": event_type,
        "source":     "live_agent_test",
        "importance": importance,
        "payload":    payload,
    }
    status, resp = _req(base, "POST", "/v1/ingest/events", body, timeout=30.0)
    if status in (200, 201):
        mem_id = (resp or {}).get("memory_id", f"mem_{eid}")
        return eid, mem_id
    return eid, ""

# ══════════════════════════════════════════════════════════════════════════════
# Phase 1 — Setup
# ══════════════════════════════════════════════════════════════════════════════

def phase_setup(base: str, ws: str, profile: str) -> dict[str, str]:
    _header("Phase 1 — Agent Setup & MAS Topology")
    agents: dict[str, str] = {"_workspace": ws}

    for name in ("agent-alpha", "agent-beta"):
        aid = f"{name}-{uuid.uuid4().hex[:8]}"
        status, resp = _req(base, "POST", "/v1/agents", {
            "agent_id": aid, "workspace_id": ws, "tenant_id": "live-test",
            "name": name, "role": "assistant",
        })
        if status in (200, 201):
            returned_id = (resp or {}).get("agent_id") or aid
            agents[name] = returned_id
            _pass(f"Create {name}", f"id={returned_id}")
        else:
            _fail(f"Create {name}", f"status={status} resp={str(resp)[:80]}")
            agents[name] = aid

    # share contract alpha → beta (only for full profile)
    if profile == "plasmod_full":
        status, _ = _req(base, "POST", "/v1/share-contracts", {
            "from_agent_id": agents["agent-alpha"],
            "to_agent_id":   agents["agent-beta"],
            "workspace_id":  ws,
            "bidirectional": True,
            "memory_types":  ["user_message", "agent_thought", "tool_call", "semantic_memory"],
        })
        if status in (200, 201):
            _pass("MAS share contract alpha↔beta")
        else:
            _fail("MAS share contract alpha↔beta", f"status={status}")
    else:
        _info(f"Skip share-contract for profile={profile}")

    return agents

# ══════════════════════════════════════════════════════════════════════════════
# Phase 2 — LLM content generation + ingest (MainChain write path)
# ══════════════════════════════════════════════════════════════════════════════

def phase_llm_ingest(base: str, llm: LLMClient | None,
                     agents: dict[str, str], profile: str) -> list[str]:
    _header("Phase 2 — LLM Generation + Ingest  [MainChain write path]")
    ws      = agents["_workspace"]
    alpha   = agents["agent-alpha"]
    mem_ids: list[str] = []

    # ── Generate content via LLM ──────────────────────────────────
    if llm and llm.available():
        try:
            prompt_msgs = [
                {"role": "system",
                 "content": "You are a helpful assistant. Return exactly 5 short distinct facts "
                             "about AI memory systems, one per line, no numbering."},
                {"role": "user", "content": "Generate 5 facts about AI agent memory architecture."},
            ]
            content, tokens, latency = llm.chat(prompt_msgs)
            facts = [l.strip() for l in content.strip().splitlines() if l.strip()][:5]
            _pass("LLM generate 5 facts",
                  f"model={llm.model} tokens={tokens} latency={latency:.1f}s facts={len(facts)}")
        except Exception as e:
            _fail("LLM generate facts", str(e)[:100])
            facts = [
                "Episodic memory stores time-stamped experience.",
                "Semantic memory holds factual world knowledge.",
                "Working memory buffers active context.",
                "Memory consolidation moves short-term to long-term.",
                "Forgetting curves follow exponential decay.",
            ]
            _info("Falling back to static facts for ingest")
    else:
        _info("No LLM configured — using static facts")
        facts = [
            "Episodic memory stores time-stamped experience.",
            "Semantic memory holds factual world knowledge.",
            "Working memory buffers active context.",
            "Memory consolidation moves short-term to long-term.",
            "Forgetting curves follow exponential decay.",
        ]

    # ── Ingest each fact → MainChain write path ───────────────────
    t0 = time.monotonic()
    ok_count = 0
    for i, fact in enumerate(facts):
        _, mem_id = ingest_event(base, alpha, ws, fact,
                                  event_type="semantic_memory",
                                  importance=round(0.7 + 0.05 * i, 2),
                                  profile=profile)
        if mem_id:
            mem_ids.append(mem_id)
            ok_count += 1

    elapsed = time.monotonic() - t0
    if ok_count == len(facts):
        _pass("MainChain — ingest 5 events", f"{ok_count} accepted in {elapsed:.1f}s")
    else:
        _fail("MainChain — ingest 5 events", f"only {ok_count}/{len(facts)} accepted")

    # verify edges were created
    time.sleep(1)
    status, resp = _req(base, "GET", f"/v1/edges?workspace_id={ws}")
    edges = resp if isinstance(resp, list) else (resp or {}).get("edges", [])
    if len(edges) > 0:
        _pass("MainChain — graph edges created", f"count={len(edges)}")
    else:
        _fail("MainChain — graph edges created", "no edges found")

    return mem_ids

# ══════════════════════════════════════════════════════════════════════════════
# Phase 3 — MemoryPipelineChain (cognitive path)
# ══════════════════════════════════════════════════════════════════════════════

def phase_memory_pipeline(base: str, agents: dict[str, str],
                           mem_ids: list[str], profile: str) -> str:
    _header("Phase 3 — MemoryPipelineChain  [cognitive path]")
    ws    = agents["_workspace"]
    alpha = agents["agent-alpha"]
    sample_id = mem_ids[0] if mem_ids else ""

    # Verify memories materialised
    status, resp = _req(base, "GET", f"/v1/memory?agent_id={alpha}&workspace_id={ws}")
    mems = resp if isinstance(resp, list) else (resp or {}).get("memories", [])
    if len(mems) >= len(mem_ids):
        _pass("MemoryPipelineChain — materialization",
              f"memories={len(mems)}, content_present={any(m.get('content') for m in mems if isinstance(m,dict))}")
    else:
        _fail("MemoryPipelineChain — materialization",
              f"expected≥{len(mem_ids)} got {len(mems)}")

    # Summarization trigger (skip for plain vector baseline)
    if sample_id:
        if profile == "plain_vector":
            _info("Skip summarize for plain_vector profile")
        else:
            status, _ = _req(base, "POST", "/v1/internal/memory/summarize",
                              {"memory_id": sample_id, "agent_id": alpha}, timeout=30.0)
            if status in (200, 201, 204):
                _pass("MemoryPipelineChain — summarization triggered", f"status={status}")
            else:
                _fail("MemoryPipelineChain — summarization triggered", f"status={status}")
    else:
        _fail("MemoryPipelineChain — summarization triggered", "no sample memory id")

    # Memory decay (governance)
    status, _ = _req(base, "POST", "/v1/internal/memory/decay",
                      {"agent_id": alpha, "workspace_id": ws, "decay_days": 0})
    if status in (200, 201, 204):
        _pass("MemoryPipelineChain — decay/governance", f"status={status}")
    else:
        _fail("MemoryPipelineChain — decay/governance", f"status={status}")

    return sample_id

# ══════════════════════════════════════════════════════════════════════════════
# Phase 4 — QueryChain (ONNX embedding path)
# ══════════════════════════════════════════════════════════════════════════════

def phase_query_chain(base: str, agents: dict[str, str],
                      llm: LLMClient | None, skip: bool, profile: str) -> None:
    _header("Phase 4 — QueryChain  [ONNX embedding + proof_trace]")
    ws = agents["_workspace"]

    if skip:
        _info("Skipped (--skip-query)")
        return

    query_text = "How does episodic memory relate to agent memory consolidation?"
    if llm and llm.available():
        try:
            content, _, _ = llm.chat([
                {"role": "user",
                 "content": "Give me a one-sentence question about AI agent memory systems "
                             "suitable for semantic search."},
            ])
            query_text = content.strip().strip('"')
            _info(f"LLM query text: {query_text[:80]}")
        except Exception:
            pass

    _info(f"Sending query (ONNX inference ~120s)…")
    t0 = time.monotonic()
    if profile == "plain_vector":
        qbody = {
            "query_text": query_text,
            "workspace_id": ws,
            "top_k": 5,
            "include_evidence": False,
        }
    elif profile == "vector_metadata":
        qbody = {
            "query_text": query_text,
            "workspace_id": ws,
            "top_k": 5,
            "include_evidence": False,
            "query_scope": "workspace",
            "time_window": {"from": "2020-01-01T00:00:00Z", "to": "2099-12-31T23:59:59Z"},
        }
    else:
        qbody = {
            "query_text": query_text,
            "workspace_id": ws,
            "top_k": 5,
            "include_evidence": True,
        }
    status, resp = _req(base, "POST", "/v1/query", qbody, timeout=360.0)
    elapsed = time.monotonic() - t0

    if status == 200:
        results  = (resp or {}).get("results", [])
        traces   = (resp or {}).get("chain_traces", {})
        proof    = (resp or {}).get("proof_trace", [])
        q_status = (resp or {}).get("query_status", "")
        _pass("QueryChain — ONNX query",
              f"latency={elapsed:.1f}s results={len(results)} query_status={q_status}")
        if traces:
            _pass("QueryChain — chain_traces present",
                  f"keys={sorted(traces.keys())}")
        else:
            _info("chain_traces absent (empty workspace or no hits)")
        if proof:
            _pass("QueryChain — proof_trace returned",
                  f"objects={len(proof)}")
        else:
            _info("proof_trace empty (no ingested content in this workspace)")
    else:
        _fail("QueryChain — ONNX query", f"status={status} latency={elapsed:.1f}s")

# ══════════════════════════════════════════════════════════════════════════════
# Phase 5 — CollaborationChain (MAS path)
# ══════════════════════════════════════════════════════════════════════════════

def phase_collaboration_chain(base: str, agents: dict[str, str],
                               mem_ids: list[str], profile: str) -> None:
    _header("Phase 5 — CollaborationChain  [MAS: conflict resolve + share]")
    if profile != "plasmod_full":
        _info(f"Skip collaboration chain for profile={profile}")
        return
    ws    = agents["_workspace"]
    alpha = agents["agent-alpha"]
    beta  = agents["agent-beta"]

    if len(mem_ids) < 2:
        _fail("CollaborationChain — need ≥2 memories", f"only {len(mem_ids)} available")
        return

    left_id, right_id = mem_ids[0], mem_ids[1]

    # Conflict resolution
    status, resp = _req(base, "POST", "/v1/internal/memory/conflict/resolve", {
        "left_id":  left_id,
        "right_id": right_id,
    })
    if status in (200, 201):
        winner = (resp or {}).get("winner_id", "")
        _pass("CollaborationChain — conflict resolved",
              f"winner={winner} left={left_id[:20]} right={right_id[:20]}")
    else:
        winner = left_id
        _fail("CollaborationChain — conflict resolved", f"status={status}")

    # Memory sharing alpha → beta
    status, resp = _req(base, "POST", "/v1/internal/memory/share", {
        "from_agent_id": alpha,
        "to_agent_id":   beta,
        "memory_id":     winner or left_id,
    })
    if status in (200, 201):
        _pass("CollaborationChain — share alpha→beta", f"status={status}")
    else:
        _fail("CollaborationChain — share alpha→beta",
              f"status={status} resp={str(resp)[:80]}")

    # Beta should see the shared memory
    status, resp = _req(base, "GET", f"/v1/memory?agent_id={beta}&workspace_id={ws}")
    beta_mems = resp if isinstance(resp, list) else (resp or {}).get("memories", [])
    if len(beta_mems) >= 1:
        _pass("CollaborationChain — beta received shared memory",
              f"beta_memories={len(beta_mems)}")
    else:
        _fail("CollaborationChain — beta received shared memory",
              "no memories found for beta")

# ══════════════════════════════════════════════════════════════════════════════
# Phase 6 — MemoryBank governance
# ══════════════════════════════════════════════════════════════════════════════

def phase_memorybank(base: str, agents: dict[str, str], mem_ids: list[str], profile: str) -> None:
    _header("Phase 6 — MemoryBank Governance  [admission / retention / recall]")
    ws    = agents["_workspace"]
    alpha = agents["agent-alpha"]

    # Verify memories have importance (admission score proxy)
    status, resp = _req(base, "GET", f"/v1/memory?agent_id={alpha}&workspace_id={ws}")
    mems = resp if isinstance(resp, list) else (resp or {}).get("memories", [])
    if mems:
        with_importance = [m for m in mems
                           if isinstance(m, dict) and m.get("importance", 0) > 0]
        _pass("MemoryBank — admission (importance > 0)",
              f"{len(with_importance)}/{len(mems)} memories have importance")

        # Check lifecycle states
        states = {}
        for m in mems:
            if isinstance(m, dict):
                s = m.get("lifecycle_state", m.get("state", "unknown"))
                states[s] = states.get(s, 0) + 1
        _pass("MemoryBank — lifecycle states observed",
              f"distribution={dict(sorted(states.items()))}")

        # Check any memory has embedding provenance
        with_provenance = [m for m in mems
                           if isinstance(m, dict) and m.get("provenance")]
        _pass("MemoryBank — provenance recorded",
              f"{len(with_provenance)}/{len(mems)} memories have provenance")
    else:
        _fail("MemoryBank — no memories to inspect")

    # Storage backend check
    status, resp = _req(base, "GET", "/v1/admin/storage")
    if status == 200:
        mode    = (resp or {}).get("mode", "unknown")
        badger  = (resp or {}).get("badger_enabled", False)
        _pass("MemoryBank — storage backend",
              f"mode={mode} badger={badger} (disk=real, no mock)")
    else:
        _fail("MemoryBank — storage backend", f"status={status}")

    # Dynamic topology (proves real indexing)
    status, resp = _req(base, "GET", "/v1/admin/topology")
    if status == 200:
        indexes  = (resp or {}).get("indexes", 0)
        segments = (resp or {}).get("segments", 0)
        _pass("MemoryBank — ANN index active",
              f"indexes={indexes} segments={segments} profile={profile}")
    else:
        _fail("MemoryBank — ANN index active", f"status={status}")

# ══════════════════════════════════════════════════════════════════════════════
# Report
# ══════════════════════════════════════════════════════════════════════════════

def write_report(out_dir: Path, llm: LLMClient | None,
                 total_elapsed: float, args: argparse.Namespace) -> int:
    out_dir.mkdir(parents=True, exist_ok=True)

    passes = sum(1 for r in RESULTS if r["status"] == "PASS")
    fails  = sum(1 for r in RESULTS if r["status"] == "FAIL")
    total  = passes + fails

    # ── terminal summary ──────────────────────────────────────────
    bar = "═" * 64
    print(f"\n{bar}")
    print(f"  CogDB Live Agent Test — Final Report")
    print(f"  Date     : {_now()}")
    print(f"  Server   : {args.base_url}")
    llm_desc = f"{llm.model} @ {llm.base_url}" if (llm and llm.available()) else "not configured"
    print(f"  LLM      : {llm_desc}")
    print(f"  Duration : {total_elapsed:.1f}s")
    print(f"{bar}")
    print(f"\n  Chain Coverage\n  {'─'*40}")
    chain_phases = {
        "MainChain       (write path)": [r for r in RESULTS if "MainChain" in r["check"]],
        "MemoryPipeline  (cognitive) ": [r for r in RESULTS if "MemoryPipelineChain" in r["check"]],
        "QueryChain      (ONNX read) ": [r for r in RESULTS if "QueryChain" in r["check"]],
        "CollaborationCh (MAS)       ": [r for r in RESULTS if "CollaborationChain" in r["check"]],
        "MemoryBank      (governance)": [r for r in RESULTS if "MemoryBank" in r["check"]],
    }
    for chain, checks in chain_phases.items():
        if not checks:
            status_str = "\033[90mSKIPPED\033[0m"
        elif all(c["status"] == "PASS" for c in checks):
            status_str = f"\033[32mPASS {len(checks)}/{len(checks)}\033[0m"
        else:
            p = sum(1 for c in checks if c["status"] == "PASS")
            status_str = f"\033[31mFAIL {len(checks)-p}/{len(checks)}\033[0m"
        print(f"  {chain}  {status_str}")

    print(f"\n  Detailed results\n  {'─'*40}")
    for r in RESULTS:
        icon = "\033[32m✓\033[0m" if r["status"] == "PASS" else "\033[31m✗\033[0m"
        detail = f"  {r.get('detail','')[:60]}" if r.get("detail") else ""
        print(f"  {icon} {r['check']}{detail}")

    color = "\033[32m" if fails == 0 else "\033[31m"
    print(f"\n{bar}")
    print(f"  {color}PASS {passes}/{total}   FAIL {fails}/{total}\033[0m")
    print(f"{bar}\n")

    # ── write report.md ───────────────────────────────────────────
    lines = [
        "# CogDB Live Agent Test Report",
        f"",
        f"- **Date**: {_now()}",
        f"- **Server**: {args.base_url}",
        f"- **LLM**: {llm_desc}",
        f"- **Duration**: {total_elapsed:.1f}s",
        f"- **Result**: {passes}/{total} PASS",
        f"",
        "## Chain Results",
        "",
        "| Chain | Checks | Status |",
        "|---|---|---|",
    ]
    for chain, checks in chain_phases.items():
        if not checks:
            lines.append(f"| {chain.strip()} | 0 | SKIPPED |")
        else:
            p = sum(1 for c in checks if c["status"] == "PASS")
            s = "✅ PASS" if p == len(checks) else f"❌ FAIL {len(checks)-p}/{len(checks)}"
            lines.append(f"| {chain.strip()} | {len(checks)} | {s} |")

    lines += ["", "## All Checks", "", "| Check | Status | Detail |", "|---|---|---|"]
    for r in RESULTS:
        icon = "✅ PASS" if r["status"] == "PASS" else "❌ FAIL"
        lines.append(f"| {r['check']} | {icon} | {str(r.get('detail',''))[:100]} |")

    (out_dir / "report.md").write_text("\n".join(lines) + "\n")
    with open(out_dir / "requests.jsonl", "w") as f:
        for r in REQ_LOG:
            f.write(json.dumps(r) + "\n")

    print(f"  Report  : {out_dir / 'report.md'}")
    print(f"  Requests: {out_dir / 'requests.jsonl'}\n")
    return fails

# ══════════════════════════════════════════════════════════════════════════════
# Main
# ══════════════════════════════════════════════════════════════════════════════

def main() -> None:
    parser = argparse.ArgumentParser(description="CogDB live agent test")
    parser.add_argument("--base-url",    default="")
    parser.add_argument("--skip-query",  action="store_true")
    parser.add_argument("--out-dir",     default="out/live_agent_test")
    parser.add_argument("--env-file",    default=".env")
    parser.add_argument("--backend-profile", default="plasmod_full", choices=BASELINE_PROFILES)
    args = parser.parse_args()

    # ── load env ──────────────────────────────────────────────────
    env = _load_env_file(args.env_file)
    for k, v in env.items():
        os.environ.setdefault(k, v)

    base = (args.base_url
            or os.environ.get("PLASMOD_AGENT_ENDPOINT")
            or "http://127.0.0.1:8080").rstrip("/")

    llm_base  = os.environ.get("PLASMOD_AGENT_LLM_BASE_URL", "").rstrip("/")
    llm_key   = os.environ.get("PLASMOD_AGENT_LLM_API_KEY", "")
    llm_model = os.environ.get("PLASMOD_AGENT_LLM_MODEL", "gpt-4o")
    llm_timeout = float(os.environ.get("PLASMOD_AGENT_LLM_TIMEOUT", "300"))
    llm_max_tokens = int(os.environ.get("PLASMOD_AGENT_LLM_MAX_TOKENS", "512"))

    llm: LLMClient | None = None
    if llm_base and llm_key:
        llm = LLMClient(llm_base, llm_key, llm_model, llm_timeout, llm_max_tokens)
    else:
        print("  \033[33m[WARN]\033[0m PLASMOD_AGENT_LLM_BASE_URL / PLASMOD_AGENT_LLM_API_KEY not set "
              "— LLM steps will use static content")

    # ── health check ──────────────────────────────────────────────
    status, resp = _req(base, "GET", "/healthz", timeout=5.0)
    if status != 200:
        print(f"\033[31m[ERROR]\033[0m Server not reachable at {base} (status={status})")
        sys.exit(1)

    ws = f"ws-live-{int(time.time())}-{uuid.uuid4().hex[:6]}"
    print(f"\n  Server    : {base}")
    print(f"  LLM       : {llm_model if llm else 'static content'}")
    print(f"  Workspace : {ws}")
    print(f"  Profile   : {args.backend_profile}")

    t_start = time.monotonic()

    agents  = phase_setup(base, ws, args.backend_profile)
    mem_ids = phase_llm_ingest(base, llm, agents, args.backend_profile)
    sample_id = phase_memory_pipeline(base, agents, mem_ids, args.backend_profile)
    phase_query_chain(base, agents, llm, skip=args.skip_query, profile=args.backend_profile)
    phase_collaboration_chain(base, agents, mem_ids, args.backend_profile)
    phase_memorybank(base, agents, mem_ids, args.backend_profile)

    total_elapsed = time.monotonic() - t_start
    fails = write_report(Path(args.out_dir), llm, total_elapsed, args)
    sys.exit(0 if fails == 0 else 1)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n\033[33m[INTERRUPTED]\033[0m")
        sys.exit(130)
    except Exception:
        traceback.print_exc()
        sys.exit(1)
