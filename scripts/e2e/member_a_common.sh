#!/usr/bin/env bash
set -euo pipefail

ma_compose() {
  local files=()
  if [[ -n "${COMPOSE_FILES:-}" ]]; then
    # shellcheck disable=SC2206
    local raw=(${COMPOSE_FILES})
    local f
    for f in "${raw[@]}"; do
      files+=(-f "${f}")
    done
  fi
  docker compose "${files[@]}" "$@"
}

ma_wait_healthz() {
  local base_url="$1"
  local retries="${2:-60}"
  local interval="${3:-2}"
  local tag="${4:-member-a}"
  local i
  for i in $(seq 1 "${retries}"); do
    if curl -fsS "${base_url%/}/healthz" >/dev/null; then
      echo "[${tag}] healthz OK"
      return 0
    fi
    sleep "${interval}"
  done
  echo "[${tag}] ERROR: /healthz not ready after timeout (${retries} x ${interval}s)"
  return 1
}

ma_enable_failure_diagnostics() {
  local tag="${1:-member-a}"
  trap 'ma_dump_failure_diagnostics "'"${tag}"'" "$?"' ERR
}

ma_dump_failure_diagnostics() {
  local tag="$1"
  local code="$2"
  local services="${DIAG_SERVICES:-andb minio minio-init}"
  echo "[${tag}] ERROR detected (exit=${code}). collecting diagnostics..."
  ma_compose ps || true
  ma_compose logs --no-color --tail "${DIAG_LOG_TAIL:-200}" ${services} || true
}
