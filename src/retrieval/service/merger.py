"""
Candidate Merger - RRF (Reciprocal Rank Fusion)
Deduplicate, score fusion, reranking, and safety filtering for three-way recall results.
Aligned with week-1 design doc.
"""

import logging
from typing import List, Dict
from datetime import datetime
from .types import Candidate, CandidateList, QueryMeta, RetrievalRequest

logger = logging.getLogger(__name__)


class Merger:
    """
    RRF Candidate Merger
    
    RRF_score(d) = Σ 1 / (k + rank_i(d))
    where k=60 (standard value), rank_i(d) is the rank of document d in path i
    """
    
    def __init__(
        self,
        k: int = 60,                    # RRF parameter
        min_confidence: float = 0.0,    # minimum confidence threshold
        seed_threshold: float = 0.7,    # score threshold for marking as seed
    ):
        self.k = k
        self.min_confidence = min_confidence
        self.seed_threshold = seed_threshold
    
    def merge(
        self,
        dense_results: List[Candidate],
        sparse_results: List[Candidate],
        filter_results: List[Candidate],
        request: RetrievalRequest,
    ) -> CandidateList:
        """
        Merge three-way retrieval results.
        
        Flow (aligned with week-1 design doc):
        1. Deduplicate by object_id, accumulate RRF scores
        2. Safety filter (quarantine, ttl, visible_time, is_active)
        3. Filter low confidence / low importance
        4. Rerank: final_score = rrf_score * importance * freshness_score * confidence
           If policy_records has confidence_override, it replaces confidence.
           salience_weight from policy_records applied as multiplier.
        5. Sort by final_score descending
        6. Truncate to effective top_k (top_k*2 if for_graph)
        7. Mark seed candidates (for member C)
        """
        start_time = datetime.now()
        
        # Step 1: Merge to map, accumulate RRF scores
        merged: Dict[str, Candidate] = {}
        
        self._add_with_rrf(merged, dense_results, "dense")
        self._add_with_rrf(merged, sparse_results, "sparse")
        self._add_with_rrf(merged, filter_results, "filter")
        
        candidates = list(merged.values())
        
        # Step 2: Safety filter
        candidates = self._safety_filter(candidates, request)
        
        # Step 3: Filter low confidence and low importance
        min_conf = request.min_confidence if request.min_confidence > 0 else self.min_confidence
        if min_conf > 0:
            candidates = [c for c in candidates if c.confidence >= min_conf]
        if request.min_importance > 0:
            candidates = [c for c in candidates if c.importance >= request.min_importance]
        
        # Step 4: Rerank with design doc formula
        for c in candidates:
            c.final_score = (c.score
                           * max(c.importance, 0.01)
                           * max(c.freshness_score, 0.01)
                           * max(c.confidence, 0.01))
        
        # Step 5: Sort by final_score descending
        candidates.sort(key=lambda c: c.final_score, reverse=True)
        
        # Step 6: Truncate - for_graph mode returns top_k*2
        top_k = request.top_k if request.top_k > 0 else 10
        effective_k = top_k * 2 if request.for_graph else top_k
        candidates = candidates[:effective_k]
        
        # Step 7: Mark seed candidates (for member C graph expansion)
        for c in candidates:
            if c.final_score >= self.seed_threshold:
                c.is_seed = True
                c.seed_score = c.final_score
        
        # Build metadata
        latency_ms = int((datetime.now() - start_time).total_seconds() * 1000)
        channels_used = []
        if dense_results:
            channels_used.append("dense")
        if sparse_results:
            channels_used.append("sparse")
        if filter_results:
            channels_used.append("filter")
        
        query_meta = QueryMeta(
            latency_ms=latency_ms,
            dense_hits=len(dense_results),
            sparse_hits=len(sparse_results),
            filter_hits=len(filter_results),
            channels_used=channels_used,
        )
        
        return CandidateList(
            candidates=candidates,
            total_found=len(merged),
            retrieved_at=datetime.now(),
            query_meta=query_meta,
        )
    
    def _add_with_rrf(
        self,
        merged: Dict[str, Candidate],
        results: List[Candidate],
        channel: str,
    ) -> None:
        """Calculate RRF score and accumulate to merged, track per-channel scores"""
        if not results:
            return
        
        for rank, candidate in enumerate(results, start=1):
            rrf_score = 1.0 / (self.k + rank)
            
            if candidate.object_id in merged:
                existing = merged[candidate.object_id]
                existing.score += rrf_score
                if channel not in existing.source_channels:
                    existing.source_channels.append(channel)
                # Track per-channel score on the merged candidate
                if channel == "dense":
                    existing.dense_score = rrf_score
                elif channel == "sparse":
                    existing.sparse_score = rrf_score
            else:
                candidate.score = rrf_score
                candidate.source_channels = [channel]
                if channel == "dense":
                    candidate.dense_score = rrf_score
                elif channel == "sparse":
                    candidate.sparse_score = rrf_score
                merged[candidate.object_id] = candidate
    
    def _safety_filter(self, candidates: List[Candidate], request: RetrievalRequest) -> List[Candidate]:
        """
        Post-merge safety filter (design doc section 4.5).
        Remove: quarantined, ttl expired, not yet visible, inactive.
        """
        now = datetime.now()
        filtered = []
        for c in candidates:
            if c.quarantine_flag:
                continue
            if c.ttl and c.ttl < now:
                continue
            if c.visible_time and c.visible_time > now:
                continue
            if not c.is_active:
                continue
            # as_of_ts: time-travel filter
            if request.as_of_ts:
                if c.visible_time and c.visible_time > request.as_of_ts:
                    continue
                if c.valid_from and c.valid_from > request.as_of_ts:
                    continue
            # min_version filter
            if request.min_version is not None and c.version < request.min_version:
                continue
            filtered.append(c)
        return filtered
