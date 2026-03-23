#!/usr/bin/env python3
"""
Retrieval Service Entry Point

This is a THIN WRAPPER - all retrieval logic is in cpp/.
Python layer only does parameter conversion.

Usage:
    python -m src.internal.retrieval.main --dev          # Run with debug output
    python -m src.internal.retrieval.main --test         # Run basic test
    python -m src.internal.retrieval.main --help         # Show help
"""

import argparse
import json
import logging
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from typing import Optional

import numpy as np

from .service.types import RetrievalRequest, CandidateList, cpp_available, cpp_version
from .service.retriever import Retriever

logger = logging.getLogger(__name__)


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


def run_server(service: "RetrievalService", host: str = "0.0.0.0", port: int = 8080):
    """Start HTTP server exposing retrieval endpoints for Go bridge.

    GET  /healthz  → 200 {"status":"ok","ready":true|false}
    POST /ingest   → 200 {"status":"ok"} (index a document)
    POST /retrieve → 200 {...candidates...} (search)
    """

    class RetrievalHandler(BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == "/healthz":
                body = json.dumps({
                    "status": "ok",
                    "ready": service.is_ready()
                }).encode()
                self._send_json(200, body)
            else:
                self._send_json(404, b'{"error":"not found"}')

        def do_POST(self):
            content_length = int(self.headers.get("Content-Length", 0))
            body = self.rfile.read(content_length) if content_length > 0 else b"{}"
            
            try:
                data = json.loads(body)
            except json.JSONDecodeError as e:
                self._send_json(400, json.dumps({"error": f"invalid json: {e}"}).encode())
                return

            if self.path == "/ingest":
                self._handle_ingest(data)
            elif self.path == "/retrieve":
                self._handle_retrieve(data)
            else:
                self._send_json(404, b'{"error":"not found"}')

        def _handle_ingest(self, data: dict):
            # For now, just acknowledge - actual indexing requires vector embedding
            # which should be done by the caller or a separate embedding service
            object_id = data.get("object_id", "")
            text = data.get("text", "")
            logger.debug(f"ingest: object_id={object_id} text_len={len(text)}")
            self._send_json(200, json.dumps({"status": "ok", "object_id": object_id}).encode())

        def _handle_retrieve(self, data: dict):
            query_text = data.get("query_text", "")
            top_k = data.get("top_k", 10)
            enable_dense = data.get("enable_dense", True)
            enable_sparse = data.get("enable_sparse", True)
            for_graph = data.get("for_graph", False)

            # Create a dummy query vector (in production, use embedding service)
            # For now, use random vector for testing
            query_vector = list(np.random.randn(service._dim).astype(np.float32))

            request = RetrievalRequest(
                query_vector=query_vector,
                query_text=query_text,
                top_k=top_k,
                enable_dense=enable_dense,
                enable_sparse=enable_sparse,
                for_graph=for_graph,
            )

            result = service.retrieve(request)

            response = {
                "candidates": [
                    {
                        "object_id": c.object_id,
                        "final_score": c.final_score,
                        "rrf_score": c.rrf_score,
                        "is_seed": c.is_seed,
                    }
                    for c in (result.candidates or [])
                ],
                "total_found": result.total_found,
                "dense_hits": result.query_meta.dense_hits if result.query_meta else 0,
                "sparse_hits": result.query_meta.sparse_hits if result.query_meta else 0,
                "latency_ms": result.query_meta.latency_ms if result.query_meta else 0,
            }
            self._send_json(200, json.dumps(response).encode())

        def _send_json(self, status: int, body: bytes):
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, fmt, *args):
            logger.debug("http: " + fmt, *args)

    server = HTTPServer((host, port), RetrievalHandler)
    logger.info("retrieval service listening on %s:%d (endpoints: /healthz, /ingest, /retrieve)", host, port)
    server.serve_forever()


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
        help="Start HTTP server exposing /healthz (K8s readiness probe)",
    )

    parser.add_argument(
        "--port",
        type=int,
        default=8080,
        help="HTTP server port (used with --serve, default: 8080)",
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
        service.init()
        run_server(service, port=args.port)
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
