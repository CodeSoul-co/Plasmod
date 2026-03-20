# Copyright 2024 CogDB Authors
# SPDX-License-Identifier: Apache-2.0
#
# Python type definitions mirroring cpp/include/andb/types.h
# These are thin wrappers - actual logic is in C++.

from dataclasses import dataclass, field
from typing import List, Optional
import numpy as np


@dataclass
class Candidate:
    """Retrieval candidate with all scoring fields."""
    object_id: str = ""
    object_type: str = ""
    
    # Scores
    dense_score: float = 0.0
    sparse_score: float = 0.0
    rrf_score: float = 0.0
    final_score: float = 0.0
    
    # Reranking factors
    importance: float = 0.0
    freshness_score: float = 1.0
    confidence: float = 0.0
    
    # Seed marking for graph expansion
    is_seed: bool = False
    seed_score: float = 0.0
    
    # Source channels
    from_dense: bool = False
    from_sparse: bool = False
    from_filter: bool = False
    
    # Internal index ID
    internal_id: int = -1


@dataclass
class RetrievalResult:
    """Result from retrieval operation."""
    candidates: List[Candidate] = field(default_factory=list)
    total_found: int = 0
    
    # Per-path hit counts
    dense_hits: int = 0
    sparse_hits: int = 0
    filter_hits: int = 0
    
    # Latency in milliseconds
    latency_ms: int = 0


@dataclass
class IndexConfig:
    """Index configuration for dense retriever."""
    index_type: str = "HNSW"
    metric_type: str = "IP"
    dim: int = 0
    
    # HNSW specific
    hnsw_m: int = 16
    hnsw_ef_construction: int = 200
    hnsw_ef_search: int = 100
    
    # IVF specific
    ivf_nlist: int = 128
    ivf_nprobe: int = 8


@dataclass
class MergeConfig:
    """RRF merge configuration."""
    rrf_k: int = 60
    seed_threshold: float = 0.7


@dataclass
class RetrievalRequest:
    """Request parameters for retrieval."""
    # Query vector (numpy array)
    query_vector: Optional[np.ndarray] = None
    
    # Query text for sparse search
    query_text: str = ""
    
    # Retrieval control
    top_k: int = 10
    enable_dense: bool = True
    enable_sparse: bool = True
    for_graph: bool = False
    
    # Filter bitset (numpy uint8 array, bit=1 means filtered out)
    filter_bitset: Optional[np.ndarray] = None


def candidate_from_cpp(cpp_candidate) -> Candidate:
    """Convert C++ Candidate to Python Candidate."""
    return Candidate(
        object_id=cpp_candidate.object_id,
        object_type=cpp_candidate.object_type,
        dense_score=cpp_candidate.dense_score,
        sparse_score=cpp_candidate.sparse_score,
        rrf_score=cpp_candidate.rrf_score,
        final_score=cpp_candidate.final_score,
        importance=cpp_candidate.importance,
        freshness_score=cpp_candidate.freshness_score,
        confidence=cpp_candidate.confidence,
        is_seed=cpp_candidate.is_seed,
        seed_score=cpp_candidate.seed_score,
        from_dense=cpp_candidate.from_dense,
        from_sparse=cpp_candidate.from_sparse,
        from_filter=cpp_candidate.from_filter,
        internal_id=cpp_candidate.internal_id,
    )


def result_from_cpp(cpp_result) -> RetrievalResult:
    """Convert C++ RetrievalResult to Python RetrievalResult."""
    return RetrievalResult(
        candidates=[candidate_from_cpp(c) for c in cpp_result.candidates],
        total_found=cpp_result.total_found,
        dense_hits=cpp_result.dense_hits,
        sparse_hits=cpp_result.sparse_hits,
        filter_hits=cpp_result.filter_hits,
        latency_ms=cpp_result.latency_ms,
    )


def index_config_to_cpp(config: IndexConfig, cpp_module):
    """Convert Python IndexConfig to C++ IndexConfig."""
    cpp_config = cpp_module.IndexConfig()
    cpp_config.index_type = config.index_type
    cpp_config.metric_type = config.metric_type
    cpp_config.dim = config.dim
    cpp_config.hnsw_m = config.hnsw_m
    cpp_config.hnsw_ef_construction = config.hnsw_ef_construction
    cpp_config.hnsw_ef_search = config.hnsw_ef_search
    cpp_config.ivf_nlist = config.ivf_nlist
    cpp_config.ivf_nprobe = config.ivf_nprobe
    return cpp_config


def merge_config_to_cpp(config: MergeConfig, cpp_module):
    """Convert Python MergeConfig to C++ MergeConfig."""
    cpp_config = cpp_module.MergeConfig()
    cpp_config.rrf_k = config.rrf_k
    cpp_config.seed_threshold = config.seed_threshold
    return cpp_config
