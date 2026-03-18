"""
Retriever main entry - unified external interface.
Member D's Query Worker calls this interface.
"""

import asyncio
import logging
from typing import Optional, List
from datetime import datetime
from .types import RetrievalRequest, CandidateList, Candidate, QueryMeta
from .interfaces import DenseRetriever, SparseRetriever, FilterRetriever
from .merger import Merger
from .version_filter import VersionFilter
from .policy_filter import PolicyFilter

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
        version_filter: Optional[VersionFilter] = None,
        policy_filter: Optional[PolicyFilter] = None,
    ):
        self.dense = dense
        self.sparse = sparse
        self.filter = filter
        self.merger = merger or Merger()
        self.version_filter = version_filter or VersionFilter()
        self.policy_filter = policy_filter or PolicyFilter()
    
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
            result = self._build_filter_only_result(filter_results, request)
        else:
            # Merge results with RRF
            result = self.merger.merge(
                dense_results=dense_results,
                sparse_results=sparse_results,
                filter_results=filter_results,
                request=request,
            )
        
        # Apply version filtering
        if request.visible_before_ts or request.version_at or request.bounded_staleness_ms:
            result.candidates = self.version_filter.filter(
                result.candidates,
                visible_before_ts=request.visible_before_ts,
                version_at=request.version_at,
                bounded_staleness_ms=request.bounded_staleness_ms,
            )
        
        # Apply policy filtering
        result.candidates = self.policy_filter.filter(
            result.candidates,
            requesting_agent_id=request.agent_id,
            requesting_tenant_id=request.tenant_id,
            exclude_quarantined=request.exclude_quarantined,
            exclude_unverified=request.exclude_unverified,
        )
        
        return result
    
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
    
    async def batch_retrieve(
        self,
        requests: List[RetrievalRequest],
    ) -> List[CandidateList]:
        """
        Batch retrieval for multiple requests.
        
        Optimizations:
        - Requests with same scope/agent_id can share filter results
        - Parallel execution of all requests
        
        Args:
            requests: List of retrieval requests
            
        Returns:
            List of CandidateList, one per request
        """
        if not requests:
            return []
        
        # Group requests by filter key for potential sharing
        # For now, execute all in parallel without sharing
        tasks = [self.retrieve(req) for req in requests]
        results = await asyncio.gather(*tasks, return_exceptions=True)
        
        # Convert exceptions to empty results
        final_results = []
        for i, result in enumerate(results):
            if isinstance(result, Exception):
                logger.warning(f"Batch request {i} failed: {result}")
                final_results.append(CandidateList(
                    candidates=[],
                    total_found=0,
                    retrieved_at=datetime.now(),
                    query_meta=QueryMeta(channels_used=[]),
                ))
            else:
                final_results.append(result)
        
        return final_results
