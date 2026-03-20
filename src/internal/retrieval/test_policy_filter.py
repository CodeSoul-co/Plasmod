"""
Test script for Policy Filter
Run: python -m src.internal.retrieval.test_policy_filter
"""

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent.parent.parent))

from src.internal.retrieval.service.policy_filter import PolicyFilter
from src.internal.retrieval.service.types import Candidate


def create_candidate(
    object_id: str,
    agent_id: str = "agent_1",
    scope: str = "private",
    quarantine_flag: bool = False,
    verified_state: str = "verified",
    visibility_policy: str = "private",
) -> Candidate:
    """Helper to create test candidate"""
    c = Candidate(
        object_id=object_id,
        object_type="memory",
        score=0.5,
        agent_id=agent_id,
        scope=scope,
        verified_state=verified_state,
    )
    c.quarantine_flag = quarantine_flag
    c.visibility_policy = visibility_policy
    return c


def test_quarantine_filter():
    """Test quarantine_flag filtering"""
    print("\n" + "=" * 60)
    print("Test 1: Quarantine filtering")
    print("=" * 60)
    
    candidates = [
        create_candidate("mem_001", quarantine_flag=False),
        create_candidate("mem_002", quarantine_flag=True),   # Should be filtered
        create_candidate("mem_003", quarantine_flag=False),
    ]
    
    pf = PolicyFilter()
    result = pf.filter(candidates)
    
    print(f"Input: 3 candidates (1 quarantined)")
    print(f"Output: {len(result)} candidates")
    
    object_ids = [c.object_id for c in result]
    print(f"Passed: {object_ids}")
    
    assert len(result) == 2, f"Expected 2, got {len(result)}"
    assert "mem_002" not in object_ids, "Quarantined object should be filtered"
    
    print("\n[PASS] Quarantine filtering works")


def test_unverified_filter():
    """Test unverified content filtering"""
    print("\n" + "=" * 60)
    print("Test 2: Unverified filtering")
    print("=" * 60)
    
    candidates = [
        create_candidate("mem_001", verified_state="verified"),
        create_candidate("mem_002", verified_state="unverified"),
        create_candidate("mem_003", verified_state="disputed"),
    ]
    
    pf = PolicyFilter(exclude_unverified=True)
    result = pf.filter(candidates)
    
    print(f"Input: 3 candidates (1 unverified)")
    print(f"Output: {len(result)} candidates")
    
    object_ids = [c.object_id for c in result]
    print(f"Passed: {object_ids}")
    
    assert len(result) == 2, f"Expected 2, got {len(result)}"
    assert "mem_002" not in object_ids, "Unverified object should be filtered"
    
    print("\n[PASS] Unverified filtering works")


def test_private_scope_acl():
    """Test private scope ACL check"""
    print("\n" + "=" * 60)
    print("Test 3: Private scope ACL")
    print("=" * 60)
    
    candidates = [
        create_candidate("mem_001", agent_id="agent_1", scope="private"),
        create_candidate("mem_002", agent_id="agent_2", scope="private"),  # Different agent
        create_candidate("mem_003", agent_id="agent_1", scope="workspace"),  # Workspace scope
    ]
    
    pf = PolicyFilter()
    result = pf.filter(candidates, requesting_agent_id="agent_1")
    
    print(f"Input: 3 candidates")
    print(f"Requesting agent: agent_1")
    print(f"Output: {len(result)} candidates")
    
    object_ids = [c.object_id for c in result]
    print(f"Passed: {object_ids}")
    
    assert len(result) == 2, f"Expected 2, got {len(result)}"
    assert "mem_002" not in object_ids, "Private scope from other agent should be filtered"
    
    print("\n[PASS] Private scope ACL works")


def test_visibility_policy():
    """Test visibility policy filtering"""
    print("\n" + "=" * 60)
    print("Test 4: Visibility policy")
    print("=" * 60)
    
    candidates = [
        create_candidate("mem_001", visibility_policy="public"),
        create_candidate("mem_002", visibility_policy="private"),
        create_candidate("mem_003", visibility_policy="restricted"),  # Not in allowed set
    ]
    
    pf = PolicyFilter(allowed_visibility={"public", "private"})
    result = pf.filter(candidates)
    
    print(f"Input: 3 candidates")
    print(f"Allowed visibility: public, private")
    print(f"Output: {len(result)} candidates")
    
    object_ids = [c.object_id for c in result]
    print(f"Passed: {object_ids}")
    
    assert len(result) == 2, f"Expected 2, got {len(result)}"
    assert "mem_003" not in object_ids, "Restricted visibility should be filtered"
    
    print("\n[PASS] Visibility policy works")


def test_combined_filters():
    """Test multiple filters combined"""
    print("\n" + "=" * 60)
    print("Test 5: Combined filters")
    print("=" * 60)
    
    candidates = [
        create_candidate("mem_001", quarantine_flag=False, verified_state="verified"),
        create_candidate("mem_002", quarantine_flag=True, verified_state="verified"),   # Quarantined
        create_candidate("mem_003", quarantine_flag=False, verified_state="unverified"), # Unverified
        create_candidate("mem_004", quarantine_flag=False, verified_state="verified"),
    ]
    
    pf = PolicyFilter(exclude_unverified=True)
    result = pf.filter(candidates)
    
    print(f"Input: 4 candidates")
    print(f"Output: {len(result)} candidates")
    
    object_ids = [c.object_id for c in result]
    print(f"Passed: {object_ids}")
    
    assert len(result) == 2, f"Expected 2, got {len(result)}"
    assert "mem_001" in object_ids
    assert "mem_004" in object_ids
    
    print("\n[PASS] Combined filters work")


def test_disable_quarantine_check():
    """Test disabling quarantine check"""
    print("\n" + "=" * 60)
    print("Test 6: Disable quarantine check")
    print("=" * 60)
    
    candidates = [
        create_candidate("mem_001", quarantine_flag=False),
        create_candidate("mem_002", quarantine_flag=True),
    ]
    
    pf = PolicyFilter()
    result = pf.filter(candidates, exclude_quarantined=False)
    
    print(f"Input: 2 candidates (1 quarantined)")
    print(f"exclude_quarantined=False")
    print(f"Output: {len(result)} candidates")
    
    assert len(result) == 2, "Both should pass when quarantine check disabled"
    
    print("\n[PASS] Disable quarantine check works")


def main():
    print("Testing Policy Filter")
    
    test_quarantine_filter()
    test_unverified_filter()
    test_private_scope_acl()
    test_visibility_policy()
    test_combined_filters()
    test_disable_quarantine_check()
    
    print("\n" + "=" * 60)
    print("ALL TESTS PASSED")
    print("=" * 60)


if __name__ == "__main__":
    main()
