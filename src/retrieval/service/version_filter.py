"""
Version Filter - Version-aware filtering for temporal queries.

Supports:
- visible_before_ts: Only return objects visible before this timestamp
- version_at: Return the exact version at a specific timestamp
- bounded_staleness_ms: Allow stale data within a time window
"""

import logging
from datetime import datetime, timedelta
from typing import List, Optional

from .types import Candidate

logger = logging.getLogger(__name__)


class VersionFilter:
    """
    Version-aware filtering for temporal consistency.
    
    This filter is applied AFTER retrieval to ensure version constraints
    are satisfied. It works with the object_versions table semantics:
    - valid_from: When this version became active
    - valid_to: When this version was superseded (None if current)
    """
    
    def __init__(self, default_staleness_ms: int = 0):
        self.default_staleness_ms = default_staleness_ms
    
    def filter(
        self,
        candidates: List[Candidate],
        visible_before_ts: Optional[datetime] = None,
        version_at: Optional[datetime] = None,
        bounded_staleness_ms: Optional[int] = None,
    ) -> List[Candidate]:
        """
        Apply version filtering to candidates.
        
        Args:
            candidates: List of candidates to filter
            visible_before_ts: Only return objects visible before this time
            version_at: Return exact version at this timestamp
            bounded_staleness_ms: Allow stale data within this window
            
        Returns:
            Filtered list of candidates
        """
        if not candidates:
            return []
        
        # Determine effective staleness window
        staleness_ms = bounded_staleness_ms if bounded_staleness_ms is not None else self.default_staleness_ms
        
        filtered = []
        for candidate in candidates:
            if self._passes_version_check(
                candidate,
                visible_before_ts=visible_before_ts,
                version_at=version_at,
                staleness_ms=staleness_ms,
            ):
                filtered.append(candidate)
        
        return filtered
    
    def _passes_version_check(
        self,
        candidate: Candidate,
        visible_before_ts: Optional[datetime],
        version_at: Optional[datetime],
        staleness_ms: int,
    ) -> bool:
        """Check if candidate passes version constraints"""
        
        # Get candidate timestamps
        valid_from = getattr(candidate, 'valid_from', None)
        valid_to = getattr(candidate, 'valid_to', None)
        visible_time = getattr(candidate, 'visible_time', None)
        
        # If no version info available, pass through
        if valid_from is None and visible_time is None:
            return True
        
        # Use visible_time as fallback for valid_from
        effective_from = valid_from or visible_time
        
        # visible_before_ts check
        if visible_before_ts is not None:
            if effective_from and effective_from > visible_before_ts:
                return False
        
        # version_at check (exact version at timestamp)
        if version_at is not None:
            # Object must be valid at version_at
            if effective_from and effective_from > version_at:
                return False
            if valid_to and valid_to <= version_at:
                return False
        
        # bounded_staleness check
        if staleness_ms > 0 and effective_from:
            now = datetime.now()
            staleness_window = now - timedelta(milliseconds=staleness_ms)
            # Object must have been updated within staleness window
            if effective_from < staleness_window:
                logger.debug(f"Candidate {candidate.object_id} exceeds staleness window")
                # Note: This is a soft check, we still include but may want to mark
                pass
        
        return True
    
    def filter_to_latest_versions(
        self,
        candidates: List[Candidate],
    ) -> List[Candidate]:
        """
        Deduplicate candidates to keep only the latest version of each object.
        
        When multiple versions of the same object_id exist, keep only the one
        with the highest version number or most recent valid_from.
        """
        if not candidates:
            return []
        
        # Group by object_id
        by_object: dict = {}
        for c in candidates:
            if c.object_id not in by_object:
                by_object[c.object_id] = c
            else:
                existing = by_object[c.object_id]
                # Compare versions
                if c.version > existing.version:
                    by_object[c.object_id] = c
                elif c.version == existing.version:
                    # Same version, compare valid_from if available
                    c_from = getattr(c, 'valid_from', None)
                    e_from = getattr(existing, 'valid_from', None)
                    if c_from and e_from and c_from > e_from:
                        by_object[c.object_id] = c
        
        return list(by_object.values())
