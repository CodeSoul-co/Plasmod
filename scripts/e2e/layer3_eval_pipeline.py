#!/usr/bin/env python3
"""
Layer3 子实验方法（3-E1~3-E6）统一评测流水线。

设计目标：
- 固定 agent/prompt/model/tool/schema，唯一变量为 backend profile
- 自动统计 success/memory recall/state awareness/evidence/step quality
"""
from __future__ import annotations

import argparse
import json
import statistics
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

PROFILES = ("plain_vector", "vector_metadata", "plasmod_full")


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(description="Layer3 E1~E6 evaluation pipeline")
    p.add_argument("--base-url", default="http://127.0.0.1:8080")
    p.add_argument("--out-dir", default="out/layer3_eval")
    p.add_argument("--env-file", default=".env")
    p.add_argument("--repeats", type=int, default=1)
    p.add_argument("--profiles", nargs="+", default=list(PROFILES), choices=PROFILES)
    p.add_argument("--skip-query", action="store_true")
    p.add_argument("--python-bin", default=sys.executable)
    return p.parse_args()


def _mean(values: list[float]) -> float:
    return statistics.mean(values) if values else 0.0


def _stdev(values: list[float]) -> float:
    return statistics.stdev(values) if len(values) >= 2 else 0.0


def _ratio(ok: int, total: int) -> float:
    return (ok / total) if total else 0.0


def _count_checks(checks: list[dict[str, Any]], keyword: str) -> tuple[int, int]:
    hits = [c for c in checks if keyword in str(c.get("check", ""))]
    ok = sum(1 for c in hits if c.get("status") == "PASS")
    return ok, len(hits)


def step_quality_score(checks: list[dict[str, Any]]) -> float:
    """
    3-E6 代理指标：以步骤 5/10/15 对应链路检查质量估计分步质量。
    - step5  -> MainChain
    - step10 -> QueryChain
    - step15 -> CollaborationChain + MemoryBank
    """
    m_ok, m_total = _count_checks(checks, "MainChain")
    q_ok, q_total = _count_checks(checks, "QueryChain")
    c_ok, c_total = _count_checks(checks, "CollaborationChain")
    b_ok, b_total = _count_checks(checks, "MemoryBank")
    score_5 = _ratio(m_ok, m_total)
    score_10 = _ratio(q_ok, q_total)
    score_15 = _ratio(c_ok + b_ok, c_total + b_total)
    return (score_5 + score_10 + score_15) / 3.0


def compute_3e_metrics(metrics_5m: dict[str, Any], checks: list[dict[str, Any]]) -> dict[str, float]:
    pass_count = sum(1 for c in checks if c.get("status") == "PASS")
    total_count = len(checks)
    query_ok, query_total = _count_checks(checks, "QueryChain")
    memory_ok, memory_total = _count_checks(checks, "MemoryPipelineChain")
    evidence_rate = float(metrics_5m.get("5-M5_evidence_supported_answer_rate", 0.0))

    return {
        "3-E2_task_success_rate": round(_ratio(pass_count, total_count), 4),
        "3-E3_memory_recall_quality": round(_ratio(query_ok, query_total), 4),
        "3-E4_state_awareness_quality": round(_ratio(memory_ok, memory_total), 4),
        "3-E5_evidence_supported_rate": round(evidence_rate, 4),
        "3-E6_step_quality_score": round(step_quality_score(checks), 4),
    }


def run_once(args: argparse.Namespace, profile: str, run_idx: int, out_root: Path) -> dict[str, Any]:
    run_dir = out_root / profile / f"run_{run_idx:02d}"
    run_dir.mkdir(parents=True, exist_ok=True)
    cmd = [
        args.python_bin,
        "scripts/e2e/live_agent_test.py",
        "--base-url",
        args.base_url,
        "--env-file",
        args.env_file,
        "--backend-profile",
        profile,
        "--out-dir",
        str(run_dir),
    ]
    if args.skip_query:
        cmd.append("--skip-query")

    t0 = time.monotonic()
    proc = subprocess.run(cmd, capture_output=True, text=True)
    elapsed = time.monotonic() - t0

    metrics_5m_path = run_dir / "metrics_5m.json"
    checks_path = run_dir / "checks.json"
    if not metrics_5m_path.exists() or not checks_path.exists():
        return {
            "profile": profile,
            "run": run_idx,
            "ok": False,
            "elapsed_s": round(elapsed, 3),
            "exit_code": proc.returncode,
            "stderr": proc.stderr[-2000:],
        }

    metrics_5m = json.loads(metrics_5m_path.read_text())
    checks = json.loads(checks_path.read_text())
    metrics_3e = compute_3e_metrics(metrics_5m, checks)

    return {
        "profile": profile,
        "run": run_idx,
        "ok": proc.returncode == 0,
        "elapsed_s": round(elapsed, 3),
        "exit_code": proc.returncode,
        "metrics_3e": metrics_3e,
        "metrics_5m_ref": metrics_5m,
    }


