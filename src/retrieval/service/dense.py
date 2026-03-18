"""
Dense Retriever - Dense vector retrieval.
Connects to Milvus, executes ANN (Approximate Nearest Neighbor) search.
"""

import logging
from typing import List, Optional
from pymilvus import MilvusClient

from .interfaces import DenseRetriever
from .types import RetrievalRequest, Candidate

logger = logging.getLogger(__name__)


class MilvusDenseRetriever(DenseRetriever):
    """
    Milvus vector retrieval implementation.
    
    Retrieval targets: memories, events, artifacts
    Vector source: vector_ref and model_id in embeddings table
    """
    
    def __init__(
        self,
        uri: str = "http://localhost:19530",
        collection_name: str = "andb_embeddings",
        vector_field: str = "vector",
        output_fields: Optional[List[str]] = None,
    ):
        self.uri = uri
        self.collection_name = collection_name
        self.vector_field = vector_field
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
    
    async def search(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute vector similarity search.
        
        Steps:
        1. Get query_vector (must be provided in request)
        2. Build Milvus search parameters
        3. Execute ANN search
        4. Convert results to Candidate list
        """
        if not request.query_vector:
            logger.warning("No query_vector provided, returning empty results")
            return []
        
        client = self._ensure_client()
        
        filter_expr = self._build_filter_expr(request)
        search_params = self._build_search_params(request)
        
        top_k = request.top_k if request.top_k > 0 else 20
        
        try:
            results = client.search(
                collection_name=self.collection_name,
                data=[request.query_vector],
                anns_field=self.vector_field,
                search_params=search_params,
                limit=top_k,
                filter=filter_expr,
                output_fields=self.output_fields,
            )
            
            return self._convert_results(results)
        except Exception as e:
            logger.error(f"Dense search failed: {e}")
            return []
    
    def _build_search_params(self, request: RetrievalRequest) -> dict:
        """Build Milvus search parameters"""
        return {
            "metric_type": "IP",
            "params": {"nprobe": 10},
        }
    
    def _build_filter_expr(self, request: RetrievalRequest) -> Optional[str]:
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
        
        return " and ".join(conditions) if conditions else None
    
    def _convert_results(self, results: List) -> List[Candidate]:
        """Convert Milvus search results to Candidate list"""
        candidates = []
        
        if not results or len(results) == 0:
            return candidates
        
        for hit in results[0]:
            entity = hit.get("entity", {})
            candidate = Candidate(
                object_id=entity.get("object_id", hit.get("id", "")),
                object_type=entity.get("object_type", "memory"),
                score=hit.get("distance", 0.0),
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
