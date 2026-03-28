#!/usr/bin/env python3
"""
Member A E2E capture: run fixture scenarios against a running ANDB server and
emit one JSON document per README "Expected output format" (plus ingest acks).

Usage (server already up, e.g. `make dev` or `docker compose up -d`):
  python scripts/e2e/member_a_capture.py
  python scripts/e2e/member_a_capture.py --out-dir ./out/member_a

Env:
  ANDB_BASE_URL  default http://127.0.0.1:8080
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import sys
import uuid
from pathlib import Path
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

DEFAULT_BASE = os.environ.get("ANDB_BASE_URL", "http://127.0.0.1:8080")
FIXTURES = Path(__file__).resolve().parents[2] / "integration_tests" / "fixtures" / "member_a"


def _now_iso() -> str:
    return dt.datetime.now(dt.timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _http_json(method: str, base: str, path: str, body: dict[str, Any] | None) -> tuple[int, Any]:
    url = base.rstrip("/") + path
    data = None if body is None else json.dumps(body).encode("utf-8")
    req = Request(url, data=data, method=method)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urlopen(req, timeout=60) as resp:
            raw = resp.read()
            if not raw:
                return resp.status, None
            return resp.status, json.loads(raw.decode("utf-8"))
    except HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        try:
            parsed = json.loads(raw) if raw else None
        except json.JSONDecodeError:
            parsed = {"_raw": raw}
        return e.code, parsed
    except URLError as e:
        raise SystemExit(f"request failed {method} {url}: {e}") from e


def _prepare_event(base: dict[str, Any], scenario_id: str, seq: int) -> dict[str, Any]:
    ev = dict(base)
    now = _now_iso()
    suffix = uuid.uuid4().hex[:12]
    ev["event_id"] = f"evt_ma_{scenario_id}_{seq}_{suffix}"
    ev.setdefault("event_time", now)
    ev.setdefault("ingest_time", now)
    ev.setdefault("visible_time", now)
    return ev


def _load_json(path: Path) -> Any:
    with path.open(encoding="utf-8") as f:
        return json.load(f)


def run_scenarios(base_url: str, fixtures_dir: Path) -> list[dict[str, Any]]:
    manifest_path = fixtures_dir / "manifest.json"
    if not manifest_path.is_file():
        raise SystemExit(f"missing manifest: {manifest_path}")

    manifest = _load_json(manifest_path)
    scenarios = manifest.get("scenarios")
    if not isinstance(scenarios, list):
        raise SystemExit("manifest.json: missing scenarios array")

    out: list[dict[str, Any]] = []
    for spec in scenarios:
        if not isinstance(spec, dict):
            continue
        sid = str(spec.get("id", "unknown"))
        test_name = str(spec.get("test_name", sid))
        notes = spec.get("notes", "")
        ingest_names = spec.get("ingest_files") or []
        query_name = spec.get("query_file")
        if not query_name:
            raise SystemExit(f"scenario {sid}: missing query_file")

        ingest_acks: list[dict[str, Any]] = []
        for i, name in enumerate(ingest_names):
            p = fixtures_dir / str(name)
            if not p.is_file():
                raise SystemExit(f"scenario {sid}: missing ingest file {p}")
            ev = _prepare_event(_load_json(p), sid, i)
            status, ack = _http_json("POST", base_url, "/v1/ingest/events", ev)
            ingest_acks.append({"status": status, "request_event_id": ev["event_id"], "ack": ack})
            if status != 200:
                raise SystemExit(f"scenario {sid}: ingest failed HTTP {status}: {ack}")

        qp = fixtures_dir / str(query_name)
        if not qp.is_file():
            raise SystemExit(f"scenario {sid}: missing query file {qp}")
        query_body = _load_json(qp)
        if not isinstance(query_body, dict):
            raise SystemExit(f"scenario {sid}: query must be a JSON object")

        qstatus, response = _http_json("POST", base_url, "/v1/query", query_body)
        if qstatus != 200:
            raise SystemExit(f"scenario {sid}: query failed HTTP {qstatus}: {response}")

        if not isinstance(response, dict):
            response = {"_non_object_response": response}

        ct = response.get("chain_traces")
        if not isinstance(ct, dict):
            ct = {}
        chain_traces = {
            "main": ct.get("main") if isinstance(ct.get("main"), list) else [],
            "memory_pipeline": ct.get("memory_pipeline") if isinstance(ct.get("memory_pipeline"), list) else [],
            "query": ct.get("query") if isinstance(ct.get("query"), list) else [],
            "collaboration": ct.get("collaboration") if isinstance(ct.get("collaboration"), list) else [],
        }

        record: dict[str, Any] = {
            "test_name": test_name,
            "scenario_id": sid,
            "scenario_notes": notes,
            "query": {"raw": query_body.get("query_text", ""), "request": query_body},
            "ingest_acks": ingest_acks,
            "response": {
                "objects": response.get("objects", []),
                "edges": response.get("edges", []),
                "proof_trace": response.get("proof_trace", []),
                "applied_filters": response.get("applied_filters", []),
                "provenance": response.get("provenance", []),
                "versions": response.get("versions", []),
            },
            "chain_traces": chain_traces,
            "_meta": {
                "evidence_cache_log_note": (
                    "Evidence fragment cache hit/miss is not yet serialized on the HTTP response; "
                    "see server logs / internal evidence.Cache for debugging."
                ),
            },
        }
        out.append(record)
    return out


def main() -> None:
    ap = argparse.ArgumentParser(description="Member A fixture-driven E2E JSON capture")
    ap.add_argument("--base-url", default=DEFAULT_BASE, help="ANDB base URL")
    ap.add_argument(
        "--fixtures",
        type=Path,
        default=FIXTURES,
        help="Directory with manifest.json and fixture JSON files",
    )
    ap.add_argument(
        "--out-dir",
        type=Path,
        default=None,
        help="If set, write one JSON file per scenario; otherwise print a single JSON array to stdout",
    )
    args = ap.parse_args()

    fixtures_dir = args.fixtures.resolve()
    results = run_scenarios(args.base_url, fixtures_dir)

    if args.out_dir:
        args.out_dir.mkdir(parents=True, exist_ok=True)
        for r in results:
            sid = r.get("scenario_id", "scenario")
            path = args.out_dir / f"{sid}.json"
            path.write_text(json.dumps(r, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
            print(path)
    else:
        json.dump(results, sys.stdout, indent=2, ensure_ascii=False)
        sys.stdout.write("\n")


if __name__ == "__main__":
    main()
