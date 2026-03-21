#!/usr/bin/env python3
"""
Retrieval Service Entry Point

This is a THIN WRAPPER - all retrieval logic is in cpp/.
Python layer only does parameter conversion.

Usage:
    python -m src.internal.retrieval.main --dev          # Run with debug output
    python -m src.internal.retrieval.main --test         # Run basic test
    python -m src.internal.retrieval.main --serve        # Run HTTP server with /healthz
    python -m src.internal.retrieval.main --help         # Show help
"""

import argparse
import logging
import time
import json
from typing import Optional
from http.server import HTTPServer, BaseHTTPRequestHandler
from threading import Thread

import numpy as np

from .service.types import RetrievalRequest, CandidateList, cpp_available, cpp_version
from .service.retriever import Retriever

logger = logging.getLogger(__name__)

# Global service instance for HTTP handlers
_service_instance: Optional["RetrievalService"] = None


class RetrievalService:
    """
    Retrieval service facade.
    
    This is a THIN WRAPPER - all retrieval logic is in cpp/.
    Python layer only does parameter conversion.
    """
    
    def __init__(
        self,
        index_type: str = "HNSW",
        metric_type: str = "IP",
        dim: int = 128,
        sparse_index_type: str = "SPARSE_INVERTED_INDEX",
        rrf_k: int = 60,
        seed_threshold: float = 0.7,
        dev_mode: bool = False,
    ):
        self.dev_mode = dev_mode
        self._index_type = index_type
        self._metric_type = metric_type
        self._dim = dim
        self._sparse_index_type = sparse_index_type
        self._rrf_k = rrf_k
        self._seed_threshold = seed_threshold
        
        # Configure logging
        log_level = logging.DEBUG if dev_mode else logging.INFO
        logging.basicConfig(
            level=log_level,
            format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        )
        
        # Initialize retriever (thin wrapper to C++)
        self.retriever = Retriever()
        
        if dev_mode:
            self._log_init_state()
    
    def _log_init_state(self):
        """Log initialization state in dev mode."""
        logger.debug("=" * 60)
        logger.debug("RetrievalService initialized in dev mode")
        logger.debug("=" * 60)
        logger.debug(f"C++ module available: {cpp_available()}")
        logger.debug(f"C++ module version: {cpp_version()}")
        logger.debug(f"Index config:")
        logger.debug(f"  index_type: {self._index_type}")
        logger.debug(f"  metric_type: {self._metric_type}")
        logger.debug(f"  dim: {self._dim}")
        logger.debug(f"Merge config:")
        logger.debug(f"  rrf_k: {self._rrf_k}")
        logger.debug(f"  seed_threshold: {self._seed_threshold}")
        logger.debug(f"Sparse index type: {self._sparse_index_type}")
        logger.debug("=" * 60)
    
    def init(self) -> bool:
        """Initialize the retriever."""
        return self.retriever.init(
            index_type=self._index_type,
            metric_type=self._metric_type,
            dim=self._dim,
            sparse_index_type=self._sparse_index_type,
            rrf_k=self._rrf_k,
            seed_threshold=self._seed_threshold,
        )
    
    def build(self, dense_vectors: np.ndarray, sparse_vectors=None) -> bool:
        """Build indexes from vectors."""
        if self.dev_mode:
            logger.debug(f"Building indexes: {len(dense_vectors)} vectors")
            if dense_vectors.ndim == 2:
                logger.debug(f"  Vector dim: {dense_vectors.shape[1]}")
        return self.retriever.build(dense_vectors, sparse_vectors)
    
    def retrieve(self, request: RetrievalRequest) -> CandidateList:
        """Execute retrieval request."""
        if self.dev_mode:
            self._log_request(request)
        
        start_time = time.time()
        result = self.retriever.retrieve(request)
        elapsed_ms = (time.time() - start_time) * 1000
        
        if self.dev_mode:
            self._log_result(result, elapsed_ms)
        
        return result
    
    def benchmark_retrieve(self, request: RetrievalRequest) -> CandidateList:
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
            if isinstance(request.query_vector, np.ndarray):
                logger.debug(f"  query_vector: shape={request.query_vector.shape}")
            else:
                logger.debug(f"  query_vector: len={len(request.query_vector)}")
        if request.query_text:
            text_preview = request.query_text[:50] + "..." if len(request.query_text) > 50 else request.query_text
            logger.debug(f"  query_text: '{text_preview}'")
    
    def _log_result(self, result: CandidateList, elapsed_ms: float, benchmark: bool = False):
        """Log result details in dev mode."""
        logger.debug("-" * 40)
        mode_str = "benchmark" if benchmark else "normal"
        logger.debug(f"Retrieval Result ({mode_str}):")
        logger.debug(f"  total_found: {result.total_found}")
        logger.debug(f"  latency_ms: {elapsed_ms:.2f}")
        
        if result.query_meta:
            logger.debug(f"  dense_hits: {result.query_meta.dense_hits}")
            logger.debug(f"  sparse_hits: {result.query_meta.sparse_hits}")
            logger.debug(f"  filter_hits: {result.query_meta.filter_hits}")
        
        if result.candidates:
            logger.debug(f"  Top candidates:")
            for i, c in enumerate(result.candidates[:5]):
                logger.debug(f"    [{i}] id={c.object_id} final={c.final_score:.4f} "
                           f"rrf={c.rrf_score:.4f} seed={c.is_seed}")
        logger.debug("-" * 40)
    
    def is_ready(self) -> bool:
        """Check if service is ready."""
        return self.retriever.is_ready()
    
    def healthz(self) -> dict:
        """Health check endpoint data."""
        return {
            "status": "healthy" if self.is_ready() else "degraded",
            "cpp_available": cpp_available(),
            "cpp_version": cpp_version(),
            "ready": self.is_ready(),
        }


