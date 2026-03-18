"""
Dense Retriever - Dense vector retrieval.
Connects to Milvus, executes ANN (Approximate Nearest Neighbor) search.
"""

from typing import List, Optional
from .interfaces import DenseRetriever
from .types import RetrievalRequest, Candidate


class MilvusDenseRetriever(DenseRetriever):
    """
    Milvus vector retrieval implementation.
    
    Retrieval targets: memories, events, artifacts
    Vector source: vector_ref and model_id in embeddings table
    """
    
    def __init__(
        self,
        host: str = "localhost",
        port: int = 19530,
        collection_name: str = "andb_embeddings",
    ):
        self.host = host
        self.port = port
        self.collection_name = collection_name
        self._client = None  # pymilvus client, lazy initialization
    
    async def search(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute vector similarity search.
        
        Steps:
        1. Get query_vector (call embedding service if not provided)
        2. Build Milvus search parameters
        3. Execute ANN search
        4. Convert results to Candidate list
        """
        # TODO: Week 2 implementation
        # 1. Connect to Milvus
        # 2. Get or compute query_vector
        # 3. Execute search
        # 4. Convert results
        
        raise NotImplementedError("Week 2 implementation")
    
    async def _get_query_vector(self, request: RetrievalRequest) -> List[float]:
        """Get query vector, call embedding service if not provided"""
        if request.query_vector:
            return request.query_vector
        
        # TODO: Call embedding service
        raise NotImplementedError("Need to connect embedding service")
    
    def _build_search_params(self, request: RetrievalRequest) -> dict:
        """Build Milvus search parameters"""
        params = {
            "metric_type": "IP",  # Inner product similarity
            "params": {"nprobe": 10},
        }
        return params
    
    def _build_filter_expr(self, request: RetrievalRequest) -> Optional[str]:
        """Build Milvus filter expression"""
        conditions = []
        
        if request.agent_id:
            conditions.append(f'agent_id == "{request.agent_id}"')
        if request.session_id:
            conditions.append(f'session_id == "{request.session_id}"')
        if request.scope:
            conditions.append(f'scope == "{request.scope}"')
        if request.memory_type:
            conditions.append(f'memory_type == "{request.memory_type}"')
        
        return " && ".join(conditions) if conditions else None
