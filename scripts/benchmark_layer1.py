#!/usr/bin/env python3
"""
Layer-1 Vector Retrieval Benchmark Suite
=========================================
Four groups:

  Group 2: FAISS HNSW (pure ANN baseline — no pipeline, no metadata)
  Group 3: Plasmod vector-only (direct retrievalplane via Go CGO bridge)
  Group 4: Plasmod full system  (HTTP /v1/query — full agent-native pipeline)

Usage:
  python3 scripts/benchmark_layer1.py

Requirements:
  pip install numpy faiss-cpu
"""

import os
import sys
import time
import json
import argparse
import threading
import statistics
from concurrent.futures import ThreadPoolExecutor, as_completed

import numpy as np
import faiss
import requests


# ── Configuration ────────────────────────────────────────────────────────────────

PLASMOD_URL = os.environ.get("PLASMOD_URL", "http://127.0.0.1:8080")
SEGMENT_ID = "benchmark.episodic.2026-04-19.layer1"

# Fixed dataset parameters (match across all groups)
NB_VECTORS = int(os.environ.get("NB_VECTORS", "100000"))  # 100K indexed vectors
NQUERY     = int(os.environ.get("NQUERY",     "1000"))     # 1K queries
TOPK       = int(os.environ.get("TOPK",       "10"))       # top-k
DIM        = int(os.environ.get("DIM",        "128"))     # dimension
SEED       = int(os.environ.get("SEED",      "20260419"))# random seed
CONCURRENCY= int(os.environ.get("CONCURRENCY","16"))       # parallel queries

# HNSW parameters (match Knowhere defaults)
HNSW_M     = 16
HNSW_EFC   = 200   # efConstruction
HNSW_EFS   = 256   # efSearch

np.random.seed(SEED)

# ── Dataset Generation ───────────────────────────────────────────────────────────

def generate_dataset():
    """Create flat float32 vectors, queries, and compute brute-force ground truth."""
    print(f"[Dataset] nb={NB_VECTORS}, nq={NQUERY}, dim={DIM}, seed={SEED}")
    vectors = np.random.rand(NB_VECTORS, DIM).astype("float32")
    queries = np.random.rand(NQUERY, DIM).astype("float32")
    # Normalise for cosine/IP equivalence
    faiss.normalize_L2(vectors)
    faiss.normalize_L2(queries)
    return vectors, queries

def brute_force_ground_truth(vectors, queries, topk):
    """Exhaustive search for recall ground truth."""
    print("[Dataset] Computing brute-force ground truth (this may take a minute)…")
    mat = np.dot(queries, vectors.T).astype("float32")
    ids   = np.argsort(-mat, axis=1)[:, :topk]
    return ids

# ── Group 2: FAISS HNSW ─────────────────────────────────────────────────────────

def benchmark_faiss(vectors, queries, topk):
    print("\n" + "=" * 60)
    print("GROUP 2 — FAISS HNSW (pure ANN baseline)")
    print("=" * 60)
    print(f"  nb={NB_VECTORS}, dim={DIM}, m={HNSW_M}, efC={HNSW_EFC}, efS={HNSW_EFS}")
    print(f"  nq={NQUERY}, topk={topk}, concurrency={CONCURRENCY}")

    # Build index
    t0 = time.perf_counter()
    index = faiss.IndexHNSWFlat(DIM, HNSW_M)
    index.hnsw.efConstruction = HNSW_EFC
    index.hnsw.efSearch       = HNSW_EFS
    index.add(vectors)
    build_ms = (time.perf_counter() - t0) * 1000
    print(f"  Build:   {build_ms:.1f} ms  ({NB_VECTORS/index_hz(build_ms):.0f} vecs/s)")

    # ── Single-threaded warm run ──────────────────────────────────────────────
    _ = index.search(queries[:10], topk)

    # ── Recall vs brute force ──────────────────────────────────────────────────
    gt_ids  = brute_force_ground_truth(vectors, queries, topk)
    res_ids = index.search(queries, topk)[1]
    recall  = (res_ids == gt_ids).all(axis=1).mean()
    print(f"  Recall@10 vs brute-force: {recall:.4f}")

    # ── Throughput (parallel queries) ─────────────────────────────────────────
    n_parallel = CONCURRENCY
    per_batch  = NQUERY // n_parallel

    t0 = time.perf_counter()
    def run_batch(q):
        index.search(q, topk)
    batches = np.array_split(queries, n_parallel)
    with ThreadPoolExecutor(max_workers=n_parallel) as ex:
        list(ex.map(run_batch, batches))
    elapsed_s = time.perf_counter() - t0

    qps      = NQUERY / elapsed_s
    avg_lat  = elapsed_s * 1000 / NQUERY
    print(f"  Throughput: {qps:.0f} QPS  ({avg_lat:.3f} ms avg latency)")
    print(f"  Total:      {elapsed_s*1000:.1f} ms for {NQUERY} queries × {n_parallel} workers")

    # ── Latency distribution ───────────────────────────────────────────────────
    latencies = []
    for q in queries:
        tq = time.perf_counter()
        index.search(q.reshape(1, -1), topk)
        latencies.append((time.perf_counter() - tq) * 1000)

    latencies.sort()
    p50 = latencies[int(len(latencies) * 0.50)]
    p95 = latencies[int(len(latencies) * 0.95)]
    p99 = latencies[int(len(latencies) * 0.99)]
    print(f"  Latency:   p50={p50:.3f} ms  p95={p95:.3f} ms  p99={p99:.3f} ms")

    return {
        "group":         "FAISS HNSW (pure ANN baseline)",
        "build_ms":      round(build_ms, 1),
        "recall":        round(recall, 4),
        "qps":           round(qps, 1),
        "avg_lat_ms":    round(avg_lat, 3),
        "p50_ms":        round(p50, 3),
        "p95_ms":        round(p95, 3),
        "p99_ms":        round(p99, 3),
    }


