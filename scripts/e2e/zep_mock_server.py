#!/usr/bin/env python3
"""
Minimal local Zep mock server for Plasmod hybrid_recall testing.

Endpoints:
- GET  /healthz
- POST /v1/memory/ingest
- POST /v1/memory/recall
- POST /v1/memory/soft-delete
- POST /v1/memory/hard-delete
"""

import json
import os
import threading
import time
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


HOST = os.environ.get("ZEP_MOCK_HOST", "127.0.0.1")
PORT = int(os.environ.get("ZEP_MOCK_PORT", "8000"))
API_KEY = os.environ.get("ZEP_MOCK_API_KEY", "")
RECALL_MODE = os.environ.get("ZEP_MOCK_RECALL_MODE", "identity").strip().lower()
RECALL_DELAY_MS = max(0, int(os.environ.get("ZEP_MOCK_RECALL_DELAY_MS", "0") or "0"))
RECALL_PERTURB = max(0, min(100, int(os.environ.get("ZEP_MOCK_RECALL_PERTURB", "0") or "0")))

_LOCK = threading.Lock()
# memory_id -> record
_MEMORIES = {}


def _json_response(handler: BaseHTTPRequestHandler, code: int, payload: dict) -> None:
    body = json.dumps(payload).encode("utf-8")
    handler.send_response(code)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def _read_json(handler: BaseHTTPRequestHandler):
    length = int(handler.headers.get("Content-Length", "0"))
    if length <= 0:
        return {}
    raw = handler.rfile.read(length)
    if not raw:
        return {}
    return json.loads(raw.decode("utf-8"))


def _authorized(handler: BaseHTTPRequestHandler) -> bool:
    if not API_KEY:
        return True
    auth = handler.headers.get("Authorization", "")
    return auth == f"Bearer {API_KEY}"


def _stable_hash(text: str) -> int:
    return int(hashlib.sha256(text.encode("utf-8")).hexdigest(), 16)


def _apply_recall_mode(memory_ids, query: str):
    if RECALL_MODE == "reverse":
        return list(reversed(memory_ids))
    if RECALL_MODE == "even_first":
        even = []
        odd = []
        for mid in memory_ids:
            # Parse trailing numeric suffix when available.
            suffix = mid.rsplit("_", 1)[-1]
            try:
                n = int(suffix)
            except ValueError:
                n = 1
            if n % 2 == 0:
                even.append(mid)
            else:
                odd.append(mid)
        return even + odd
    if RECALL_MODE == "hash_shuffle":
        return sorted(memory_ids, key=lambda mid: _stable_hash(f"{query}|{mid}"))
    return memory_ids


def _apply_perturb(memory_ids):
    if RECALL_PERTURB <= 0 or len(memory_ids) <= 1:
        return memory_ids
    n = max(1, int(len(memory_ids) * RECALL_PERTURB / 100))
    n = min(n, len(memory_ids))
    prefix = list(reversed(memory_ids[:n]))
    return prefix + memory_ids[n:]


class ZepMockHandler(BaseHTTPRequestHandler):
    server_version = "ZepMock/1.0"

    def log_message(self, fmt: str, *args) -> None:
        # Keep stdout clean for script users.
        return

    def do_GET(self):
        if self.path == "/healthz":
            _json_response(
                self,
                200,
                {
                    "status": "ok",
                    "service": "zep-mock",
                    "recall_mode": RECALL_MODE,
                    "recall_perturb": RECALL_PERTURB,
                    "recall_delay_ms": RECALL_DELAY_MS,
                },
            )
            return
        _json_response(self, 404, {"error": "not found"})

    def do_POST(self):
        if not _authorized(self):
            _json_response(self, 401, {"error": "unauthorized"})
            return

        try:
            payload = _read_json(self)
        except Exception as exc:
            _json_response(self, 400, {"error": f"invalid json: {exc}"})
            return

        if self.path == "/v1/memory/ingest":
            self._handle_ingest(payload)
            return
        if self.path == "/v1/memory/recall":
            self._handle_recall(payload)
            return
        if self.path == "/v1/memory/soft-delete":
            self._handle_soft_delete(payload)
            return
        if self.path == "/v1/memory/hard-delete":
            self._handle_hard_delete(payload)
            return

        _json_response(self, 404, {"error": "not found"})

    def _handle_ingest(self, payload):
        memory_id = str(payload.get("memory_id", "")).strip()
        if not memory_id:
            _json_response(self, 400, {"error": "memory_id is required"})
            return
        content = str(payload.get("content", ""))
        workspace_id = str(payload.get("workspace_id", ""))
        collection = str(payload.get("metadata", {}).get("collection", payload.get("collection", "")))
        with _LOCK:
            _MEMORIES[memory_id] = {
                "memory_id": memory_id,
                "content": content,
                "workspace_id": workspace_id,
                "collection": collection,
                "active": True,
            }
        _json_response(self, 200, {"status": "ok", "memory_id": memory_id})

    def _handle_recall(self, payload):
        if RECALL_DELAY_MS > 0:
            time.sleep(RECALL_DELAY_MS / 1000.0)
        query = str(payload.get("query", "")).strip().lower()
        top_k = int(payload.get("top_k", 10) or 10)
        workspace_id = str(payload.get("workspace_id", "")).strip()
        collection = str(payload.get("collection", "")).strip()
        with _LOCK:
            items = list(_MEMORIES.values())
        candidates = []
        for item in items:
            if not item.get("active", False):
                continue
            if workspace_id and item.get("workspace_id") != workspace_id:
                continue
            if collection and item.get("collection") and item.get("collection") != collection:
                continue
            text = str(item.get("content", "")).lower()
            score = 1 if (query and query in text) else 0
            candidates.append((score, item["memory_id"]))
        candidates.sort(key=lambda x: x[0], reverse=True)
        memory_ids = [mid for _, mid in candidates]
        memory_ids = _apply_recall_mode(memory_ids, query)
        memory_ids = _apply_perturb(memory_ids)
        memory_ids = memory_ids[: max(1, top_k)]
        _json_response(
            self,
            200,
            {
                "status": "ok",
                "memory_ids": memory_ids,
                "visible_memory_refs": memory_ids,
                "count": len(memory_ids),
                "mock_recall_mode": RECALL_MODE,
                "mock_recall_perturb": RECALL_PERTURB,
            },
        )

    def _handle_soft_delete(self, payload):
        memory_id = str(payload.get("memory_id", "")).strip()
        if not memory_id:
            _json_response(self, 400, {"error": "memory_id is required"})
            return
        with _LOCK:
            rec = _MEMORIES.get(memory_id)
            if rec is not None:
                rec["active"] = False
        _json_response(self, 200, {"status": "ok", "memory_id": memory_id})

    def _handle_hard_delete(self, payload):
        memory_id = str(payload.get("memory_id", "")).strip()
        if not memory_id:
            _json_response(self, 400, {"error": "memory_id is required"})
            return
        with _LOCK:
            _MEMORIES.pop(memory_id, None)
        _json_response(self, 200, {"status": "ok", "memory_id": memory_id})


def main():
    server = ThreadingHTTPServer((HOST, PORT), ZepMockHandler)
    print(f"[zep-mock] listening on http://{HOST}:{PORT}")
    server.serve_forever()


if __name__ == "__main__":
    main()
