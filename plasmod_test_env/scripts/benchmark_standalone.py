#!/usr/bin/env python3
"""Four-group benchmark — all groups run as Go subprocesses for consistent measurement.

Pipeline:
  G1: plasmod_bench --mode=faiss           (FAISS HNSW via CGO)
  G2: plasmod_bench --mode=knowhere-build   (Knowhere HNSW via CGO, OpenMP parallel batch)
  G3: plasmod_bench --mode=vector-only      (GlobalSegmentRetriever.Search via CGO, OpenMP batch)
  G4: plasmod_bench --mode=http-query        (HTTP batch query)

Modes per group:
  - old: repeated single-query batch
  - new: OpenMP batch + plugin
  - raw: Standard batch (no plugin)

Metrics per group:
  build_ms    — HNSW index construction time (separate)
  batch_ms    — single batch_search call (nq queries together, parallel)
  batch_qps   — nq / (batch_ms / 1000)
  serial_ms    — sum of nq individual search calls
  serial_qps   — nq / (serial_ms / 1000)
  recall@K    — vs ground truth from G1 batch results

For deep/ dataset:
  --indexed-dataset=data/deep/base.10M.fbin
  --query-dataset=data/deep/query.public.10K.fbin
  --groundtruth=data/deep/groundtruth.public.10K.ibin
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import time
import pathlib
import numpy as np

ROOT = pathlib.Path(__file__).resolve().parents[1]
DATA_DIR = ROOT / "data"
BIN_DIR = ROOT / "bin"
BENCH_BIN = BIN_DIR / "plasmod_bench"
PLASMOD_URL = os.environ.get("PLASMOD_URL", "http://127.0.0.1:8080")
WARM_SEGMENT_ID = "warm.four_group"

os.environ.setdefault("KMP_DUPLICATE_LIB_OK", "TRUE")


def run_group(mode: str, indexed_dataset: str, query_dataset: str,
              indexed_count: int, nq: int, topk: int, segment: str,
              server_url: str = PLASMOD_URL,
              groundtruth: str = "") -> dict | None:
    """Run a benchmark group as a Go subprocess and return parsed JSON result."""
    args = [
        str(BENCH_BIN),
        f"--mode={mode}",
        f"--indexed-dataset={indexed_dataset}",
        f"--query-dataset={query_dataset}",
        f"--indexed-count={indexed_count}",
        f"--queries={nq}",
        f"--topk={topk}",
        f"--segment={segment}",
    ]
    if groundtruth:
        args.append(f"--groundtruth={groundtruth}")
    if mode in ("http-query", "http-query-raw"):
        args.append(f"--server-url={server_url}")
    env = dict(os.environ, KMP_DUPLICATE_LIB_OK="TRUE")
    r = subprocess.run(args, capture_output=True, text=True, timeout=3600, env=env)
    if r.returncode != 0:
        print(f"  [{mode}] failed: {r.stderr[:300].strip()}")
        return None
    return json.loads(r.stdout)


def recall_at_k(gt_ids: np.ndarray, got_ids: np.ndarray, topk: int) -> float:
    hits = 0
    for i in range(gt_ids.shape[0]):
        hits += len(set(gt_ids[i, :topk].tolist()) & set(got_ids[i, :topk].tolist()))
    return hits / float(gt_ids.shape[0] * topk)


def main():
    ap = argparse.ArgumentParser(description="Four-group benchmark")
    ap.add_argument("--indexed-dataset", type=pathlib.Path,
                    default="", help="Path to .fbin containing indexed vectors")
    ap.add_argument("--query-dataset", type=pathlib.Path,
                    default="", help="Path to .fbin containing query vectors")
    ap.add_argument("--groundtruth", type=pathlib.Path,
                    default="", help="Path to .ibin ground truth file")
    ap.add_argument("--indexed-count", type=int, default=0,
                    help="Number of indexed vectors (0=use all in dataset)")
    ap.add_argument("--num-queries", type=int, default=10000)
    ap.add_argument("--topk", type=int, default=10)
    ap.add_argument("--skip-faiss", action="store_true")
    ap.add_argument("--skip-knowhere", action="store_true")
    ap.add_argument("--skip-cgo", action="store_true")
    ap.add_argument("--skip-http", action="store_true")
    ap.add_argument("--old-only", action="store_true",
                    help="Run only the 'old' repeated single-query modes")
    ap.add_argument("--new-raw-only", action="store_true",
                    help="Run only the 'new' OpenMP batch and 'raw' standard modes")
    args = ap.parse_args()

    indexed_ds = str(args.indexed_dataset) if args.indexed_dataset else ""
    query_ds = str(args.query_dataset) if args.query_dataset else ""
    groundtruth = str(args.groundtruth) if args.groundtruth else ""
    indexed_count = args.indexed_count
    nq = args.num_queries
    topk = args.topk

    results = []

    # --- G1: FAISS HNSW ---
    if not args.skip_faiss and not args.new_raw_only:
        print(f"\n[G1-old] FAISS HNSW — repeated single-query batch (indexed={indexed_count})…")
        r = run_group("faiss-single", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g1_faiss_old",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G1-old"
            results.append(r)

    if not args.skip_faiss and not args.old_only:
        print(f"\n[G1-new] FAISS HNSW — true batch nq={nq} (indexed={indexed_count})…")
        r = run_group("faiss", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g1_faiss_new",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G1-new"
            results.append(r)

    # --- G2: Knowhere HNSW via CGO ---
    if not args.skip_knowhere and not args.new_raw_only:
        print(f"\n[G2-old] Knowhere HNSW — repeated single-query batch (indexed={indexed_count})…")
        r = run_group("knowhere-single", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g2_knowhere_old",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G2-old"
            results.append(r)

    if not args.skip_knowhere and not args.old_only:
        print(f"\n[G2-new] Knowhere HNSW — OpenMP batch + plugin (indexed={indexed_count})…")
        r = run_group("knowhere-build", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g2_knowhere_new",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G2-new"
            results.append(r)

    if not args.skip_knowhere and not args.old_only:
        print(f"\n[G2-raw] Knowhere HNSW — standard batch (no plugin) (indexed={indexed_count})…")
        r = run_group("knowhere-raw", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g2_knowhere_raw",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G2-raw"
            results.append(r)

    # --- G3: Plasmod Bridge ---
    if not args.skip_cgo and not args.new_raw_only:
        print(f"\n[G3-old] Plasmod Bridge — repeated single-query batch (indexed={indexed_count})…")
        r = run_group("vector-only", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g3_plasmod_old",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G3-old"
            results.append(r)

    if not args.skip_cgo and not args.old_only:
        print(f"\n[G3-new] Plasmod Bridge — OpenMP batch + plugin (indexed={indexed_count})…")
        # Reuse same segment for G2/G3 since it's the same library
        r = run_group("vector-only", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g3_plasmod_new",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G3-new"
            results.append(r)

    if not args.skip_cgo and not args.old_only:
        print(f"\n[G3-raw] Plasmod Bridge — standard batch (no plugin) (indexed={indexed_count})…")
        r = run_group("vector-only-raw", indexed_ds, query_ds,
                      indexed_count, nq, topk, "bench.g3_plasmod_raw",
                      groundtruth=groundtruth)
        if r:
            r["label"] = "G3-raw"
            results.append(r)

    # --- G4: HTTP E2E ---
    if not args.skip_http and not args.new_raw_only:
        print(f"\n[G4-old] Plasmod HTTP E2E — repeated single-query batch (indexed={indexed_count})…")
        r = run_group("http-query", indexed_ds, query_ds,
                      indexed_count, nq, topk, WARM_SEGMENT_ID,
                      server_url=PLASMOD_URL, groundtruth=groundtruth)
        if r:
            r["label"] = "G4-old"
            results.append(r)

    if not args.skip_http and not args.old_only:
        print(f"\n[G4-new] Plasmod HTTP E2E — OpenMP batch + plugin (indexed={indexed_count})…")
        r = run_group("http-query", indexed_ds, query_ds,
                      indexed_count, nq, topk, WARM_SEGMENT_ID,
                      server_url=PLASMOD_URL, groundtruth=groundtruth)
        if r:
            r["label"] = "G4-new"
            results.append(r)

    if not args.skip_http and not args.old_only:
        print(f"\n[G4-raw] Plasmod HTTP E2E — standard batch (no plugin) (indexed={indexed_count})…")
        r = run_group("http-query-raw", indexed_ds, query_ds,
                      indexed_count, nq, topk, WARM_SEGMENT_ID,
                      server_url=PLASMOD_URL, groundtruth=groundtruth)
        if r:
            r["label"] = "G4-raw"
            results.append(r)

    # ── Print summary table ──────────────────────────────────────────────────────
    print("\n" + "=" * 110)
    print(f"FOUR-GROUP BENCHMARK  (indexed={indexed_count}  queries={nq}  topk={topk})")
    print("=" * 110)
    hdr = (f"{'Label':<10} {'Group':<18} {'Layer':<22} {'Mode':<30} "
           f"{'Build_ms':>9} {'Batch_ms':>9} {'BQPS':>8} {'S-QPS':>8} {'Recall':>8}")
    print(hdr)
    print("-" * 110)

    for r in results:
        label = r.get("label", r.get("mode", "?"))
        mode_str = r.get("mode", "?")
        build_ms = r.get("build_ms", 0)
        batch_ms = r.get("batch_ms", 0)
        bqps = r.get("batch_qps", 0)
        sqps = r.get("serial_qps", 0)
        recall = r.get("recall", None)
        recall_str = f"{recall:.4f}" if recall is not None else "   N/A"

        # Determine layer/mode from mode string
        if "G1_FAISS" in mode_str:
            if "single" in mode_str:
                group = "FAISS HNSW"
                layer = "Native C++"
                mode_name = "Repeated single-query batch"
            else:
                group = "FAISS HNSW"
                layer = "Native C++"
                mode_name = "True batch"
        elif "G2_Knowhere" in mode_str:
            group = "Knowhere HNSW"
            layer = "CGO+OpenMP"
            if "single" in mode_str:
                mode_name = "Repeated single-query batch"
            elif "raw" in mode_str:
                mode_name = "Standard batch (no plugin)"
            else:
                mode_name = "OpenMP batch + plugin"
        elif "G3_Plasmod" in mode_str or "G3_VectorOnly" in mode_str:
            group = "Plasmod Bridge"
            layer = "Go→CGO"
            if "raw" in mode_str:
                mode_name = "Standard batch (no plugin)"
            else:
                mode_name = "OpenMP batch + plugin"
        elif "G4_HTTP" in mode_str:
            group = "Plasmod HTTP E2E"
            layer = "HTTP→Bridge"
            if "raw" in mode_str:
                mode_name = "Standard batch (no plugin)"
            else:
                mode_name = "OpenMP batch + plugin"
        else:
            group, layer, mode_name = mode_str, "", ""

        print(f"{label:<10} {group:<18} {layer:<22} {mode_name:<30} "
              f"{build_ms:>9.1f} {batch_ms:>9.1f} {bqps:>8.0f} {sqps:>8.0f} {recall_str:>8}")

    print()

    # Save results
    ts = time.strftime("%Y%m%d_%H%M%S")
    out = ROOT / "results" / "four_group" / f"deep_bench_{ts}.json"
    out.parent.mkdir(parents=True, exist_ok=True)
    with open(out, "w") as f:
        json.dump(results, f, indent=2)
    print(f"Results saved to {out}")


if __name__ == "__main__":
    sys.exit(main())