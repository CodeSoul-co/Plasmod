# Benchmark Plan

Primary metrics:
- P50/P95 latency
- Recall@K
- Evidence completeness

Cold-tier experiment coverage:
- include_cold correctness on archived datasets
- cold search mode reporting: hnsw / vector / lexical
- cold candidate count and fallback visibility
- 10K archived-memory latency targets
- EvidenceCache cold_hits / cold_misses

Recommended benchmark table columns:
- dataset_name
- dataset_size
- top_k
- include_cold
- cold_search_mode
- cold_used_fallback
- recall_at_k
- p50_ms
- p95_ms
- p99_ms
- cold_candidate_count
- evidence_cold_hits
- evidence_cold_misses

Suggested runner for Member C cold-tier experiments:
- `python scripts/e2e/member_c_benchmark_summary.py`
- optional JSON export: `python scripts/e2e/member_c_benchmark_summary.py --json out/member_c_benchmark.json`
- optional CSV export: `python scripts/e2e/member_c_benchmark_summary.py --csv out/member_c_benchmark.csv`
- dataset scaling run: `python scripts/e2e/member_c_benchmark_summary.py --include-scaling --csv out/member_c_scaling.csv`
- recall-throughput curve run: `python scripts/e2e/member_c_benchmark_summary.py --include-curve --csv out/member_c_curve.csv`
- baseline comparison: `python scripts/e2e/member_c_baseline_compare.py --full out/member_c_benchmark.csv --baseline <baseline.csv> --csv out/member_c_vs_baseline.csv`
- scope / governance checks: `python scripts/e2e/member_c_scope_governance_summary.py --csv out/member_c_scope_governance.csv`
