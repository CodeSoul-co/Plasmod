"""Python SDK integration tests: validates AndbClient.ingest_event() and AndbClient.query()."""
import sys
import time
from common import assert_keys, make_sdk_client, now_iso, wait_server


def _sample_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"sdk_evt_{suffix}",
        "tenant_id": "t_demo",
        "workspace_id": "w_demo",
        "agent_id": "sdk_agent_a",
        "session_id": "sdk_sess_a",
        "event_type": "user_message",
        "event_time": now,
        "ingest_time": now,
        "visible_time": now,
        "logical_ts": 1,
        "parent_event_id": "",
        "causal_refs": [],
        "payload": {"text": f"sdk integration test {suffix}"},
        "source": "python_sdk_integration_tests",
        "importance": 0.7,
        "visibility": "private",
        "version": 1,
    }


def _sample_query() -> dict:
    return {
        "query_text": "sdk integration test",
        "query_scope": "workspace",
        "session_id": "sdk_sess_a",
        "agent_id": "sdk_agent_a",
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


def test_sdk_ingest_returns_ack() -> None:
    client = make_sdk_client()
    ev = _sample_event(str(int(time.time() * 1000)))
    ack = client.ingest_event(ev)
    assert isinstance(ack, dict), f"ack is not a dict: {type(ack)}"
    assert_keys(ack, "status", "lsn", "event_id", context="ingest ack: ")
    assert ack["status"] == "accepted", f"ack status: {ack['status']}"
    assert ack["event_id"] == ev["event_id"], f"ack event_id mismatch: {ack['event_id']}"
    print(f"  [PASS] sdk ingest ack: lsn={ack['lsn']} event_id={ack['event_id']}")


def test_sdk_ingest_lsn_monotonic() -> None:
    client = make_sdk_client()
    r1 = client.ingest_event(_sample_event(f"mono_a_{int(time.time()*1000)}"))
    r2 = client.ingest_event(_sample_event(f"mono_b_{int(time.time()*1000)+1}"))
    assert float(r2["lsn"]) > float(r1["lsn"]), (
        f"lsn should be monotonically increasing: r1={r1['lsn']} r2={r2['lsn']}"
    )
    print(f"  [PASS] sdk lsn monotonic: {r1['lsn']} < {r2['lsn']}")


def test_sdk_query_returns_evidence_fields() -> None:
    client = make_sdk_client()
    client.ingest_event(_sample_event(str(int(time.time() * 1000))))
    result = client.query(_sample_query())
    assert isinstance(result, dict), f"query result is not a dict: {type(result)}"
    assert_keys(result, "objects", "edges", "provenance", "versions", "applied_filters", "proof_trace",
                context="query response: ")
    print(f"  [PASS] sdk query structured evidence fields present")


def test_sdk_query_provenance_non_empty() -> None:
    client = make_sdk_client()
    client.ingest_event(_sample_event(str(int(time.time() * 1000))))
    result = client.query(_sample_query())
    provenance = result.get("provenance", [])
    assert isinstance(provenance, list) and len(provenance) > 0, (
        f"expected non-empty provenance, got: {provenance}"
    )
    print(f"  [PASS] sdk query provenance non-empty: {provenance}")


def test_sdk_query_proof_trace_non_empty() -> None:
    client = make_sdk_client()
    client.ingest_event(_sample_event(str(int(time.time() * 1000))))
    result = client.query(_sample_query())
    trace = result.get("proof_trace", [])
    # Use >= 1 instead of exact count since ProofTraceWorker BFS depth may vary (up to 8)
    assert isinstance(trace, list) and len(trace) >= 1, (
        f"expected proof_trace with at least 1 entry, got: {trace}"
    )
    print(f"  [PASS] sdk query proof_trace non-empty (len={len(trace)}): {trace[:3]}...")


def test_sdk_query_top_k_limits_results() -> None:
    client = make_sdk_client()
    q = _sample_query()
    q["top_k"] = 1
    result = client.query(q)
    objects = result.get("objects", [])
    assert len(objects) <= 1, f"top_k=1 should return at most 1 object, got {len(objects)}"
    print(f"  [PASS] sdk query top_k=1 objects={len(objects)}")


def test_sdk_ingest_then_query_e2e() -> None:
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))
    ev = _sample_event(suffix)
    ack = client.ingest_event(ev)
    assert ack["status"] == "accepted"

    result = client.query(_sample_query())
    assert_keys(result, "objects", "provenance", "proof_trace")
    objs = result.get("objects", [])
    print(f"  [PASS] sdk e2e: ingest→query objects={len(objs)} provenance={result['provenance']}")


def main() -> None:
    wait_server()
    tests = [
        test_sdk_ingest_returns_ack,
        test_sdk_ingest_lsn_monotonic,
        test_sdk_query_returns_evidence_fields,
        test_sdk_query_provenance_non_empty,
        test_sdk_query_proof_trace_non_empty,
        test_sdk_query_top_k_limits_results,
        test_sdk_ingest_then_query_e2e,
    ]
    failures = []
    for fn in tests:
        try:
            fn()
        except Exception as exc:
            failures.append(f"{fn.__name__}: {exc}")
            print(f"  [FAIL] {fn.__name__}: {exc}", file=sys.stderr)

    print(f"\nPython SDK tests: {len(tests) - len(failures)}/{len(tests)} passed")
    if failures:
        sys.exit(1)


if __name__ == "__main__":
    main()
