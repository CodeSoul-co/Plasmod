#!/usr/bin/env python3
"""
Layer-5 evaluation pipeline for 5-M1~5-M8.

This script orchestrates repeated runs of `scripts/e2e/live_agent_test.py`
across baseline profiles and aggregates metrics into comparable summaries.
"""

from __future__ import annotations

import argparse
import json
import statistics
import subprocess
import sys
from pathlib import Path
from typing import Any


PROFILES = ("plain_vector", "vector_metadata", "plasmod_full")
METRIC_KEYS = (
    "5-M1_task_success_rate",
    "5-M2_completion_time_s",
    "5-M3_token_cost_total_tokens",
    "5-M4_task_quality_proxy_pass_rate",
    "5-M5_evidence_supported_answer_rate",
    "5-M6_hallucination_rate_proxy",
    "5-M7_long_session_stability",
    "5-M8_user_facing_consistency",
)


def parse_args() -> argparse.Namespace:
    ap = argparse.ArgumentParser(description="Layer-5 baseline evaluation pipeline")
    ap.add_argument("--base-url", default="", help="override server URL")
    ap.add_argument("--env-file", default=".env")
    ap.add_argument("--profiles", default="plain_vector,vector_metadata,plasmod_full")
    ap.add_argument("--repeats", type=int, default=2)
    ap.add_argument("--skip-query", action="store_true")
    ap.add_argument("--out-dir", default="out/layer5_eval")
    return ap.parse_args()


def run_once(repo_root: Path, profile: str, run_idx: int, args: argparse.Namespace, out_dir: Path) -> dict[str, Any]:
    run_dir = out_dir / profile / f"run_{run_idx:02d}"
    run_dir.mkdir(parents=True, exist_ok=True)
    cmd = [
        "python3",
        str(repo_root / "scripts/e2e/live_agent_test.py"),
        "--backend-profile",
        profile,
        "--env-file",
        args.env_file,
        "--out-dir",
        str(run_dir),
    ]
    if args.base_url:
        cmd += ["--base-url", args.base_url]
    if args.skip_query:
        cmd += ["--skip-query"]

    print(f"[layer5-pipeline] run profile={profile} idx={run_idx} ...")
    proc = subprocess.run(cmd, cwd=str(repo_root), check=False)

    metrics_path = run_dir / "metrics_5m.json"
    checks_path = run_dir / "checks.json"
    req_path = run_dir / "requests.jsonl"
    if not metrics_path.exists():
        return {
            "ok": False,
            "exit_code": proc.returncode,
            "profile": profile,
            "run": run_idx,
            "run_dir": str(run_dir),
            "metrics": {},
            "checks": [],
            "request_paths": [],
        }
    metrics = json.loads(metrics_path.read_text(encoding="utf-8"))
    checks: list[dict[str, Any]] = []
    if checks_path.exists():
        checks = json.loads(checks_path.read_text(encoding="utf-8"))
    req_paths: list[str] = []
    if req_path.exists():
        for line in req_path.read_text(encoding="utf-8").splitlines():
            if not line.strip():
                continue
            try:
                row = json.loads(line)
            except Exception:
                continue
            p = row.get("path")
            if isinstance(p, str):
                req_paths.append(p)
    return {
        "ok": proc.returncode == 0,
        "exit_code": proc.returncode,
        "profile": profile,
        "run": run_idx,
        "run_dir": str(run_dir),
        "metrics": metrics,
        "checks": checks,
        "request_paths": req_paths,
    }


def aggregate(records: list[dict[str, Any]]) -> dict[str, Any]:
    by_profile: dict[str, list[dict[str, Any]]] = {}
    for r in records:
        by_profile.setdefault(r["profile"], []).append(r)

    summary: dict[str, Any] = {}
    for profile, rows in by_profile.items():
        profile_summary: dict[str, Any] = {"runs": len(rows), "pass_runs": sum(1 for x in rows if x["ok"])}
        for key in METRIC_KEYS:
            vals = [float(x["metrics"].get(key, 0.0)) for x in rows if x.get("metrics")]
            if not vals:
                profile_summary[key] = {"mean": 0.0, "stdev": 0.0}
                continue
            mu = statistics.mean(vals)
            sd = statistics.pstdev(vals) if len(vals) > 1 else 0.0
            profile_summary[key] = {"mean": round(mu, 6), "stdev": round(sd, 6)}
        summary[profile] = profile_summary
    return summary


