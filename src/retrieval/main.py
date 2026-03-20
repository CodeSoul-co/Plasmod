#!/usr/bin/env python3
# Copyright 2024 CogDB Authors
# SPDX-License-Identifier: Apache-2.0
#
# Retrieval Service Entry Point
#
# Usage:
#     python -m src.retrieval.main --dev          # Run with debug output
#     python -m src.retrieval.main --test         # Run basic test
#     python -m src.retrieval.main --help         # Show help

import argparse
import logging
import sys
import time
from typing import Optional

import numpy as np

from .service.retriever import Retriever
from .service.types import (
    RetrievalRequest,
    RetrievalResult,
    IndexConfig,
    MergeConfig,
)

logger = logging.getLogger(__name__)


class RetrievalService:
    """
    Retrieval service facade.
    
    This is a thin wrapper around the C++ retrieval module.
    All retrieval logic is in cpp/, this layer only does parameter conversion.
    """
    
    def __init__(
        self,
        index_config: Optional[IndexConfig] = None,
        merge_config: Optional[MergeConfig] = None,
        sparse_index_type: str = "SPARSE_INVERTED_INDEX",
        dev_mode: bool = False,
    ):
        self.dev_mode = dev_mode
        self._index_config = index_config or IndexConfig()
        self._merge_config = merge_config or MergeConfig()
        self._sparse_index_type = sparse_index_type
        
        # Configure logging
        log_level = logging.DEBUG if dev_mode else logging.INFO
        logging.basicConfig(
            level=log_level,
            format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        )
        
        # Initialize retriever (thin wrapper to C++)
        self.retriever = Retriever(
            index_config=self._index_config,
            merge_config=self._merge_config,
            sparse_index_type=self._sparse_index_type,
        )
        
        if dev_mode:
            self._log_init_state()
    
    def _log_init_state(self):
        """Log initialization state in dev mode."""
        logger.debug("=" * 60)
        logger.debug("RetrievalService initialized in dev mode")
        logger.debug("=" * 60)
        logger.debug(f"C++ module available: {Retriever.cpp_available()}")
        logger.debug(f"C++ module version: {Retriever.version()}")
        logger.debug(f"Index config:")
        logger.debug(f"  index_type: {self._index_config.index_type}")
        logger.debug(f"  metric_type: {self._index_config.metric_type}")
        logger.debug(f"  dim: {self._index_config.dim}")
        logger.debug(f"  hnsw_m: {self._index_config.hnsw_m}")
        logger.debug(f"  hnsw_ef_construction: {self._index_config.hnsw_ef_construction}")
        logger.debug(f"  hnsw_ef_search: {self._index_config.hnsw_ef_search}")
        logger.debug(f"Merge config:")
        logger.debug(f"  rrf_k: {self._merge_config.rrf_k}")
        logger.debug(f"  seed_threshold: {self._merge_config.seed_threshold}")
        logger.debug(f"Sparse index type: {self._sparse_index_type}")
        logger.debug("=" * 60)
    
    def init(self) -> bool:
        """Initialize the retriever."""
        return self.retriever.init(
            index_config=self._index_config,
            merge_config=self._merge_config,
            sparse_index_type=self._sparse_index_type,
        )
    
    def build(
        self,
        dense_vectors: np.ndarray,
        sparse_vectors: Optional[list] = None,
    ) -> bool:
        """Build indexes from vectors."""
        if self.dev_mode:
            logger.debug(f"Building indexes: {len(dense_vectors)} vectors")
            if dense_vectors.ndim == 2:
                logger.debug(f"  Vector dim: {dense_vectors.shape[1]}")
        
        return self.retriever.build(dense_vectors, sparse_vectors)
    
    def retrieve(self, request: RetrievalRequest) -> RetrievalResult:
        """Execute retrieval request."""
        if self.dev_mode:
            self._log_request(request)
        
        start_time = time.time()
        result = self.retriever.retrieve(request)
        elapsed_ms = (time.time() - start_time) * 1000
        
        if self.dev_mode:
            self._log_result(result, elapsed_ms)
        
        return result
    
    def benchmark_retrieve(self, request: RetrievalRequest) -> RetrievalResult:
        """Execute benchmark retrieval (no truncation)."""
        if self.dev_mode:
            logger.debug("[BENCHMARK MODE]")
            self._log_request(request)
        
        start_time = time.time()
        result = self.retriever.benchmark_retrieve(request)
        elapsed_ms = (time.time() - start_time) * 1000
        
        if self.dev_mode:
            self._log_result(result, elapsed_ms, benchmark=True)
        
        return result
    
    def _log_request(self, request: RetrievalRequest):
        """Log request details in dev mode."""
        logger.debug("-" * 40)
        logger.debug("Retrieval Request:")
        logger.debug(f"  top_k: {request.top_k}")
        logger.debug(f"  enable_dense: {request.enable_dense}")
        logger.debug(f"  enable_sparse: {request.enable_sparse}")
        logger.debug(f"  for_graph: {request.for_graph}")
        if request.query_vector is not None:
            logger.debug(f"  query_vector: shape={request.query_vector.shape}")
        if request.query_text:
            logger.debug(f"  query_text: '{request.query_text[:50]}...'")
        if request.filter_bitset is not None:
            logger.debug(f"  filter_bitset: {len(request.filter_bitset)} bytes")
    
    def _log_result(self, result: RetrievalResult, elapsed_ms: float, benchmark: bool = False):
        """Log result details in dev mode."""
        logger.debug("-" * 40)
        logger.debug(f"Retrieval Result ({'benchmark' if benchmark else 'normal'}):")
        logger.debug(f"  total_found: {result.total_found}")
        logger.debug(f"  dense_hits: {result.dense_hits}")
        logger.debug(f"  sparse_hits: {result.sparse_hits}")
        logger.debug(f"  filter_hits: {result.filter_hits}")
        logger.debug(f"  latency_ms: {elapsed_ms:.2f}")
        
        if result.candidates:
            logger.debug(f"  Top candidates:")
            for i, c in enumerate(result.candidates[:5]):
                logger.debug(f"    [{i}] id={c.internal_id} final={c.final_score:.4f} "
                           f"rrf={c.rrf_score:.4f} seed={c.is_seed}")
        logger.debug("-" * 40)
    
    def is_ready(self) -> bool:
        """Check if service is ready."""
        return self.retriever.is_ready()


