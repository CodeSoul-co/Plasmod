import datetime as dt

from andb_sdk import AndbClient


def main() -> None:
    c = AndbClient()
    now = dt.datetime.utcnow().replace(microsecond=0).isoformat() + "Z"
    event = {
        "event_id": "evt_seed_001",
        "tenant_id": "t_demo",
        "workspace_id": "w_demo",
        "agent_id": "agent_a",
        "session_id": "sess_a",
        "event_type": "user_message",
        "event_time": now,
        "ingest_time": now,
        "visible_time": now,
        "logical_ts": 1,
        "parent_event_id": "",
        "causal_refs": [],
        "payload": {"text": "hello"},
        "source": "seed",
        "importance": 0.5,
        "visibility": "private",
        "version": 1,
    }
    print(c.ingest_event(event))


if __name__ == "__main__":
    main()
