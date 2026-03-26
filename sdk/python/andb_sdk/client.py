import requests


class AndbClient:
    def __init__(self, base_url: str = "http://127.0.0.1:8080"):
        self.base_url = base_url.rstrip("/")

    def ingest_event(self, event: dict) -> dict:
        resp = requests.post(f"{self.base_url}/v1/ingest/events", json=event, timeout=10)
        resp.raise_for_status()
        return resp.json()

    def query(self, payload: dict) -> dict:
        resp = requests.post(f"{self.base_url}/v1/query", json=payload, timeout=10)
        resp.raise_for_status()
        return resp.json()
