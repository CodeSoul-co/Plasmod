"""
Test script for Filter Retriever (Milvus scalar query)
Run: python -m src.retrieval.test_filter
"""

import sys
import asyncio
import logging
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

from pymilvus import MilvusClient, DataType
from src.retrieval.service.filter import MilvusFilterRetriever
from src.retrieval.service.types import RetrievalRequest

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

MILVUS_URI = "http://localhost:19530"
TEST_COLLECTION = "test_filter_retrieval"


def setup_test_collection(client: MilvusClient):
    """Create test collection for filter queries"""
    
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
        logger.info(f"Dropped existing collection: {TEST_COLLECTION}")
    
    # Create schema (Milvus requires at least one vector field)
    schema = client.create_schema(auto_id=False, enable_dynamic_field=True)
    schema.add_field("id", DataType.INT64, is_primary=True)
    schema.add_field("object_id", DataType.VARCHAR, max_length=64)
    schema.add_field("object_type", DataType.VARCHAR, max_length=32)
    schema.add_field("vector", DataType.FLOAT_VECTOR, dim=4)  # dummy vector for Milvus requirement
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
    
    # Create index for vector field
    index_params = client.prepare_index_params()
    index_params.add_index(
        field_name="vector",
        index_type="FLAT",
        metric_type="IP",
    )
    
    client.create_collection(
        collection_name=TEST_COLLECTION,
        schema=schema,
        index_params=index_params,
    )
    logger.info(f"Created collection: {TEST_COLLECTION}")
    
    # Insert test data
    test_data = [
        {
            "id": 1,
            "object_id": "mem_001",
            "object_type": "memory",
            "vector": [0.1, 0.1, 0.1, 0.1],
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
            "vector": [0.1, 0.1, 0.1, 0.1],
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
            "vector": [0.1, 0.1, 0.1, 0.1],
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
            "vector": [0.1, 0.1, 0.1, 0.1],
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
        {
            "id": 5,
            "object_id": "mem_005",
            "object_type": "memory",
            "vector": [0.1, 0.1, 0.1, 0.1],
            "tenant_id": "tenant_1",
            "workspace_id": "workspace_1",
            "agent_id": "agent_1",
            "session_id": "session_1",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_005",
            "content": "Low confidence memory",
            "summary": "Low conf",
            "confidence": 0.3,
            "importance": 0.2,
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "unverified",
            "salience_weight": 0.5,
        },
    ]
    
    client.insert(collection_name=TEST_COLLECTION, data=test_data)
    logger.info(f"Inserted {len(test_data)} test records")
    
    client.load_collection(TEST_COLLECTION)
    logger.info(f"Loaded collection: {TEST_COLLECTION}")
    time.sleep(1)


async def test_basic_filter():
    """Test basic filter query"""
    print("\n" + "=" * 60)
    print("Test 1: Basic filter query")
    print("=" * 60)
    
    retriever = MilvusFilterRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
    )
    
    request = RetrievalRequest(
        query_text="",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.filter(request)
    
    print(f"Results count: {len(results)}")
    for i, c in enumerate(results, 1):
        print(f"  {i}. {c.object_id}: scope={c.scope}, memory_type={c.memory_type}")
    
    assert len(results) == 4, f"Should return 4 results for tenant_1/workspace_1, got {len(results)}"
    
    retriever.close()
    print("\n[PASS] Basic filter query works")


async def test_tenant_isolation():
    """Test tenant isolation"""
    print("\n" + "=" * 60)
    print("Test 2: Tenant isolation")
    print("=" * 60)
    
    retriever = MilvusFilterRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
    )
    
    request = RetrievalRequest(
        query_text="",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    results = await retriever.filter(request)
    
    print(f"Results count: {len(results)}")
    object_ids = [c.object_id for c in results]
    print(f"Object IDs: {object_ids}")
    
    assert "mem_004" not in object_ids, "Should not return data from other tenants"
    
    retriever.close()
    print("\n[PASS] Tenant isolation works")


async def test_agent_filter():
    """Test agent_id filtering"""
    print("\n" + "=" * 60)
    print("Test 3: Agent filtering")
    print("=" * 60)
    
    retriever = MilvusFilterRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
    )
    
    request = RetrievalRequest(
        query_text="",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        agent_id="agent_1",
        top_k=10,
    )
    
    results = await retriever.filter(request)
    
    print(f"Results count: {len(results)}")
    for c in results:
        print(f"  {c.object_id}: agent_id={c.agent_id}")
        assert c.agent_id == "agent_1", f"Should only return agent_1 data, got {c.agent_id}"
    
    retriever.close()
    print("\n[PASS] Agent filtering works")


async def test_memory_type_filter():
    """Test memory_type filtering"""
    print("\n" + "=" * 60)
    print("Test 4: Memory type filtering")
    print("=" * 60)
    
    retriever = MilvusFilterRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
    )
    
    request = RetrievalRequest(
        query_text="",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        memory_type="episodic",
        top_k=10,
    )
    
    results = await retriever.filter(request)
    
    print(f"Results count: {len(results)}")
    for c in results:
        print(f"  {c.object_id}: memory_type={c.memory_type}")
        assert c.memory_type == "episodic", f"Should only return episodic, got {c.memory_type}"
    
    retriever.close()
    print("\n[PASS] Memory type filtering works")


async def test_confidence_threshold():
    """Test min_confidence filtering"""
    print("\n" + "=" * 60)
    print("Test 5: Confidence threshold")
    print("=" * 60)
    
    retriever = MilvusFilterRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
    )
    
    request = RetrievalRequest(
        query_text="",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        min_confidence=0.8,
        top_k=10,
    )
    
    results = await retriever.filter(request)
    
    print(f"Results count: {len(results)}")
    for c in results:
        print(f"  {c.object_id}: confidence={c.confidence}")
        assert c.confidence >= 0.8, f"Should only return confidence >= 0.8, got {c.confidence}"
    
    # mem_005 has confidence 0.3, should be excluded
    object_ids = [c.object_id for c in results]
    assert "mem_005" not in object_ids, "mem_005 should be excluded (confidence 0.3)"
    
    retriever.close()
    print("\n[PASS] Confidence threshold works")


async def test_no_filter():
    """Test behavior when no filter provided"""
    print("\n" + "=" * 60)
    print("Test 6: No filter")
    print("=" * 60)
    
    retriever = MilvusFilterRetriever(
        uri=MILVUS_URI,
        collection_name=TEST_COLLECTION,
    )
    
    request = RetrievalRequest(
        query_text="",
        top_k=10,
    )
    
    results = await retriever.filter(request)
    
    print(f"Results count: {len(results)}")
    assert len(results) == 0, "Should return empty when no filter"
    
    retriever.close()
    print("\n[PASS] No filter returns empty")


def cleanup(client: MilvusClient):
    """Clean up test collection"""
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
        logger.info(f"Cleaned up collection: {TEST_COLLECTION}")


async def main():
    print("Testing Filter Retriever (Milvus Scalar Query)")
    print()
    
    client = MilvusClient(uri=MILVUS_URI)
    setup_test_collection(client)
    client.close()
    
    await test_basic_filter()
    await test_tenant_isolation()
    await test_agent_filter()
    await test_memory_type_filter()
    await test_confidence_threshold()
    await test_no_filter()
    
    print("\n" + "=" * 60)
    print("ALL TESTS PASSED")
    print("=" * 60)
    
    client = MilvusClient(uri=MILVUS_URI)
    cleanup(client)
    client.close()


if __name__ == "__main__":
    asyncio.run(main())
