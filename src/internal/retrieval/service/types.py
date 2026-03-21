"""
Core data types for retrieval module.
Keeps consistent with proto/retrieval.proto
"""

from dataclasses import dataclass, field
from typing import List, Optional
from datetime import datetime

# Try to import C++ module
try:
    import andb_retrieval as _cpp
    _CPP_AVAILABLE = True
except ImportError:
    _cpp = None
    _CPP_AVAILABLE = False


def cpp_available() -> bool:
    """Check if C++ module is available."""
    return _CPP_AVAILABLE


def cpp_version() -> str:
    """Get C++ module version."""
    if _CPP_AVAILABLE:
        return _cpp.version()
    return "cpp-not-available"


@dataclass
class TimeRange:
    from_ts: Optional[datetime] = None
    to_ts: Optional[datetime] = None


@dataclass
class RetrievalRequest:
    """Retrieval request - aligned with week-1 design doc and query-schema.md"""
    query_id: str = ""                             # Unique query ID for tracing
    query_text: str = ""                           # Natural language query for dense+sparse
    query_vector: Optional[List[float]] = None     # Pre-computed embedding from D, skip re-vectorization
    
    # Isolation boundaries (required)
    tenant_id: str = ""
    workspace_id: str = ""
    
    # Filter conditions
    agent_id: Optional[str] = None
    session_id: Optional[str] = None
    scope: Optional[str] = None                    # private / session / workspace / global
    memory_types: Optional[List[str]] = None       # Filter by memory types (list), empty = all
    object_types: Optional[List[str]] = None       # Filter by object types (memory/event/artifact/state)
    
    # Retrieval control
    top_k: int = 10
    min_confidence: float = 0.0
    min_importance: float = 0.0
    time_range: Optional[TimeRange] = None
    
    # Version constraints
    as_of_ts: Optional[datetime] = None            # Time-travel: only visible_time <= as_of_ts
    min_version: Optional[int] = None              # Only return version >= min_version
    
    # Policy constraints
    exclude_quarantined: bool = True               # Exclude quarantined objects
    exclude_unverified: bool = False               # Exclude unverified objects
    
    # Retrieval path switches
    enable_dense: bool = True
    enable_sparse: bool = True
    enable_filter: bool = True                     # Enable attribute filter path
    enable_filter_only: bool = False               # Skip dense+sparse, only filter
    
    # Graph expansion mode (for member C)
    for_graph: bool = False                        # When true: return top_k*2, must include source_event_ids
    
    # Timeout settings (increased for S3ColdStore latency)
    timeout_ms: int = 5000                         # Default 5s, increase for cold reads


@dataclass
class Candidate:
    """Single candidate object - aligned with week-1 design doc CandidateObject"""
    object_id: str
    object_type: str                     # memory / event / artifact
    score: float = 0.0                   # RRF merged score (before reranking)
    final_score: float = 0.0             # After reranking: rrf * importance * freshness * confidence
    
    # Per-channel scores (for Benchmark Layer)
    dense_score: float = 0.0
    sparse_score: float = 0.0
    rrf_score: float = 0.0                   # RRF score before reranking (for benchmark analysis)
    
    # Metadata
    agent_id: str = ""
    session_id: str = ""
    scope: str = ""
    version: int = 0
    provenance_ref: str = ""             # For member C to trace provenance
    
    # Content
    content: str = ""
    summary: str = ""
    confidence: float = 0.0
    importance: float = 0.0
    freshness_score: float = 1.0         # Computed by member A, read from memories table
    level: int = 0                       # distillation depth: 0=raw, 1=summary, 2=abstraction
    memory_type: str = ""                # episodic / semantic / procedural
    verified_state: str = ""             # verified / unverified / disputed
    salience_weight: float = 1.0         # From policy_records, for governance override
    
    # Version info
    valid_from: Optional[datetime] = None    # When this version became active
    valid_to: Optional[datetime] = None      # When this version was superseded
    visible_time: Optional[datetime] = None  # When object became visible
    
    # Governance
    quarantine_flag: bool = False            # Whether object is quarantined
    visibility_policy: str = ""              # public / private / workspace
    is_active: bool = True                   # Active flag, inactive = excluded
    ttl: Optional[datetime] = None           # TTL expiry time
    
    # Source channels
    source_channels: List[str] = field(default_factory=list)  # ["dense", "sparse", "filter"]
    
    # Graph expansion (for member C)
    is_seed: bool = False
    seed_score: float = 0.0
    source_event_ids: List[str] = field(default_factory=list)  # Required when for_graph=true


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
