"""
Test script for Merger (RRF fusion logic)
Run: python -m src.internal.retrieval.test_merger
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

from src.internal.retrieval.service.types import Candidate, RetrievalRequest
from src.internal.retrieval.service.merger import Merger


def create_candidate(
    object_id: str,
    confidence: float = 0.8,
    importance: float = 0.5,
    salience_weight: float = 1.0,
    freshness_score: float = 1.0,
) -> Candidate:
    return Candidate(
        object_id=object_id,
        object_type="memory",
        score=0.0,
        agent_id="agent_1",
        session_id="session_1",
        scope="private",
        version=1,
        provenance_ref=f"prov_{object_id}",
        content=f"Content of {object_id}",
        summary=f"Summary of {object_id}",
        confidence=confidence,
        importance=importance,
        freshness_score=freshness_score,
        level=0,
        memory_type="episodic",
        verified_state="verified",
        salience_weight=salience_weight,
    )


def test_basic_rrf():
    """Test basic RRF fusion with three channels"""
    print("=" * 60)
    print("Test 1: Basic RRF fusion")
    print("=" * 60)
    
    # Dense results: doc_a (rank 1), doc_b (rank 2), doc_c (rank 3)
    dense_results = [
        create_candidate("doc_a"),
        create_candidate("doc_b"),
        create_candidate("doc_c"),
    ]
    
    # Sparse results: doc_b (rank 1), doc_d (rank 2), doc_a (rank 3)
    sparse_results = [
        create_candidate("doc_b"),
        create_candidate("doc_d"),
        create_candidate("doc_a"),
    ]
    
    # Filter results: doc_a (rank 1), doc_e (rank 2)
    filter_results = [
        create_candidate("doc_a"),
        create_candidate("doc_e"),
    ]
    
    request = RetrievalRequest(
        query_text="test query",
        tenant_id="tenant_1",
        workspace_id="workspace_1",
        top_k=10,
    )
    
    merger = Merger(k=60)
    result = merger.merge(dense_results, sparse_results, filter_results, request)
    
    print(f"Total found: {result.total_found}")
    print(f"Channels used: {result.query_meta.channels_used}")
    print(f"Dense hits: {result.query_meta.dense_hits}")
    print(f"Sparse hits: {result.query_meta.sparse_hits}")
    print(f"Filter hits: {result.query_meta.filter_hits}")
    print()
    
    print("Candidates (sorted by RRF score):")
    for i, c in enumerate(result.candidates, 1):
        print(f"  {i}. {c.object_id}: score={c.score:.6f}, sources={c.source_channels}, is_seed={c.is_seed}")
    
    # Verify doc_a has highest score (appears in all 3 channels)
    assert result.candidates[0].object_id == "doc_a", "doc_a should be ranked first (appears in all channels)"
    assert "dense" in result.candidates[0].source_channels
    assert "sparse" in result.candidates[0].source_channels
    assert "filter" in result.candidates[0].source_channels
    
    print("\n[PASS] doc_a ranked first with all 3 source channels")


def test_rrf_score_calculation():
    """Verify RRF score calculation: score = sum(1/(k+rank))"""
    print("\n" + "=" * 60)
    print("Test 2: RRF score calculation")
    print("=" * 60)
    
    # doc_x appears at rank 1 in dense, rank 2 in sparse
    dense_results = [create_candidate("doc_x")]
    sparse_results = [create_candidate("doc_y"), create_candidate("doc_x")]
    filter_results = []
    
    request = RetrievalRequest(
        query_text="test",
        tenant_id="t1",
        workspace_id="w1",
        top_k=10,
    )
    
    merger = Merger(k=60)
    result = merger.merge(dense_results, sparse_results, filter_results, request)
    
    # Expected score for doc_x: 1/(60+1) + 1/(60+2) = 1/61 + 1/62
    expected_score = 1/61 + 1/62
    actual_score = next(c.score for c in result.candidates if c.object_id == "doc_x")
    
    print(f"doc_x expected score: {expected_score:.6f}")
    print(f"doc_x actual score:   {actual_score:.6f}")
    
    assert abs(actual_score - expected_score) < 1e-9, "RRF score calculation mismatch"
    print("\n[PASS] RRF score calculation correct")


def test_reranking():
    """Test reranking formula: final_score = rrf * importance * freshness * confidence"""
    print("\n" + "=" * 60)
    print("Test 3: Reranking (rrf * importance * freshness * confidence)")
    print("=" * 60)
    
    # doc_a: higher RRF rank but low importance
    # doc_b: lower RRF rank but high importance and confidence
    dense_results = [
        create_candidate("doc_a", importance=0.1, confidence=0.5, freshness_score=0.5),  # rank 1
        create_candidate("doc_b", importance=0.9, confidence=0.9, freshness_score=1.0),  # rank 2
    ]
    
    request = RetrievalRequest(
        query_text="test",
        tenant_id="t1",
        workspace_id="w1",
        top_k=10,
    )
    
    merger = Merger(k=60)
    result = merger.merge(dense_results, [], [], request)
    
    rrf_a = 1/61
    rrf_b = 1/62
    expected_final_a = rrf_a * 0.1 * 0.5 * 0.5
    expected_final_b = rrf_b * 0.9 * 0.9 * 1.0
    
    print(f"  doc_a: rrf={rrf_a:.6f}, final={expected_final_a:.8f} (low importance/confidence)")
    print(f"  doc_b: rrf={rrf_b:.6f}, final={expected_final_b:.8f} (high importance/confidence)")
    
    print("\nAfter reranking:")
    for i, c in enumerate(result.candidates, 1):
        print(f"  {i}. {c.object_id}: rrf={c.score:.6f}, final_score={c.final_score:.8f}")
    
    # doc_b should be ranked first because importance*freshness*confidence >> doc_a
    assert result.candidates[0].object_id == "doc_b", "doc_b should be ranked first after reranking"
    print("\n[PASS] Reranking works correctly")


def test_confidence_filtering():
    """Test min_confidence filtering"""
    print("\n" + "=" * 60)
    print("Test 4: Confidence filtering")
    print("=" * 60)
    
    dense_results = [
        create_candidate("doc_high", confidence=0.9),
        create_candidate("doc_low", confidence=0.3),
    ]
    
    request = RetrievalRequest(
        query_text="test",
        tenant_id="t1",
        workspace_id="w1",
        top_k=10,
        min_confidence=0.5,
    )
    
    merger = Merger(k=60)
    result = merger.merge(dense_results, [], [], request)
    
    print(f"min_confidence: {request.min_confidence}")
    print(f"Candidates returned: {len(result.candidates)}")
    for c in result.candidates:
        print(f"  {c.object_id}: confidence={c.confidence}")
    
    assert len(result.candidates) == 1, "Should filter out low confidence candidate"
    assert result.candidates[0].object_id == "doc_high"
    print("\n[PASS] Confidence filtering works correctly")


def test_top_k_truncation():
    """Test top_k truncation"""
    print("\n" + "=" * 60)
    print("Test 5: Top-k truncation")
    print("=" * 60)
    
    dense_results = [create_candidate(f"doc_{i}") for i in range(10)]
    
    request = RetrievalRequest(
        query_text="test",
        tenant_id="t1",
        workspace_id="w1",
        top_k=3,
    )
    
    merger = Merger(k=60)
    result = merger.merge(dense_results, [], [], request)
    
    print(f"Input candidates: 10")
    print(f"top_k: {request.top_k}")
    print(f"Output candidates: {len(result.candidates)}")
    
    assert len(result.candidates) == 3, "Should truncate to top_k"
    print("\n[PASS] Top-k truncation works correctly")


def test_seed_marking():
    """Test seed marking for graph expansion"""
    print("\n" + "=" * 60)
    print("Test 6: Seed marking")
    print("=" * 60)
    
    # Create candidates with high importance/confidence/freshness so final_score > threshold
    dense_results = [
        create_candidate("doc_a", importance=0.9, confidence=0.9, freshness_score=1.0),
        create_candidate("doc_b", importance=0.01, confidence=0.01, freshness_score=0.01),
    ]
    
    request = RetrievalRequest(
        query_text="test",
        tenant_id="t1",
        workspace_id="w1",
        top_k=10,
    )
    
    # final_score for doc_a: (1/61)*0.9*1.0*0.9 = ~0.01327 -> below 0.5
    # Use a low seed_threshold to test marking
    merger = Merger(k=60, seed_threshold=0.001)
    result = merger.merge(dense_results, [], [], request)
    
    print(f"Seed threshold: 0.001")
    for c in result.candidates:
        print(f"  {c.object_id}: final_score={c.final_score:.8f}, is_seed={c.is_seed}")
    
    doc_a = next(c for c in result.candidates if c.object_id == "doc_a")
    assert doc_a.is_seed == True, "doc_a should be marked as seed"
    
    print("\n[PASS] Seed marking works correctly")


if __name__ == "__main__":
    print("Testing Merger (RRF Fusion)")
    print()
    
    test_basic_rrf()
    test_rrf_score_calculation()
    test_reranking()
    test_confidence_filtering()
    test_top_k_truncation()
    test_seed_marking()
    
    print("\n" + "=" * 60)
    print("ALL TESTS PASSED")
    print("=" * 60)
