"""Python SDK integration tests: MainChain (write path).
Tests event ingestion through ObjectMaterialization, StateMaterialization,
ToolTrace, IndexBuild, and GraphRelation workers at the SDK level."""
import sys
import time
from common import assert_keys, make_sdk_client, now_iso, wait_server


def _state_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"sdk_chain_state_{suffix}",
        "agent_id": "sdk_chain_agent",
        "session_id": "sdk_chain_sess",
        "event_type": "state_update",
        "event_time": now,
        "payload": {"state_key": f"key_{suffix}", "state_value": f"value_{suffix}"},
        "importance": 0.5,
        "visibility": "private",
        "version": 1,
    }


def _tool_call_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"sdk_chain_tool_{suffix}",
        "agent_id": "sdk_chain_agent",
        "session_id": "sdk_chain_sess",
        "event_type": "tool_call",
        "event_time": now,
        "payload": {"tool_name": "search", "tool_args": '{"query": "test"}'},
        "importance": 0.5,
        "visibility": "private",
        "version": 1,
    }


def _checkpoint_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"sdk_chain_ckpt_{suffix}",
        "agent_id": "sdk_chain_agent",
        "session_id": "sdk_chain_sess",
        "event_type": "checkpoint",
        "event_time": now,
        "payload": {},
        "importance": 0.5,
        "visibility": "private",
        "version": 1,
    }


def _thought_event(suffix: str) -> dict:
    now = now_iso()
    return {
        "event_id": f"sdk_chain_thought_{suffix}",
        "agent_id": "sdk_chain_agent",
        "session_id": "sdk_chain_sess",
        "event_type": "agent_thought",
        "event_time": now,
        "payload": {"text": f"chain main test {suffix}"},
        "importance": 0.5,
        "visibility": "private",
        "version": 1,
    }


def _query() -> dict:
    return {
        "query_text": "chain main test",
        "query_scope": "session",
        "session_id": "sdk_chain_sess",
        "agent_id": "sdk_chain_agent",
        "top_k": 5,
        "object_types": ["memory", "state", "artifact"],
        "relation_constraints": [],
        "response_mode": "structured_evidence",
    }


def test_state_update_materializes() -> None:
    """state_update event → State object in query response."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))
    ev = _state_event(suffix)
    ack = client.ingest_event(ev)
    assert ack.get("status") == "accepted", f"ingest failed: {ack}"

    q = _query()
    q["object_types"] = ["state"]
    result = client.query(q)
    objects = result.get("objects", [])
    assert len(objects) > 0, f"expected at least one state object, got {len(objects)}"
    print(f"  [PASS] state_update → {len(objects)} state object(s)")


def test_tool_call_artifact() -> None:
    """tool_call event → Artifact object in query response."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))
    ev = _tool_call_event(suffix)
    ack = client.ingest_event(ev)
    assert ack.get("status") == "accepted", f"ingest failed: {ack}"

    q = _query()
    q["object_types"] = ["artifact"]
    result = client.query(q)
    objects = result.get("objects", [])
    assert len(objects) > 0, f"expected at least one artifact, got {len(objects)}"
    print(f"  [PASS] tool_call → {len(objects)} artifact(s)")


def test_checkpoint_snapshot() -> None:
    """checkpoint event → ObjectVersion snapshot created."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))

    # Seed a state first.
    state_ev = _state_event(f"pre_{suffix}")
    client.ingest_event(state_ev)

    # Then ingest checkpoint.
    ckpt_ev = _checkpoint_event(suffix)
    ack = client.ingest_event(ckpt_ev)
    assert ack.get("status") == "accepted", f"checkpoint ingest failed: {ack}"

    q = _query()
    q["object_types"] = ["state"]
    result = client.query(q)
    versions = result.get("versions", [])
    assert len(versions) > 0, f"expected version entries after checkpoint, got {len(versions)}"
    print(f"  [PASS] checkpoint → {len(versions)} version entry/entries")


def test_edge_indexed() -> None:
    """Ingest thought event → edges present in query response."""
    client = make_sdk_client()
    suffix = str(int(time.time() * 1000))
    ev = _thought_event(suffix)
    ack = client.ingest_event(ev)
    assert ack.get("status") == "accepted", f"ingest failed: {ack}"

    result = client.query(_query())
    edges = result.get("edges", [])
    assert len(edges) > 0, f"expected edges after ingest, got {len(edges)}"
    print(f"  [PASS] ingest → {len(edges)} edge(s)")


def main() -> None:
    wait_server()
    tests = [
        test_state_update_materializes,
        test_tool_call_artifact,
        test_checkpoint_snapshot,
        test_edge_indexed,
    ]
    failures = []
    for fn in tests:
        try:
            fn()
        except Exception as exc:
            failures.append(f"{fn.__name__}: {exc}")
            print(f"  [FAIL] {fn.__name__}: {exc}", file=sys.stderr)

    print(f"\nPython SDK MainChain tests: {len(tests) - len(failures)}/{len(tests)} passed")
    if failures:
        sys.exit(1)


if __name__ == "__main__":
    main()
