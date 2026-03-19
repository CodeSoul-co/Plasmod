import time

from andb_sdk import AndbClient


def main() -> None:
    c = AndbClient()
    t0 = time.time()
    for i in range(20):
        c.query(
            {
                "query_text": f"q{i}",
                "query_scope": "workspace",
                "session_id": "sess_a",
                "agent_id": "agent_a",
                "top_k": 5,
                "time_window": {"from": "2026-01-01T00:00:00Z", "to": "2026-12-31T00:00:00Z"},
                "relation_constraints": [],
                "response_mode": "structured_evidence",
            }
        )
    print({"queries": 20, "elapsed_sec": round(time.time() - t0, 4)})


if __name__ == "__main__":
    main()
