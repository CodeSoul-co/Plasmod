"""
Sparse Retriever - Sparse keyword retrieval.
Based on inverted index for exact vocabulary matching.
"""

from typing import List
from .interfaces import SparseRetriever
from .types import RetrievalRequest, Candidate


class BM25SparseRetriever(SparseRetriever):
    """
    BM25 keyword retrieval implementation.
    
    Use cases:
    - Tool name (tool_name) exact matching
    - Error codes, tags, entity names
    - Reranking in scenarios where semantics are similar but vocabulary differs
    """
    
    def __init__(self):
        self._index = None  # BM25 index, lazy initialization
    
    async def search(self, request: RetrievalRequest) -> List[Candidate]:
        """
        Execute keyword matching search.
        
        Steps:
        1. Tokenize, extract keywords
        2. Search in sparse index
        3. Return Candidate list
        """
        # TODO: Week 2 implementation
        raise NotImplementedError("Week 2 implementation")
    
    def _tokenize(self, text: str) -> List[str]:
        """Tokenize text"""
        return text.split()
