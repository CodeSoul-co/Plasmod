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

logger = logging.getLogger(__name__)


class Retriever:
    """
    Unified retrieval entry - aligned with week-1 design doc.
    
    Execution flow (design doc section 4.1):
    1. Filter runs FIRST -> produces whitelist of valid object_ids
    2. Dense + Sparse run in parallel WITHIN the whitelist
    3. Three-way results merged via RRF
    4. Safety filter (quarantine/ttl/visible_time/is_active) applied in Merger
    5. Reranking: final_score = rrf * importance * freshness * confidence
    
    Special modes:
    - enable_filter_only: skip Dense+Sparse, return filter results ordered by importance
    - for_graph: return top_k*2 with source_event_ids (for member C)
    
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
        Execute retrieval following design doc flow:
        Filter first -> Dense+Sparse in whitelist -> RRF merge -> safety filter -> rerank
        """
        dense_results: List[Candidate] = []
        sparse_results: List[Candidate] = []
        filter_results: List[Candidate] = []
        
        # Step 1: Filter runs first to produce whitelist
        if self.filter and request.enable_filter:
            filter_results = await self._safe_filter(self.filter.filter, request)
        
        # Handle filter-only mode: skip Dense+Sparse
        if request.enable_filter_only:
            return self._build_filter_only_result(filter_results, request)
        
        # Step 2: Dense + Sparse run in parallel (within whitelist context)
        tasks = []
        task_names = []
        
        if request.enable_dense and self.dense:
            tasks.append(self._safe_search(self.dense.search, request, "dense"))
            task_names.append("dense")
        
        if request.enable_sparse and self.sparse:
            tasks.append(self._safe_search(self.sparse.search, request, "sparse"))
            task_names.append("sparse")
        
        if tasks:
            results = await asyncio.gather(*tasks)
            for name, result in zip(task_names, results):
                if name == "dense":
                    dense_results = result
                elif name == "sparse":
                    sparse_results = result
        
        # Step 3-7: Merge (RRF + safety filter + rerank + truncate + seed marking)
        result = self.merger.merge(
            dense_results=dense_results,
            sparse_results=sparse_results,
            filter_results=filter_results,
            request=request,
        )
        
        return result
    
    def _build_filter_only_result(
        self,
        filter_results: List[Candidate],
        request: RetrievalRequest,
    ) -> CandidateList:
        """Build result for filter-only mode without RRF fusion"""
        # Set score = 1.0 * salience_weight, source_channels = ["filter"]
        for c in filter_results:
            c.score = 1.0 * c.salience_weight
            c.final_score = c.score * max(c.importance, 0.01) * max(c.freshness_score, 0.01) * max(c.confidence, 0.01)
            c.source_channels = ["filter"]
        
        # Order by importance descending, then confidence descending
        filter_results.sort(key=lambda c: (c.importance, c.confidence), reverse=True)
        
        # Truncate to top_k (or top_k*2 for graph mode)
        top_k = request.top_k if request.top_k > 0 else 10
        effective_k = top_k * 2 if request.for_graph else top_k
        candidates = filter_results[:effective_k]
        
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
        
        Parallel execution of all requests.
        Future optimization: share filter results for same scope/agent_id.
        """
        if not requests:
            return []
        
        tasks = [self.retrieve(req) for req in requests]
        results = await asyncio.gather(*tasks, return_exceptions=True)
        
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