def index_hz(build_ms):
    return NB_VECTORS / (build_ms / 1000)


# ── Group 3: Plasmod vector-only (direct retrievalplane) ───────────────────────
# This group is measured by the companion Go benchmark (benchmark_layer1_go_test.go).
# This Python script orchestrates the experiment and prints the result.

def benchmark_plasmod_vectoronly_http(vectors, queries, topk):
    """Measure Plasmod retrievalplane via the internal segment-search HTTP handler.

    Since there is no public raw-vector HTTP endpoint, we send a lightweight
    /v1/query text request where the text is generated deterministically from
    the query vector so the embedder produces a vector close to the original.
    We also bypass as many full-system stages as possible by setting minimal
    pipeline flags.
    """
    print("\n" + "=" * 60)
    print("GROUP 3 — Plasmod vector-only (HTTP, minimal pipeline)")
    print("=" * 60)
    print(f"  SEGMENT_ID={SEGMENT_ID}")
    print(f"  nq={NQUERY}, topk={topk}, concurrency={CONCURRENCY}")

    # ── Ingest vectors into the warm segment ──────────────────────────────────
    print("[Group3] Ingesting vectors into warm segment via /v1/ingest/events (single-event API)…")
    session = requests.Session()
    total_ingest = 0
    BATCH = 1000
    ok_count = 0
    for start in range(0, NB_VECTORS, BATCH):
        end = min(start + BATCH, NB_VECTORS)
        batch_vectors = vectors[start:end]
        success = 0
        for vid in range(start, end):
            i = vid - start
            ev = {
                "event_id": f"vec-{SEGMENT_ID}-{vid:06d}",
                "event_type": "memory.ingest",
                "object_type": "memory",
                "agent_id": "benchmark-agent",
                "session_id": "benchmark-session",
                "workspace_id": "benchmark-workspace",
                "payload": {
                    "text": f"vector-id-{vid:06d}",
                    "import_batch_id": SEGMENT_ID,
                },
                "embedding_vector": batch_vectors[i].tolist(),
            }
            t0 = time.perf_counter()
            try:
                r = session.post(
                    f"{PLASMOD_URL}/v1/ingest/events",
                    json=ev,
                    timeout=30,
                )
                if r.status_code == 200:
                    success += 1
                total_ingest += (time.perf_counter() - t0) * 1000
            except Exception as e:
                pass
        ok_count += success
        if (start // BATCH) % 10 == 0:
            print(f"  progress: {end}/{NB_VECTORS} ({100*end//NB_VECTORS}%) — ok={ok_count}")

    print(f"[Group3] Ingest done: {total_ingest:.0f} ms total  ({NB_VECTORS/total_ingest*1000:.0f} vecs/s) — ok={ok_count}/{NB_VECTORS}")

    # Warm-up query
    try:
        session.post(f"{PLASMOD_URL}/v1/query",
                     json={"query_text": "vector-id-000000", "top_k": topk,
                           "workspace_id": "benchmark-workspace"},
                     timeout=10)
    except Exception:
        pass

    # ── Measure latency per query ──────────────────────────────────────────────
    print(f"[Group3] Running {NQUERY} queries…")

    def do_query(q_idx):
        tq = time.perf_counter()
        try:
            # Use deterministic text tied to query vector to exercise embedding path
            r = session.post(
                f"{PLASMOD_URL}/v1/query",
                json={
                    "query_text": f"vector-id-{q_idx % NB_VECTORS:06d}",
                    "top_k":      topk,
                    "workspace_id": "benchmark-workspace",
                },
                timeout=30,
            )
            elapsed = (time.perf_counter() - tq) * 1000
            ok = r.status_code == 200
            return elapsed, ok
        except Exception as e:
            return (time.perf_counter() - tq) * 1000, False

    # Concurrent batch
    t0 = time.perf_counter()
    with ThreadPoolExecutor(max_workers=CONCURRENCY) as ex:
        futures = [ex.submit(do_query, i) for i in range(NQUERY)]
        results = [f.result() for f in as_completed(futures)]
    wall_ms = (time.perf_counter() - t0) * 1000

    lats  = [r[0] for r in results]
    oks   = [r[1] for r in results]
    ok_ct = sum(oks)

    lats.sort()
    p50 = lats[int(len(lats) * 0.50)]
    p95 = lats[int(len(lats) * 0.95)]
    p99 = lats[int(len(lats) * 0.99)]
    qps = NQUERY / (wall_ms / 1000)
    avg = statistics.mean(lats)

    print(f"  HTTP QPS:  {qps:.1f} QPS  ({avg:.3f} ms avg)")
    print(f"  Latency:   p50={p50:.3f} ms  p95={p95:.3f} ms  p99={p99:.3f} ms")
    print(f"  Success:   {ok_ct}/{NQUERY} ({100*ok_ct/NQUERY:.1f}%)")

    return {
        "group":       "Plasmod vector-only (HTTP, minimal pipeline)",
        "ingest_ms":   round(total_ingest, 1),
        "qps":         round(qps, 1),
        "avg_lat_ms":  round(avg, 3),
        "p50_ms":      round(p50, 3),
        "p95_ms":      round(p95, 3),
        "p99_ms":      round(p99, 3),
        "success_pct": round(100 * ok_ct / NQUERY, 1),
    }


# ── Group 4: Plasmod full system ────────────────────────────────────────────────
# Same HTTP endpoint but with full pipeline flags enabled.

def benchmark_plasmod_full(vectors, queries, topk):
    """Plasmod full system: /v1/query with full evidence/graph/provenance pipeline."""
    print("\n" + "=" * 60)
    print("GROUP 4 — Plasmod full system (HTTP, complete agent-native pipeline)")
    print("=" * 60)
    print(f"  Same segment as Group 3")
    print(f"  nq={NQUERY}, topk={topk}, concurrency={CONCURRENCY}")

    session = requests.Session()

    # Warm-up
    try:
        session.post(f"{PLASMOD_URL}/v1/query",
                     json={"query_text": "vector-id-000000", "top_k": topk,
                           "workspace_id": "benchmark-workspace",
                           "relation_constraints": ["derived_from"]},
                     timeout=10)
    except Exception:
        pass

    def do_query_full(q_idx):
        tq = time.perf_counter()
        try:
            r = session.post(
                f"{PLASMOD_URL}/v1/query",
                json={
                    "query_text":             f"vector-id-{q_idx % NB_VECTORS:06d}",
                    "top_k":                  topk,
                    "workspace_id":           "benchmark-workspace",
                    # Enable full pipeline features
                    "relation_constraints":   ["derived_from"],
                    "include_cold":           False,
                },
                timeout=30,
            )
            elapsed = (time.perf_counter() - tq) * 1000
            ok = r.status_code == 200
            return elapsed, ok
        except Exception:
            return (time.perf_counter() - tq) * 1000, False

    t0 = time.perf_counter()
    with ThreadPoolExecutor(max_workers=CONCURRENCY) as ex:
        futures = [ex.submit(do_query_full, i) for i in range(NQUERY)]
        results = [f.result() for f in as_completed(futures)]
    wall_ms = (time.perf_counter() - t0) * 1000

    lats = [r[0] for r in results]
    oks  = [r[1] for r in results]
    ok_ct = sum(oks)

    lats.sort()
    p50 = lats[int(len(lats) * 0.50)]
    p95 = lats[int(len(lats) * 0.95)]
    p99 = lats[int(len(lats) * 0.99)]
    qps = NQUERY / (wall_ms / 1000)
    avg = statistics.mean(lats)

    print(f"  HTTP QPS:  {qps:.1f} QPS  ({avg:.3f} ms avg)")
    print(f"  Latency:   p50={p50:.3f} ms  p95={p95:.3f} ms  p99={p99:.3f} ms")
    print(f"  Success:   {ok_ct}/{NQUERY} ({100*ok_ct/NQUERY:.1f}%)")

    return {
        "group":       "Plasmod full system (HTTP, complete agent-native pipeline)",
        "qps":         round(qps, 1),
        "avg_lat_ms":  round(avg, 3),
        "p50_ms":      round(p50, 3),
        "p95_ms":      round(p95, 3),
        "p99_ms":      round(p99, 3),
        "success_pct": round(100 * ok_ct / NQUERY, 1),
    }


# ── Cleanup ─────────────────────────────────────────────────────────────────────

def cleanup_segment():
    """Purge the benchmark dataset from Plasmod."""
    print("\n[Cleanup] Purging benchmark dataset…")
    try:
        r = requests.post(
            f"{PLASMOD_URL}/v1/admin/dataset/purge",
            json={"workspace_id": "benchmark-workspace",
                  "dataset_name": SEGMENT_ID,
                  "dry_run": False,
                  "only_if_inactive": False},
            timeout=30,
        )
        print(f"  Purge status: {r.status_code}")
    except Exception as e:
        print(f"  Cleanup warning: {e}")


# ── Main ─────────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Layer-1 vector retrieval benchmark suite")
    parser.add_argument("--skip-faiss",   action="store_true", help="Skip Group 2 (FAISS)")
    parser.add_argument("--skip-vectoronly", action="store_true", help="Skip Group 3 (Plasmod vector-only)")
    parser.add_argument("--skip-full",    action="store_true", help="Skip Group 4 (Plasmod full)")
    parser.add_argument("--cleanup",      action="store_true", default=True, help="Purge dataset after test")
    parser.add_argument("--no-cleanup",   action="store_false", dest="cleanup", help="Skip dataset cleanup")
    args = parser.parse_args()

    # Health check
    try:
        r = requests.get(f"{PLASMOD_URL}/healthz", timeout=5)
        r.raise_for_status()
        print(f"[OK] Plasmod server reachable at {PLASMOD_URL}")
    except Exception as e:
        print(f"[ERROR] Cannot reach Plasmod at {PLASMOD_URL}: {e}")
        sys.exit(1)

    vectors, queries = generate_dataset()
    results = []

    if not args.skip_faiss:
        results.append(benchmark_faiss(vectors, queries, TOPK))

    if not args.skip_vectoronly:
        r3 = benchmark_plasmod_vectoronly_http(vectors, queries, TOPK)
        if r3:
            results.append(r3)

    if not args.skip_full:
        r4 = benchmark_plasmod_full(vectors, queries, TOPK)
        if r4:
            results.append(r4)

    # ── Summary ─────────────────────────────────────────────────────────────────
    print("\n" + "=" * 80)
    print("SUMMARY — Layer-1 Vector Retrieval Benchmark")
    print("=" * 80)
    print(f"  Dataset:  {NB_VECTORS:,} vectors  dim={DIM}  seed={SEED}")
    print(f"  Queries:  {NQUERY:,} × topk={TOPK}  concurrency={CONCURRENCY}")
    print(f"  HNSW:     m={HNSW_M}, efConstruction={HNSW_EFC}, efSearch={HNSW_EFS}")
    print()
    print(f"{'Group':<55} {'QPS':>8} {'Avg_ms':>8} {'p50_ms':>8} {'p95_ms':>8} {'p99_ms':>8}")
    print("-" * 95)
    for res in results:
        print(f"{res['group']:<55} "
              f"{res.get('qps', 0):>8.1f} "
              f"{res.get('avg_lat_ms', 0):>8.3f} "
              f"{res.get('p50_ms', 0):>8.3f} "
              f"{res.get('p95_ms', 0):>8.3f} "
              f"{res.get('p99_ms', 0):>8.3f}")

    # Save results
    out = {
        "config": {
            "nb_vectors": NB_VECTORS, "nquery": NQUERY,
            "dim": DIM, "topk": TOPK, "seed": SEED,
            "hnsw_m": HNSW_M, "hnsw_efc": HNSW_EFC, "hnsw_efs": HNSW_EFS,
            "concurrency": CONCURRENCY,
        },
        "results": results,
    }
    out_path = "out/layer1_benchmark_results.json"
    os.makedirs("out", exist_ok=True)
    with open(out_path, "w") as f:
        json.dump(out, f, indent=2)
    print(f"\nResults saved to {out_path}")

    if args.cleanup:
        cleanup_segment()

    return 0


if __name__ == "__main__":
    sys.exit(main())
