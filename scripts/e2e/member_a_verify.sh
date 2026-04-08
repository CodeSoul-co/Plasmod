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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

ANDB_BASE_URL="${ANDB_BASE_URL:-http://127.0.0.1:8080}"
OUT_DIR="${OUT_DIR:-${REPO_ROOT}/out/member_a_verify}"
S3_BUCKET="${S3_BUCKET:-andb-integration}"
S3_PREFIX="${S3_PREFIX:-andb/member_a_verify}"

echo "[member-a-verify] start docker compose stack..."
docker compose up -d minio minio-init andb

echo "[member-a-verify] wait /healthz..."
for _ in $(seq 1 60); do
  if curl -fsS "${ANDB_BASE_URL}/healthz" >/dev/null; then
    echo "[member-a-verify] healthz OK"
    break
  fi
  sleep 2
done

if ! curl -fsS "${ANDB_BASE_URL}/healthz" >/dev/null; then
  echo "[member-a-verify] ERROR: /healthz not ready after timeout"
  exit 1
fi

echo "[member-a-verify] run fixture capture..."
ANDB_BASE_URL="${ANDB_BASE_URL}" python3 scripts/e2e/member_a_capture.py --out-dir "${OUT_DIR}"

echo "[member-a-verify] outputs:"
ls -la "${OUT_DIR}"

echo "[member-a-verify] hard-check MinIO write/read..."
PROBE_KEY="${S3_PREFIX%/}/member_a_verify_probe_$(date +%s).json"
docker compose run --rm --entrypoint /bin/sh minio-init -lc "
  set -e
  mc alias set local http://minio:9000 minioadmin minioadmin >/dev/null
  mc mb local/${S3_BUCKET} >/dev/null 2>&1 || true
  printf '{\"ok\":true,\"source\":\"member_a_verify\"}\n' | mc pipe local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
  mc stat local/${S3_BUCKET}/${PROBE_KEY} >/dev/null
"
echo "[member-a-verify] S3 probe object: s3://${S3_BUCKET}/${PROBE_KEY}"
echo "[member-a-verify] done."
