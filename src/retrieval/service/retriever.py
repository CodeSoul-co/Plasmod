# Copyright 2024 CogDB Authors
# SPDX-License-Identifier: Apache-2.0
#
# Retriever - thin wrapper calling cpp/ pybind11 module.
# NO retrieval logic here - all logic is in C++.

import logging
from typing import Optional
import numpy as np

from .types import (
    RetrievalRequest,
    RetrievalResult,
    IndexConfig,
    MergeConfig,
    result_from_cpp,
    index_config_to_cpp,
    merge_config_to_cpp,
)

logger = logging.getLogger(__name__)

# Try to import C++ module, fall back to stub if not available
try:
    import andb_retrieval as _cpp
    _CPP_AVAILABLE = True
    logger.info("C++ retrieval module loaded successfully")
except ImportError:
    _cpp = None
    _CPP_AVAILABLE = False
    logger.warning("C++ retrieval module not available, using stub implementation")


class Retriever:
    """
    Retriever - thin wrapper calling cpp/ pybind11 module.
    
    All retrieval logic (dense, sparse, filter, RRF merge, reranking) is in C++.
    This class only does parameter conversion.
    """
    
    def __init__(
        self,
        index_config: Optional[IndexConfig] = None,
        merge_config: Optional[MergeConfig] = None,
        sparse_index_type: str = "SPARSE_INVERTED_INDEX",
    ):
        self._index_config = index_config or IndexConfig()
        self._merge_config = merge_config or MergeConfig()
        self._sparse_index_type = sparse_index_type
        self._cpp_retriever = None
        self._ready = False
        
        if _CPP_AVAILABLE:
            self._cpp_retriever = _cpp.Retriever()
    
    def init(
        self,
        index_config: Optional[IndexConfig] = None,
        merge_config: Optional[MergeConfig] = None,
        sparse_index_type: Optional[str] = None,
    ) -> bool:
        """Initialize the retriever with configurations."""
        if index_config:
            self._index_config = index_config
        if merge_config:
            self._merge_config = merge_config
        if sparse_index_type:
            self._sparse_index_type = sparse_index_type
        
        if not _CPP_AVAILABLE:
            logger.warning("C++ module not available, init skipped")
            return False
        
        cpp_index_config = index_config_to_cpp(self._index_config, _cpp)
        cpp_merge_config = merge_config_to_cpp(self._merge_config, _cpp)
        
        success = self._cpp_retriever.init(
            cpp_index_config,
            self._sparse_index_type,
            cpp_merge_config,
        )
        
        if success:
            self._ready = True
            logger.info("Retriever initialized successfully")
        else:
            logger.error("Retriever initialization failed")
        
        return success
    
    def build(
        self,
        dense_vectors: np.ndarray,
        sparse_vectors: Optional[list] = None,
    ) -> bool:
        """Build indexes from vectors."""
        if not _CPP_AVAILABLE:
            logger.warning("C++ module not available, build skipped")
            return False
        
        if sparse_vectors is None:
            sparse_vectors = []
        
        # Convert sparse vectors to C++ format
        cpp_sparse = []
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
    
    def retrieve(self, request: RetrievalRequest) -> RetrievalResult:
        """
        Execute retrieval request.
        
        This is a thin wrapper - all logic is in C++.
        """
        if not _CPP_AVAILABLE or not self._ready:
            logger.warning("Retriever not ready, returning empty result")
            return RetrievalResult()
        
        # Prepare query vector
        query_vector = request.query_vector
        if query_vector is None:
            query_vector = np.array([], dtype=np.float32)
        elif not isinstance(query_vector, np.ndarray):
            query_vector = np.array(query_vector, dtype=np.float32)
        
        # Prepare filter bitset
        filter_bitset = request.filter_bitset
        
        # Call C++ retriever
        cpp_result = self._cpp_retriever.retrieve(
            query_vector=query_vector,
            query_text=request.query_text,
            top_k=request.top_k,
            enable_dense=request.enable_dense,
            enable_sparse=request.enable_sparse,
            for_graph=request.for_graph,
            filter_bitset=filter_bitset,
        )
        
        # Convert result to Python
        return result_from_cpp(cpp_result)
    
    def benchmark_retrieve(self, request: RetrievalRequest) -> RetrievalResult:
        """
        Execute benchmark retrieval (no truncation).
        
        Returns all candidates with rrf_score populated.
        """
        if not _CPP_AVAILABLE or not self._ready:
            logger.warning("Retriever not ready, returning empty result")
            return RetrievalResult()
        
        query_vector = request.query_vector
        if query_vector is None:
            query_vector = np.array([], dtype=np.float32)
        elif not isinstance(query_vector, np.ndarray):
            query_vector = np.array(query_vector, dtype=np.float32)
        
        filter_bitset = request.filter_bitset
        
        cpp_result = self._cpp_retriever.benchmark_retrieve(
            query_vector=query_vector,
            query_text=request.query_text,
            enable_dense=request.enable_dense,
            enable_sparse=request.enable_sparse,
            filter_bitset=filter_bitset,
        )
        
        return result_from_cpp(cpp_result)
    
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
        if _CPP_AVAILABLE:
            return _cpp.version()
        return "cpp-not-available"
