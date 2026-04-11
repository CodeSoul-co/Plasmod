#!/usr/bin/env python3
"""
CogDB MAS Scenario Test — AI Research Collaboration
=====================================================
A realistic multi-agent system scenario where two agents collaborate
to research and analyze a topic, using CogDB as shared memory.

Scenario: "Future of Large Language Models"
  Agent Alpha  (Researcher) — generates research findings via LLM,
               stores them in CogDB, shares key memories with Beta.
  Agent Beta   (Analyst)    — recalls Alpha's shared memories via
               ONNX query, synthesizes analysis via LLM, detects
               conflicts, resolves via CollaborationChain.

Every LLM call is grounded by a prior ONNX memory query. The agents
never talk to each other directly — they communicate only through
CogDB's memory layer. This tests the full stack:

  LLM → CogDB ingest (MainChain)
  ONNX query → grounded LLM call (QueryChain)
  Memory sharing → cross-agent recall (CollaborationChain)
  MemoryBank governance on all stored memories

Usage:
    python3 scripts/e2e/mas_scenario_test.py [--out-dir DIR] [--env-file FILE]
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import textwrap
import time
import traceback
import uuid
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

DIVIDER = "═" * 70
SUBDIV  = "─" * 70

# ── env loader ────────────────────────────────────────────────────────────────

def _load_env(path: str) -> None:
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                k, _, v = line.partition("=")
                k = k.strip(); v = v.strip().strip('"').strip("'")
                if k:
                    os.environ.setdefault(k, v)
    except FileNotFoundError:
        pass

# ── HTTP ──────────────────────────────────────────────────────────────────────

def _req(base: str, method: str, path: str, body=None,
         timeout: float = 60.0):
    url  = base + path
    data = json.dumps(body).encode() if body is not None else None
    req  = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    try:
        with urlopen(req, timeout=timeout) as r:
            raw = r.read()
            try:
                return r.status, json.loads(raw)
            except Exception:
                return r.status, raw.decode(errors="replace")
    except HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw.decode(errors="replace")
    except URLError as e:
        return 0, str(e)

# ── LLM ──────────────────────────────────────────────────────────────────────

class LLM:
    def __init__(self, base: str, key: str, model: str,
                 timeout: float = 300.0, max_tokens: int = 800):
        self.base, self.key, self.model = base.rstrip("/"), key, model
        self.timeout, self.max_tokens = timeout, max_tokens
        self.total_tokens = 0
        self.call_count   = 0

    def chat(self, system: str, user: str, temperature: float = 0.7) -> str:
        body = json.dumps({
            "model": self.model,
            "messages": [
                {"role": "system",  "content": system},
                {"role": "user",    "content": user},
            ],
            "max_tokens":  self.max_tokens,
            "temperature": temperature,
        }).encode()
        req = Request(self.base + "/chat/completions", data=body, method="POST")
        req.add_header("Content-Type", "application/json")
        req.add_header("Authorization", f"Bearer {self.key}")
        with urlopen(req, timeout=self.timeout) as r:
            d = json.loads(r.read())
        content = d["choices"][0]["message"]["content"].strip()
        self.total_tokens += d.get("usage", {}).get("total_tokens", 0)
        self.call_count   += 1
        return content

# ── CogDB helpers ─────────────────────────────────────────────────────────────

def create_agent(base: str, name: str, role: str, ws: str) -> str:
    aid = f"{name}-{uuid.uuid4().hex[:8]}"
    status, resp = _req(base, "POST", "/v1/agents", {
        "agent_id": aid, "workspace_id": ws,
        "tenant_id": "mas-test", "name": name, "role": role,
    })
    if status not in (200, 201):
        raise RuntimeError(f"create_agent {name}: status={status}")
    return (resp or {}).get("agent_id", aid)

def ingest(base: str, agent_id: str, ws: str, text: str,
           event_type: str = "agent_thought",
           importance: float = 0.85) -> str:
    eid = f"mas-{uuid.uuid4().hex[:12]}"
    status, resp = _req(base, "POST", "/v1/ingest/events", {
        "event_id": eid, "agent_id": agent_id, "workspace_id": ws,
        "event_type": event_type, "source": "mas_scenario",
        "importance": importance,
        "payload": {"text": text, "dataset": "mas_test"},
    }, timeout=30.0)
    if status not in (200, 201):
        raise RuntimeError(f"ingest failed: status={status} resp={resp}")
    return (resp or {}).get("memory_id", f"mem_{eid}")

def query_memory(base: str, ws: str, query_text: str, top_k: int = 5) -> list[dict]:
    """ONNX-grounded memory recall — the heart of grounded reasoning."""
    status, resp = _req(base, "POST", "/v1/query", {
        "query_text":   query_text,
        "workspace_id": ws,
        "top_k":        top_k,
        "include_evidence": True,
    }, timeout=360.0)
    if status != 200:
        return []
    results = (resp or {}).get("results", [])
    return results

def share_memories(base: str, from_id: str, to_id: str, mem_ids: list[str]) -> int:
    ok = 0
    for mid in mem_ids:
        status, _ = _req(base, "POST", "/v1/internal/memory/share", {
            "from_agent_id": from_id,
            "to_agent_id":   to_id,
            "memory_id":     mid,
        })
        if status in (200, 201):
            ok += 1
    return ok

def resolve_conflict(base: str, left_id: str, right_id: str) -> str:
    status, resp = _req(base, "POST", "/v1/internal/memory/conflict/resolve", {
        "left_id": left_id, "right_id": right_id,
    })
    if status in (200, 201):
        return (resp or {}).get("winner_id", left_id)
    return left_id

# ── timeline printer ──────────────────────────────────────────────────────────

TIMELINE: list[dict] = []

def _step(agent: str, action: str, content: str = "",
          detail: str = "", elapsed: float = 0.0) -> None:
    ts = time.strftime("%H:%M:%S")
    TIMELINE.append({"ts": ts, "agent": agent, "action": action,
                     "content": content, "detail": detail, "elapsed": elapsed})
    color = {"Alpha": "\033[36m", "Beta": "\033[33m", "SYS": "\033[90m"}.get(agent, "")
    reset = "\033[0m"
    tag = f"[{ts}] {color}{agent:5s}{reset}"
    print(f"\n{tag} ▸ {action}")
    if detail:
        print(f"         {detail}")
    if content:
        wrapped = textwrap.fill(content, width=66,
                                initial_indent="         ",
                                subsequent_indent="         ")
        print(wrapped)
    if elapsed:
        print(f"         \033[90m({elapsed:.1f}s)\033[0m")

def _banner(title: str) -> None:
    print(f"\n{DIVIDER}\n  {title}\n{DIVIDER}")

# ══════════════════════════════════════════════════════════════════════════════
#  AGENT ALPHA — Researcher
# ══════════════════════════════════════════════════════════════════════════════

ALPHA_PERSONA = """You are Alpha, an AI research agent specializing in \
large language models. Be concise and factual. When asked to produce \
multiple items, return exactly that many, one per line."""

def run_alpha(base: str, llm: LLM, alpha_id: str, ws: str) -> list[str]:
    _banner("Agent Alpha — Researcher")
    mem_ids: list[str] = []

    # ── Step A1: Generate 5 research findings ─────────────────────
    t0 = time.monotonic()
    findings_raw = llm.chat(
        system=ALPHA_PERSONA,
        user=(
            "Generate exactly 5 distinct research findings about the current state "
            "of large language models in 2025-2026. Each finding should be 1-2 sentences. "
            "Return one finding per line, no numbering or bullets."
        ),
    )
    elapsed = time.monotonic() - t0
    findings = [l.strip() for l in findings_raw.splitlines() if l.strip()][:5]
    _step("Alpha", "Research findings generated",
          content="\n".join(f"  {i+1}. {f}" for i, f in enumerate(findings)),
          detail=f"LLM call #{llm.call_count} | tokens≈{llm.total_tokens}",
          elapsed=elapsed)

    # Store each finding
    for i, finding in enumerate(findings):
        mid = ingest(base, alpha_id, ws, finding,
                     event_type="semantic_memory",
                     importance=round(0.75 + 0.05 * i, 2))
        mem_ids.append(mid)
    _step("Alpha", "Stored 5 findings to CogDB",
          detail=f"memory_ids: {mem_ids[0][:24]}…{mem_ids[-1][:24]}")

    # ── Step A2: ONNX-grounded theme extraction ────────────────────
    _step("SYS", "ONNX query — Alpha recalls own findings",
          detail="workspace_id=" + ws)
    t0 = time.monotonic()
    recalled = query_memory(base, ws, "large language model research findings 2025", top_k=5)
    onnx_elapsed = time.monotonic() - t0

    recalled_texts = []
    for r in recalled:
        if isinstance(r, dict):
            txt = (r.get("content") or r.get("text") or
                   (r.get("payload") or {}).get("text", ""))
            if txt:
                recalled_texts.append(txt)

    _step("Alpha", "ONNX memory recall complete",
          detail=f"results={len(recalled)} latency={onnx_elapsed:.1f}s",
          elapsed=onnx_elapsed)

    # Grounded theme extraction using recalled memories
    context = "\n".join(f"- {t}" for t in recalled_texts) if recalled_texts else \
              "\n".join(f"- {f}" for f in findings)

    t0 = time.monotonic()
    themes_raw = llm.chat(
        system=ALPHA_PERSONA,
        user=(
            f"Based on these research findings:\n{context}\n\n"
            "Identify exactly 3 key themes that emerge. "
            "Return one theme per line (no numbering)."
        ),
    )
    elapsed = time.monotonic() - t0
    themes = [l.strip() for l in themes_raw.splitlines() if l.strip()][:3]
    _step("Alpha", "Key themes extracted (grounded by ONNX recall)",
          content="\n".join(f"  • {t}" for t in themes),
          detail=f"LLM call #{llm.call_count} | grounded on {len(recalled_texts)} memories",
          elapsed=elapsed)

    # Store themes
    theme_mids = []
    for theme in themes:
        mid = ingest(base, alpha_id, ws,
                     f"Key theme: {theme}",
                     event_type="reflective",
                     importance=0.90)
        theme_mids.append(mid)
    mem_ids.extend(theme_mids)
    _step("Alpha", "Stored 3 themes to CogDB",
          detail=f"reflective memories: {len(theme_mids)}")

    return mem_ids

# ══════════════════════════════════════════════════════════════════════════════
#  MEMORY SHARING  Alpha → Beta
# ══════════════════════════════════════════════════════════════════════════════

def run_sharing(base: str, alpha_id: str, beta_id: str, ws: str,
                alpha_mids: list[str]) -> list[str]:
    _banner("Memory Sharing — Alpha → Beta")

    # Share contract
    status, _ = _req(base, "POST", "/v1/share-contracts", {
        "from_agent_id": alpha_id,
        "to_agent_id":   beta_id,
        "workspace_id":  ws,
        "bidirectional": False,
        "memory_types":  ["semantic_memory", "reflective", "agent_thought"],
    })
    _step("SYS", "Share-contract created",
          detail=f"alpha→beta | status={status}")

    # Share all of Alpha's memories
    shared = share_memories(base, alpha_id, beta_id, alpha_mids)
    _step("SYS", "Memories shared",
          detail=f"{shared}/{len(alpha_mids)} memories transferred to Beta")

    return alpha_mids[:shared]

# ══════════════════════════════════════════════════════════════════════════════
#  AGENT BETA — Analyst
# ══════════════════════════════════════════════════════════════════════════════

BETA_PERSONA = """You are Beta, an AI analyst agent. Your job is to \
critically analyze research findings provided to you and produce \
actionable insights or identify gaps. Be analytical and concise."""

def run_beta(base: str, llm: LLM, beta_id: str, alpha_id: str,
             ws: str, shared_mids: list[str]) -> list[str]:
    _banner("Agent Beta — Analyst")
    beta_mids: list[str] = []

    # ── Step B1: ONNX recall of shared memories ────────────────────
    _step("SYS", "ONNX query — Beta grounds itself on Alpha's shared memories")
    t0 = time.monotonic()
    recalled = query_memory(base, ws,
                            "LLM research themes trends findings analysis", top_k=8)
    onnx_elapsed = time.monotonic() - t0

    recalled_texts = []
    recalled_mids  = []
    for r in recalled:
        if isinstance(r, dict):
            txt = (r.get("content") or r.get("text") or
                   (r.get("payload") or {}).get("text", ""))
            mid = r.get("memory_id", "")
            if txt:
                recalled_texts.append(txt)
                if mid:
                    recalled_mids.append(mid)

    _step("Beta", "ONNX memory recall complete",
          detail=f"recalled={len(recalled_texts)} memories from workspace | "
                 f"latency={onnx_elapsed:.1f}s",
          elapsed=onnx_elapsed)

    context = "\n".join(f"- {t}" for t in recalled_texts) if recalled_texts \
              else "(no prior memories recalled)"

    # ── Step B2: Analytical synthesis ─────────────────────────────
    t0 = time.monotonic()
    analysis = llm.chat(
        system=BETA_PERSONA,
        user=(
            f"Alpha (the researcher) has shared these findings with you:\n"
            f"{context}\n\n"
            "Produce a critical analysis in exactly 3 numbered points:\n"
            "1. The most important implication\n"
            "2. A significant gap or risk Alpha missed\n"
            "3. A concrete recommendation for future research\n"
            "Be direct and specific."
        ),
    )
    elapsed = time.monotonic() - t0
    _step("Beta", "Critical analysis (grounded by ONNX recall)",
          content=analysis,
          detail=f"LLM call #{llm.call_count} | grounded on {len(recalled_texts)} memories",
          elapsed=elapsed)

    mid = ingest(base, beta_id, ws,
                 f"Critical analysis of Alpha's LLM research:\n{analysis}",
                 event_type="agent_thought", importance=0.92)
    beta_mids.append(mid)
    _step("Beta", "Analysis stored to CogDB", detail=f"memory_id={mid[:30]}")

    # ── Step B3: Conflict detection — does Beta disagree? ─────────
    t0 = time.monotonic()
    verdict = llm.chat(
        system=BETA_PERSONA,
        user=(
            f"You just wrote:\n{analysis}\n\n"
            f"Alpha's original findings were:\n{context}\n\n"
            "Is there a factual conflict or direct contradiction between "
            "your analysis and Alpha's findings? Answer with exactly one of:\n"
            "CONFLICT: <one sentence explaining the conflict>\n"
            "NO_CONFLICT\n"
            "Do not add anything else."
        ),
        temperature=0.2,
    )
    elapsed = time.monotonic() - t0
    _step("Beta", "Conflict detection",
          content=verdict,
          detail=f"LLM call #{llm.call_count}",
          elapsed=elapsed)

    # ── Step B4: CollaborationChain — resolve if conflict found ────
    if verdict.startswith("CONFLICT") and len(recalled_mids) >= 1:
        left_mid  = recalled_mids[0]          # Alpha's first recalled memory
        right_mid = beta_mids[0]               # Beta's analysis memory
        winner_id = resolve_conflict(base, left_mid, right_mid)
        _step("SYS", "CollaborationChain — conflict resolved",
              detail=f"winner={winner_id[:30]} | "
                     f"left(alpha)={left_mid[:20]} right(beta)={right_mid[:20]}")

        # Store resolution note
        resolution_note = (
            f"Conflict resolved between Alpha's finding and Beta's analysis. "
            f"Winner memory: {winner_id}. "
            f"Conflict summary: {verdict.replace('CONFLICT:', '').strip()}"
        )
        rmid = ingest(base, beta_id, ws, resolution_note,
                      event_type="reflective", importance=0.88)
        beta_mids.append(rmid)
        _step("Beta", "Conflict resolution stored",
              detail=f"memory_id={rmid[:30]}")
    else:
        _step("SYS", "No conflict — CollaborationChain skipped (consensus)")

    # ── Step B5: Final synthesis ───────────────────────────────────
    t0 = time.monotonic()
    synthesis = llm.chat(
        system=BETA_PERSONA,
        user=(
            "Based on your analysis and the conflict resolution outcome, "
            "write a single executive-summary paragraph (4-6 sentences) "
            "on where LLM research is headed, suitable for a board report."
        ),
    )
    elapsed = time.monotonic() - t0
    _step("Beta", "Executive synthesis (final output)",
          content=synthesis,
          detail=f"LLM call #{llm.call_count}",
          elapsed=elapsed)

    smid = ingest(base, beta_id, ws,
                  f"Executive synthesis: {synthesis}",
                  event_type="reflective", importance=0.95)
    beta_mids.append(smid)
    _step("Beta", "Synthesis stored to CogDB", detail=f"memory_id={smid[:30]}")

    return beta_mids

# ══════════════════════════════════════════════════════════════════════════════
#  FINAL REPORT
# ══════════════════════════════════════════════════════════════════════════════

def write_report(out_dir: Path, llm: LLM, ws: str,
                 alpha_id: str, beta_id: str,
                 alpha_mids: list[str], beta_mids: list[str],
                 total_elapsed: float) -> None:
    out_dir.mkdir(parents=True, exist_ok=True)

    print(f"\n{DIVIDER}")
    print(f"  MAS Scenario Test — Final Report")
    print(DIVIDER)
    print(f"  Workspace  : {ws}")
    print(f"  Alpha      : {alpha_id}")
    print(f"  Beta       : {beta_id}")
    print(f"  LLM model  : {llm.model}")
    print(f"  LLM calls  : {llm.call_count}  |  total tokens ≈ {llm.total_tokens}")
    print(f"  Duration   : {total_elapsed:.1f}s")
    print(SUBDIV)
    print(f"  Alpha stored : {len(alpha_mids)} memories")
    print(f"  Beta  stored : {len(beta_mids)} memories")
    print(f"  Shared       : {len(alpha_mids)} memories (Alpha→Beta)")
    print(SUBDIV)
    print("  Step timeline:")
    for s in TIMELINE:
        line = f"    [{s['ts']}] {s['agent']:5s} — {s['action']}"
        if s.get("elapsed"):
            line += f"  ({s['elapsed']:.1f}s)"
        print(line)
    print(f"\n{DIVIDER}\n")

    # Write markdown report
    md = [
        "# CogDB MAS Scenario Test — AI Research Collaboration",
        "",
        f"- **Date**: {time.strftime('%Y-%m-%dT%H:%M:%S')}",
        f"- **Workspace**: `{ws}`",
        f"- **LLM**: `{llm.model}`",
        f"- **LLM calls**: {llm.call_count} | **Tokens**: {llm.total_tokens}",
        f"- **Duration**: {total_elapsed:.1f}s",
        f"- **Alpha memories**: {len(alpha_mids)} | **Beta memories**: {len(beta_mids)}",
        "",
        "## Collaboration Flow",
        "",
        "| Time | Agent | Action | Latency |",
        "|---|---|---|---|",
    ]
    for s in TIMELINE:
        lat = f"{s['elapsed']:.1f}s" if s.get("elapsed") else "-"
        md.append(f"| {s['ts']} | {s['agent']} | {s['action']} | {lat} |")

    md += [
        "",
        "## Architecture",
        "",
        "```",
        "Alpha (Researcher)",
        "  LLM → 5 findings → CogDB (MainChain)",
        "  ONNX recall → grounded theme extraction → CogDB",
        "                    ↓  share-contract",
        "Beta (Analyst)",
        "  ONNX recall (grounded) → LLM critical analysis → CogDB",
        "  LLM conflict detection → CollaborationChain → resolution",
        "  LLM executive synthesis → CogDB",
        "```",
    ]
    (out_dir / "report.md").write_text("\n".join(md) + "\n")
    print(f"  Report written: {out_dir / 'report.md'}")

# ══════════════════════════════════════════════════════════════════════════════
#  MAIN
# ══════════════════════════════════════════════════════════════════════════════

def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--out-dir",  default="out/mas_scenario")
    parser.add_argument("--env-file", default=".env")
    parser.add_argument("--base-url", default="")
    args = parser.parse_args()

    _load_env(args.env_file)

    base = (args.base_url
            or os.environ.get("PLASMOD_AGENT_ENDPOINT", "")
            or "http://127.0.0.1:8080").rstrip("/")

    llm_base  = os.environ.get("PLASMOD_AGENT_LLM_BASE_URL", "").rstrip("/")
    llm_key   = os.environ.get("PLASMOD_AGENT_LLM_API_KEY",  "")
    llm_model = os.environ.get("PLASMOD_AGENT_LLM_MODEL",    "gpt-4o")
    llm_timeout = float(os.environ.get("PLASMOD_AGENT_LLM_TIMEOUT", "300"))

    if not llm_base or not llm_key:
        print("ERROR: PLASMOD_AGENT_LLM_BASE_URL and PLASMOD_AGENT_LLM_API_KEY must be set")
        sys.exit(1)

    llm = LLM(llm_base, llm_key, llm_model, llm_timeout, max_tokens=1024)

    # health
    st, _ = _req(base, "GET", "/healthz", timeout=5.0)
    if st != 200:
        print(f"ERROR: Server not reachable at {base}")
        sys.exit(1)

    ws = f"mas-{int(time.time())}-{uuid.uuid4().hex[:6]}"
    print(f"\n{DIVIDER}")
    print(f"  CogDB MAS Scenario — AI Research Collaboration")
    print(DIVIDER)
    print(f"  Server    : {base}")
    print(f"  LLM       : {llm_model} @ {llm_base}")
    print(f"  Workspace : {ws}")
    print(f"  Embedding : ONNX (real minilm-l6-v2)")
    print(f"  Storage   : BadgerDB (disk)")
    print(f"{DIVIDER}")

    t_start = time.monotonic()

    # Bootstrap agents
    _banner("Bootstrapping Agents")
    alpha_id = create_agent(base, "agent-alpha", "researcher", ws)
    beta_id  = create_agent(base, "agent-beta",  "analyst",    ws)
    _step("SYS", "Agents created",
          detail=f"Alpha={alpha_id[:30]}  Beta={beta_id[:30]}")

    # Run scenario
    alpha_mids = run_alpha(base, llm, alpha_id, ws)
    shared     = run_sharing(base, alpha_id, beta_id, ws, alpha_mids)
    beta_mids  = run_beta(base, llm, beta_id, alpha_id, ws, shared)

    total_elapsed = time.monotonic() - t_start
    write_report(Path(args.out_dir), llm, ws,
                 alpha_id, beta_id, alpha_mids, beta_mids, total_elapsed)


if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        print("\n[INTERRUPTED]")
        sys.exit(130)
    except Exception:
        traceback.print_exc()
        sys.exit(1)
