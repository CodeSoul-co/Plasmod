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
    
    Per design doc section 4.4, this runs FIRST to produce a whitelist
    of valid object_ids that Dense and Sparse search within.
    
    Supported filter fields:
    - tenant_id, workspace_id (isolation boundaries)
    - agent_id, session_id
    - scope (private / session / workspace / global)
    - memory_types (list of memory types)
    - object_types (list of object types)
    - confidence, importance
    - time_range (temporal bounds)
    - as_of_ts (time-travel)
    - min_version
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
        if request.memory_types:
            types_str = ', '.join(f'"{t}"' for t in request.memory_types)
            conditions.append(f'memory_type in [{types_str}]')
        if request.object_types:
            types_str = ', '.join(f'"{t}"' for t in request.object_types)
            conditions.append(f'object_type in [{types_str}]')
        
        # Confidence and importance thresholds
        if request.min_confidence > 0:
            conditions.append(f'confidence >= {request.min_confidence}')
        if request.min_importance > 0:
            conditions.append(f'importance >= {request.min_importance}')
        
        # Time range filtering (design doc: valid_from <= end AND valid_to >= start)
        if request.time_range:
            if request.time_range.from_ts:
                ts = int(request.time_range.from_ts.timestamp())
                conditions.append(f'valid_from <= {ts}')
            if request.time_range.to_ts:
                ts = int(request.time_range.to_ts.timestamp())
                conditions.append(f'valid_to >= {ts}')
        
        # Version constraints
        if request.as_of_ts:
            ts = int(request.as_of_ts.timestamp())
            conditions.append(f'valid_from <= {ts}')
        if request.min_version is not None:
            conditions.append(f'version >= {request.min_version}')
        
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
                freshness_score=entity.get("freshness_score", 1.0),
                source_event_ids=entity.get("source_event_ids", []),
                is_active=entity.get("is_active", True),
            )
            candidates.append(candidate)
        
        return candidates
    
    def close(self):
        """Close Milvus connection"""
        if self._client:
            self._client.close()
            self._client = None
