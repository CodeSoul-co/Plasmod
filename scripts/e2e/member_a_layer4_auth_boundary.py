#!/usr/bin/env python3
"""Member-A Layer4 auth boundary check."""

from __future__ import annotations

import json
import os
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def call(url: str, key: str | None) -> int:
    req = Request(url, method="GET")
    if key:
        req.add_header("X-Admin-Key", key)
    try:
        with urlopen(req, timeout=10.0) as resp:
            _ = resp.read()
            return resp.status
    except HTTPError as e:
        return e.code


def main() -> int:
    base = os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
    key = (
        os.environ.get("PLASMOD_ADMIN_API_KEY")
        or os.environ.get("ANDB_ADMIN_API_KEY")
        or ""
    ).strip()
    target = f"{base}/v1/admin/storage"
    no_key_status = call(target, None)
    with_key_status = call(target, key if key else None)
    auth_enabled = bool(key)

    ok = True
    if auth_enabled:
        ok = no_key_status == 401 and with_key_status == 200
    else:
        ok = no_key_status == 200 and with_key_status == 200

    print(
        json.dumps(
            {
                "target": target,
                "auth_enabled": auth_enabled,
                "no_key_status": no_key_status,
                "with_key_status": with_key_status,
                "ok": ok,
            },
            ensure_ascii=False,
        )
    )
    return 0 if ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
