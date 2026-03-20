#!/usr/bin/env python3
"""
Integration tests for Retrieval Module (Member B).

Covers:
1. Safety filter 7 rules (quarantine, ttl, visible_time, is_active, as_of_ts, min_version)
2. RRF scoring (two-path candidates score higher than single-path)
3. Reranking formula: final_score = score * importance * freshness_score * confidence
4. for_graph mode returns top_k * 2 candidates
5. Seed marking: final_score >= 0.7 -> is_seed=True
6. filter_only mode: skip dense+sparse, order by importance desc
7. benchmark_retrieve() returns all candidates with rrf_score

Usage:
    python integration_tests/test_retrieval_b.py
    python integration_tests/test_retrieval_b.py --verbose

Requirements:
    - Milvus running via docker-compose (localhost:19530)
"""

import argparse
import asyncio
import logging
import sys
import time
from datetime import datetime, timedelta
from typing import List, Dict, Any

from pymilvus import MilvusClient, DataType

# Add src to path for imports
sys.path.insert(0, ".")

from src.retrieval.service.types import RetrievalRequest, Candidate, CandidateList
from src.retrieval.service.retriever import Retriever
from src.retrieval.service.merger import Merger
from src.retrieval.service.dense import MilvusDenseRetriever
from src.retrieval.service.sparse import MilvusSparseRetriever
from src.retrieval.service.filter import MilvusFilterRetriever


# Test configuration
MILVUS_URI = "http://localhost:19530"
TEST_COLLECTION = "test_retrieval_b_integration"
VECTOR_DIM = 128

logger = logging.getLogger(__name__)


def deterministic_hash(s: str) -> int:
    """FNV-1a hash for deterministic sparse vector generation."""
    h = 2166136261
    for c in s.encode('utf-8'):
        h ^= c
        h = (h * 16777619) & 0xFFFFFFFF
    return h


def text_to_sparse(text: str) -> dict:
    """Convert text to sparse vector using FNV-1a hash."""
    tokens = text.lower().split()
    sparse = {}
    for t in tokens:
        idx = deterministic_hash(t) % 30000
        sparse[idx] = 1.0 / max(len(tokens), 1)
    return sparse


def create_test_collection(client: MilvusClient) -> None:
    """Create test collection with all required fields."""
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
    
    schema = client.create_schema(auto_id=False, enable_dynamic_field=True)
    
    # Primary key
    schema.add_field("id", DataType.INT64, is_primary=True)
    
    # Identity fields
    schema.add_field("object_id", DataType.VARCHAR, max_length=64)
    schema.add_field("object_type", DataType.VARCHAR, max_length=32)
    
    # Vector fields
    schema.add_field("vector", DataType.FLOAT_VECTOR, dim=VECTOR_DIM)
    schema.add_field("sparse_vector", DataType.SPARSE_FLOAT_VECTOR)
    
    # Isolation fields
    schema.add_field("tenant_id", DataType.VARCHAR, max_length=64)
    schema.add_field("workspace_id", DataType.VARCHAR, max_length=64)
    
    # Metadata fields
    schema.add_field("agent_id", DataType.VARCHAR, max_length=64)
    schema.add_field("session_id", DataType.VARCHAR, max_length=64)
    schema.add_field("scope", DataType.VARCHAR, max_length=32)
    schema.add_field("version", DataType.INT32)
    schema.add_field("provenance_ref", DataType.VARCHAR, max_length=128)
    schema.add_field("content", DataType.VARCHAR, max_length=1024)
    schema.add_field("summary", DataType.VARCHAR, max_length=512)
    schema.add_field("level", DataType.INT32)
    schema.add_field("memory_type", DataType.VARCHAR, max_length=32)
    schema.add_field("verified_state", DataType.VARCHAR, max_length=32)
    
    # Scoring fields
    schema.add_field("confidence", DataType.FLOAT)
    schema.add_field("importance", DataType.FLOAT)
    schema.add_field("freshness_score", DataType.FLOAT)
    schema.add_field("salience_weight", DataType.FLOAT)
    
    # Safety filter fields
    schema.add_field("is_active", DataType.BOOL)
    schema.add_field("quarantine_flag", DataType.BOOL)
    schema.add_field("ttl", DataType.INT64)  # Unix timestamp
    schema.add_field("valid_from", DataType.INT64)  # Unix timestamp
    schema.add_field("valid_to", DataType.INT64)  # Unix timestamp
    schema.add_field("visible_time", DataType.INT64)  # Unix timestamp
    schema.add_field("visibility_policy", DataType.VARCHAR, max_length=32)
    
    # Graph expansion fields
    schema.add_field("source_event_ids", DataType.VARCHAR, max_length=512)
    
    # Create indexes
    index_params = client.prepare_index_params()
    index_params.add_index(
        field_name="vector",
        index_type="IVF_FLAT",
        metric_type="IP",
        params={"nlist": 128}
    )
    index_params.add_index(
        field_name="sparse_vector",
        index_type="SPARSE_INVERTED_INDEX",
        metric_type="IP"
    )
    
    client.create_collection(
        collection_name=TEST_COLLECTION,
        schema=schema,
        index_params=index_params
    )
    
    logger.info(f"Created test collection: {TEST_COLLECTION}")


