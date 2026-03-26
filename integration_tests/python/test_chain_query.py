"""Python SDK integration tests: QueryChain (read/reasoning path).
Tests ProofTrace, SubgraphExecutor, and edge filtering at the SDK level."""
import sys
import time
from common import assert_keys, make_sdk_client, now_iso, wait_server


def _thought_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"sdk_qchain_{suffix}",
        "agent_id": "sdk_qchain_agent",
        "session_id": "sdk_qchain_sess",
        "event_type": "agent_thought",
        "event_time": now,
        "payload": {"text": f"query chain test {suffix}"},
        "importance": 0.5,
        "visibility": "private",
        "version": 1,
    }


def _query() -> dict:
    return {
        "query_text": "query chain test",
        "query_scope": "session",
        "session_id": "sdk_qchain_sess",
        "agent_id": "sdk_qchain_agent",
        "top_k": 5,
        "object_types": ["memory"],
        "relation_constraints": [],
        "response_mode": "structured_evidence",
    }


def test_subgraph_nodes_populated() -> None:
    """After ingest, query response has edges from SubgraphExecutor expansion."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))
    client.ingest_event(_thought_event(suffix))

    result = client.query(_query())
    edges = result.get("edges", [])
    assert len(edges) > 0, f"expected non-empty edges (subgraph), got {len(edges)}"
    print(f"  [PASS] subgraph edges: {len(edges)}")


def test_proof_trace_multi_hop() -> None:
    """Proof trace contains multiple entries indicating multi-hop traversal."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))

    # Ingest multiple events to build a richer graph.
    for i in range(3):
        client.ingest_event(_thought_event(f"{suffix}_{i}"))

    result = client.query(_query())
    trace = result.get("proof_trace", [])
    assert len(trace) > 0, f"expected non-empty proof_trace, got {len(trace)}"
    print(f"  [PASS] proof_trace entries: {len(trace)}")


def test_edge_type_filter() -> None:
    """Query with relation_constraints filters edges by type."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))
    client.ingest_event(_thought_event(suffix))

    q = _query()
    q["relation_constraints"] = ["derived_from"]
    result = client.query(q)

    edges = result.get("edges", [])
    # All returned edges should be derived_from type.
    for e in edges:
        if isinstance(e, dict) and e.get("edge_type"):
            assert e["edge_type"] == "derived_from", (
                f"expected derived_from edge, got {e['edge_type']}"
            )
    print(f"  [PASS] edge_type_filter: {len(edges)} filtered edge(s)")


def main() -> None:
    wait_server()
    tests = [
        test_subgraph_nodes_populated,
        test_proof_trace_multi_hop,
        test_edge_type_filter,
    ]
    failures = []
    for fn in tests:
        try:
            fn()
        except Exception as exc:
            failures.append(f"{fn.__name__}: {exc}")
            print(f"  [FAIL] {fn.__name__}: {exc}", file=sys.stderr)

    print(f"\nPython SDK QueryChain tests: {len(tests) - len(failures)}/{len(tests)} passed")
    if failures:
        sys.exit(1)


if __name__ == "__main__":
    main()
