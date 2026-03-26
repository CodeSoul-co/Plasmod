"""
Retriever main entry - unified external interface.
Member D's Query Worker calls this interface.

This is a THIN WRAPPER - all retrieval logic is in cpp/.
Python layer only does parameter conversion.
"""

import logging
import time
from typing import Optional, List, Callable, TypeVar
from datetime import datetime
import numpy as np

T = TypeVar("T")


def _retry_with_backoff(
    fn: Callable[[], T],
    max_retries: int = 3,
    base_delay: float = 0.1,
    max_delay: float = 2.0,
    exceptions: tuple = (TimeoutError, RuntimeError),
) -> T:
    """Exponential back-off retry wrapper (max 3 retries by default)."""
    last_exc: Exception | None = None
    for attempt in range(max_retries + 1):
        try:
            return fn()
        except exceptions as e:
            last_exc = e
            if attempt < max_retries:
                delay = min(base_delay * (2 ** attempt), max_delay)
                logger.warning(
                    "retrieve retry %d/%d after %.2fs: %s", attempt + 1, max_retries, delay, e
                )
                time.sleep(delay)
            else:
                logger.error("retrieve failed after %d retries: %s", max_retries, e)
    raise last_exc

from .types import (
    RetrievalRequest, CandidateList, Candidate, QueryMeta,
    cpp_available, cpp_version, _cpp, _CPP_AVAILABLE,
)

logger = logging.getLogger(__name__)


def _candidate_from_cpp(cpp_c) -> Candidate:
    """Convert C++ Candidate to Python Candidate."""
    return Candidate(
        object_id=cpp_c.object_id,
        object_type=cpp_c.object_type,
        score=cpp_c.rrf_score,
        final_score=cpp_c.final_score,
        dense_score=cpp_c.dense_score,
        sparse_score=cpp_c.sparse_score,
        rrf_score=cpp_c.rrf_score,
        importance=cpp_c.importance,
        freshness_score=cpp_c.freshness_score,
        confidence=cpp_c.confidence,
        is_seed=cpp_c.is_seed,
        seed_score=cpp_c.seed_score,
        source_channels=_get_source_channels(cpp_c),
    )


def _get_source_channels(cpp_c) -> List[str]:
    """Get source channels from C++ candidate flags."""
    channels = []
    if cpp_c.from_dense:
        channels.append("dense")
    if cpp_c.from_sparse:
        channels.append("sparse")
    if cpp_c.from_filter:
        channels.append("filter")
    return channels


def _result_from_cpp(cpp_result) -> CandidateList:
    """Convert C++ RetrievalResult to Python CandidateList."""
    candidates = [_candidate_from_cpp(c) for c in cpp_result.candidates]
    
    channels_used = []
    if cpp_result.dense_hits > 0:
        channels_used.append("dense")
    if cpp_result.sparse_hits > 0:
        channels_used.append("sparse")
    if cpp_result.filter_hits > 0:
        channels_used.append("filter")
    
    query_meta = QueryMeta(
        latency_ms=cpp_result.latency_ms,
        dense_hits=cpp_result.dense_hits,
        sparse_hits=cpp_result.sparse_hits,
        filter_hits=cpp_result.filter_hits,
        channels_used=channels_used,
    )
    
    return CandidateList(
        candidates=candidates,
        total_found=cpp_result.total_found,
        retrieved_at=datetime.now(),
        query_meta=query_meta,
    )


