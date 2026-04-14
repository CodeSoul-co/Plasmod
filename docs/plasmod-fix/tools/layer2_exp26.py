#!/usr/bin/env python3
"""
Plasmod.md §2.6 实验方法 — 可执行入口

  exp1  写入速率阶梯：write-to-visible、p95 query、stale rate
  exp2  一致性模式：协议说明 + 可选单路径基线复测
  exp3  恢复与重放：golden ingest → wipe → recovery_time → 再 ingest → 校验条数（破坏性）

  python3 docs/plasmod-fix/tools/layer2_exp26.py exp1
  python3 docs/plasmod-fix/tools/layer2_exp26.py exp2
  python3 docs/plasmod-fix/tools/layer2_exp26.py exp3 --golden-n 80 --i-understand-wipe

详见: docs/plasmod-fix/experiments/layer2-section-2-6.md
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import sys
import time
from pathlib import Path
from typing import Any


def _load_layer2() -> Any:
    path = Path(__file__).resolve().parent / "layer2_visibility_test.py"
    spec = importlib.util.spec_from_file_location("layer2_visibility_test", path)
    if spec is None or spec.loader is None:
        raise RuntimeError("cannot load layer2_visibility_test.py")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def cmd_exp1(args: argparse.Namespace, L: Any) -> int:
    base = args.base_url.rstrip("/")
    if not L.health_ok(base):
        print("[exp26 exp1] /healthz failed", file=sys.stderr)
        return 1
    ladder = [float(x.strip()) for x in args.ladder.split(",") if x.strip()]
    rows: list[dict[str, Any]] = []
    ids = (args.agent_id, args.session_id, args.tenant_id, args.workspace_id)

    for hz in ladder:
        print(f"\n{'='*60}\n[exp26 exp1] ladder step target_ingest_hz={hz} (step_seconds={args.step_seconds})\n{'='*60}")
        print("[exp26 exp1] (a) sequential write-to-visible probes")
        _poll, e2e_ms, _ing = L.run_sequential(
            base,
            args.w2v_probes,
            args.agent_id,
            args.session_id,
            args.tenant_id,
            args.workspace_id,
            args.visibility_timeout,
            args.poll,
            False,
        )
        p95_w2v = L.percentile(sorted(e2e_ms), 95) if e2e_ms else 0.0
        p50_w2v = L.percentile(sorted(e2e_ms), 50) if e2e_ms else 0.0
        print(f"  p50_e2e_write_to_visible_ms={p50_w2v:.2f}  p95={p95_w2v:.2f}")

        qhz = min(args.query_hz_cap, max(0.5, hz * args.query_hz_ratio))
        print(f"[exp26 exp1] (b) retrieval under write load: ingest≈{hz}/s query≈{qhz}/s for {args.step_seconds}s")
        qlats, ic, qc = L.run_retrieval_under_write_load(
            base, args.step_seconds, hz, qhz, *ids
        )
        p95_q = L.percentile(sorted(qlats), 95) if qlats else 0.0
        p50_q = L.percentile(sorted(qlats), 50) if qlats else 0.0
        print(f"  p50_query_latency_ms={p50_q:.2f}  p95={p95_q:.2f}  (ingest_events≈{ic} queries={qc})")

        print(f"[exp26 exp1] (c) stale probe n={args.stale_samples}")
        stale, good = L.run_stale_probe(
            base, args.stale_samples, *ids, args.poll
        )
        total = stale + good
        rate = (stale / total) if total else 0.0
        print(f"  stale_result_rate={rate:.4f}  (stale={stale} ok={good})")

        rows.append(
            {
                "ingest_hz_target": hz,
                "p50_e2e_write_to_visible_ms": p50_w2v,
                "p95_e2e_write_to_visible_ms": p95_w2v,
                "p50_query_latency_ms": p50_q,
                "p95_query_latency_ms": p95_q,
                "stale_result_rate": rate,
                "under_load_ingest_events": ic,
                "under_load_query_requests": qc,
            }
        )

        if args.cooldown_seconds > 0:
            print(f"[exp26 exp1] cooldown {args.cooldown_seconds}s …")
            time.sleep(args.cooldown_seconds)

    print("\n[exp26 exp1] summary table")
    hdr = f"{'ingest_hz':>10} {'p95_w2v':>10} {'p95_query':>10} {'stale_rate':>12}"
    print(hdr)
    print("-" * len(hdr))
    for r in rows:
        print(
            f"{r['ingest_hz_target']:>10.1f} {r['p95_e2e_write_to_visible_ms']:>10.2f} "
            f"{r['p95_query_latency_ms']:>10.2f} {r['stale_result_rate']:>12.4f}"
        )

    if args.json_out:
        outp = Path(args.json_out)
        outp.write_text(json.dumps(rows, indent=2), encoding="utf-8")
        print(f"\n[exp26 exp1] wrote {outp}")

    return 0


def cmd_exp2(args: argparse.Namespace, L: Any) -> int:
    print(
        """
