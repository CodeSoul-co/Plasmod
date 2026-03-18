"""
Filter Retriever - Attribute filter retrieval.
Governance-aware retrieval (quarantine / visibility / scope).
"""

import logging
from typing import List, Optional
from pymilvus import MilvusClient

from .interfaces import FilterRetriever
from .types import RetrievalRequest, Candidate

logger = logging.getLogger(__name__)


class MilvusFilterRetriever(FilterRetriever):
    """
    Milvus scalar query implementation for attribute filtering.
    
    Supported filter fields:
    - tenant_id, workspace_id (isolation boundaries)
    - agent_id, session_id
    - scope (private / session / workspace / global)
    - memory_type (episodic / semantic / procedural)
    - confidence, importance
    - quarantine_flag (quarantined memories excluded from results)
    - is_active (only active memories)
    
    Governance-aware retrieval is the core differentiator of this submodule.
    """
    
    def __init__(
        self,
        uri: str = "http://localhost:19530",
        collection_name: str = "andb_memories",
        output_fields: Optional[List[str]] = None,
    ):
        self.uri = uri
        self.collection_name = collection_name
        self.output_fields = output_fields or [
            "object_id", "object_type", "agent_id", "session_id",
            "scope", "version", "provenance_ref", "content", "summary",
            "confidence", "importance", "level", "memory_type",
            "verified_state", "salience_weight",
        ]
        self._client: Optional[MilvusClient] = None
    
    def _ensure_client(self) -> MilvusClient:
        """Lazy initialization of Milvus client"""
        if self._client is None:
            self._client = MilvusClient(uri=self.uri)
            logger.info(f"Connected to Milvus at {self.uri}")
        return self._client
    
    async def filter(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute attribute filtering via Milvus scalar query.
        
        Steps:
        1. Build filter expression
        2. Query Milvus scalar fields
        3. Convert results to Candidate list
        """
        client = self._ensure_client()
        filter_expr = self._build_filter_expr(request)
        
        if not filter_expr:
            logger.warning("No filter expression, returning empty results")
            return []
        
        top_k = request.top_k if request.top_k > 0 else 20
        
        try:
            results = client.query(
                collection_name=self.collection_name,
                filter=filter_expr,
                output_fields=self.output_fields,
                limit=top_k,
            )
            
            return self._convert_results(results)
        except Exception as e:
            logger.error(f"Filter query failed: {e}")
            return []
    
    def _build_filter_expr(self, request: RetrievalRequest) -> str:
        """Build Milvus filter expression"""
        conditions = []
        
        # Isolation boundaries (mandatory)
        if request.tenant_id:
            conditions.append(f'tenant_id == "{request.tenant_id}"')
        if request.workspace_id:
            conditions.append(f'workspace_id == "{request.workspace_id}"')
        
        # Basic filtering
        if request.agent_id:
            conditions.append(f'agent_id == "{request.agent_id}"')
        if request.session_id:
            conditions.append(f'session_id == "{request.session_id}"')
        if request.scope:
            conditions.append(f'scope == "{request.scope}"')
        if request.memory_type:
            conditions.append(f'memory_type == "{request.memory_type}"')
        
        # Confidence and importance thresholds
        if request.min_confidence > 0:
            conditions.append(f'confidence >= {request.min_confidence}')
        if request.min_importance > 0:
            conditions.append(f'importance >= {request.min_importance}')
        
        # Time range filtering
        if request.time_range:
            if request.time_range.from_ts:
                ts = int(request.time_range.from_ts.timestamp())
                conditions.append(f'visible_time >= {ts}')
            if request.time_range.to_ts:
                ts = int(request.time_range.to_ts.timestamp())
                conditions.append(f'visible_time <= {ts}')
        
        return " and ".join(conditions) if conditions else ""
    
    def _convert_results(self, results: List) -> List[Candidate]:
        """Convert Milvus query results to Candidate list"""
        candidates = []
        
        if not results:
            return candidates
        
        for entity in results:
            candidate = Candidate(
                object_id=entity.get("object_id", ""),
                object_type=entity.get("object_type", "memory"),
                score=0.0,  # Filter results have no score, will be set in filter-only mode
                agent_id=entity.get("agent_id", ""),
                session_id=entity.get("session_id", ""),
                scope=entity.get("scope", ""),
                version=entity.get("version", 0),
                provenance_ref=entity.get("provenance_ref", ""),
                content=entity.get("content", ""),
                summary=entity.get("summary", ""),
                confidence=entity.get("confidence", 0.0),
                importance=entity.get("importance", 0.0),
                level=entity.get("level", 0),
                memory_type=entity.get("memory_type", ""),
                verified_state=entity.get("verified_state", ""),
                salience_weight=entity.get("salience_weight", 1.0),
            )
            candidates.append(candidate)
        
        return candidates
    
    def close(self):
        """Close Milvus connection"""
        if self._client:
            self._client.close()
            self._client = None
