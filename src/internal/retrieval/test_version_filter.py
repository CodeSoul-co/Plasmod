"""
Test script for Version Filter
Run: python -m src.internal.retrieval.test_version_filter
"""

import sys
from pathlib import Path
from datetime import datetime, timedelta

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

from src.internal.retrieval.service.version_filter import VersionFilter
from src.internal.retrieval.service.types import Candidate


def create_candidate(
    object_id: str,
    version: int = 1,
    valid_from: datetime = None,
    valid_to: datetime = None,
    visible_time: datetime = None,
) -> Candidate:
    """Helper to create test candidate"""
    c = Candidate(
        object_id=object_id,
        object_type="memory",
        score=0.5,
        version=version,
    )
    c.valid_from = valid_from
    c.valid_to = valid_to
    c.visible_time = visible_time
    return c


def test_visible_before_ts():
    """Test visible_before_ts filtering"""
    print("\n" + "=" * 60)
    print("Test 1: visible_before_ts")
    print("=" * 60)
    
    now = datetime.now()
    hour_ago = now - timedelta(hours=1)
    two_hours_ago = now - timedelta(hours=2)
    
    candidates = [
        create_candidate("mem_001", valid_from=two_hours_ago),  # Should pass
        create_candidate("mem_002", valid_from=hour_ago),        # Should pass
        create_candidate("mem_003", valid_from=now),             # Should fail
    ]
    
    vf = VersionFilter()
    result = vf.filter(candidates, visible_before_ts=hour_ago + timedelta(minutes=30))
    
    print(f"Input: 3 candidates")
    print(f"Filter: visible_before_ts = {hour_ago + timedelta(minutes=30)}")
    print(f"Output: {len(result)} candidates")
    
    object_ids = [c.object_id for c in result]
    print(f"Passed: {object_ids}")
    
    assert len(result) == 2, f"Expected 2, got {len(result)}"
    assert "mem_001" in object_ids
    assert "mem_002" in object_ids
    assert "mem_003" not in object_ids
    
    print("\n[PASS] visible_before_ts works")


def test_version_at():
    """Test version_at exact version filtering"""
    print("\n" + "=" * 60)
    print("Test 2: version_at")
    print("=" * 60)
    
    now = datetime.now()
    hour_ago = now - timedelta(hours=1)
    two_hours_ago = now - timedelta(hours=2)
    half_hour_ago = now - timedelta(minutes=30)
    
    candidates = [
        # Version active from 2 hours ago to 1 hour ago
        create_candidate("mem_001", version=1, valid_from=two_hours_ago, valid_to=hour_ago),
        # Version active from 1 hour ago, still current
        create_candidate("mem_001", version=2, valid_from=hour_ago, valid_to=None),
        # Different object, active from 30 min ago
        create_candidate("mem_002", version=1, valid_from=half_hour_ago),
    ]
    
    vf = VersionFilter()
    
    # Query at 90 minutes ago - should get mem_001 v1 only
    query_time = now - timedelta(minutes=90)
    result = vf.filter(candidates, version_at=query_time)
    
    print(f"Query time: 90 minutes ago")
    print(f"Output: {len(result)} candidates")
    for c in result:
        print(f"  {c.object_id} v{c.version}")
    
    assert len(result) == 1, f"Expected 1, got {len(result)}"
    assert result[0].object_id == "mem_001"
    assert result[0].version == 1
    
    print("\n[PASS] version_at works")


def test_filter_to_latest_versions():
    """Test deduplication to latest versions"""
    print("\n" + "=" * 60)
    print("Test 3: filter_to_latest_versions")
    print("=" * 60)
    
    now = datetime.now()
    hour_ago = now - timedelta(hours=1)
    
    candidates = [
        create_candidate("mem_001", version=1, valid_from=hour_ago),
        create_candidate("mem_001", version=2, valid_from=now),
        create_candidate("mem_001", version=3, valid_from=now),
        create_candidate("mem_002", version=1, valid_from=now),
    ]
    
    vf = VersionFilter()
    result = vf.filter_to_latest_versions(candidates)
    
    print(f"Input: 4 candidates (3 versions of mem_001, 1 of mem_002)")
    print(f"Output: {len(result)} candidates")
    for c in result:
        print(f"  {c.object_id} v{c.version}")
    
    assert len(result) == 2, f"Expected 2, got {len(result)}"
    
    mem_001 = next(c for c in result if c.object_id == "mem_001")
    assert mem_001.version == 3, f"Expected v3, got v{mem_001.version}"
    
    print("\n[PASS] filter_to_latest_versions works")


def test_no_version_info():
    """Test candidates without version info pass through"""
    print("\n" + "=" * 60)
    print("Test 4: No version info")
    print("=" * 60)
    
    candidates = [
        create_candidate("mem_001"),  # No version info
        create_candidate("mem_002"),
    ]
    
    vf = VersionFilter()
    result = vf.filter(
        candidates,
        visible_before_ts=datetime.now() - timedelta(hours=1),
    )
    
    print(f"Input: 2 candidates without version info")
    print(f"Output: {len(result)} candidates")
    
    assert len(result) == 2, "Candidates without version info should pass through"
    
    print("\n[PASS] No version info passes through")


def main():
    print("Testing Version Filter")
    
    test_visible_before_ts()
    test_version_at()
    test_filter_to_latest_versions()
    test_no_version_info()
    
    print("\n" + "=" * 60)
    print("ALL TESTS PASSED")
    print("=" * 60)


if __name__ == "__main__":
    main()
