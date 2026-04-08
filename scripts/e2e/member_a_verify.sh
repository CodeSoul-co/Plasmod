#!/usr/bin/env bash
set -euo pipefail

# Member A one-command verification entrypoint:
# 1) docker compose up
# 2) healthz check
# 3) fixture-driven capture output
# 4) optional MinIO listing hint
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

echo "[member-a-verify] optional S3 listing:"
echo "  docker compose exec minio-init mc ls local/andb-integration"
echo "[member-a-verify] done."
