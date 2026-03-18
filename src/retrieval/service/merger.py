"""
Candidate Merger - RRF (Reciprocal Rank Fusion)
Deduplicate and score fusion for three-way recall results.
"""

from typing import List, Dict
from datetime import datetime
from .types import Candidate, CandidateList, QueryMeta, RetrievalRequest


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
        
        Steps:
        1. Calculate RRF score for each path
        2. Deduplicate by object_id, accumulate scores
        3. Filter low confidence candidates
        4. Sort by score descending
        5. Truncate to top_k
        6. Mark seed candidates (for member C)
        """
        start_time = datetime.now()
        
        # Merge to map, accumulate RRF scores
        merged: Dict[str, Candidate] = {}
        
        self._add_with_rrf(merged, dense_results, "dense")
        self._add_with_rrf(merged, sparse_results, "sparse")
        self._add_with_rrf(merged, filter_results, "filter")
        
        # Convert to list
        candidates = list(merged.values())
        
        # Filter low confidence
        min_conf = request.min_confidence if request.min_confidence > 0 else self.min_confidence
        candidates = [c for c in candidates if c.confidence >= min_conf]
        
        # Sort by score descending
        candidates.sort(key=lambda c: c.score, reverse=True)
        
        # Truncate to top_k
        top_k = request.top_k if request.top_k > 0 else 20
        candidates = candidates[:top_k]
        
        # Apply salience reranking: final_score = rrf_score * salience_weight
        for c in candidates:
            c.score = c.score * c.salience_weight
        
        # Re-sort after salience reranking
        candidates.sort(key=lambda c: c.score, reverse=True)
        
        # Mark seed (candidates with score above threshold as graph expansion starting points)
        for c in candidates:
            if c.score >= self.seed_threshold:
                c.is_seed = True
                c.seed_score = c.score
        
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
        """Calculate RRF score and accumulate to merged"""
        if not results:
            return
        
        for rank, candidate in enumerate(results, start=1):
            rrf_score = 1.0 / (self.k + rank)
            
            if candidate.object_id in merged:
                # Already exists, accumulate score, merge sources
                existing = merged[candidate.object_id]
                existing.score += rrf_score
                if channel not in existing.source_channels:
                    existing.source_channels.append(channel)
            else:
                # New candidate
                candidate.score = rrf_score
                candidate.source_channels = [channel]
                merged[candidate.object_id] = candidate
