#!/bin/bash
# Stop the running Plasmod server

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PID_FILE="${SCRIPT_DIR}/.server.pid"

if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        kill "$PID"
        echo "Server (PID: $PID) stopped"
    else
        echo "Server not running (stale PID file)"
    fi
    rm -f "$PID_FILE"
else
    echo "No PID file found, attempting to kill by port..."
    pkill -f "plasmod\|andb" 2>/dev/null || true
fi
