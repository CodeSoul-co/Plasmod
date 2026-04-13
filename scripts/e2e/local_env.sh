#!/usr/bin/env bash
# Local E2E defaults. Source this file before running test commands:
#   source scripts/e2e/local_env.sh

export ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
export FBIN="${FBIN:-/home/yangyongsheng/database/testQuery10K.fbin}"
export DATASET="${DATASET:-testQuery10K.fbin}"
export WORKSPACE="${WORKSPACE:-w_test}"
export ANDB_CONFLICT_MERGE_SKIP_DATASET_LOADER="${ANDB_CONFLICT_MERGE_SKIP_DATASET_LOADER:-true}"
export IMPORT_BATCH_ID="${IMPORT_BATCH_ID:-batch_$(date -u +%Y%m%dT%H%M%SZ)}"

# Optional helper: call before each new ingest round to avoid batch-id reuse.
refresh_import_batch_id() {
  export IMPORT_BATCH_ID="batch_$(date -u +%Y%m%dT%H%M%SZ)"
}
