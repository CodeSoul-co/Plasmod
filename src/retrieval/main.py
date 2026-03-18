"""
Retrieval Service Entry Point

Usage:
    python -m src.retrieval.main --dev          # Run with debug output
    python -m src.retrieval.main --test         # Run integration tests
    python -m src.retrieval.main --help         # Show help
"""

import argparse
import asyncio
import logging
import sys
from typing import Optional

from .service.types import RetrievalRequest, CandidateList
from .service.retriever import Retriever
from .service.merger import Merger
from .service.dense import MilvusDenseRetriever
from .service.sparse import MilvusSparseRetriever
from .service.filter import MilvusFilterRetriever


class RetrievalService:
    """
    Retrieval service facade.
    
    Provides a unified interface for retrieval operations.
    """
    
    def __init__(
        self,
        milvus_uri: str = "http://localhost:19530",
        dense_collection: str = "andb_embeddings",
        sparse_collection: str = "andb_embeddings",
        filter_collection: str = "andb_memories",
        dense_vector_field: str = "vector",
        sparse_vector_field: str = "sparse_vector",
        dev_mode: bool = False,
    ):
        self.milvus_uri = milvus_uri
        self.dev_mode = dev_mode
        
        # Configure logging
        log_level = logging.DEBUG if dev_mode else logging.INFO
        logging.basicConfig(
            level=log_level,
            format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
        )
        self.logger = logging.getLogger(__name__)
        
        # Initialize retrievers
        self.dense_retriever = MilvusDenseRetriever(
            uri=milvus_uri,
            collection_name=dense_collection,
            vector_field=dense_vector_field,
        )
        
        self.sparse_retriever = MilvusSparseRetriever(
            uri=milvus_uri,
            collection_name=sparse_collection,
            sparse_field=sparse_vector_field,
        )
        
        self.filter_retriever = MilvusFilterRetriever(
            uri=milvus_uri,
            collection_name=filter_collection,
        )
        
        self.merger = Merger()
        
        self.retriever = Retriever(
            dense=self.dense_retriever,
            sparse=self.sparse_retriever,
            filter=self.filter_retriever,
            merger=self.merger,
        )
        
        if dev_mode:
            self.logger.debug("RetrievalService initialized in dev mode")
            self.logger.debug(f"  Milvus URI: {milvus_uri}")
            self.logger.debug(f"  Dense collection: {dense_collection}")
            self.logger.debug(f"  Sparse collection: {sparse_collection}")
            self.logger.debug(f"  Filter collection: {filter_collection}")
    
    async def retrieve(self, request: RetrievalRequest) -> CandidateList:
        """Execute retrieval request"""
        if self.dev_mode:
            self.logger.debug(f"Retrieve request: query_text='{request.query_text}', "
                            f"tenant_id={request.tenant_id}, workspace_id={request.workspace_id}")
        
        result = await self.retriever.retrieve(request)
        
        if self.dev_mode:
            self.logger.debug(f"Retrieve result: {len(result.candidates)} candidates, "
                            f"total_found={result.total_found}")
            if result.query_meta:
                self.logger.debug(f"  Query meta: dense_hits={result.query_meta.dense_hits}, "
                                f"sparse_hits={result.query_meta.sparse_hits}, "
                                f"filter_hits={result.query_meta.filter_hits}, "
                                f"latency_ms={result.query_meta.latency_ms}")
        
        return result
    
    def close(self):
        """Close all connections"""
        self.dense_retriever.close()
        self.sparse_retriever.close()
        self.filter_retriever.close()
        if self.dev_mode:
            self.logger.debug("RetrievalService closed")


