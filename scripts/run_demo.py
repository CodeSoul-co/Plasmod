import datetime as dt

from andb_sdk import AndbClient


def main() -> None:
    c = AndbClient()
    now = dt.datetime.utcnow().replace(microsecond=0).isoformat() + "Z"
    c.ingest_event(
        {
            "event_id": "evt_demo_001",
            "tenant_id": "t_demo",
            "workspace_id": "w_demo",
            "agent_id": "agent_a",
            "session_id": "sess_a",
            "event_type": "assistant_message",
            "event_time": now,
            "ingest_time": now,
            "visible_time": now,
            "logical_ts": 2,
            "parent_event_id": "",
            "causal_refs": [],
            "payload": {"text": "demo"},
            "source": "demo",
            "importance": 0.8,
            "visibility": "shared",
            "version": 1,
        }
    )
    result = c.query(
        {
            "query_text": "demo",
            "query_scope": "workspace",
            "session_id": "sess_a",
            "agent_id": "agent_a",
            "top_k": 3,
            "time_window": {"from": now, "to": now},
            "relation_constraints": [],
            "response_mode": "structured_evidence",
        }
    )
    print(result)


if __name__ == "__main__":
    main()
