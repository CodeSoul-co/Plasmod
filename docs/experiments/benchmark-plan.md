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

Suggested runner for cold-tier experiments:
- `python scripts/e2e/cold_tier_benchmark_summary.py`
- vector-only baseline run: `python scripts/e2e/vector_only_baseline_runner.py --csv out/vector_only_baseline.csv`
- session/scope pressure run: `python scripts/e2e/session_scope_pressure.py --query-text "<query>" --agent-id <agent> --session-id <session> --workspace-id <ws> --csv out/session_scope_pressure.csv`
- optional JSON export: `python scripts/e2e/cold_tier_benchmark_summary.py --json out/cold_tier_benchmark.json`
- optional CSV export: `python scripts/e2e/cold_tier_benchmark_summary.py --csv out/cold_tier_benchmark.csv`
- dataset scaling run: `python scripts/e2e/cold_tier_benchmark_summary.py --include-scaling --csv out/dataset_scaling.csv`
- recall-throughput curve run: `python scripts/e2e/cold_tier_benchmark_summary.py --include-curve --csv out/recall_throughput_curve.csv`
- baseline comparison: `python scripts/e2e/baseline_comparison_report.py --full out/cold_tier_benchmark.csv --baseline <baseline.csv> --csv out/full_vs_baseline.csv`
- scope / governance checks: `python scripts/e2e/scope_governance_summary.py --csv out/scope_governance.csv`
- governance gradient checks: `python scripts/e2e/governance_gradient_summary.py --csv out/governance_gradient.csv`