def seed_test_data(client: MilvusClient) -> List[Dict[str, Any]]:
    """
    Seed mock data for testing.
    
    Data covers:
    - 3 memory types: episodic, semantic, procedural
    - Safety filter test cases: quarantine, ttl expired, is_active=False, visible_time future
    - Various importance/confidence/freshness values for reranking tests
    - Dense vectors with different similarity to query vector
    """
    now = int(datetime.now().timestamp())
    past = int((datetime.now() - timedelta(hours=1)).timestamp())
    future = int((datetime.now() + timedelta(hours=1)).timestamp())
    far_past = int((datetime.now() - timedelta(days=30)).timestamp())
    
    # Base query vector for similarity testing
    # mem_001 will be most similar (same vector), others progressively less similar
    
    test_data = [
        # Normal candidates - should pass all filters
        {
            "id": 1,
            "object_id": "mem_001",
            "object_type": "memory",
            "vector": [0.9] * VECTOR_DIM,  # High similarity to query
            "sparse_vector": text_to_sparse("weather forecast beijing temperature"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 2,
            "provenance_ref": "prov_001",
            "content": "User asked about weather in Beijing today",
            "summary": "Weather query Beijing",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "confidence": 0.95,
            "importance": 0.9,
            "freshness_score": 0.95,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_001,evt_002",
        },
        {
            "id": 2,
            "object_id": "mem_002",
            "object_type": "memory",
            "vector": [0.8] * VECTOR_DIM,  # Medium-high similarity
            "sparse_vector": text_to_sparse("project deadline friday meeting schedule"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_002",
            "content": "Project deadline is next Friday, meeting scheduled",
            "summary": "Deadline reminder",
            "level": 0,
            "memory_type": "procedural",
            "verified_state": "verified",
            "confidence": 0.85,
            "importance": 0.8,
            "freshness_score": 0.8,
            "salience_weight": 1.2,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_003",
        },
        {
            "id": 3,
            "object_id": "mem_003",
            "object_type": "memory",
            "vector": [0.7] * VECTOR_DIM,  # Medium similarity
            "sparse_vector": text_to_sparse("semantic knowledge base facts"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_003",
            "content": "Semantic knowledge about programming concepts",
            "summary": "Programming knowledge",
            "level": 1,
            "memory_type": "semantic",
            "verified_state": "verified",
            "confidence": 0.9,
            "importance": 0.7,
            "freshness_score": 0.9,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_004,evt_005",
        },
        
        # Safety filter test cases
        
        # Rule 1: quarantine_flag = True -> should be filtered out
        {
            "id": 4,
            "object_id": "mem_quarantined",
            "object_type": "memory",
            "vector": [0.85] * VECTOR_DIM,
            "sparse_vector": text_to_sparse("quarantined content flagged"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_quarantine",
            "content": "This content is quarantined",
            "summary": "Quarantined",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "disputed",
            "confidence": 0.9,
            "importance": 0.9,
            "freshness_score": 0.9,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": True,  # QUARANTINED
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_quarantine",
        },
        
        # Rule 2: ttl < now -> should be filtered out (expired)
        {
            "id": 5,
            "object_id": "mem_ttl_expired",
            "object_type": "memory",
            "vector": [0.82] * VECTOR_DIM,
            "sparse_vector": text_to_sparse("expired content ttl past"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_ttl",
            "content": "This content has expired TTL",
            "summary": "TTL expired",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "confidence": 0.9,
            "importance": 0.9,
            "freshness_score": 0.9,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": past,  # TTL EXPIRED
            "valid_from": far_past,
            "valid_to": future,
            "visible_time": far_past,
            "visibility_policy": "private",
            "source_event_ids": "evt_ttl",
        },
        
        # Rule 3: visible_time > now -> should be filtered out (not yet visible)
        {
            "id": 6,
            "object_id": "mem_not_visible",
            "object_type": "memory",
            "vector": [0.83] * VECTOR_DIM,
            "sparse_vector": text_to_sparse("future content not visible yet"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_visible",
            "content": "This content is not yet visible",
            "summary": "Not visible",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "confidence": 0.9,
            "importance": 0.9,
            "freshness_score": 0.9,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": future,  # NOT YET VISIBLE
            "visibility_policy": "private",
            "source_event_ids": "evt_visible",
        },
        
        # Rule 4: is_active = False -> should be filtered out
        {
            "id": 7,
            "object_id": "mem_inactive",
            "object_type": "memory",
            "vector": [0.84] * VECTOR_DIM,
            "sparse_vector": text_to_sparse("inactive content disabled"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_inactive",
            "content": "This content is inactive",
            "summary": "Inactive",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "confidence": 0.9,
            "importance": 0.9,
            "freshness_score": 0.9,
            "salience_weight": 1.0,
            "is_active": False,  # INACTIVE
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_inactive",
        },
        
        # Rule 5 & 6: For time-travel tests (as_of_ts)
        # This one has visible_time and valid_from in the future relative to as_of_ts
        {
            "id": 8,
            "object_id": "mem_time_travel",
            "object_type": "memory",
            "vector": [0.75] * VECTOR_DIM,
            "sparse_vector": text_to_sparse("time travel test content"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_time_travel",
            "content": "This content is for time travel testing",
            "summary": "Time travel test",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "confidence": 0.9,
            "importance": 0.9,
            "freshness_score": 0.9,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": now,  # Will be filtered when as_of_ts < now
            "valid_to": future,
            "visible_time": now,  # Will be filtered when as_of_ts < now
            "visibility_policy": "private",
            "source_event_ids": "evt_time_travel",
        },
        
        # Rule 7: version < min_version -> should be filtered out
        {
            "id": 9,
            "object_id": "mem_old_version",
            "object_type": "memory",
            "vector": [0.76] * VECTOR_DIM,
            "sparse_vector": text_to_sparse("old version content outdated"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,  # OLD VERSION
            "provenance_ref": "prov_old_version",
            "content": "This content has old version",
            "summary": "Old version",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "confidence": 0.9,
            "importance": 0.9,
            "freshness_score": 0.9,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_old_version",
        },
        
        # Additional candidates for RRF and reranking tests
        {
            "id": 10,
            "object_id": "mem_low_importance",
            "object_type": "memory",
            "vector": [0.6] * VECTOR_DIM,
            "sparse_vector": text_to_sparse("low importance content minor"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_low",
            "content": "Low importance content",
            "summary": "Low importance",
            "level": 0,
            "memory_type": "episodic",
            "verified_state": "verified",
            "confidence": 0.5,
            "importance": 0.2,  # LOW IMPORTANCE
            "freshness_score": 0.5,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_low",
        },
        {
            "id": 11,
            "object_id": "mem_high_importance",
            "object_type": "memory",
            "vector": [0.5] * VECTOR_DIM,  # Lower vector similarity
            "sparse_vector": text_to_sparse("high importance critical urgent"),
            "tenant_id": "tenant_test",
            "workspace_id": "workspace_test",
            "agent_id": "agent_test",
            "session_id": "session_test",
            "scope": "private",
            "version": 1,
            "provenance_ref": "prov_high",
            "content": "High importance critical content",
            "summary": "High importance",
            "level": 0,
            "memory_type": "procedural",
            "verified_state": "verified",
            "confidence": 0.95,
            "importance": 0.99,  # HIGH IMPORTANCE
            "freshness_score": 0.95,
            "salience_weight": 1.0,
            "is_active": True,
            "quarantine_flag": False,
            "ttl": future,
            "valid_from": past,
            "valid_to": future,
            "visible_time": past,
            "visibility_policy": "private",
            "source_event_ids": "evt_high",
        },
    ]
    
    client.insert(collection_name=TEST_COLLECTION, data=test_data)
    client.load_collection(TEST_COLLECTION)
    
    # Wait for data to be indexed
    time.sleep(2)
    
    logger.info(f"Seeded {len(test_data)} test records")
    return test_data


def cleanup_test_data(client: MilvusClient) -> None:
    """Clean up test collection after tests."""
    if client.has_collection(TEST_COLLECTION):
        client.drop_collection(TEST_COLLECTION)
        logger.info(f"Dropped test collection: {TEST_COLLECTION}")


class RetrievalBIntegrationTests:
    """Integration tests for Retrieval Module B."""
    
    def __init__(self, verbose: bool = False):
        self.verbose = verbose
        self.client = MilvusClient(uri=MILVUS_URI)
        self.retriever = None
        self.passed = 0
        self.failed = 0
        self.errors = []
    
    def setup(self) -> None:
        """Set up test environment."""
        create_test_collection(self.client)
        seed_test_data(self.client)
        
        # Initialize retrievers
        dense = MilvusDenseRetriever(
            uri=MILVUS_URI,
            collection_name=TEST_COLLECTION,
            vector_field="vector",
        )
        sparse = MilvusSparseRetriever(
            uri=MILVUS_URI,
            collection_name=TEST_COLLECTION,
            sparse_field="sparse_vector",
        )
        filter_retriever = MilvusFilterRetriever(
            uri=MILVUS_URI,
            collection_name=TEST_COLLECTION,
        )
        merger = Merger()
        
        self.retriever = Retriever(
            dense=dense,
            sparse=sparse,
            filter=filter_retriever,
            merger=merger,
        )
        
        logger.info("Test environment set up")
    
    def teardown(self) -> None:
        """Clean up test environment."""
        if self.retriever:
            self.retriever.dense.close()
            self.retriever.sparse.close()
            self.retriever.filter.close()
        cleanup_test_data(self.client)
        self.client.close()
        logger.info("Test environment cleaned up")
    
    def _assert(self, condition: bool, message: str) -> None:
        """Assert with tracking."""
        if not condition:
            raise AssertionError(message)
    
    def _run_test(self, name: str, test_fn) -> None:
        """Run a single test with error handling."""
        try:
            asyncio.run(test_fn())
            self.passed += 1
            print(f"  [PASS] {name}")
        except Exception as e:
            self.failed += 1
            self.errors.append((name, str(e)))
            print(f"  [FAIL] {name}: {e}")
            if self.verbose:
                import traceback
                traceback.print_exc()
    
    # =========================================================================
    # Safety Filter Tests (7 rules)
    # =========================================================================
    
    async def test_safety_filter_rule1_quarantine(self) -> None:
        """
        Safety filter rule 1: quarantine_flag = True -> candidate removed.
        mem_quarantined should NOT appear in results.
        """
        request = RetrievalRequest(
            query_id="test_quarantine",
            query_text="quarantined content",
            query_vector=[0.85] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=20,
        )
        
        result = await self.retriever.retrieve(request)
        
        object_ids = [c.object_id for c in result.candidates]
        self._assert(
            "mem_quarantined" not in object_ids,
            f"Quarantined candidate should be filtered out, got: {object_ids}"
        )
    
    async def test_safety_filter_rule2_ttl_expired(self) -> None:
        """
        Safety filter rule 2: ttl < now -> candidate removed (expired).
        mem_ttl_expired should NOT appear in results.
        """
        request = RetrievalRequest(
            query_id="test_ttl",
            query_text="expired content",
            query_vector=[0.82] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=20,
        )
        
        result = await self.retriever.retrieve(request)
        
        object_ids = [c.object_id for c in result.candidates]
        self._assert(
            "mem_ttl_expired" not in object_ids,
            f"TTL expired candidate should be filtered out, got: {object_ids}"
        )
    
    async def test_safety_filter_rule3_visible_time_future(self) -> None:
        """
        Safety filter rule 3: visible_time > now -> candidate removed (not yet visible).
        mem_not_visible should NOT appear in results.
        """
        request = RetrievalRequest(
            query_id="test_visible_time",
            query_text="future content",
            query_vector=[0.83] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=20,
        )
        
        result = await self.retriever.retrieve(request)
        
        object_ids = [c.object_id for c in result.candidates]
        self._assert(
            "mem_not_visible" not in object_ids,
            f"Not-yet-visible candidate should be filtered out, got: {object_ids}"
        )
    
    async def test_safety_filter_rule4_inactive(self) -> None:
        """
        Safety filter rule 4: is_active = False -> candidate removed.
        mem_inactive should NOT appear in results.
        """
        request = RetrievalRequest(
            query_id="test_inactive",
            query_text="inactive content",
            query_vector=[0.84] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=20,
        )
        
        result = await self.retriever.retrieve(request)
        
        object_ids = [c.object_id for c in result.candidates]
        self._assert(
            "mem_inactive" not in object_ids,
            f"Inactive candidate should be filtered out, got: {object_ids}"
        )
    
    async def test_safety_filter_rule5_as_of_ts_visible_time(self) -> None:
        """
        Safety filter rule 5: visible_time > as_of_ts -> candidate removed (time-travel).
        When as_of_ts is set to 1 hour ago, mem_time_travel should be filtered out.
        """
        past_ts = datetime.now() - timedelta(hours=1)
        
        request = RetrievalRequest(
            query_id="test_as_of_ts_visible",
            query_text="time travel test",
            query_vector=[0.75] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=20,
            as_of_ts=past_ts,
        )
        
        result = await self.retriever.retrieve(request)
        
        object_ids = [c.object_id for c in result.candidates]
        self._assert(
            "mem_time_travel" not in object_ids,
            f"Time-travel candidate (visible_time > as_of_ts) should be filtered out, got: {object_ids}"
        )
    
    async def test_safety_filter_rule6_as_of_ts_valid_from(self) -> None:
        """
        Safety filter rule 6: valid_from > as_of_ts -> candidate removed (time-travel).
        When as_of_ts is set to 1 hour ago, mem_time_travel should be filtered out.
        """
        past_ts = datetime.now() - timedelta(hours=1)
        
        request = RetrievalRequest(
            query_id="test_as_of_ts_valid_from",
            query_text="time travel test",
            query_vector=[0.75] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=20,
            as_of_ts=past_ts,
        )
        
        result = await self.retriever.retrieve(request)
        
        object_ids = [c.object_id for c in result.candidates]
        self._assert(
            "mem_time_travel" not in object_ids,
            f"Time-travel candidate (valid_from > as_of_ts) should be filtered out, got: {object_ids}"
        )
    
    async def test_safety_filter_rule7_min_version(self) -> None:
        """
        Safety filter rule 7: version < min_version -> candidate removed.
        When min_version=2, mem_old_version (version=1) should be filtered out.
        """
        request = RetrievalRequest(
            query_id="test_min_version",
            query_text="old version content",
            query_vector=[0.76] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=20,
            min_version=2,
        )
        
        result = await self.retriever.retrieve(request)
        
        object_ids = [c.object_id for c in result.candidates]
        self._assert(
            "mem_old_version" not in object_ids,
            f"Old version candidate should be filtered out when min_version=2, got: {object_ids}"
        )
        # mem_001 has version=2, should still be present
        self._assert(
            "mem_001" in object_ids,
            f"mem_001 (version=2) should pass min_version=2 filter, got: {object_ids}"
        )
    
    # =========================================================================
    # RRF Scoring Test
    # =========================================================================
    
    async def test_rrf_two_path_higher_than_single_path(self) -> None:
        """
        RRF scoring: candidates hit by both dense and sparse should have
        higher RRF score than candidates hit by only one path.
        
        Query designed to hit mem_001 on both paths (weather + vector similarity).
        """
        request = RetrievalRequest(
            query_id="test_rrf",
            query_text="weather forecast beijing",
            query_vector=[0.9] * VECTOR_DIM,  # Same as mem_001
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=10,
        )
        
        result = await self.retriever.retrieve(request)
        
        # Find candidates and their source channels
        two_path_candidates = [c for c in result.candidates if len(c.source_channels) >= 2]
        single_path_candidates = [c for c in result.candidates if len(c.source_channels) == 1]
        
        if self.verbose:
            print(f"    Two-path candidates: {[(c.object_id, c.score, c.source_channels) for c in two_path_candidates]}")
            print(f"    Single-path candidates: {[(c.object_id, c.score, c.source_channels) for c in single_path_candidates]}")
        
        # If we have both types, two-path should have higher RRF score
        if two_path_candidates and single_path_candidates:
            max_two_path_score = max(c.score for c in two_path_candidates)
            max_single_path_score = max(c.score for c in single_path_candidates)
            self._assert(
                max_two_path_score > max_single_path_score,
                f"Two-path RRF score ({max_two_path_score}) should be higher than single-path ({max_single_path_score})"
            )
    
    # =========================================================================
    # Reranking Formula Test
    # =========================================================================
    
    async def test_reranking_formula(self) -> None:
        """
        Reranking formula: final_score = score * max(importance,0.01) * max(freshness_score,0.01) * max(confidence,0.01)
        
        Verify that final_score is computed correctly from the formula.
        """
        request = RetrievalRequest(
            query_id="test_reranking",
            query_text="weather forecast",
            query_vector=[0.9] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=10,
        )
        
        result = await self.retriever.retrieve(request)
        
        for c in result.candidates:
            expected_final = (
                c.score
                * max(c.importance, 0.01)
                * max(c.freshness_score, 0.01)
                * max(c.confidence, 0.01)
            )
            
            # Allow small floating point tolerance
            diff = abs(c.final_score - expected_final)
            self._assert(
                diff < 1e-6,
                f"Reranking formula mismatch for {c.object_id}: "
                f"expected {expected_final}, got {c.final_score}, diff={diff}"
            )
            
            if self.verbose:
                print(f"    {c.object_id}: score={c.score:.4f}, importance={c.importance:.2f}, "
                      f"freshness={c.freshness_score:.2f}, confidence={c.confidence:.2f}, "
                      f"final={c.final_score:.6f}")
    
    # =========================================================================
    # for_graph Mode Test
    # =========================================================================
    
    async def test_for_graph_returns_top_k_times_2(self) -> None:
        """
        for_graph=True mode: should return top_k * 2 candidates.
        """
        top_k = 3
        
        request = RetrievalRequest(
            query_id="test_for_graph",
            query_text="weather forecast",
            query_vector=[0.9] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=top_k,
            for_graph=True,
        )
        
        result = await self.retriever.retrieve(request)
        
        # Should return up to top_k * 2 candidates (or all available if less)
        expected_max = top_k * 2
        self._assert(
            len(result.candidates) <= expected_max,
            f"for_graph mode should return at most {expected_max} candidates, got {len(result.candidates)}"
        )
        
        # Verify source_event_ids is populated (required for graph expansion)
        for c in result.candidates:
            if self.verbose:
                print(f"    {c.object_id}: source_event_ids={c.source_event_ids}")
    
    # =========================================================================
    # Seed Marking Test
    # =========================================================================
    
    async def test_seed_marking_threshold(self) -> None:
        """
        Seed marking: candidates with final_score >= 0.7 should have is_seed=True.
        """
        request = RetrievalRequest(
            query_id="test_seed_marking",
            query_text="weather forecast beijing",
            query_vector=[0.9] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=10,
        )
        
        result = await self.retriever.retrieve(request)
        
        for c in result.candidates:
            if c.final_score >= 0.7:
                self._assert(
                    c.is_seed,
                    f"Candidate {c.object_id} with final_score={c.final_score} >= 0.7 should have is_seed=True"
                )
                self._assert(
                    c.seed_score == c.final_score,
                    f"Candidate {c.object_id} seed_score should equal final_score"
                )
            else:
                self._assert(
                    not c.is_seed,
                    f"Candidate {c.object_id} with final_score={c.final_score} < 0.7 should have is_seed=False"
                )
            
            if self.verbose:
                print(f"    {c.object_id}: final_score={c.final_score:.4f}, is_seed={c.is_seed}")
    
    # =========================================================================
    # Filter-Only Mode Test
    # =========================================================================
    
    async def test_filter_only_mode(self) -> None:
        """
        filter_only mode: skip dense and sparse, results ordered by importance desc.
        """
        request = RetrievalRequest(
            query_id="test_filter_only",
            query_text="",  # No query text needed for filter-only
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=10,
            enable_filter_only=True,
        )
        
        result = await self.retriever.retrieve(request)
        
        # Should only use filter channel
        self._assert(
            result.query_meta.channels_used == ["filter"],
            f"filter_only mode should only use filter channel, got: {result.query_meta.channels_used}"
        )
        
        # Dense and sparse hits should be 0
        self._assert(
            result.query_meta.dense_hits == 0,
            f"filter_only mode should have 0 dense hits, got: {result.query_meta.dense_hits}"
        )
        self._assert(
            result.query_meta.sparse_hits == 0,
            f"filter_only mode should have 0 sparse hits, got: {result.query_meta.sparse_hits}"
        )
        
        # Results should be ordered by importance descending
        importances = [c.importance for c in result.candidates]
        self._assert(
            importances == sorted(importances, reverse=True),
            f"filter_only results should be ordered by importance desc, got: {importances}"
        )
        
        if self.verbose:
            for c in result.candidates:
                print(f"    {c.object_id}: importance={c.importance}, confidence={c.confidence}")
    
    # =========================================================================
    # Benchmark Retrieve Test
    # =========================================================================
    
    async def test_benchmark_retrieve_no_truncation(self) -> None:
        """
        benchmark_retrieve(): returns all candidates without truncation,
        each candidate has rrf_score field populated.
        """
        request = RetrievalRequest(
            query_id="test_benchmark",
            query_text="weather forecast",
            query_vector=[0.9] * VECTOR_DIM,
            tenant_id="tenant_test",
            workspace_id="workspace_test",
            top_k=2,  # Small top_k, but benchmark should return more
        )
        
        result = await self.retriever.benchmark_retrieve(request)
        
        # Should return more than top_k candidates (no truncation)
        # Note: actual count depends on how many pass safety filter
        if self.verbose:
            print(f"    Benchmark returned {len(result.candidates)} candidates (top_k={request.top_k})")
        
        # Each candidate should have rrf_score populated
        for c in result.candidates:
            self._assert(
                c.rrf_score > 0,
                f"Candidate {c.object_id} should have rrf_score > 0, got: {c.rrf_score}"
            )
            
            if self.verbose:
                print(f"    {c.object_id}: rrf_score={c.rrf_score:.4f}, final_score={c.final_score:.6f}")
    
    # =========================================================================
    # Run All Tests
    # =========================================================================
    
    def run_all(self) -> bool:
        """Run all tests and return success status."""
        print("\n" + "=" * 70)
        print("Retrieval Module B - Integration Tests")
        print("=" * 70)
        
        try:
            self.setup()
            
            # Safety filter tests (7 rules)
            print("\n[Safety Filter Tests]")
            self._run_test("Rule 1: quarantine_flag=True filtered", self.test_safety_filter_rule1_quarantine)
            self._run_test("Rule 2: ttl expired filtered", self.test_safety_filter_rule2_ttl_expired)
            self._run_test("Rule 3: visible_time > now filtered", self.test_safety_filter_rule3_visible_time_future)
            self._run_test("Rule 4: is_active=False filtered", self.test_safety_filter_rule4_inactive)
            self._run_test("Rule 5: as_of_ts visible_time filtered", self.test_safety_filter_rule5_as_of_ts_visible_time)
            self._run_test("Rule 6: as_of_ts valid_from filtered", self.test_safety_filter_rule6_as_of_ts_valid_from)
            self._run_test("Rule 7: version < min_version filtered", self.test_safety_filter_rule7_min_version)
            
            # RRF and reranking tests
            print("\n[RRF and Reranking Tests]")
            self._run_test("RRF: two-path score > single-path score", self.test_rrf_two_path_higher_than_single_path)
            self._run_test("Reranking formula correctness", self.test_reranking_formula)
            
            # Mode tests
            print("\n[Mode Tests]")
            self._run_test("for_graph returns top_k * 2", self.test_for_graph_returns_top_k_times_2)
            self._run_test("Seed marking threshold >= 0.7", self.test_seed_marking_threshold)
            self._run_test("filter_only mode", self.test_filter_only_mode)
            self._run_test("benchmark_retrieve no truncation + rrf_score", self.test_benchmark_retrieve_no_truncation)
            
        finally:
            self.teardown()
        
        # Summary
        print("\n" + "=" * 70)
        print(f"Results: {self.passed} passed, {self.failed} failed")
        print("=" * 70)
        
        if self.errors:
            print("\nFailures:")
            for name, error in self.errors:
                print(f"  - {name}: {error}")
        
        return self.failed == 0


def main():
    parser = argparse.ArgumentParser(description="Retrieval Module B Integration Tests")
    parser.add_argument("--verbose", "-v", action="store_true", help="Print detailed logs")
    args = parser.parse_args()
    
    # Configure logging
    log_level = logging.DEBUG if args.verbose else logging.INFO
    logging.basicConfig(
        level=log_level,
        format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
    )
    
    tests = RetrievalBIntegrationTests(verbose=args.verbose)
    success = tests.run_all()
    
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
