"""
Retrieval module interface definitions.
All submodules are injected via interfaces, supporting pluggable replacement.
"""

from abc import ABC, abstractmethod
from typing import List
from .types import RetrievalRequest, Candidate


class DenseRetriever(ABC):
    """Dense vector retrieval interface - connects to Milvus"""
    
    @abstractmethod
    async def search(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute vector similarity search.
        
        Args:
            request: Retrieval request, uses query_text or query_vector
            
        Returns:
            Candidate list, each candidate's source_channels contains "dense"
        """
        pass


class SparseRetriever(ABC):
    """Sparse keyword retrieval interface - BM25 or Milvus sparse vector"""
    
    @abstractmethod
    async def search(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute keyword matching search.
        
        Use cases:
        - Tool name exact matching
        - Error codes, tags
        - Entity names
        
        Returns:
            Candidate list, each candidate's source_channels contains "sparse"
        """
        pass


class FilterRetriever(ABC):
    """Attribute filter retrieval interface - scalar field filtering"""
    
    @abstractmethod
    async def filter(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute attribute filtering.
        
        Supported filter fields:
        - agent_id, session_id
        - scope, memory_type
        - confidence, importance
        - time_range
        - quarantine_flag, visibility_policy (governance-aware)
        
        Returns:
            Candidate list, each candidate's source_channels contains "filter"
        """
        pass
