"""
Test script for Sparse Retriever (Milvus sparse vector search)
Run: python -m src.internal.retrieval.test_sparse
"""

import sys
import asyncio
import logging
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

from pymilvus import MilvusClient, DataType
from src.internal.retrieval.service.sparse import MilvusSparseRetriever
from src.internal.retrieval.service.types import RetrievalRequest

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

MILVUS_URI = "http://localhost:19530"
TEST_COLLECTION = "test_sparse_retrieval"


def deterministic_hash(s: str) -> int:
    """Deterministic hash function using FNV-1a algorithm"""
    h = 2166136261
    for c in s.encode('utf-8'):
        h ^= c
        h = (h * 16777619) & 0xFFFFFFFF
    return h


def text_to_sparse_vector(text: str) -> dict:
    """Convert text to sparse vector (same logic as retriever)"""
    tokens = text.lower().split()
    if not tokens:
        return {}
    
    token_counts = {}
    for token in tokens:
        token_counts[token] = token_counts.get(token, 0) + 1
    
    sparse_vec = {}
    for token, count in token_counts.items():
        idx = deterministic_hash(token) % 30000
        weight = count / len(tokens)
        sparse_vec[idx] = weight
    
    return sparse_vec


def setup_test_collection(client: MilvusClient):
    """Create test collection with sparse vector field"""
    
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
        logger.info(f"Dropped existing collection: {TEST_COLLECTION}")
    
    # Create schema with sparse vector
    schema = client.create_schema(auto_id=False, enable_dynamic_field=True)
    schema.add_field("id", DataType.INT64, is_primary=True)
    schema.add_field("object_id", DataType.VARCHAR, max_length=64)
    schema.add_field("object_type", DataType.VARCHAR, max_length=32)
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
    
    # Create sparse index
    index_params = client.prepare_index_params()
    index_params.add_index(
        field_name="sparse_vector",
        index_type="SPARSE_INVERTED_INDEX",
        metric_type="IP",
    )
    
    client.create_collection(
        collection_name=TEST_COLLECTION,
        schema=schema,
        index_params=index_params,
    )
    logger.info(f"Created collection: {TEST_COLLECTION}")
    
    # Insert test data with sparse vectors
    test_data = [
        {
            "id": 1,
            "object_id": "mem_001",
            "object_type": "memory",
            "sparse_vector": text_to_sparse_vector("weather forecast beijing temperature"),
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_1",
            "session_id": "session_1",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_001",
            "content": "User asked about weather forecast in Beijing",
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
            "sparse_vector": text_to_sparse_vector("metric units celsius fahrenheit"),
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_1",
            "session_id": "session_1",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_002",
            "content": "User prefers metric units and celsius",
            "summary": "Unit preference",
            "confidence": 0.85,
            "importance": 0.6,
            "level": 0,
            "memory_type": "semantic",
            "verified_state": "verified",
            "salience_weight": 1.2,
        },
        {
            "id": 3,
            "object_id": "mem_003",
            "object_type": "memory",
            "sparse_vector": text_to_sparse_vector("project deadline friday meeting"),
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_2",
            "session_id": "session_2",
            "scope": "workspace",
            "version": 1,
            "provenance_ref": "prov_003",
            "content": "Project deadline is next Friday",
            "summary": "Deadline reminder",
            "confidence": 0.95,
            "importance": 0.9,
            "level": 0,
            "memory_type": "procedural",
            "verified_state": "verified",
            "salience_weight": 1.5,
        },
        {
            "id": 4,
            "object_id": "mem_004",
            "object_type": "memory",
            "sparse_vector": text_to_sparse_vector("different tenant data secret"),
            "tenant_id": "tenant_2",
            "workspace_id": "workspace_2",
            "agent_id": "agent_3",
            "session_id": "session_3",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_004",
            "content": "Different tenant data",
            "summary": "Other tenant",
            "confidence": 0.7,
            "importance": 0.5,
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "unverified",
            "salience_weight": 0.8,
        },
    ]
    
    client.insert(collection_name=TEST_COLLECTION, data=test_data)
    logger.info(f"Inserted {len(test_data)} test records")
    
    client.load_collection(TEST_COLLECTION)
    logger.info(f"Loaded collection: {TEST_COLLECTION}")
    
    # Wait for index to be ready
    import time
    time.sleep(1)


async def test_basic_sparse_search():
    """Test basic sparse vector search"""
    print("\n" + "=" * 60)
    print("Test 1: Basic sparse search")
    print("=" * 60)
    
    retriever = MilvusSparseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        sparse_field="sparse_vector",
    )
    
    request = RetrievalRequest(
        query_text="weather forecast",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    for i, c in enumerate(results, 1):
        print(f"  {i}. {c.object_id}: score={c.score:.4f}, content='{c.content[:40]}...'")
    
    assert len(results) > 0, "Should return results"
    assert results[0].object_id == "mem_001", "mem_001 should match 'weather forecast'"
    
    retriever.close()
    print("\n[PASS] Basic sparse search works")


async def test_keyword_matching():
    """Test keyword matching - should find exact keyword matches"""
    print("\n" + "=" * 60)
    print("Test 2: Keyword matching")
    print("=" * 60)
    
    retriever = MilvusSparseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        sparse_field="sparse_vector",
    )
    
    request = RetrievalRequest(
        query_text="deadline friday",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    for i, c in enumerate(results, 1):
        print(f"  {i}. {c.object_id}: score={c.score:.4f}")
    
    assert len(results) > 0, "Should return results"
    assert results[0].object_id == "mem_003", "mem_003 should match 'deadline friday'"
    
    retriever.close()
    print("\n[PASS] Keyword matching works")


async def test_tenant_isolation():
    """Test tenant isolation in sparse search"""
    print("\n" + "=" * 60)
    print("Test 3: Tenant isolation")
    print("=" * 60)
    
    retriever = MilvusSparseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        sparse_field="sparse_vector",
    )
    
    request = RetrievalRequest(
        query_text="data secret",  # matches mem_004 content
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    for c in results:
        print(f"  {c.object_id}")
    
    # Should not include mem_004 (tenant_2)
    object_ids = [c.object_id for c in results]
    assert "mem_004" not in object_ids, "Should not return data from other tenants"
    
    retriever.close()
    print("\n[PASS] Tenant isolation works")


async def test_no_query_text():
    """Test behavior when no query_text provided"""
    print("\n" + "=" * 60)
    print("Test 4: No query text")
    print("=" * 60)
    
    retriever = MilvusSparseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        sparse_field="sparse_vector",
    )
    
    request = RetrievalRequest(
        query_text="",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    assert len(results) == 0, "Should return empty when no query_text"
    
    retriever.close()
    print("\n[PASS] No query text returns empty")


def cleanup(client: MilvusClient):
    """Clean up test collection"""
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
        logger.info(f"Cleaned up collection: {TEST_COLLECTION}")


async def main():
    print("Testing Sparse Retriever (Milvus Sparse Vector)")
    print()
    
    client = MilvusClient(uri=MILVUS_URI)
    setup_test_collection(client)
    client.close()
    
    await test_basic_sparse_search()
    await test_keyword_matching()
    await test_tenant_isolation()
    await test_no_query_text()
    
    print("\n" + "=" * 60)
    print("ALL TESTS PASSED")
    print("=" * 60)
    
    client = MilvusClient(uri=MILVUS_URI)
    cleanup(client)
    client.close()


if __name__ == "__main__":
    asyncio.run(main())
