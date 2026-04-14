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
MEMBER_A_RUN_LAYER2="${MEMBER_A_RUN_LAYER2:-true}"
MEMBER_A_RUN_DESTRUCTIVE="${MEMBER_A_RUN_DESTRUCTIVE:-false}"

ADMIN_HEADER=()
if [[ -n "${PLASMOD_ADMIN_API_KEY:-}" ]]; then
  ADMIN_HEADER=(-H "X-Admin-Key: ${PLASMOD_ADMIN_API_KEY}")
elif [[ -n "${ANDB_ADMIN_API_KEY:-}" ]]; then
  ADMIN_HEADER=(-H "X-Admin-Key: ${ANDB_ADMIN_API_KEY}")
fi

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

echo "[member-a-verify] admin API smoke checks..."
curl -fsS "${ADMIN_HEADER[@]}" "${ANDB_BASE_URL%/}/v1/admin/storage" >/dev/null
curl -fsS "${ADMIN_HEADER[@]}" "${ANDB_BASE_URL%/}/v1/admin/topology" >/dev/null
curl -fsS "${ADMIN_HEADER[@]}" "${ANDB_BASE_URL%/}/v1/admin/consistency-mode" >/dev/null
curl -fsS -X POST "${ANDB_BASE_URL%/}/v1/admin/consistency-mode" \
  -H 'Content-Type: application/json' "${ADMIN_HEADER[@]}" \
  -d '{"mode":"strict_visible"}' >/dev/null
curl -fsS -X POST "${ANDB_BASE_URL%/}/v1/admin/replay" \
  -H 'Content-Type: application/json' "${ADMIN_HEADER[@]}" \
  -d '{"from_lsn":0,"limit":20,"dry_run":true}' >/dev/null
curl -fsS -X POST "${ANDB_BASE_URL%/}/v1/admin/dataset/delete" \
  -H 'Content-Type: application/json' "${ADMIN_HEADER[@]}" \
  -d '{"workspace_id":"ws_member_a","dataset_name":"exp_member_a","dry_run":true}' >/dev/null
curl -fsS -X POST "${ANDB_BASE_URL%/}/v1/admin/dataset/purge" \
  -H 'Content-Type: application/json' "${ADMIN_HEADER[@]}" \
  -d '{"workspace_id":"ws_member_a","dataset_name":"exp_member_a","only_if_inactive":true,"dry_run":true}' >/dev/null
curl -fsS -X POST "${ANDB_BASE_URL%/}/v1/admin/s3/cold-purge" \
  -H 'Content-Type: application/json' "${ADMIN_HEADER[@]}" \
  -d '{"confirm":"purge_cold_tier","dry_run":true}' >/dev/null

MEM_ID="$(curl -fsS "${ANDB_BASE_URL%/}/v1/memory" | python3 -c 'import sys,json; d=json.load(sys.stdin); m=d[0] if isinstance(d,list) and d else {}; print(m.get("memory_id","") if isinstance(m,dict) else "")')"
if [[ -n "${MEM_ID}" ]]; then
  echo "[member-a-verify] rollback dry-run probe memory_id=${MEM_ID}"
  curl -fsS -X POST "${ANDB_BASE_URL%/}/v1/admin/rollback" \
    -H 'Content-Type: application/json' "${ADMIN_HEADER[@]}" \
    -d "{\"memory_id\":\"${MEM_ID}\",\"action\":\"deactivate\",\"dry_run\":true,\"reason\":\"member_a_verify\"}" >/dev/null
fi

if [[ "${MEMBER_A_RUN_LAYER2}" == "true" ]]; then
  echo "[member-a-verify] layer2 quick run (exp2 baseline repeat=1)..."
  python3 scripts/e2e/layer2_exp26.py \
    --base-url "${ANDB_BASE_URL%/}" \
    exp2 \
    --baseline-ladder-step 4 \
    --baseline-repeats 1 \
    --step-seconds 5 >/dev/null
fi

if [[ "${MEMBER_A_RUN_DESTRUCTIVE}" == "true" ]]; then
  echo "[member-a-verify] destructive check enabled: admin wipe"
  curl -fsS -X POST "${ANDB_BASE_URL%/}/v1/admin/data/wipe" \
    -H 'Content-Type: application/json' "${ADMIN_HEADER[@]}" \
    -d '{"confirm":"delete_all_data"}' >/dev/null
fi

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