class HealthzHandler(BaseHTTPRequestHandler):
    """HTTP handler for /healthz endpoint."""
    
    def log_message(self, format, *args):
        logger.debug(f"HTTP: {format % args}")
    
    def do_GET(self):
        if self.path == "/healthz":
            self._handle_healthz()
        else:
            self.send_error(404, "Not Found")
    
    def _handle_healthz(self):
        global _service_instance
        if _service_instance is None:
            data = {
                "status": "unavailable",
                "cpp_available": cpp_available(),
                "cpp_version": cpp_version(),
                "ready": False,
            }
        else:
            data = _service_instance.healthz()
        
        response = json.dumps(data).encode("utf-8")
        self.send_response(200 if data.get("ready") else 503)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)


def run_server(host: str, port: int, service: "RetrievalService"):
    """Run HTTP server with /healthz endpoint."""
    global _service_instance
    _service_instance = service
    
    server = HTTPServer((host, port), HealthzHandler)
    logger.info(f"Starting HTTP server on {host}:{port}")
    logger.info(f"  /healthz endpoint available")
    
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logger.info("Shutting down HTTP server")
        server.shutdown()


def run_test(dev_mode: bool = False):
    """Run basic test to verify the module works."""
    print("=" * 60)
    print("Running retrieval module test")
    print("=" * 60)
    
    # Check C++ module availability
    print(f"C++ module available: {cpp_available()}")
    print(f"C++ module version: {cpp_version()}")
    
    if not cpp_available():
        print("\nC++ module not available. Build it with:")
        print("  cd cpp && mkdir build && cd build")
        print("  cmake .. -DANDB_WITH_PYBIND=ON")
        print("  make")
        print("\nThen add the build directory to PYTHONPATH.")
        return
    
    # Create service
    service = RetrievalService(
        index_type="HNSW",
        metric_type="IP",
        dim=128,
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
        query_vector=list(query),
        top_k=10,
        enable_dense=True,
        enable_sparse=False,
    )
    
    result = service.retrieve(request)
    
    print(f"\nResults:")
    print(f"  Total found: {result.total_found}")
    if result.query_meta:
        print(f"  Dense hits: {result.query_meta.dense_hits}")
        print(f"  Latency: {result.query_meta.latency_ms}ms")
    
    if result.candidates:
        print(f"\n  Top 5 candidates:")
        for i, c in enumerate(result.candidates[:5]):
            print(f"    [{i}] id={c.object_id} score={c.final_score:.4f}")
    
    print("\n" + "=" * 60)
    print("Test completed successfully")
    print("=" * 60)


def main():
    parser = argparse.ArgumentParser(
        description="CogDB Retrieval Service",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
    python -m src.internal.retrieval.main --dev          # Run with debug output
    python -m src.internal.retrieval.main --test         # Run basic test
    python -m src.internal.retrieval.main --test --dev   # Run test with debug output
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
    
    parser.add_argument(
        "--serve",
        action="store_true",
        help="Run HTTP server with /healthz endpoint",
    )
    
    parser.add_argument(
        "--host",
        type=str,
        default="127.0.0.1",
        help="HTTP server host",
    )
    
    parser.add_argument(
        "--port",
        type=int,
        default=8081,
        help="HTTP server port",
    )
    
    args = parser.parse_args()
    
    if args.test:
        run_test(dev_mode=args.dev)
        return
    
    if args.serve:
        service = RetrievalService(
            index_type=args.index_type,
            metric_type=args.metric_type,
            dim=args.dim,
            rrf_k=args.rrf_k,
            seed_threshold=args.seed_threshold,
            dev_mode=args.dev,
        )
        run_server(args.host, args.port, service)
        return
    
    # Print configuration and status
    print("CogDB Retrieval Service")
    print("=" * 40)
    print(f"C++ module available: {cpp_available()}")
    print(f"C++ module version: {cpp_version()}")
    print(f"Dev mode: {args.dev}")
    print(f"Index type: {args.index_type}")
    print(f"Metric type: {args.metric_type}")
    print(f"Dimension: {args.dim}")
    print(f"RRF k: {args.rrf_k}")
    print(f"Seed threshold: {args.seed_threshold}")
    print("=" * 40)
    
    if not cpp_available():
        print("\nWarning: C++ module not available.")
        print("Build it with: cd cpp && mkdir build && cd build && cmake .. && make")


if __name__ == "__main__":
    main()
