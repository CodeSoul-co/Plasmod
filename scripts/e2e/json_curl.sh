#!/usr/bin/env bash
# Pretty-print JSON from curl with jq (fallback: python3 -m json.tool, then raw body).
#
# Run as a command (passes all args to curl):
#   ./scripts/e2e/json_curl.sh -fsS -X POST "$ANDB_BASE_URL/v1/query" \
#     -H 'Content-Type: application/json' -d '{"query_text":"hello",...}'
#
# Or load helpers into your shell (add to ~/.bashrc):
#   source /path/to/Plasmod/scripts/e2e/json_curl.sh
# Then use:
#   jsoncurl -fsS -X POST "$ANDB_BASE_URL/v1/query" ...
#   jqc ...    # short alias

_json_curl_impl() {
  local body
  body="$(curl -sS "$@")" || return $?
  if command -v jq >/dev/null 2>&1; then
    if echo "$body" | jq -e . >/dev/null 2>&1; then
      echo "$body" | jq .
    else
      printf '%s\n' "$body"
    fi
  else
    echo "$body" | python3 -m json.tool 2>/dev/null || printf '%s\n' "$body"
  fi
}

if [[ "${BASH_SOURCE[0]:-$0}" == "${0}" ]]; then
  set -euo pipefail
  _json_curl_impl "$@"
else
  jsoncurl() { _json_curl_impl "$@"; }
  jqc() { _json_curl_impl "$@"; }
fi
