#!/usr/bin/env bash
# Bring up the Plasmod docker-compose stack without host port conflicts.
#
# After a reboot (or when another project left MinIO/ANDB on 8080/9000/9001),
# plain `docker compose up` can fail with "port is already allocated" even if you
# already ran `compose down` in this repo — another container may still publish
# those ports.
#
# This script:
#   1) Runs `docker compose down` for this project (optional skip).
#   2) Stops any *other* running containers that publish host ports 8080, 9000, or 9001.
#   3) Runs `docker compose up` with your args (default: `-d --build`).
#
# Usage (from repo root):
#   bash scripts/e2e/plasmod_stack_up.sh
#   APP_MODE=test bash scripts/e2e/plasmod_stack_up.sh
#   bash scripts/e2e/plasmod_stack_up.sh --no-free-ports
#   bash scripts/e2e/plasmod_stack_up.sh -d   # pass through to compose up
#
# Env:
#   COMPOSE_FILE   default docker-compose.yml (relative to repo root)
#   APP_MODE       passed to compose (default: prod)
#   PLASMOD_SKIP_DOWN=1       skip step 1
#   PLASMOD_FREE_PORTS=0      skip step 2 (same as --no-free-ports)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
APP_MODE="${APP_MODE:-prod}"
FREE_PORTS="${PLASMOD_FREE_PORTS:-1}"
SKIP_DOWN="${PLASMOD_SKIP_DOWN:-0}"

usage() {
  sed -n '2,/^$/p' "$0" | head -n 20
}

args=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    -h | --help)
      usage
      exit 0
      ;;
    --no-free-ports)
      FREE_PORTS=0
      shift
      ;;
    --skip-down)
      SKIP_DOWN=1
      shift
      ;;
    *)
      args+=("$1")
      shift
      ;;
  esac
done

if [[ ${#args[@]} -eq 0 ]]; then
  args=(-d --build)
fi

compose() {
  docker compose -f "${COMPOSE_FILE}" "$@"
}

_plasmod_conflicts_host_ports() {
  local cid="$1"
  docker port "${cid}" 2>/dev/null | grep -qE '(0\.0\.0\.0|\[::\]):(8080|9000|9001)(/tcp)?$'
}

stop_foreign_publishers() {
  local cid name cids
  mapfile -t cids < <(docker ps -q 2>/dev/null || true)
  [[ ${#cids[@]} -eq 0 ]] && return 0
  for cid in "${cids[@]}"; do
    if _plasmod_conflicts_host_ports "${cid}"; then
      name="$(docker inspect --format '{{.Name}}' "${cid}" 2>/dev/null | sed 's|^/||')"
      echo "[plasmod_stack_up] stopping container ${name} (${cid:0:12}) — publishes Plasmod host port(s) 8080/9000/9001"
      docker stop "${cid}" >/dev/null
    fi
  done
}

if [[ "${SKIP_DOWN}" != "1" ]]; then
  echo "[plasmod_stack_up] docker compose down (${COMPOSE_FILE})"
  compose down
fi

if [[ "${FREE_PORTS}" == "1" ]]; then
  echo "[plasmod_stack_up] freeing host ports 8080 / 9000 / 9001 (other Docker containers)"
  stop_foreign_publishers
fi

echo "[plasmod_stack_up] docker compose up APP_MODE=${APP_MODE} ${args[*]}"
APP_MODE="${APP_MODE}" compose up "${args[@]}"
