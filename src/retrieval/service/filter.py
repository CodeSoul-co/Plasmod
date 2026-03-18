"""
Filter Retriever - Attribute filter retrieval.
Governance-aware retrieval (quarantine / visibility / scope).
"""

from typing import List, Optional
from .interfaces import FilterRetriever
from .types import RetrievalRequest, Candidate


class AttributeFilterRetriever(FilterRetriever):
    """
    Attribute filter retrieval implementation.
    
    Supported filter fields:
    - agent_id, session_id
    - scope (private / session / workspace / global)
    - memory_type (episodic / semantic / procedural)
    - confidence, importance
    - time_range (valid_from / valid_to)
    - quarantine_flag (quarantined memories excluded from results)
    - visibility_policy (ACL check)
    
    Governance-aware retrieval is the core differentiator of this submodule.
    """
    
    def __init__(
        self,
        host: str = "localhost",
        port: int = 19530,
        collection_name: str = "andb_memories",
    ):
        self.host = host
        self.port = port
        self.collection_name = collection_name
        self._client = None
    
    async def filter(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute attribute filtering.
        
        Steps:
        1. Build filter expression
        2. Query Milvus scalar fields
        3. Filter out quarantine_flag=True objects
        4. Return Candidate list
        """
        # TODO: Week 2 implementation
        raise NotImplementedError("Week 2 implementation")
    
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
        
        # Time range
        if request.time_range:
            if request.time_range.from_ts:
                ts = int(request.time_range.from_ts.timestamp())
                conditions.append(f'visible_time >= {ts}')
            if request.time_range.to_ts:
                ts = int(request.time_range.to_ts.timestamp())
                conditions.append(f'visible_time <= {ts}')
        
        # Governance filtering: exclude quarantined memories
        conditions.append('quarantine_flag == false')
        
        # Only return active memories
        conditions.append('is_active == true')
        
        return " && ".join(conditions) if conditions else ""
