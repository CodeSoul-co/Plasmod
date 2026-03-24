import os
from typing import Optional, List, Dict, Any

import requests


class AndbClient:
    def __init__(self, base_url: str = "http://127.0.0.1:8080", timeout: int = None):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout or int(os.environ.get("ANDB_HTTP_TIMEOUT", "10"))

    def ingest_event(
        self,
        event_id: str,
        agent_id: str,
        session_id: str,
        payload: Dict[str, Any],
        workspace_id: Optional[str] = None,
        tenant_id: Optional[str] = None,
        event_type: str = "observation",
    ) -> dict:
        """Ingest an event into CogDB.
        
        Args:
            event_id: Unique identifier for this event
            agent_id: ID of the agent generating this event
            session_id: ID of the current session
            payload: Event payload data
            workspace_id: Optional workspace ID for multi-tenant isolation
            tenant_id: Optional tenant ID
            event_type: Type of event (observation, action, tool_call, etc.)
        """
        event = {
            "event_id": event_id,
            "agent_id": agent_id,
            "session_id": session_id,
            "event_type": event_type,
            "payload": payload,
        }
        if workspace_id:
            event["workspace_id"] = workspace_id
        if tenant_id:
            event["tenant_id"] = tenant_id
            
        resp = requests.post(
            f"{self.base_url}/v1/ingest/events",
            json=event,
            timeout=self.timeout,
        )
        resp.raise_for_status()
        return resp.json()

    def query(
        self,
        query_text: str,
        session_id: str = "",
        agent_id: str = "",
        top_k: int = 10,
        query_scope: str = "session",
        tenant_id: Optional[str] = None,
        workspace_id: Optional[str] = None,
        object_types: Optional[List[str]] = None,
        memory_types: Optional[List[str]] = None,
        relation_constraints: Optional[List[str]] = None,
        time_window: Optional[Dict[str, str]] = None,
        response_mode: str = "structured_evidence",
    ) -> dict:
        """Query CogDB for evidence.
        
        Args:
            query_text: The query string
            session_id: Session ID to scope the query
            agent_id: Agent ID to scope the query
            top_k: Maximum number of results to return
            query_scope: Scope of query (session, agent, global)
            tenant_id: Optional tenant ID for multi-tenant filtering
            workspace_id: Optional workspace ID for multi-tenant filtering
            object_types: Filter by object types (Memory, State, Agent, etc.)
            memory_types: Filter by memory types (episodic, semantic, etc.)
            relation_constraints: Edge type constraints
            time_window: Time range filter with 'from' and 'to' keys
            response_mode: structured_evidence or objects_only
            
        Returns:
            QueryResponse with objects, edges, provenance, versions, 
            applied_filters, and proof_trace
        """
        payload = {
            "query_text": query_text,
            "query_scope": query_scope,
            "session_id": session_id,
            "agent_id": agent_id,
            "top_k": top_k,
            "response_mode": response_mode,
            "time_window": time_window or {"from": "", "to": ""},
            "relation_constraints": relation_constraints or [],
        }
        if tenant_id:
            payload["tenant_id"] = tenant_id
        if workspace_id:
            payload["workspace_id"] = workspace_id
        if object_types:
            payload["object_types"] = object_types
        if memory_types:
            payload["memory_types"] = memory_types
            
        resp = requests.post(
            f"{self.base_url}/v1/query",
            json=payload,
            timeout=self.timeout,
        )
        resp.raise_for_status()
        return resp.json()
    
    def query_raw(self, payload: dict) -> dict:
        """Send a raw query payload to CogDB."""
        resp = requests.post(
            f"{self.base_url}/v1/query",
            json=payload,
            timeout=self.timeout,
        )
        resp.raise_for_status()
        return resp.json()
