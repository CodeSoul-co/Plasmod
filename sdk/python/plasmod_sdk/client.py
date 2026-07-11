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
        base_url: Optional[str] = None,
        timeout: Optional[float] = None,
    ):
        # Unified dev :8080; split compose / Milvus-aligned API :19530.
        if base_url is None:
            base_url = os.environ.get(
                "PLASMOD_URI",
                os.environ.get("PLASMOD_BASE_URL", "http://127.0.0.1:19530"),
            )
        self.base_url = base_url.rstrip("/")
        self._timeout = timeout or float(
            os.environ.get("PLASMOD_HTTP_TIMEOUT", os.environ.get("ANDB_HTTP_TIMEOUT", "10"))
        )

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

    def ingest_vectors(
        self,
        vectors: List[List[float]],
        *,
        segment_id: str = "",
        object_ids: Optional[List[str]] = None,
        index_type: str = "",
        ivf_nlist: int = 0,
        ivf_nprobe: int = 0,
        ivf_m: int = 0,
        ivf_nbits: int = 0,
        ivf_sq_type: str = "",
    ) -> dict:
        """
        Ingest precomputed vectors into a warm segment (POST /v1/ingest/vectors).

        index_type: HNSW (default), IVF_FLAT, IVF_PQ, IVF_SQ8, or DISKANN.
        IVF_* fields map to ivf_nlist, ivf_nprobe, ivf_m, ivf_nbits, ivf_sq_type.
        """
        body: Dict[str, Any] = {"vectors": vectors}
        if segment_id:
            body["segment_id"] = segment_id
        if object_ids:
            body["object_ids"] = object_ids
        if index_type:
            body["index_type"] = index_type
        if ivf_nlist:
            body["ivf_nlist"] = ivf_nlist
        if ivf_nprobe:
            body["ivf_nprobe"] = ivf_nprobe
        if ivf_m:
            body["ivf_m"] = ivf_m
        if ivf_nbits:
            body["ivf_nbits"] = ivf_nbits
        if ivf_sq_type:
            body["ivf_sq_type"] = ivf_sq_type
        resp = requests.post(
            f"{self.base_url}/v1/ingest/vectors",
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

    # ── Consistency controls ─────────────────────────────────────────────────

    def get_consistency_mode(self) -> dict:
        """Return the active consistency mode and projection health."""
        return self.get("/v1/admin/consistency-mode")

    def set_consistency_mode(self, mode: str) -> dict:
        """Set the runtime default consistency mode and return its status."""
        return self.post("/v1/admin/consistency-mode", {"mode": mode})

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
