"""
Sparse Retriever - Sparse keyword retrieval.
Based on Milvus sparse vector for BM25-style matching.
"""

import logging
from typing import List, Dict, Optional
from pymilvus import MilvusClient

from .interfaces import SparseRetriever
from .types import RetrievalRequest, Candidate

logger = logging.getLogger(__name__)


class MilvusSparseRetriever(SparseRetriever):
    """
    Milvus sparse vector retrieval implementation.
    
    Use cases:
    - Tool name (tool_name) exact matching
    - Error codes, tags, entity names
    - Reranking in scenarios where semantics are similar but vocabulary differs
    
    Requires a collection with sparse_vector field indexed.
    """
    
    def __init__(
        self,
        uri: str = "http://localhost:19530",
        collection_name: str = "andb_embeddings",
        sparse_field: str = "sparse_vector",
        output_fields: Optional[List[str]] = None,
    ):
        self.uri = uri
        self.collection_name = collection_name
        self.sparse_field = sparse_field
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
        Execute sparse vector search.
        
        Steps:
        1. Convert query_text to sparse vector (BM25 weights)
        2. Execute sparse search in Milvus
        3. Convert results to Candidate list
        """
        if not request.query_text:
            logger.warning("No query_text provided, returning empty results")
            return []
        
        # Convert query text to sparse vector
        sparse_vector = self._text_to_sparse_vector(request.query_text)
        if not sparse_vector:
            return []
        
        client = self._ensure_client()
        filter_expr = self._build_filter_expr(request)
        top_k = request.top_k if request.top_k > 0 else 20
        
        try:
            results = client.search(
                collection_name=self.collection_name,
                data=[sparse_vector],
                anns_field=self.sparse_field,
                search_params={"metric_type": "IP"},
                limit=top_k,
                filter=filter_expr,
                output_fields=self.output_fields,
            )
            
            return self._convert_results(results)
        except Exception as e:
            logger.error(f"Sparse search failed: {e}")
            return []
    
    def _text_to_sparse_vector(self, text: str) -> Dict[int, float]:
        """
        Convert text to sparse vector representation.
        
        Simple implementation: hash each token to an index, weight by frequency.
        In production, use a proper BM25 encoder or Milvus built-in BM25.
        """
        tokens = self._tokenize(text)
        if not tokens:
            return {}
        
        # Count token frequencies
        token_counts: Dict[str, int] = {}
        for token in tokens:
            token_counts[token] = token_counts.get(token, 0) + 1
        
        # Convert to sparse vector: deterministic hash -> weight
        sparse_vec: Dict[int, float] = {}
        for token, count in token_counts.items():
            # Use deterministic hash (not Python's hash which varies by process)
            idx = self._deterministic_hash(token) % 30000
            weight = count / len(tokens)  # simple TF weight
            sparse_vec[idx] = weight
        
        return sparse_vec
    
    def _deterministic_hash(self, s: str) -> int:
        """Deterministic hash function using FNV-1a algorithm"""
        h = 2166136261  # FNV offset basis
        for c in s.encode('utf-8'):
            h ^= c
            h = (h * 16777619) & 0xFFFFFFFF  # FNV prime, keep 32-bit
        return h
    
    def _tokenize(self, text: str) -> List[str]:
        """Simple whitespace tokenization with lowercasing"""
        return text.lower().split()
    
    def _build_filter_expr(self, request: RetrievalRequest) -> Optional[str]:
        """Build Milvus filter expression"""
        conditions = []
        
        if request.tenant_id:
            conditions.append(f'tenant_id == "{request.tenant_id}"')
        if request.workspace_id:
            conditions.append(f'workspace_id == "{request.workspace_id}"')
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
