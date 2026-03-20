"""
Policy Filter - Governance-aware filtering.

Filters out objects based on:
- quarantine_flag: Quarantined objects are excluded
- visibility_policy: ACL-based access control
- verified_state: Optionally filter by verification status
"""

import logging
from typing import List, Optional, Set

from .types import Candidate

logger = logging.getLogger(__name__)


class PolicyFilter:
    """
    Governance-aware filtering for access control and quarantine.
    
    This filter ensures:
    1. Quarantined objects never appear in results
    2. Visibility policies are respected
    3. Unverified content can be optionally excluded
    """
    
    def __init__(
        self,
        exclude_quarantined: bool = True,
        exclude_unverified: bool = False,
        allowed_visibility: Optional[Set[str]] = None,
    ):
        self.exclude_quarantined = exclude_quarantined
        self.exclude_unverified = exclude_unverified
        self.allowed_visibility = allowed_visibility or {"public", "private", "workspace"}
    
    def filter(
        self,
        candidates: List[Candidate],
        requesting_agent_id: Optional[str] = None,
        requesting_tenant_id: Optional[str] = None,
        exclude_quarantined: Optional[bool] = None,
        exclude_unverified: Optional[bool] = None,
    ) -> List[Candidate]:
        """
        Apply policy filtering to candidates.
        
        Args:
            candidates: List of candidates to filter
            requesting_agent_id: Agent making the request (for ACL checks)
            requesting_tenant_id: Tenant making the request (for isolation)
            exclude_quarantined: Override default quarantine exclusion
            exclude_unverified: Override default unverified exclusion
            
        Returns:
            Filtered list of candidates
        """
        if not candidates:
            return []
        
        # Determine effective settings
        check_quarantine = exclude_quarantined if exclude_quarantined is not None else self.exclude_quarantined
        check_unverified = exclude_unverified if exclude_unverified is not None else self.exclude_unverified
        
        filtered = []
        for candidate in candidates:
            if self._passes_policy_check(
                candidate,
                requesting_agent_id=requesting_agent_id,
                requesting_tenant_id=requesting_tenant_id,
                check_quarantine=check_quarantine,
                check_unverified=check_unverified,
            ):
                filtered.append(candidate)
        
        return filtered
    
    def _passes_policy_check(
        self,
        candidate: Candidate,
        requesting_agent_id: Optional[str],
        requesting_tenant_id: Optional[str],
        check_quarantine: bool,
        check_unverified: bool,
    ) -> bool:
        """Check if candidate passes all policy constraints"""
        
        # Quarantine check
        if check_quarantine:
            quarantine_flag = getattr(candidate, 'quarantine_flag', False)
            if quarantine_flag:
                logger.debug(f"Filtered {candidate.object_id}: quarantined")
                return False
        
        # Verified state check
        if check_unverified:
            verified_state = getattr(candidate, 'verified_state', 'verified')
            if verified_state == 'unverified':
                logger.debug(f"Filtered {candidate.object_id}: unverified")
                return False
        
        # Visibility policy check
        visibility = getattr(candidate, 'visibility_policy', None)
        if visibility and visibility not in self.allowed_visibility:
            logger.debug(f"Filtered {candidate.object_id}: visibility={visibility} not allowed")
            return False
        
        # ACL check for private scope
        scope = getattr(candidate, 'scope', '')
        if scope == 'private':
            # Private scope requires matching agent_id
            if requesting_agent_id and candidate.agent_id != requesting_agent_id:
                logger.debug(f"Filtered {candidate.object_id}: private scope, agent mismatch")
                return False
        
        return True
    
    def mark_quarantined(
        self,
        candidates: List[Candidate],
        object_ids: Set[str],
    ) -> List[Candidate]:
        """
        Mark specific objects as quarantined (for soft filtering).
        
        Instead of removing, this marks objects so downstream can decide.
        """
        for candidate in candidates:
            if candidate.object_id in object_ids:
                setattr(candidate, 'quarantine_flag', True)
        return candidates