def run_test(dev_mode: bool = False):
    """Run basic test to verify the module works."""
    print("=" * 60)
    print("Running retrieval module test")
    print("=" * 60)
    
    # Check C++ module availability
    print(f"C++ module available: {Retriever.cpp_available()}")
    print(f"C++ module version: {Retriever.version()}")
    
    if not Retriever.cpp_available():
        print("\nC++ module not available. Build it with:")
        print("  cd cpp && mkdir build && cd build")
        print("  cmake .. -DANDB_WITH_PYBIND=ON")
        print("  make")
        print("\nThen add the build directory to PYTHONPATH.")
        return
    
    # Create service
    config = IndexConfig(
        index_type="HNSW",
        metric_type="IP",
        dim=128,
    )
    
    service = RetrievalService(
        index_config=config,
        dev_mode=dev_mode,
    )
    
    # Initialize
    print("\nInitializing retriever...")
    if not service.init():
        print("Failed to initialize retriever")
        return
    
    # Build with random vectors
    print("\nBuilding index with 1000 random vectors...")
    vectors = np.random.randn(1000, 128).astype(np.float32)
    if not service.build(vectors):
        print("Failed to build index")
        return
    
    # Search
    print("\nSearching...")
    query = np.random.randn(128).astype(np.float32)
    request = RetrievalRequest(
        query_vector=query,
        top_k=10,
        enable_dense=True,
        enable_sparse=False,
    )
    
    result = service.retrieve(request)
    
    print(f"\nResults:")
    print(f"  Total found: {result.total_found}")
    print(f"  Dense hits: {result.dense_hits}")
    print(f"  Latency: {result.latency_ms}ms")
    
    if result.candidates:
        print(f"\n  Top 5 candidates:")
        for i, c in enumerate(result.candidates[:5]):
            print(f"    [{i}] id={c.internal_id} score={c.final_score:.4f}")
    
    print("\n" + "=" * 60)
    print("Test completed successfully")
    print("=" * 60)


def main():
    parser = argparse.ArgumentParser(
        description="CogDB Retrieval Service",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
    python -m src.retrieval.main --dev          # Run with debug output
    python -m src.retrieval.main --test         # Run basic test
    python -m src.retrieval.main --test --dev   # Run test with debug output
        """,
    )
    
    parser.add_argument(
        "--dev",
        action="store_true",
        help="Enable dev mode (verbose logging, internal state output)",
    )
    
    parser.add_argument(
        "--test",
        action="store_true",
        help="Run basic test",
    )
    
    parser.add_argument(
        "--index-type",
        type=str,
        default="HNSW",
        help="Index type (HNSW, IVF_FLAT, etc.)",
    )
    
    parser.add_argument(
        "--metric-type",
        type=str,
        default="IP",
        help="Metric type (IP, L2, COSINE)",
    )
    
    parser.add_argument(
        "--dim",
        type=int,
        default=128,
        help="Vector dimension",
    )
    
    parser.add_argument(
        "--rrf-k",
        type=int,
        default=60,
        help="RRF smoothing parameter k",
    )
    
    parser.add_argument(
        "--seed-threshold",
        type=float,
        default=0.7,
        help="Seed marking threshold",
    )
    
    args = parser.parse_args()
    
    if args.test:
        run_test(dev_mode=args.dev)
        return
    
    # Print configuration and status
    print("CogDB Retrieval Service")
    print("=" * 40)
    print(f"C++ module available: {Retriever.cpp_available()}")
    print(f"C++ module version: {Retriever.version()}")
    print(f"Dev mode: {args.dev}")
    print(f"Index type: {args.index_type}")
    print(f"Metric type: {args.metric_type}")
    print(f"Dimension: {args.dim}")
    print(f"RRF k: {args.rrf_k}")
    print(f"Seed threshold: {args.seed_threshold}")
    print("=" * 40)
    
    if not Retriever.cpp_available():
        print("\nWarning: C++ module not available.")
        print("Build it with: cd cpp && mkdir build && cd build && cmake .. && make")


if __name__ == "__main__":
    main()
