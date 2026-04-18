from __future__ import annotations
import os
import requests
from typing import Optional, List, Dict, Any, Union


class PlasmodClient:
    """
    Python SDK client for Plasmod.

    Field names and JSON keys are kept in sync with schemas.QueryRequest
    and the /v1/ingest and /v1/query HTTP contracts in access/gateway.go.
    """

    def __init__(
        self,
        base_url: str = "http://127.0.0.1:8080",
        timeout: Optional[float] = None,
    ):
        self.base_url = base_url.rstrip("/")
        self._timeout = timeout or float(os.environ.get("ANDB_HTTP_TIMEOUT", "10"))

    # ── Ingest ────────────────────────────────────────────────────────────────

    def ingest_event(
        self,
        event_id: str,
        agent_id: str,
        session_id: str,
        event_type: str,
        payload: dict,
        *,
        tenant_id: str = "",
        workspace_id: str = "",
        **extra,
    ) -> dict:
        """
        Ingest a single event into Plasmod.

        Required fields mirror /v1/ingest body (access/gateway.go → schemas.Event):
          event_id, agent_id, session_id, event_type, payload

        Optional:
          tenant_id, workspace_id
        """
        body = {
            "event_id": event_id,
            "agent_id": agent_id,
            "session_id": session_id,
            "event_type": event_type,
            "payload": payload,
        }
        if tenant_id:
            body["tenant_id"] = tenant_id
        if workspace_id:
            body["workspace_id"] = workspace_id
        body.update(extra)

        resp = requests.post(
            f"{self.base_url}/v1/ingest/events",
            json=body,
            timeout=self._timeout,
        )
        resp.raise_for_status()
        return resp.json()

    # ── Query ─────────────────────────────────────────────────────────────────

    def query(
        self,
        query_text: str,
        *,
        query_scope: str = "global",
        session_id: str = "",
        agent_id: str = "",
        tenant_id: str = "",
        workspace_id: str = "",
        top_k: int = 10,
        object_types: List[str] | None = None,
        memory_types: List[str] | None = None,
        relation_constraints: List[str] | None = None,
        time_window: Optional[dict] = None,
        **extra,
    ) -> dict:
        """
        Query Plasmod for relevant objects.

        Field names mirror schemas.QueryRequest JSON tags (schemas/query.go):
          query_text, query_scope, session_id, agent_id, tenant_id,
          workspace_id, top_k, object_types, memory_types,
          relation_constraints, time_window
        """
        body: dict = {
            "query_text": query_text,
            "query_scope": query_scope,
            "top_k": top_k,
            "relation_constraints": relation_constraints or [],
        }
        if session_id:
            body["session_id"] = session_id
        if agent_id:
            body["agent_id"] = agent_id
        if tenant_id:
            body["tenant_id"] = tenant_id
        if workspace_id:
            body["workspace_id"] = workspace_id
        if object_types:
            body["object_types"] = object_types
        if memory_types:
            body["memory_types"] = memory_types
        if time_window:
            body["time_window"] = time_window
        body.update(extra)

        resp = requests.post(
            f"{self.base_url}/v1/query",
            json=body,
            timeout=self._timeout,
        )
        resp.raise_for_status()
        return resp.json()

    # ── Canonical CRUD helpers ────────────────────────────────────────────────

    def get(self, path: str) -> dict:
        resp = requests.get(f"{self.base_url}{path}", timeout=self._timeout)
        resp.raise_for_status()
        return resp.json()

    def post(self, path: str, body: dict) -> dict:
        resp = requests.post(
            f"{self.base_url}{path}", json=body, timeout=self._timeout
        )
        resp.raise_for_status()
        return resp.json()
