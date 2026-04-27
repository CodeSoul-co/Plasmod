#!/usr/bin/env python3
"""Four-group benchmark — all groups run as Go subprocesses for consistent measurement.

Pipeline:
  G1: plasmod_bench --mode=faiss           (FAISS HNSW via CGO)
  G2: plasmod_bench --mode=knowhere-build   (Knowhere HNSW via CGO, OpenMP parallel batch)
  G3: plasmod_bench --mode=vector-only      (GlobalSegmentRetriever.Search via CGO, OpenMP batch)
  G4: plasmod_bench --mode=http-query        (HTTP batch query)

Metrics per group:
  build_ms    — HNSW index construction time (separate)
  batch_ms    — single batch_search call (nq queries together, parallel)
  batch_qps   — nq / (batch_ms / 1000)
  serial_ms    — sum of nq individual search calls
  serial_qps   — nq / (serial_ms / 1000)
  p50/p95/p99 — percentiles of individual search latencies
  recall@K    — vs ground truth from G1 batch results

Recall setup: first n_indexed vectors are indexed, last nq vectors are queries
  (disjoint sets, no query vector appears in the index).
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
BIN_DIR = ROOT.parent.parent / "bin"
BENCH_BIN = BIN_DIR / "plasmod_bench"
PLASMOD_URL = os.environ.get("PLASMOD_URL", "http://127.0.0.1:8080")
WARM_SEGMENT_ID = "warm.four_group"

os.environ.setdefault("KMP_DUPLICATE_LIB_OK", "TRUE")


def run_group(mode: str, dataset: pathlib.Path, limit: int, nq: int,
              topk: int, segment: str, indexed_count: int,
              server_url: str = PLASMOD_URL) -> dict | None:
    """Run a benchmark group as a Go subprocess and return parsed JSON result."""
    args = [str(BENCH_BIN), f"--mode={mode}",
            f"--dataset={dataset}", f"--limit={limit}",
            f"--queries={nq}", f"--topk={topk}",
            f"--segment={segment}",
            f"--indexed-count={indexed_count}"]
    if mode == "http-query":
        args.append(f"--server-url={server_url}")
    env = dict(os.environ, KMP_DUPLICATE_LIB_OK="TRUE")
    r = subprocess.run(args, capture_output=True, text=True, timeout=300, env=env)
    if r.returncode != 0:
        print(f"  [{mode}] failed: {r.stderr[:300].strip()}")
        return None
    return json.loads(r.stdout)


def run_benchmark(
    dataset: pathlib.Path,
    limit: int,
    num_queries: int,
    topk: int,
) -> list[dict]:
    """Run all four groups. Returns list of result dicts."""
    n_indexed = limit - num_queries
    indexed_count = n_indexed

    results = []

    # G1: FAISS HNSW via CGO
    print(f"\n[G1] FAISS HNSW via CGO (indexed={indexed_count})…")
    r = run_group("faiss", dataset, limit, num_queries, topk,
                  "bench.g1_faiss", indexed_count)
    if r:
        results.append(r)

    # G2: Knowhere HNSW via CGO (OpenMP parallel batch search)
    print(f"\n[G2] Knowhere HNSW via CGO — OpenMP batch (indexed={indexed_count})…")
    r = run_group("knowhere-build", dataset, limit, num_queries, topk,
                  "bench.g2_knowhere", indexed_count)
    if r:
        results.append(r)

    # G3: GlobalSegmentRetriever.Search via CGO (same OpenMP batch path as G2)
    print(f"\n[G3] Plasmod GlobalSegmentRetriever.Search via CGO — OpenMP batch (indexed={indexed_count})…")
    r = run_group("vector-only", dataset, limit, num_queries, topk,
                  "bench.g3_plasmod", indexed_count)
    if r:
        results.append(r)

    # G4: HTTP batch query
    print(f"\n[G4] HTTP batch query (indexed={indexed_count})…")
    r = run_group("http-query", dataset, limit, num_queries, topk,
                  WARM_SEGMENT_ID, indexed_count, server_url=PLASMOD_URL)
    if r:
        results.append(r)

    return results


def recall_at_k(gt_ids: np.ndarray, got_ids: np.ndarray, topk: int) -> float:
    hits = 0
    for i in range(gt_ids.shape[0]):
        hits += len(set(gt_ids[i, :topk].tolist()) & set(got_ids[i, :topk].tolist()))
    return hits / float(gt_ids.shape[0] * topk)


def main():
    ap = argparse.ArgumentParser(description="Four-group benchmark")
    ap.add_argument("--dataset", type=pathlib.Path,
                    default=DATA_DIR / "testQuery10K.fbin")
    ap.add_argument("--limit", type=int, default=10000)
    ap.add_argument("--num-queries", type=int, default=1000)
    ap.add_argument("--topk", type=int, default=10)
    ap.add_argument("--skip-faiss", action="store_true")
    ap.add_argument("--skip-knowhere", action="store_true")
    ap.add_argument("--skip-cgo", action="store_true")
    ap.add_argument("--skip-http", action="store_true")
    args = ap.parse_args()

    n_indexed = args.limit - args.num_queries
    results = []

    # G1
    if not args.skip_faiss:
        print(f"\n[G1] FAISS HNSW via CGO (indexed={n_indexed})…")
        r = run_group("faiss", args.dataset, args.limit, args.num_queries,
                      args.topk, "bench.g1_faiss", n_indexed)
        if r:
            results.append(r)

    # G2
    if not args.skip_knowhere:
        print(f"\n[G2] Knowhere HNSW — OpenMP batch (indexed={n_indexed})…")
        r = run_group("knowhere-build", args.dataset, args.limit,
                      args.num_queries, args.topk, "bench.g2_knowhere", n_indexed)
        if r:
            results.append(r)

    # G3
    if not args.skip_cgo:
        print(f"\n[G3] Plasmod GlobalSegmentRetriever.Search — OpenMP batch (indexed={n_indexed})…")
        r = run_group("vector-only", args.dataset, args.limit,
                      args.num_queries, args.topk, "bench.g3_plasmod", n_indexed)
        if r:
            results.append(r)

    # G4
    if not args.skip_http:
        print(f"\n[G4] HTTP batch (indexed={n_indexed})…")
        r = run_group("http-query", args.dataset, args.limit,
                      args.num_queries, args.topk, WARM_SEGMENT_ID, n_indexed,
                      server_url=PLASMOD_URL)
        if r:
            results.append(r)

    # Recall vs G1 ground truth
    gt_ids = None
    for r in results:
        if r.get("mode") == "G1_FAISS" and r.get("int_ids"):
            nq = r["n_queries"]
            topk = r["topk"]
            if len(r["int_ids"]) == nq * topk:
                gt_ids = np.array(r["int_ids"], dtype="int64").reshape(nq, topk)
            break

    # Print summary table
    print("\n" + "=" * 100)
    print(f"FOUR-GROUP BENCHMARK  (indexed={n_indexed}  queries={args.num_queries}  dim={r.get('dim', '?')}  topk={args.topk})")
    print("=" * 100)
    hdr = (f"{'Group':<22} {'Build_ms':>9} {'Batch_ms':>9} {'Batch_QPS':>10} "
           f"{'Serial_QPS':>11} {'p50_ms':>9} {'Recall@K':>10}")
    print(hdr)
    print("-" * 100)
    for r in results:
        int_ids = r.get("int_ids", [])
        nq = r["n_queries"]
        topk = r["topk"]
        recall = None
        if int_ids and gt_ids is not None and len(int_ids) == nq * topk:
            got = np.array(int_ids, dtype="int64").reshape(nq, topk)
            recall = recall_at_k(gt_ids, got, topk)
        recall_str = f"{recall:.4f}" if recall is not None else "    N/A"
        print(f"{r.get('mode', '?'):<22} "
              f"{r.get('build_ms', 0):>9.1f} "
              f"{r.get('batch_ms', 0):>9.2f} "
              f"{r.get('batch_qps', 0):>10.0f} "
              f"{r.get('serial_qps', 0):>11.0f} "
              f"{r.get('p50_ms', 0):>9.4f} "
              f"{recall_str:>10}")
    print()


if __name__ == "__main__":
    sys.exit(main())
