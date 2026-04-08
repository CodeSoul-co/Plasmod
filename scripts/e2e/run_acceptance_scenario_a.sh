#!/usr/bin/env bash
set -euo pipefail

# Member A 验收方案 A（Linux/macOS）：
# Docker 运行 MinIO + andb，随后执行 fixture 驱动的 ingest/query 验证并导出结果。
#
# 用法：
#   bash scripts/e2e/run_acceptance_scenario_a.sh
# 可选环境变量：
#   ANDB_BASE_URL（默认 http://127.0.0.1:8080）
#   OUT_DIR（默认 ./out/member_a_fullstack_verify）
#   S3_BUCKET（默认 andb-integration）
#   S3_PREFIX（默认 andb/acceptance）
#   COMPOSE_FILES（可选 compose 文件列表）
#   COMPOSE_SERVICES（默认 "minio minio-init andb"）
#   HEALTHZ_RETRIES / HEALTHZ_INTERVAL_SEC
#   CAPTURE_HTTP_TIMEOUT（默认 60）
#   MINIO_ALIAS_URL / MINIO_ACCESS_KEY / MINIO_SECRET_KEY

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"
source "${REPO_ROOT}/scripts/e2e/member_a_common.sh"
ma_enable_failure_diagnostics "member-a"

ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/out/member_a_fullstack_verify}"
COMPOSE_SERVICES="${COMPOSE_SERVICES:-minio minio-init andb}"
HEALTHZ_RETRIES="${HEALTHZ_RETRIES:-60}"
HEALTHZ_INTERVAL_SEC="${HEALTHZ_INTERVAL_SEC:-2}"
CAPTURE_HTTP_TIMEOUT="${CAPTURE_HTTP_TIMEOUT:-60}"
MINIO_ALIAS_URL="${MINIO_ALIAS_URL:-http://minio:9000}"
MINIO_ACCESS_KEY="${MINIO_ACCESS_KEY:-minioadmin}"
MINIO_SECRET_KEY="${MINIO_SECRET_KEY:-minioadmin}"
S3_BUCKET="${S3_BUCKET:-andb-integration}"
S3_PREFIX="${S3_PREFIX:-andb/acceptance}"

echo "[member-a] repo: ${REPO_ROOT}"
echo "[member-a] start docker compose stack..."
ma_compose up -d ${COMPOSE_SERVICES}

echo "[member-a] wait /healthz..."
ma_wait_healthz "${ANDB_BASE_URL}" "${HEALTHZ_RETRIES}" "${HEALTHZ_INTERVAL_SEC}" "member-a"

echo "[member-a] run fixture capture..."
ANDB_BASE_URL="${ANDB_BASE_URL}" python3 scripts/e2e/member_a_capture.py \
  --http-timeout "${CAPTURE_HTTP_TIMEOUT}" \
  --out-dir "${OUT_DIR}"

echo "[member-a] outputs:"
ls -la "${OUT_DIR}"

echo "[member-a] hard-check MinIO write/read..."
PROBE_KEY="${S3_PREFIX%/}/acceptance_probe_$(date +%s).json"
ma_compose run --rm --entrypoint /bin/sh minio-init -lc "
  set -e
  mc alias set local ${MINIO_ALIAS_URL} ${MINIO_ACCESS_KEY} ${MINIO_SECRET_KEY} >/dev/null
  mc mb local/${S3_BUCKET} >/dev/null 2>&1 || true
  printf '{\"ok\":true,\"source\":\"run_acceptance_scenario_a\"}\n' | mc pipe local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
  mc stat local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
"
echo "[member-a] S3 probe object: s3://${S3_BUCKET}/${PROBE_KEY}"
echo "[member-a] done."
