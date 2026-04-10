#!/usr/bin/env bash
set -euo pipefail

# Member A one-command verification entrypoint:
# 1) docker compose up
# 2) healthz check
# 3) fixture-driven capture output
# 4) hard S3 smoke check (write + stat via MinIO mc)
#
# Usage:
#   bash scripts/e2e/member_a_verify.sh
# Env:
#   ANDB_BASE_URL (default http://127.0.0.1:8080)
#   OUT_DIR       (default ./out/member_a_verify)
#   COMPOSE_FILES (optional, e.g. "docker-compose.yml docker-compose.override.yml")
#   COMPOSE_SERVICES (default "minio minio-init andb")
#   HEALTHZ_RETRIES / HEALTHZ_INTERVAL_SEC
#   CAPTURE_HTTP_TIMEOUT (default 60)
#   MINIO_ALIAS_URL / MINIO_ACCESS_KEY / MINIO_SECRET_KEY

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"
source "${REPO_ROOT}/scripts/e2e/member_a_common.sh"
ma_enable_failure_diagnostics "member-a-verify"

ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/out/member_a_verify}"
COMPOSE_SERVICES="${COMPOSE_SERVICES:-minio minio-init andb}"
HEALTHZ_RETRIES="${HEALTHZ_RETRIES:-60}"
HEALTHZ_INTERVAL_SEC="${HEALTHZ_INTERVAL_SEC:-2}"
CAPTURE_HTTP_TIMEOUT="${CAPTURE_HTTP_TIMEOUT:-60}"
MINIO_ALIAS_URL="${MINIO_ALIAS_URL:-http://minio:9000}"
MINIO_ACCESS_KEY="${MINIO_ACCESS_KEY:-minioadmin}"
MINIO_SECRET_KEY="${MINIO_SECRET_KEY:-minioadmin}"
S3_BUCKET="${S3_BUCKET:-andb-integration}"
S3_PREFIX="${S3_PREFIX:-andb/member_a_verify}"

echo "[member-a-verify] start docker compose stack..."
ma_compose up -d ${COMPOSE_SERVICES}

echo "[member-a-verify] wait /healthz..."
ma_wait_healthz "${ANDB_BASE_URL}" "${HEALTHZ_RETRIES}" "${HEALTHZ_INTERVAL_SEC}" "member-a-verify"

echo "[member-a-verify] run fixture capture..."
ANDB_BASE_URL="${ANDB_BASE_URL}" python3 scripts/e2e/member_a_capture.py \
  --http-timeout "${CAPTURE_HTTP_TIMEOUT}" \
  --out-dir "${OUT_DIR}"

echo "[member-a-verify] outputs:"
ls -la "${OUT_DIR}"

echo "[member-a-verify] hard-check MinIO write/read..."
PROBE_KEY="${S3_PREFIX%/}/member_a_verify_probe_$(date +%s).json"
ma_compose run --rm --entrypoint /bin/sh minio-init -lc "
  set -e
  mc alias set local ${MINIO_ALIAS_URL} ${MINIO_ACCESS_KEY} ${MINIO_SECRET_KEY} >/dev/null
  mc mb local/${S3_BUCKET} >/dev/null 2>&1 || true
  printf '{\"ok\":true,\"source\":\"member_a_verify\"}\n' | mc pipe local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
  mc stat local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
"
echo "[member-a-verify] S3 probe object: s3://${S3_BUCKET}/${PROBE_KEY}"
echo "[member-a-verify] done."
