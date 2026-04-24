#!/usr/bin/env bash
set -euo pipefail

set -a
[ -f .env ] && source .env
set +a

: "${PLASMOD_ZEP_BASE_URL:=http://127.0.0.1:8000}"
: "${PLASMOD_ZEP_API_KEY:=yyspasswd}"
: "${ZEP_MOCK_HOST:=127.0.0.1}"
: "${ZEP_MOCK_PORT:=8000}"
: "${ZEP_MOCK_LOG_DIR:=/tmp}"

zep_pid=""
cleanup() {
  if [[ -n "${zep_pid}" ]]; then
    kill "${zep_pid}" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

if [[ "${PLASMOD_ZEP_BASE_URL}" == "http://127.0.0.1:8000" ]]; then
  if ! curl -fsS "${PLASMOD_ZEP_BASE_URL}/healthz" >/dev/null 2>&1; then
    mkdir -p "${ZEP_MOCK_LOG_DIR}"
    zep_log_file="${ZEP_MOCK_LOG_DIR}/plasmod-zep-mock-$(date +%Y%m%d-%H%M%S).log"
    ZEP_MOCK_API_KEY="${PLASMOD_ZEP_API_KEY}" \
    ZEP_MOCK_HOST="${ZEP_MOCK_HOST}" \
    ZEP_MOCK_PORT="${ZEP_MOCK_PORT}" \
      python3 scripts/e2e/zep_mock_server.py >"${zep_log_file}" 2>&1 &
    zep_pid=$!
    sleep 0.3
  fi
fi

go run ${RETRIEVAL_TAG:-} ./src/cmd/server
