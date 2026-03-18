"""
Retriever main entry - unified external interface.
Member D's Query Worker calls this interface.
"""

import asyncio
import logging
from typing import Optional, List
from .types import RetrievalRequest, CandidateList, Candidate
from .interfaces import DenseRetriever, SparseRetriever, FilterRetriever
from .merger import Merger

logger = logging.getLogger(__name__)


class Retriever:
    """
    Unified retrieval entry.
    
    Responsibilities:
    1. Parallel call three-way retrieval (Dense / Sparse / Filter)
    2. Call Merger to merge results
    3. Handle degradation logic when single path fails
    
    Usage:
        retriever = Retriever(dense, sparse, filter, merger)
        result = await retriever.retrieve(request)
    """
    
    def __init__(
        self,
        dense: Optional[DenseRetriever] = None,
        sparse: Optional[SparseRetriever] = None,
        filter: Optional[FilterRetriever] = None,
        merger: Optional[Merger] = None,
    ):
        self.dense = dense
        self.sparse = sparse
        self.filter = filter
        self.merger = merger or Merger()
    
    async def retrieve(self, request: RetrievalRequest) -> CandidateList:
        """
        Execute retrieval.
        
        Parallel call three-way retrieval, failure of any path does not affect others.
        """
        dense_results: List[Candidate] = []
        sparse_results: List[Candidate] = []
        filter_results: List[Candidate] = []
        
        # Build task list
        tasks = []
        task_names = []
        
        if request.enable_dense and self.dense and not request.enable_filter_only:
            tasks.append(self._safe_search(self.dense.search, request, "dense"))
            task_names.append("dense")
        
        if request.enable_sparse and self.sparse and not request.enable_filter_only:
            tasks.append(self._safe_search(self.sparse.search, request, "sparse"))
            task_names.append("sparse")
        
        if self.filter:
            tasks.append(self._safe_filter(self.filter.filter, request))
            task_names.append("filter")
        
        # Execute in parallel
        if tasks:
            results = await asyncio.gather(*tasks)
            
            for name, result in zip(task_names, results):
                if name == "dense":
                    dense_results = result
                elif name == "sparse":
                    sparse_results = result
                elif name == "filter":
                    filter_results = result
        
        # Handle filter-only mode: skip RRF, use importance/confidence ordering
        if request.enable_filter_only:
            return self._build_filter_only_result(filter_results, request)
        
        # Merge results with RRF
        return self.merger.merge(
            dense_results=dense_results,
            sparse_results=sparse_results,
            filter_results=filter_results,
            request=request,
        )
    
    def _build_filter_only_result(
        self,
        filter_results: List[Candidate],
        request: RetrievalRequest,
    ) -> CandidateList:
        """Build result for filter-only mode without RRF fusion"""
        from datetime import datetime
        from .types import CandidateList, QueryMeta
        
        # Set score = 1.0 * salience_weight, source_channels = ["filter"]
        for c in filter_results:
            c.score = 1.0 * c.salience_weight
            c.source_channels = ["filter"]
        
        # Order by importance descending, then confidence descending
        filter_results.sort(key=lambda c: (c.importance, c.confidence), reverse=True)
        
        # Truncate to top_k
        top_k = request.top_k if request.top_k > 0 else 20
        candidates = filter_results[:top_k]
        
        query_meta = QueryMeta(
            latency_ms=0,
            dense_hits=0,
            sparse_hits=0,
            filter_hits=len(filter_results),
            channels_used=["filter"],
        )
        
        return CandidateList(
            candidates=candidates,
            total_found=len(filter_results),
            retrieved_at=datetime.now(),
            query_meta=query_meta,
        )
    
    async def _safe_search(self, search_fn, request: RetrievalRequest, name: str) -> List[Candidate]:
        """Safely execute search, catch exceptions and return empty list"""
        try:
            return await search_fn(request)
        except Exception as e:
            logger.warning(f"{name} retrieval failed: {e}")
            return []
    
    async def _safe_filter(self, filter_fn, request: RetrievalRequest) -> List[Candidate]:
        """Safely execute filter, catch exceptions and return empty list"""
        try:
            return await filter_fn(request)
        except Exception as e:
            logger.warning(f"filter retrieval failed: {e}")
            return []
