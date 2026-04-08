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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/out/member_a_fullstack_verify}"
S3_BUCKET="${S3_BUCKET:-andb-integration}"
S3_PREFIX="${S3_PREFIX:-andb/acceptance}"

echo "[member-a] repo: ${REPO_ROOT}"
echo "[member-a] start docker compose stack..."
docker compose up -d minio minio-init andb

echo "[member-a] wait /healthz..."
for _ in $(seq 1 60); do
  if curl -fsS "${ANDB_BASE_URL}/healthz" >/dev/null; then
    echo "[member-a] healthz OK"
    break
  fi
  sleep 2
done

if ! curl -fsS "${ANDB_BASE_URL}/healthz" >/dev/null; then
  echo "[member-a] ERROR: /healthz not ready after timeout"
  exit 1
fi

echo "[member-a] run fixture capture..."
ANDB_BASE_URL="${ANDB_BASE_URL}" python3 scripts/e2e/member_a_capture.py --out-dir "${OUT_DIR}"

echo "[member-a] outputs:"
ls -la "${OUT_DIR}"

echo "[member-a] hard-check MinIO write/read..."
PROBE_KEY="${S3_PREFIX%/}/acceptance_probe_$(date +%s).json"
docker compose run --rm --entrypoint /bin/sh minio-init -lc "
  set -e
  mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null
  mc mb local/${S3_BUCKET} >/dev/null 2>&1 || true
  printf '{\"ok\":true,\"source\":\"run_acceptance_scenario_a\"}\n' | mc pipe local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
  mc stat local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
"
echo "[member-a] S3 probe object: s3://${S3_BUCKET}/${PROBE_KEY}"
echo "[member-a] done."