async def run_integration_test(dev_mode: bool = False):
    """Run integration test with mock data"""
    from pymilvus import MilvusClient, DataType
    import time
    
    logger = logging.getLogger(__name__)
    milvus_uri = "http://localhost:19530"
    test_collection = "test_retrieval_integration"
    
    def deterministic_hash(s: str) -> int:
        h = 2166136261
        for c in s.encode('utf-8'):
            h ^= c
            h = (h * 16777619) & 0xFFFFFFFF
        return h
    
    def text_to_sparse(text: str) -> dict:
        tokens = text.lower().split()
        sparse = {}
        for t in tokens:
            idx = deterministic_hash(t) % 30000
            sparse[idx] = 1.0 / len(tokens)
        return sparse
    
    # Setup test collection
    client = MilvusClient(uri=milvus_uri)
    
    if client.has_collection(test_collection):
        client.drop_collection(test_collection)
    
    schema = client.create_schema(auto_id=False, enable_dynamic_field=True)
    schema.add_field("id", DataType.INT64, is_primary=True)
    schema.add_field("object_id", DataType.VARCHAR, max_length=64)
    schema.add_field("object_type", DataType.VARCHAR, max_length=32)
    schema.add_field("vector", DataType.FLOAT_VECTOR, dim=128)
    schema.add_field("sparse_vector", DataType.SPARSE_FLOAT_VECTOR)
    schema.add_field("tenant_id", DataType.VARCHAR, max_length=64)
    schema.add_field("workspace_id", DataType.VARCHAR, max_length=64)
    schema.add_field("agent_id", DataType.VARCHAR, max_length=64)
    schema.add_field("session_id", DataType.VARCHAR, max_length=64)
    schema.add_field("scope", DataType.VARCHAR, max_length=32)
    schema.add_field("version", DataType.INT32)
    schema.add_field("provenance_ref", DataType.VARCHAR, max_length=128)
    schema.add_field("content", DataType.VARCHAR, max_length=1024)
    schema.add_field("summary", DataType.VARCHAR, max_length=512)
    schema.add_field("confidence", DataType.FLOAT)
    schema.add_field("importance", DataType.FLOAT)
    schema.add_field("level", DataType.INT32)
    schema.add_field("memory_type", DataType.VARCHAR, max_length=32)
    schema.add_field("verified_state", DataType.VARCHAR, max_length=32)
    schema.add_field("salience_weight", DataType.FLOAT)
    
    index_params = client.prepare_index_params()
    index_params.add_index(field_name="vector", index_type="IVF_FLAT", metric_type="IP", params={"nlist": 128})
    index_params.add_index(field_name="sparse_vector", index_type="SPARSE_INVERTED_INDEX", metric_type="IP")
    
    client.create_collection(collection_name=test_collection, schema=schema, index_params=index_params)
    
    # Insert test data
    test_data = [
        {
            "id": 1,
            "object_id": "mem_001",
            "object_type": "memory",
            "vector": [0.1] * 128,
            "sparse_vector": text_to_sparse("weather forecast beijing temperature"),
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_1",
            "session_id": "session_1",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_001",
            "content": "User asked about weather in Beijing",
            "summary": "Weather query",
            "confidence": 0.9,
            "importance": 0.8,
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "salience_weight": 1.0,
        },
        {
            "id": 2,
            "object_id": "mem_002",
            "object_type": "memory",
            "vector": [0.2] * 128,
            "sparse_vector": text_to_sparse("project deadline friday meeting"),
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_1",
            "session_id": "session_1",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_002",
            "content": "Project deadline is next Friday",
            "summary": "Deadline reminder",
            "confidence": 0.95,
            "importance": 0.9,
            "level": 0,
            "memory_type": "procedural",
            "verified_state": "verified",
            "salience_weight": 1.2,
        },
    ]
    
    client.insert(collection_name=test_collection, data=test_data)
    client.load_collection(test_collection)
    time.sleep(1)
    client.close()
    
    logger.info(f"Created test collection: {test_collection}")
    
    # Run tests
    service = RetrievalService(
        milvus_uri=milvus_uri,
        dense_collection=test_collection,
        sparse_collection=test_collection,
        filter_collection=test_collection,
        dev_mode=dev_mode,
    )
    
    print("\n" + "=" * 60)
    print("Integration Test: Three-way retrieval with RRF fusion")
    print("=" * 60)
    
    # Test 1: Full retrieval (dense + sparse + filter)
    request = RetrievalRequest(
        query_text="weather forecast",
        query_vector=[0.1] * 128,
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    result = await service.retrieve(request)
    
    print(f"\nResults: {len(result.candidates)} candidates")
    print(f"Total found: {result.total_found}")
    if result.query_meta:
        print(f"Channels used: {result.query_meta.channels_used}")
        print(f"Dense hits: {result.query_meta.dense_hits}")
        print(f"Sparse hits: {result.query_meta.sparse_hits}")
        print(f"Filter hits: {result.query_meta.filter_hits}")
    
    print("\nCandidates:")
    for i, c in enumerate(result.candidates, 1):
        print(f"  {i}. {c.object_id}: score={c.score:.4f}, sources={c.source_channels}")
    
    assert len(result.candidates) > 0, "Should return candidates"
    print("\n[PASS] Three-way retrieval works")
    
    # Test 2: Filter-only mode
    print("\n" + "=" * 60)
    print("Integration Test: Filter-only mode")
    print("=" * 60)
    
    request = RetrievalRequest(
        query_text="",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        enable_filter_only=True,
        top_k=10,
    )
    
    result = await service.retrieve(request)
    
    print(f"\nResults: {len(result.candidates)} candidates")
    if result.query_meta:
        print(f"Channels used: {result.query_meta.channels_used}")
    
    print("\nCandidates (ordered by importance):")
    for i, c in enumerate(result.candidates, 1):
        print(f"  {i}. {c.object_id}: importance={c.importance}, confidence={c.confidence}")
    
    assert len(result.candidates) > 0, "Should return candidates"
    assert result.candidates[0].importance >= result.candidates[-1].importance, "Should be ordered by importance"
    print("\n[PASS] Filter-only mode works")
    
    # Cleanup
    service.close()
    client = MilvusClient(uri=milvus_uri)
    client.drop_collection(test_collection)
    client.close()
    
    print("\n" + "=" * 60)
    print("ALL INTEGRATION TESTS PASSED")
    print("=" * 60)


def main():
    parser = argparse.ArgumentParser(description="Retrieval Service")
    parser.add_argument("--dev", action="store_true", help="Enable dev mode with debug output")
    parser.add_argument("--test", action="store_true", help="Run integration tests")
    parser.add_argument("--milvus-uri", default="http://localhost:19530", help="Milvus URI")
    
    args = parser.parse_args()
    
    if args.test:
        asyncio.run(run_integration_test(dev_mode=args.dev))
    else:
        print("Retrieval Service")
        print()
        print("Usage:")
        print("  python -m src.retrieval.main --test        Run integration tests")
        print("  python -m src.retrieval.main --test --dev  Run tests with debug output")
        print()
        print("As a library:")
        print("  from src.retrieval.main import RetrievalService")
        print("  service = RetrievalService(dev_mode=True)")
        print("  result = await service.retrieve(request)")


if __name__ == "__main__":
    main()
