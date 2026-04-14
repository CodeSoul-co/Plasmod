#!/usr/bin/env python3
"""Member-A Layer5 ops stability checks: replay/rollback/wipe/cold-purge."""

from __future__ import annotations

import json
import os
import time
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def http_json(method: str, url: str, body: dict | None, key: str) -> tuple[int, object]:
    data = json.dumps(body).encode() if body is not None else None
    req = Request(url, data=data, method=method)
    if data:
        req.add_header("Content-Type", "application/json")
    if key:
        req.add_header("X-Admin-Key", key)
    try:
        with urlopen(req, timeout=120.0) as resp:
            raw = resp.read()
            return resp.status, json.loads(raw) if raw else {}
    except HTTPError as e:
        raw = e.read()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, raw.decode(errors="replace")


def main() -> int:
    base = os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
    key = (
        os.environ.get("PLASMOD_ADMIN_API_KEY")
        or os.environ.get("ANDB_ADMIN_API_KEY")
        or ""
    ).strip()
    destructive = os.environ.get("MEMBER_A_RUN_DESTRUCTIVE", "false").lower() == "true"

    report: dict[str, object] = {"base_url": base, "destructive": destructive}
    checks: dict[str, dict[str, object]] = {}

    st, body = http_json("GET", f"{base}/healthz", None, key)
    checks["healthz"] = {"status": st, "ok": st == 200, "body": body}

    st, body = http_json("POST", f"{base}/v1/admin/replay", {"from_lsn": 0, "limit": 50, "dry_run": True}, key)
    checks["replay_preview"] = {"status": st, "ok": st == 200, "body": body}

    st, body = http_json("POST", f"{base}/v1/admin/s3/cold-purge", {"confirm": "purge_cold_tier", "dry_run": True}, key)
    checks["cold_purge_dry_run"] = {"status": st, "ok": st == 200, "body": body}

    st_mem, mems = http_json("GET", f"{base}/v1/memory", None, key)
    mem_id = ""
    if st_mem == 200 and isinstance(mems, list) and mems:
        first = mems[0]
        if isinstance(first, dict):
            mem_id = str(first.get("memory_id", ""))
    if mem_id:
        st, body = http_json(
            "POST",
            f"{base}/v1/admin/rollback",
            {"memory_id": mem_id, "action": "deactivate", "dry_run": True, "reason": "layer5_ops"},
            key,
        )
        checks["rollback_dry_run"] = {"status": st, "ok": st == 200, "memory_id": mem_id, "body": body}
    else:
        checks["rollback_dry_run"] = {"status": 0, "ok": True, "note": "no memory rows, skip"}

    if destructive:
        t0 = time.perf_counter()
        st, body = http_json("POST", f"{base}/v1/admin/data/wipe", {"confirm": "delete_all_data"}, key)
        checks["wipe"] = {
            "status": st,
            "ok": st == 200,
            "elapsed_s": round(time.perf_counter() - t0, 3),
            "body": body,
        }
    else:
        checks["wipe"] = {"status": 0, "ok": True, "note": "skipped (MEMBER_A_RUN_DESTRUCTIVE=false)"}

    report["checks"] = checks
    report["ok"] = all(v.get("ok", False) for v in checks.values())
    print(json.dumps(report, ensure_ascii=False))
    return 0 if report["ok"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