def aggregate(records: list[dict[str, Any]]) -> dict[str, Any]:
    grouped: dict[str, list[dict[str, Any]]] = {}
    for rec in records:
        grouped.setdefault(rec["profile"], []).append(rec)

    summary: dict[str, Any] = {}
    for profile, rs in grouped.items():
        ok_runs = [r for r in rs if r.get("ok") and r.get("metrics_3e")]
        item: dict[str, Any] = {
            "runs": len(rs),
            "ok_runs": len(ok_runs),
        }
        for key in (
            "3-E2_task_success_rate",
            "3-E3_memory_recall_quality",
            "3-E4_state_awareness_quality",
            "3-E5_evidence_supported_rate",
            "3-E6_step_quality_score",
        ):
            vals = [float(r["metrics_3e"][key]) for r in ok_runs]
            item[key] = {"mean": round(_mean(vals), 4), "stdev": round(_stdev(vals), 4)}
        summary[profile] = item
    return summary


def write_outputs(out_root: Path, records: list[dict[str, Any]], summary: dict[str, Any]) -> None:
    out_root.mkdir(parents=True, exist_ok=True)
    (out_root / "records.json").write_text(json.dumps(records, indent=2, ensure_ascii=False) + "\n")
    (out_root / "summary.json").write_text(json.dumps(summary, indent=2, ensure_ascii=False) + "\n")

    md = [
        "# Layer3 子实验方法（3-E1~3-E6）评测结果",
        "",
        "- 3-E1: 固定 agent/prompt/model/tool/schema，唯一变量为 backend profile",
        "- 指标来自 `live_agent_test.py` 运行输出，按 profile 聚合",
        "",
        "| Profile | Runs(ok/total) | 3-E2 成功率 | 3-E3 memory检索 | 3-E4 状态感知 | 3-E5 证据支持率 | 3-E6 分步质量 |",
        "|---|---:|---:|---:|---:|---:|---:|",
    ]
    for profile, data in summary.items():
        md.append(
            f"| {profile} | {data['ok_runs']}/{data['runs']} | "
            f"{data['3-E2_task_success_rate']['mean']:.4f}±{data['3-E2_task_success_rate']['stdev']:.4f} | "
            f"{data['3-E3_memory_recall_quality']['mean']:.4f}±{data['3-E3_memory_recall_quality']['stdev']:.4f} | "
            f"{data['3-E4_state_awareness_quality']['mean']:.4f}±{data['3-E4_state_awareness_quality']['stdev']:.4f} | "
            f"{data['3-E5_evidence_supported_rate']['mean']:.4f}±{data['3-E5_evidence_supported_rate']['stdev']:.4f} | "
            f"{data['3-E6_step_quality_score']['mean']:.4f}±{data['3-E6_step_quality_score']['stdev']:.4f} |"
        )
    (out_root / "summary.md").write_text("\n".join(md) + "\n")


def main() -> None:
    args = parse_args()
    out_root = Path(args.out_dir)
    records: list[dict[str, Any]] = []

    for profile in args.profiles:
        for i in range(1, args.repeats + 1):
            print(f"[layer3_eval] profile={profile} run={i}/{args.repeats} ...")
            rec = run_once(args, profile, i, out_root)
            records.append(rec)
            print(
                f"[layer3_eval] done profile={profile} run={i} "
                f"ok={rec.get('ok')} exit={rec.get('exit_code')}"
            )

    summary = aggregate(records)
    write_outputs(out_root, records, summary)
    print(f"[layer3_eval] outputs: {out_root / 'summary.md'}")


if __name__ == "__main__":
    main()

