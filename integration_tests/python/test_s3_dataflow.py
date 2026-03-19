"""S3 dataflow integration test via Python SDK + MinIO.

Ingests an event and runs a query via AndbClient, then writes the full
capture (ack + query request + response) to S3 and reads it back to
validate round-trip data integrity.

Skip unless ANDB_RUN_S3_TESTS=true.
"""
import io
import json
import os
import sys
import time
from common import assert_keys, make_sdk_client, now_iso, wait_server
from s3_common import ensure_bucket, load_s3_config, make_minio_client


def _sample_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"s3_evt_{suffix}",
        "tenant_id": "t_demo",
        "workspace_id": "w_demo",
        "agent_id": "s3_agent_a",
        "session_id": "s3_sess_a",
        "event_type": "user_message",
        "event_time": now,
        "ingest_time": now,
        "visible_time": now,
        "logical_ts": 1,
        "parent_event_id": "",
        "causal_refs": [],
        "payload": {"text": f"s3 dataflow test {suffix}"},
        "source": "s3_integration_tests",
        "importance": 0.8,
        "visibility": "private",
        "version": 1,
    }


def _sample_query() -> dict:
    return {
        "query_text": "s3 dataflow test",
        "query_scope": "workspace",
        "session_id": "s3_sess_a",
        "agent_id": "s3_agent_a",
        "tenant_id": "t_demo",
        "workspace_id": "w_demo",
        "top_k": 5,
        "time_window": {
            "from": "2026-01-01T00:00:00Z",
            "to": "2027-01-01T00:00:00Z",
        },
        "object_types": ["memory", "state", "artifact"],
        "memory_types": ["semantic", "episodic", "procedural"],
        "relation_constraints": [],
        "response_mode": "structured_evidence",
    }


def main() -> None:
    run_s3 = os.getenv("ANDB_RUN_S3_TESTS", "").lower()
    if run_s3 not in ("1", "true", "yes"):
        print("[SKIP] S3 tests disabled (set ANDB_RUN_S3_TESTS=true to enable)")
        return

    wait_server()

    cfg = load_s3_config()
    minio_client = make_minio_client(cfg)

    suffix = str(int(time.time() * 1000))
    client = make_sdk_client()

    # 1. Ingest via SDK.
    ev = _sample_event(suffix)
    ack = client.ingest_event(ev)
    assert_keys(ack, "status", "lsn", "event_id", context="ingest ack: ")
    assert ack["status"] == "accepted", f"ack status: {ack['status']}"
    print(f"  [OK] ingest ack: lsn={ack['lsn']} event_id={ack['event_id']}")

    # 2. Query via SDK.
    q = _sample_query()
    result = client.query(q)
    assert_keys(result, "objects", "provenance", "proof_trace", context="query response: ")
    print(f"  [OK] query: objects={len(result.get('objects', []))} provenance={result.get('provenance', [])}")

    # 3. Build capture payload.
    capture = {
        "captured_at": now_iso(),
        "base_url": os.getenv("ANDB_BASE_URL", "http://127.0.0.1:8080"),
        "ack": ack,
        "query_request": q,
        "query_response": result,
    }
    capture_bytes = json.dumps(capture, indent=2, ensure_ascii=False).encode("utf-8")

    # 4. Ensure bucket and upload.
    ensure_bucket(minio_client, cfg["bucket"])
    object_key = f"{cfg['prefix']}/py_capture_{time.strftime('%Y%m%dT%H%M%SZ', time.gmtime())}.json"

    minio_client.put_object(
        cfg["bucket"],
        object_key,
        io.BytesIO(capture_bytes),
        length=len(capture_bytes),
        content_type="application/json",
    )
    print(f"  [OK] uploaded {len(capture_bytes)} bytes → s3://{cfg['bucket']}/{object_key}")

    # 5. Download and verify round-trip.
    response = minio_client.get_object(cfg["bucket"], object_key)
    downloaded = response.read()
    response.close()
    response.release_conn()

    assert downloaded == capture_bytes, (
        f"S3 round-trip mismatch: uploaded {len(capture_bytes)} bytes, "
        f"downloaded {len(downloaded)} bytes"
    )
    print(f"  [OK] S3 round-trip verified: {len(downloaded)} bytes match")

    # 6. Validate decoded content.
    decoded = json.loads(downloaded)
    assert decoded["ack"]["event_id"] == ev["event_id"], "event_id mismatch after S3 round-trip"
    assert "proof_trace" in decoded["query_response"], "proof_trace missing from S3 stored response"
    assert "provenance" in decoded["query_response"], "provenance missing from S3 stored response"
    print("  [OK] decoded S3 content fields verified")

    print("\n[PASS] test_s3_dataflow: all assertions passed")


if __name__ == "__main__":
    main()
