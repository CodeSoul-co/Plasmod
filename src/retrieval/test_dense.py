"""
Test script for Dense Retriever (Milvus ANN search)
Run: python -m src.retrieval.test_dense
"""

import sys
import asyncio
import logging
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

from pymilvus import MilvusClient, DataType
from src.retrieval.service.dense import MilvusDenseRetriever
from src.retrieval.service.types import RetrievalRequest

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

MILVUS_URI = "http://localhost:19530"
TEST_COLLECTION = "test_dense_retrieval"
VECTOR_DIM = 128


def setup_test_collection(client: MilvusClient):
    """Create test collection with sample data"""
    
    # Drop if exists
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
        logger.info(f"Dropped existing collection: {TEST_COLLECTION}")
    
    # Create collection schema
    schema = client.create_schema(auto_id=False, enable_dynamic_field=True)
    schema.add_field("id", DataType.INT64, is_primary=True)
    schema.add_field("object_id", DataType.VARCHAR, max_length=64)
    schema.add_field("object_type", DataType.VARCHAR, max_length=32)
    schema.add_field("vector", DataType.FLOAT_VECTOR, dim=VECTOR_DIM)
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
    
    # Create index
    index_params = client.prepare_index_params()
    index_params.add_index(
        field_name="vector",
        index_type="IVF_FLAT",
        metric_type="IP",
        params={"nlist": 128}
    )
    
    # Create collection
    client.create_collection(
        collection_name=TEST_COLLECTION,
        schema=schema,
        index_params=index_params,
    )
    logger.info(f"Created collection: {TEST_COLLECTION}")
    
    # Insert test data
    import random
    random.seed(42)
    
    def random_vector(base_value: float = 0.0) -> list:
        vec = [base_value + random.uniform(-0.1, 0.1) for _ in range(VECTOR_DIM)]
        norm = sum(x*x for x in vec) ** 0.5
        return [x / norm for x in vec]  # normalize for IP similarity
    
    test_data = [
        {
            "id": 1,
            "object_id": "mem_001",
            "object_type": "memory",
            "vector": random_vector(1.0),  # similar to query
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
            "vector": random_vector(0.8),  # somewhat similar
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_1",
            "session_id": "session_1",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_002",
            "content": "User prefers metric units",
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
            "vector": random_vector(0.3),  # less similar
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_2",  # different agent
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
            "vector": random_vector(-0.5),  # dissimilar
            "tenant_id": "tenant_2",  # different tenant
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
    
    # Load collection
    client.load_collection(TEST_COLLECTION)
    logger.info(f"Loaded collection: {TEST_COLLECTION}")


async def test_basic_search():
    """Test basic vector search"""
    print("\n" + "=" * 60)
    print("Test 1: Basic vector search")
    print("=" * 60)
    
    retriever = MilvusDenseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        vector_field="vector",
    )
    
    # Use simple query vector
    query_vector = [0.1] * VECTOR_DIM
    
    request = RetrievalRequest(
        query_text="weather query",
        query_vector=query_vector,
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    for i, c in enumerate(results, 1):
        print(f"  {i}. {c.object_id}: score={c.score:.4f}, content='{c.content[:40]}...'")
    
    assert len(results) > 0, "Should return results"
    
    retriever.close()
    print("\n[PASS] Basic vector search works")


async def test_tenant_isolation():
    """Test tenant isolation - should not return data from other tenants"""
    print("\n" + "=" * 60)
    print("Test 2: Tenant isolation")
    print("=" * 60)
    
    retriever = MilvusDenseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        vector_field="vector",
    )
    
    query_vector = [0.1] * VECTOR_DIM
    
    request = RetrievalRequest(
        query_text="test",
        query_vector=query_vector,
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    for c in results:
        print(f"  {c.object_id}: tenant_id should be tenant_1")
    
    # Should not include mem_004 (tenant_2)
    object_ids = [c.object_id for c in results]
    assert "mem_004" not in object_ids, "Should not return data from other tenants"
    
    retriever.close()
    print("\n[PASS] Tenant isolation works")


async def test_agent_filter():
    """Test agent_id filtering"""
    print("\n" + "=" * 60)
    print("Test 3: Agent filtering")
    print("=" * 60)
    
    retriever = MilvusDenseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        vector_field="vector",
    )
    
    query_vector = [0.1] * VECTOR_DIM
    
    request = RetrievalRequest(
        query_text="test",
        query_vector=query_vector,
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        agent_id="agent_1",  # filter by agent
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    for c in results:
        print(f"  {c.object_id}: agent_id={c.agent_id}")
    
    # Should only return agent_1 data
    for c in results:
        assert c.agent_id == "agent_1", f"Should only return agent_1 data, got {c.agent_id}"
    
    retriever.close()
    print("\n[PASS] Agent filtering works")


async def test_no_query_vector():
    """Test behavior when no query_vector provided"""
    print("\n" + "=" * 60)
    print("Test 4: No query vector")
    print("=" * 60)
    
    retriever = MilvusDenseRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
        vector_field="vector",
    )
    
    request = RetrievalRequest(
        query_text="test without vector",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.search(request)
    
    print(f"Results count: {len(results)}")
    assert len(results) == 0, "Should return empty when no query_vector"
    
    retriever.close()
    print("\n[PASS] No query vector returns empty")


def cleanup(client: MilvusClient):
    """Clean up test collection"""
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
        logger.info(f"Cleaned up collection: {TEST_COLLECTION}")


async def main():
    print("Testing Dense Retriever (Milvus ANN)")
    print()
    
    # Setup
    client = MilvusClient(uri=MILVUS_URI)
    setup_test_collection(client)
    client.close()
    
    # Run tests
    await test_basic_search()
    await test_tenant_isolation()
    await test_agent_filter()
    await test_no_query_vector()
    
    print("\n" + "=" * 60)
    print("ALL TESTS PASSED")
    print("=" * 60)
    
    # Cleanup after all tests pass
    client = MilvusClient(uri=MILVUS_URI)
    cleanup(client)
    client.close()


if __name__ == "__main__":
    asyncio.run(main())