def write_outputs(out_dir: Path, records: list[dict[str, Any]], summary: dict[str, Any]) -> None:
    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "records.json").write_text(json.dumps(records, indent=2), encoding="utf-8")
    (out_dir / "summary.json").write_text(json.dumps(summary, indent=2), encoding="utf-8")

    lines = [
        "# Layer-5 Baseline Evaluation Summary",
        "",
        "| profile | runs | pass_runs | 5-M1(success) | 5-M2(time_s) | 5-M3(tokens) | 5-M5(evidence) | 5-M6(hallucination_proxy) | 5-M7(stability) | 5-M8(consistency) |",
        "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for profile in PROFILES:
        s = summary.get(profile, {})
        def m(name: str) -> float:
            return float(s.get(name, {}).get("mean", 0.0))

        lines.append(
            f"| {profile} | {int(s.get('runs', 0))} | {int(s.get('pass_runs', 0))} | "
            f"{m('5-M1_task_success_rate'):.4f} | {m('5-M2_completion_time_s'):.3f} | "
            f"{m('5-M3_token_cost_total_tokens'):.1f} | {m('5-M5_evidence_supported_answer_rate'):.4f} | "
            f"{m('5-M6_hallucination_rate_proxy'):.4f} | {m('5-M7_long_session_stability'):.4f} | "
            f"{m('5-M8_user_facing_consistency'):.4f} |"
        )

    summary_md = "\n".join(lines) + "\n"
    (out_dir / "summary.md").write_text(summary_md, encoding="utf-8")
    # 5-E1: one unified overall result table
    (out_dir / "overall_results_table.md").write_text(summary_md, encoding="utf-8")

    # 5-E2: fixed auto-sampling rule for representative case studies
    case_rows = [r for r in records if r.get("metrics")]
    def case_score(r: dict[str, Any]) -> float:
        m = r["metrics"]
        return float(m.get("5-M1_task_success_rate", 0.0)) + float(m.get("5-M5_evidence_supported_answer_rate", 0.0)) - float(m.get("5-M6_hallucination_rate_proxy", 0.0))
    case_rows.sort(key=case_score, reverse=True)
    best = case_rows[0] if case_rows else None
    hard = case_rows[-1] if case_rows else None
    case_studies = {
        "rule": "best=argmax(M1+M5-M6), hard=argmin(M1+M5-M6)",
        "best_case": best,
        "hard_case": hard,
    }
    (out_dir / "case_studies.json").write_text(json.dumps(case_studies, indent=2), encoding="utf-8")
    case_md = [
        "# Layer-5 Case Studies",
        "",
        "## Sampling Rule",
        "- best case: maximize `5-M1 + 5-M5 - 5-M6`",
        "- hard case: minimize `5-M1 + 5-M5 - 5-M6`",
        "",
    ]
    for title, row in (("Best Case", best), ("Hard Case", hard)):
        if not row:
            continue
        case_md += [
            f"## {title}",
            f"- profile: `{row['profile']}`",
            f"- run: `{row['run']}`",
            f"- run_dir: `{row['run_dir']}`",
            f"- M1: `{row['metrics'].get('5-M1_task_success_rate', 0)}`",
            f"- M5: `{row['metrics'].get('5-M5_evidence_supported_answer_rate', 0)}`",
            f"- M6: `{row['metrics'].get('5-M6_hallucination_rate_proxy', 0)}`",
            "",
        ]
    (out_dir / "case_studies.md").write_text("\n".join(case_md) + "\n", encoding="utf-8")

    # 5-E3~E5: automated error attribution scripts outputs
    old_state = []
    provenance_success = []
    rollback_versioning = []
    for r in records:
        m = r.get("metrics", {})
        checks = r.get("checks", [])
        req_paths = r.get("request_paths", [])
        # E3: old-state not seen proxy
        has_query = float(m.get("5-M2_completion_time_s", 0.0)) > 0 and ("/v1/query" in req_paths)
        low_evidence = float(m.get("5-M5_evidence_supported_answer_rate", 0.0)) < 0.5
        proof_fail = any(c.get("check") == "QueryChain — proof_trace returned" and c.get("status") != "PASS" for c in checks)
        if has_query and (low_evidence or proof_fail):
            old_state.append({"profile": r["profile"], "run": r["run"], "run_dir": r["run_dir"], "reason": "low evidence/proof missing"})
        # E4: provenance helped success
        if float(m.get("5-M5_evidence_supported_answer_rate", 0.0)) >= 0.8:
            provenance_success.append({"profile": r["profile"], "run": r["run"], "run_dir": r["run_dir"]})
        # E5: rollback/versioning/governance avoided accumulation (proxy)
        if "/v1/internal/memory/conflict/resolve" in req_paths and any(c.get("check") == "CollaborationChain — conflict resolved" and c.get("status") == "PASS" for c in checks):
            rollback_versioning.append({"profile": r["profile"], "run": r["run"], "run_dir": r["run_dir"]})
    attribution = {
        "e3_old_state_not_seen": old_state,
        "e4_provenance_success": provenance_success,
        "e5_rollback_versioning_proxy": rollback_versioning,
    }
    (out_dir / "error_attribution.json").write_text(json.dumps(attribution, indent=2), encoding="utf-8")
    attr_md = [
        "# Layer-5 Error Attribution",
        "",
        "## E3 Old State Not Seen",
        f"- count: `{len(old_state)}`",
        "",
        "## E4 Provenance Success",
        f"- count: `{len(provenance_success)}`",
        "",
        "## E5 Rollback/Versioning Proxy",
        f"- count: `{len(rollback_versioning)}`",
        "",
        "> Rule details are in `error_attribution.json`.",
    ]
    (out_dir / "error_attribution.md").write_text("\n".join(attr_md) + "\n", encoding="utf-8")


def main() -> int:
    args = parse_args()
    repo_root = Path(__file__).resolve().parents[2]
    out_dir = (repo_root / args.out_dir).resolve()
    profiles = [x.strip() for x in args.profiles.split(",") if x.strip()]
    for p in profiles:
        if p not in PROFILES:
            print(f"[layer5-pipeline] invalid profile: {p}", file=sys.stderr)
            return 2
    if args.repeats <= 0:
        print("[layer5-pipeline] repeats must be > 0", file=sys.stderr)
        return 2

    records: list[dict[str, Any]] = []
    for p in profiles:
        for i in range(1, args.repeats + 1):
            records.append(run_once(repo_root, p, i, args, out_dir))

    summary = aggregate(records)
    write_outputs(out_dir, records, summary)

    total = len(records)
    passed = sum(1 for r in records if r["ok"])
    print(f"[layer5-pipeline] done: pass_runs={passed}/{total}")
    print(f"[layer5-pipeline] outputs: {out_dir / 'summary.md'}")
    return 0 if passed == total else 1


if __name__ == "__main__":
    raise SystemExit(main())

