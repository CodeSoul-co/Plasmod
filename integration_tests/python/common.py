"""Shared utilities for Python SDK integration tests."""
import os
import time
import sys

BASE_URL: str = os.getenv("ANDB_BASE_URL", "http://127.0.0.1:8080")
HTTP_TIMEOUT: int = int(os.getenv("ANDB_HTTP_TIMEOUT", "10"))


def now_iso() -> str:
    from datetime import datetime, timezone
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def wait_server(timeout: float = 20.0) -> None:
    import requests
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            r = requests.get(f"{BASE_URL}/healthz", timeout=2)
            if r.status_code == 200:
                return
        except Exception:
            pass
        time.sleep(0.25)
    print(f"[ERROR] server not ready at {BASE_URL} after {timeout}s", file=sys.stderr)
    sys.exit(1)


def assert_keys(data: dict, *keys: str, context: str = "") -> None:
    for k in keys:
        assert k in data, f"{context}missing key {k!r} in {list(data.keys())}"


def make_sdk_client():
    """Return an AndbClient pointing at the test server."""
    import sys, os
    sdk_path = os.path.join(os.path.dirname(__file__), "..", "..", "sdk", "python")
    if sdk_path not in sys.path:
        sys.path.insert(0, sdk_path)
    from andb_sdk.client import AndbClient
    return AndbClient(base_url=BASE_URL)
