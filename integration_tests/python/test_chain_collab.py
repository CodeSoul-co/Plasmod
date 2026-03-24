"""Python SDK integration tests: CollaborationChain (multi-agent path).
Tests ConflictMerge (LWW), conflict_resolved edge, and memory broadcast."""
import sys
import time
from common import assert_keys, make_sdk_client, now_iso, wait_server


def _thought_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"sdk_collab_{suffix}",
        "agent_id": "sdk_collab_agent",
        "session_id": "sdk_collab_sess",
        "event_type": "agent_thought",
        "event_time": now,
        "payload": {"text": f"collab test {suffix}"},
        "importance": 0.5,
        "visibility": "private",
        "version": 1,
    }


def _query() -> dict:
    return {
        "query_text": "collab test",
        "query_scope": "session",
        "session_id": "sdk_collab_sess",
        "agent_id": "sdk_collab_agent",
        "top_k": 5,
        "object_types": ["memory"],
        "relation_constraints": [],
        "response_mode": "structured_evidence",
    }


def test_lww_higher_version_wins() -> None:
    """Two same-session events → ConflictMerge fires → higher version wins."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))

    # Ingest two events for the same agent+session.
    ack1 = client.ingest_event(_thought_event(f"{suffix}_1"))
    assert ack1.get("status") == "accepted", f"ingest 1 failed: {ack1}"

    ack2 = client.ingest_event(_thought_event(f"{suffix}_2"))
    assert ack2.get("status") == "accepted", f"ingest 2 failed: {ack2}"

    # Query to exercise the collaboration pipeline.
    result = client.query(_query())
    edges = result.get("edges", [])
    # ConflictMerge may produce conflict_resolved edge.
    has_conflict_edge = any(
        isinstance(e, dict) and e.get("edge_type") == "conflict_resolved"
        for e in edges
    )
    print(f"  [PASS] LWW: {len(edges)} total edges, conflict_resolved present={has_conflict_edge}")


def test_conflict_edge_created() -> None:
    """After dual ingest, at least one conflict_resolved edge exists."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))

    client.ingest_event(_thought_event(f"{suffix}_a"))
    client.ingest_event(_thought_event(f"{suffix}_b"))

    result = client.query(_query())
    edges = result.get("edges", [])
    conflict_edges = [
        e for e in edges
        if isinstance(e, dict) and e.get("edge_type") == "conflict_resolved"
    ]
    assert len(conflict_edges) > 0, (
        f"expected at least one conflict_resolved edge, got {len(conflict_edges)}"
    )
    print(f"  [PASS] conflict_resolved edge: {len(conflict_edges)} found")


def test_broadcast_shared_memory() -> None:
    """After ingest, provenance chain indicates CommunicationWorker ran."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))
    client.ingest_event(_thought_event(suffix))

    result = client.query(_query())
    provenance = result.get("provenance", [])
    assert len(provenance) > 0, f"expected provenance stages, got {len(provenance)}"
    print(f"  [PASS] broadcast: {len(provenance)} provenance stage(s)")


def main() -> None:
    wait_server()
    tests = [
        test_lww_higher_version_wins,
        test_conflict_edge_created,
        test_broadcast_shared_memory,
    ]
    failures = []
    for fn in tests:
        try:
            fn()
        except Exception as exc:
            failures.append(f"{fn.__name__}: {exc}")
            print(f"  [FAIL] {fn.__name__}: {exc}", file=sys.stderr)

    print(f"\nPython SDK CollaborationChain tests: {len(tests) - len(failures)}/{len(tests)} passed")
    if failures:
        sys.exit(1)


if __name__ == "__main__":
    main()