[exp26 exp2] §2.6 第二个实验 — 一致性模式（设计说明）

论文要求对比三类语义：
  • 严格可见（strong / read-your-writes）
  • bounded staleness（有界陈旧）
  • eventual visibility（最终可见）

本脚本会直接调用 /v1/admin/consistency-mode：
  1) GET 当前模式；
  2) 依次 POST strict_visible / bounded_staleness / eventual_visibility；
  3) 每次 POST 后 GET 校验；
  4) 结束后恢复初始模式。

可选：使用 --baseline-ladder-step 在某一固定写入速率下自动跑与 exp1 相同的一档（需服务可用）。
"""
    )
    if not args.skip_consistency_api:
        print("[exp26 exp2] exercise /v1/admin/consistency-mode …")
        st0, b0 = _admin_consistency_get(args.base_url.rstrip("/"), L)
        if st0 != 200 or not isinstance(b0, dict):
            print(f"[exp26 exp2] consistency GET failed {st0} {b0!r}", file=sys.stderr)
            return 1
        original = str(b0.get("mode", "strict_visible"))
        print(f"  current_mode={original}")
        modes = ["strict_visible", "bounded_staleness", "eventual_visibility"]
        for mode in modes:
            st1, b1 = _admin_consistency_set(args.base_url.rstrip("/"), L, mode)
            if st1 != 200:
                print(f"[exp26 exp2] consistency POST {mode} failed {st1} {b1!r}", file=sys.stderr)
                return 1
            st2, b2 = _admin_consistency_get(args.base_url.rstrip("/"), L)
            got = b2.get("mode") if isinstance(b2, dict) else None
            print(f"  set={mode} verify_mode={got}")
            if st2 != 200 or got != mode:
                print(f"[exp26 exp2] consistency verify mismatch for {mode}: {st2} {b2!r}", file=sys.stderr)
                return 1
        if original not in modes:
            original = "strict_visible"
        st3, b3 = _admin_consistency_set(args.base_url.rstrip("/"), L, original)
        if st3 != 200:
            print(f"[exp26 exp2] consistency restore failed {st3} {b3!r}", file=sys.stderr)
            return 1
        print(f"  restored_mode={original}")

    if args.baseline_ladder_step <= 0:
        return 0
    base = args.base_url.rstrip("/")
    if not L.health_ok(base):
        print("[exp26 exp2] /healthz failed", file=sys.stderr)
        return 1
    hz = args.baseline_ladder_step
    print(f"\n[exp26 exp2] optional baseline at ingest_hz={hz} (repeats={args.baseline_repeats})")
    ids = (args.agent_id, args.session_id, args.tenant_id, args.workspace_id)
    for r in range(args.baseline_repeats):
        print(f"\n--- baseline repeat {r + 1}/{args.baseline_repeats} ---")
        _poll, e2e_ms, _ing = L.run_sequential(
            base, 6, *ids, args.visibility_timeout, args.poll, False
        )
        p95_w2v = L.percentile(sorted(e2e_ms), 95) if e2e_ms else 0.0
        qhz = min(8.0, max(0.5, hz * 0.5))
        qlats, ic, qc = L.run_retrieval_under_write_load(
            base, args.step_seconds, hz, qhz, *ids
        )
        p95_q = L.percentile(sorted(qlats), 95) if qlats else 0.0
        stale, good = L.run_stale_probe(base, 20, *ids, args.poll)
        rate = stale / (stale + good) if (stale + good) else 0.0
        print(
            f"  p95_w2v_ms={p95_w2v:.2f} p95_query_ms={p95_q:.2f} stale_rate={rate:.4f} "
            f"(load ingest≈{ic} q={qc})"
        )
    return 0


def _admin_wipe(base: str, L: Any) -> tuple[int, Any]:
    h = _admin_headers(L)
    return L._http_json(
        "POST",
        f"{base}/v1/admin/data/wipe",
        {"confirm": "delete_all_data"},
        timeout=120.0,
        extra_headers=h if h else None,
    )


def _admin_headers(L: Any) -> dict[str, str]:
    h: dict[str, str] = {}
    k = L._admin_key()
    if k:
        h["X-Admin-Key"] = k
    return h


def _admin_consistency_get(base: str, L: Any) -> tuple[int, Any]:
    return L._http_json("GET", f"{base}/v1/admin/consistency-mode", None, timeout=30.0, extra_headers=_admin_headers(L))


def _admin_consistency_set(base: str, L: Any, mode: str) -> tuple[int, Any]:
    return L._http_json(
        "POST",
        f"{base}/v1/admin/consistency-mode",
        {"mode": mode},
        timeout=30.0,
        extra_headers=_admin_headers(L),
    )


def _admin_replay(base: str, L: Any, *, from_lsn: int, limit: int, dry_run: bool, apply: bool) -> tuple[int, Any]:
    body: dict[str, Any] = {
        "from_lsn": int(from_lsn),
        "limit": int(limit),
        "dry_run": bool(dry_run),
        "apply": bool(apply),
    }
    if apply and not dry_run:
        body["confirm"] = "apply_replay"
    return L._http_json(
        "POST",
        f"{base}/v1/admin/replay",
        body,
        timeout=120.0,
        extra_headers=_admin_headers(L),
    )


def _recovery_seconds_after_wipe(base: str, L: Any, ids: tuple[str, str, str, str]) -> tuple[float, bool]:
    """从发起 wipe 到 health + 一次 ingest 成功。"""
    agent_id, session_id, tenant_id, workspace_id = ids
    t0 = time.perf_counter()
    st, body = _admin_wipe(base, L)
    if st != 200:
        print(f"[exp26 exp3] wipe failed {st} {body!r}", file=sys.stderr)
        return -1.0, False
    deadline = time.monotonic() + 90.0
    ok = False
    while time.monotonic() < deadline:
        if L.health_ok(base):
            ev = L.build_event(
                event_id=f"evt_recovery_probe_{int(time.time())}",
                agent_id=agent_id,
                session_id=session_id,
                tenant_id=tenant_id,
                workspace_id=workspace_id,
                event_type="user_message",
                text="recovery probe",
                causal_refs=[],
            )
            st2, _ = L.ingest_event(base, ev)
            if st2 == 200:
                ok = True
                break
        time.sleep(0.05)
    return time.perf_counter() - t0, ok


def _ingest_golden(base: str, L: Any, n: int, ids: tuple[str, str, str, str]) -> int:
    agent_id, session_id, tenant_id, workspace_id = ids
    ok_n = 0
    for i in range(n):
        eid = f"evt_exp3_g_{i:04d}"
        ev = L.build_event(
            event_id=eid,
            agent_id=agent_id,
            session_id=session_id,
            tenant_id=tenant_id,
            workspace_id=workspace_id,
            event_type="user_message",
            text=f"golden exp3 row {i} deterministic",
            causal_refs=[],
        )
        st, _ = L.ingest_event(base, ev)
        if st == 200:
            ok_n += 1
    return ok_n


def cmd_exp3(args: argparse.Namespace, L: Any) -> int:
    if not args.i_understand_wipe:
        print("[exp26 exp3] 拒绝：破坏性实验请加 --i-understand-wipe", file=sys.stderr)
        return 1
    base = args.base_url.rstrip("/")
    if not L.health_ok(base):
        print("[exp26 exp3] /healthz failed", file=sys.stderr)
        return 1

    ids = (args.agent_id, args.session_id, args.tenant_id, args.workspace_id)
    n = args.golden_n

    if not args.skip_initial_wipe:
        print("[exp26 exp3] initial wipe (clean slate)…")
        st, b = _admin_wipe(base, L)
        if st != 200:
            print(f"[exp26 exp3] initial wipe failed {st} {b!r}", file=sys.stderr)
            return 1
        time.sleep(0.3)

    print(f"[exp26 exp3] ingest golden N={n} …")
    got = _ingest_golden(base, L, n, ids)
    st, rows = L.list_memories(base, args.agent_id, args.session_id)
    cnt1 = len(rows) if st == 200 else -1
    print(f"  ingest_ok={got}/{n}  list_memories_count={cnt1}")

    if not args.skip_replay_check:
        print(f"[exp26 exp3] replay dry-run from_lsn={args.replay_from_lsn} limit={args.replay_limit} …")
        stp, bp = _admin_replay(
            base,
            L,
            from_lsn=args.replay_from_lsn,
            limit=args.replay_limit,
            dry_run=True,
            apply=False,
        )
        if stp != 200:
            print(f"[exp26 exp3] replay dry-run failed {stp} {bp!r}", file=sys.stderr)
            return 1
        print(f"  replay_preview_ok status={stp}")

        print(f"[exp26 exp3] replay apply from_lsn={args.replay_from_lsn} limit={args.replay_limit} …")
        sta, ba = _admin_replay(
            base,
            L,
            from_lsn=args.replay_from_lsn,
            limit=args.replay_limit,
            dry_run=False,
            apply=True,
        )
        if sta != 200:
            print(f"[exp26 exp3] replay apply failed {sta} {ba!r}", file=sys.stderr)
            return 1
        print(f"  replay_apply_ok status={sta}")

    print("[exp26 exp3] disaster wipe + measure recovery_time …")
    rec_s, rec_ok = _recovery_seconds_after_wipe(base, L, ids)
    print(f"  recovery_time_s={rec_s:.3f}  recovery_ok={rec_ok}")

    print(f"[exp26 exp3] re-ingest same {n} golden event_ids …")
    got2 = _ingest_golden(base, L, n, ids)
    st2, rows2 = L.list_memories(base, args.agent_id, args.session_id)
    cnt2 = len(rows2) if st2 == 200 else -1
    print(f"  re_ingest_ok={got2}/{n}  list_memories_count={cnt2}")

    # 恢复阶段会插入 1 条 probe ingest，故 cnt2 可能为 n 或 n+1
    print(f"[exp26 exp3] correctness: list_memories_count>={n} → {cnt2 >= n}")
    ok = bool(rec_ok and got == n and got2 == n and cnt2 >= n)
    return 0 if ok else 1


def main() -> int:
    ap = argparse.ArgumentParser(description="Plasmod.md §2.6 experiment driver")
    ap.add_argument("--base-url", default=None)
    ap.add_argument("--agent-id", default="agent_layer2")
    ap.add_argument("--session-id", default="sess_layer2_demo")
    ap.add_argument("--tenant-id", default="t_layer2")
    ap.add_argument("--workspace-id", default="w_layer2")
    ap.add_argument("--visibility-timeout", type=float, default=15.0)
    ap.add_argument("--poll", type=float, default=0.02)
    sub = ap.add_subparsers(dest="cmd", required=True)

    p1 = sub.add_parser("exp1", help="写入速率阶梯（§2.6 实验一）")
    p1.add_argument(
        "--ladder",
        default="1,4,8,16",
        help="comma-separated target ingest rates (Hz) for background writer",
    )
    p1.add_argument("--step-seconds", type=float, default=18.0, dest="step_seconds")
    p1.add_argument("--w2v-probes", type=int, default=8, dest="w2v_probes")
    p1.add_argument("--stale-samples", type=int, default=22, dest="stale_samples")
    p1.add_argument("--query-hz-ratio", type=float, default=0.5, dest="query_hz_ratio")
    p1.add_argument("--query-hz-cap", type=float, default=6.0, dest="query_hz_cap")
    p1.add_argument("--cooldown-seconds", type=float, default=5.0, dest="cooldown_seconds")
    p1.add_argument("--json-out", default="", dest="json_out")

    p2 = sub.add_parser("exp2", help="一致性模式说明 + 可选基线（§2.6 实验二）")
    p2.add_argument("--skip-consistency-api", action="store_true", dest="skip_consistency_api")
    p2.add_argument("--baseline-ladder-step", type=float, default=0.0, dest="baseline_ladder_step")
    p2.add_argument("--baseline-repeats", type=int, default=3, dest="baseline_repeats")
    p2.add_argument("--step-seconds", type=float, default=12.0, dest="step_seconds")

    p3 = sub.add_parser("exp3", help="恢复与重放（§2.6 实验三，破坏性）")
    p3.add_argument("--golden-n", type=int, default=60, dest="golden_n")
    p3.add_argument("--i-understand-wipe", action="store_true", dest="i_understand_wipe")
    p3.add_argument("--skip-initial-wipe", action="store_true", dest="skip_initial_wipe")
    p3.add_argument("--skip-replay-check", action="store_true", dest="skip_replay_check")
    p3.add_argument("--replay-from-lsn", type=int, default=0, dest="replay_from_lsn")
    p3.add_argument("--replay-limit", type=int, default=120, dest="replay_limit")
    args = ap.parse_args()
    L = _load_layer2()
    if args.base_url is None:
        args.base_url = L._base_url()

    if args.cmd == "exp1":
        return cmd_exp1(args, L)
    if args.cmd == "exp2":
        return cmd_exp2(args, L)
    if args.cmd == "exp3":
        return cmd_exp3(args, L)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
