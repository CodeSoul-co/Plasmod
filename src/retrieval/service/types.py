"""
Core data types for retrieval module.
Keeps consistent with proto/retrieval.proto
"""

from dataclasses import dataclass, field
from typing import List, Optional
from datetime import datetime


@dataclass
class TimeRange:
    from_ts: Optional[datetime] = None
    to_ts: Optional[datetime] = None


@dataclass
class RetrievalRequest:
    """Retrieval request"""
    query_text: str = ""
    query_vector: Optional[List[float]] = None
    
    # Isolation boundaries (required)
    tenant_id: str = ""
    workspace_id: str = ""
    
    # Filter conditions
    agent_id: Optional[str] = None
    session_id: Optional[str] = None
    scope: Optional[str] = None          # private / session / workspace / global
    memory_type: Optional[str] = None    # episodic / semantic / procedural
    
    # Retrieval control
    top_k: int = 20
    min_confidence: float = 0.0
    min_importance: float = 0.0
    time_range: Optional[TimeRange] = None
    
    # Version constraints
    visible_before_ts: Optional[datetime] = None   # Only return objects visible before this time
    version_at: Optional[datetime] = None          # Return exact version at this timestamp
    bounded_staleness_ms: Optional[int] = None     # Allow stale data within this window
    
    # Policy constraints
    exclude_quarantined: bool = True               # Exclude quarantined objects
    exclude_unverified: bool = False               # Exclude unverified objects
    
    # Retrieval path switches
    enable_dense: bool = True
    enable_sparse: bool = True
    enable_filter_only: bool = False


@dataclass
class Candidate:
    """Single candidate object"""
    object_id: str
    object_type: str                     # memory / event / artifact
    score: float                         # RRF merged score
    
    # Metadata
    agent_id: str = ""
    session_id: str = ""
    scope: str = ""
    version: int = 0
    provenance_ref: str = ""             # for member C to trace provenance
    
    # Content
    content: str = ""
    summary: str = ""
    confidence: float = 0.0
    importance: float = 0.0
    level: int = 0                       # distillation depth: 0=raw, 1=summary, 2=abstraction
    memory_type: str = ""                # episodic / semantic / procedural
    verified_state: str = ""             # verified / unverified / disputed
    salience_weight: float = 1.0         # for final reranking after RRF
    
    # Version info
    valid_from: Optional[datetime] = None    # When this version became active
    valid_to: Optional[datetime] = None      # When this version was superseded
    visible_time: Optional[datetime] = None  # When object became visible
    
    # Governance
    quarantine_flag: bool = False            # Whether object is quarantined
    visibility_policy: str = ""              # public / private / workspace
    
    # Source channels
    source_channels: List[str] = field(default_factory=list)  # ["dense", "sparse", "filter"]
    
    # Graph expansion seed marker (for member C)
    is_seed: bool = False
    seed_score: float = 0.0


@dataclass
class QueryMeta:
    """Query metadata"""
    latency_ms: int = 0
    dense_hits: int = 0
    sparse_hits: int = 0
    filter_hits: int = 0
    channels_used: List[str] = field(default_factory=list)


@dataclass
class CandidateList:
    """Candidate list (retrieval result)"""
    candidates: List[Candidate] = field(default_factory=list)
    total_found: int = 0
    retrieved_at: Optional[datetime] = None
    query_meta: Optional[QueryMeta] = None
