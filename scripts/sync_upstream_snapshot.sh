#!/usr/bin/env bash
set -euo pipefail

UPSTREAM_SRC="${1:-/Users/erwin/Downloads/codespace/milvus}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"

if [ ! -d "$UPSTREAM_SRC" ]; then
  echo "upstream source path not found: $UPSTREAM_SRC" >&2
  exit 1
fi

DP_DIR="$ROOT_DIR/src/internal/dataplane/retrievalplane"
COORD_DIR="$ROOT_DIR/src/internal/coordinator/controlplane"
EVT_DIR="$ROOT_DIR/src/internal/eventbackbone/streamplane"
PKG_DIR="$ROOT_DIR/src/internal/platformpkg"

mkdir -p "$DP_DIR" "$COORD_DIR" "$EVT_DIR" "$PKG_DIR"
mkdir -p \
  "$DP_DIR/queryruntime" \
  "$DP_DIR/storageshared" \
  "$DP_DIR/objectstore" \
  "$COORD_DIR/datacontrol" \
  "$COORD_DIR/querycontrol" \
  "$COORD_DIR/metacontrol" \
  "$COORD_DIR/accessproxy" \
  "$EVT_DIR/clockservice" \
  "$EVT_DIR/streamcoord" \
  "$EVT_DIR/streamnode" \
  "$EVT_DIR/flushpipeline"

rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/pkg" "$PKG_DIR/"
cp "$UPSTREAM_SRC/LICENSE" "$PKG_DIR/UPSTREAM_LICENSE"
cp "$UPSTREAM_SRC/README.md" "$PKG_DIR/UPSTREAM_README.md"

rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/core/" "$DP_DIR/core/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/querynodev2/" "$DP_DIR/queryruntime/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/storage/" "$DP_DIR/storage/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/storagev2/" "$DP_DIR/objectstore/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/storagecommon/" "$DP_DIR/storageshared/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/compaction/" "$DP_DIR/compaction/"

rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/coordinator/" "$COORD_DIR/coordinator/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/rootcoord/" "$COORD_DIR/metacontrol/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/datacoord/" "$COORD_DIR/datacontrol/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/querycoordv2/" "$COORD_DIR/querycontrol/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/proxy/" "$COORD_DIR/accessproxy/"

rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/tso/" "$EVT_DIR/clockservice/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/streamingcoord/" "$EVT_DIR/streamcoord/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/streamingnode/" "$EVT_DIR/streamnode/"
rsync -a --delete --exclude='.git' "$UPSTREAM_SRC/internal/flushcommon/" "$EVT_DIR/flushpipeline/"

echo "upstream sources synced into ANDB module areas"