class Retriever:
    """
    Unified retrieval entry - THIN WRAPPER calling cpp/ module.
    
    All retrieval logic (Dense, Sparse, Filter, RRF merge, reranking) is in C++.
    This class only does parameter conversion.
    
    Execution flow (all in C++):
    1. Filter (BitsetView) applied during Search
    2. Dense (HNSW) + Sparse (SPARSE_INVERTED_INDEX) search
    3. RRF merge with k=60
    4. Reranking: final_score = rrf * importance * freshness * confidence
    5. Seed marking: final_score >= 0.7 -> is_seed=True
    
    Usage:
        retriever = Retriever()
        retriever.init(index_config, merge_config)
        retriever.build(vectors, sparse_vectors)
        result = retriever.retrieve(request)
    """
    
    def __init__(self):
        self._cpp_retriever = None
        self._ready = False
        
        if _CPP_AVAILABLE:
            self._cpp_retriever = _cpp.Retriever()
            logger.info(f"C++ retrieval module loaded: {cpp_version()}")
        else:
            logger.warning("C++ retrieval module not available")
    
    def init(
        self,
        index_type: str = "HNSW",
        metric_type: str = "IP",
        dim: int = 128,
        sparse_index_type: str = "SPARSE_INVERTED_INDEX",
        rrf_k: int = 60,
        seed_threshold: float = 0.7,
    ) -> bool:
        """Initialize the retriever with configurations."""
        if not _CPP_AVAILABLE:
            logger.error("C++ module not available, cannot init")
            return False
        
        # Create C++ config objects
        index_config = _cpp.IndexConfig()
        index_config.index_type = index_type
        index_config.metric_type = metric_type
        index_config.dim = dim
        
        merge_config = _cpp.MergeConfig()
        merge_config.rrf_k = rrf_k
        merge_config.seed_threshold = seed_threshold
        
        success = self._cpp_retriever.init(index_config, sparse_index_type, merge_config)
        if success:
            self._ready = True
            logger.info("Retriever initialized successfully")
        else:
            logger.error("Retriever initialization failed")
        
        return success
    
    def build(
        self,
        dense_vectors: np.ndarray,
        sparse_vectors: Optional[List[dict]] = None,
    ) -> bool:
        """Build indexes from vectors."""
        if not _CPP_AVAILABLE:
            logger.error("C++ module not available, cannot build")
            return False
        
        # Convert sparse vectors to C++ format
        cpp_sparse = []
        if sparse_vectors:
            for sv in sparse_vectors:
                cpp_sv = _cpp.SparseVector()
                cpp_sv.indices = list(sv.get("indices", []))
                cpp_sv.values = list(sv.get("values", []))
                cpp_sparse.append(cpp_sv)
        
        success = self._cpp_retriever.build(dense_vectors, cpp_sparse)
        if success:
            self._ready = True
            logger.info(f"Indexes built: {len(dense_vectors)} vectors")
        else:
            logger.error("Index build failed")
        
        return success
    
    def retrieve(self, request: RetrievalRequest) -> CandidateList:
        """
        Execute retrieval request.
        
        This is a THIN WRAPPER - all logic is in C++.
        """
        if not _CPP_AVAILABLE or not self._ready:
            logger.warning("Retriever not ready, returning empty result")
            return CandidateList(
                candidates=[],
                total_found=0,
                retrieved_at=datetime.now(),
                query_meta=QueryMeta(channels_used=[]),
            )
        
        # Prepare query vector
        query_vector = request.query_vector
        if query_vector is None:
            query_vector = np.array([], dtype=np.float32)
        elif isinstance(query_vector, list):
            query_vector = np.array(query_vector, dtype=np.float32)
        
        def _do_retrieve():
            return self._cpp_retriever.retrieve(
                query_vector=query_vector,
                query_text=request.query_text or "",
                top_k=request.top_k,
                enable_dense=request.enable_dense,
                enable_sparse=request.enable_sparse,
                for_graph=request.for_graph,
                filter_bitset=None,
            )

        try:
            cpp_result = _retry_with_backoff(_do_retrieve)
        except Exception as e:
            logger.error("retrieve failed after retries: %s", e)
            return CandidateList(
                candidates=[],
                total_found=0,
                retrieved_at=datetime.now(),
                query_meta=QueryMeta(channels_used=[]),
            )

        return _result_from_cpp(cpp_result)
    
    def benchmark_retrieve(self, request: RetrievalRequest) -> CandidateList:
        """
        Execute benchmark retrieval (no truncation).
        
        Returns all candidates with rrf_score populated for analysis.
        """
        if not _CPP_AVAILABLE or not self._ready:
            logger.warning("Retriever not ready, returning empty result")
            return CandidateList(
                candidates=[],
                total_found=0,
                retrieved_at=datetime.now(),
                query_meta=QueryMeta(channels_used=[]),
            )
        
        query_vector = request.query_vector
        if query_vector is None:
            query_vector = np.array([], dtype=np.float32)
        elif isinstance(query_vector, list):
            query_vector = np.array(query_vector, dtype=np.float32)
        
        cpp_result = self._cpp_retriever.benchmark_retrieve(
            query_vector=query_vector,
            query_text=request.query_text or "",
            enable_dense=request.enable_dense,
            enable_sparse=request.enable_sparse,
            filter_bitset=None,
        )
        
        return _result_from_cpp(cpp_result)
    
    def is_ready(self) -> bool:
        """Check if retriever is ready for search."""
        if not _CPP_AVAILABLE:
            return False
        return self._cpp_retriever.is_ready()
    
    @staticmethod
    def cpp_available() -> bool:
        """Check if C++ module is available."""
        return _CPP_AVAILABLE
    
    @staticmethod
    def version() -> str:
        """Get C++ module version."""
        return cpp_version()
