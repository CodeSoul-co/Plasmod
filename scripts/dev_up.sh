#!/usr/bin/env bash
set -euo pipefail

set -a
[ -f .env ] && source .env
set +a

go run ${RETRIEVAL_TAG:-} ./src/cmd/server
