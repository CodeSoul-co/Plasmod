"""
Retrieval service - internal interface.

This is a THIN WRAPPER - all retrieval logic is in cpp/.
Python layer only does parameter conversion.
"""

from .types import (
    RetrievalRequest,
    CandidateList,
    Candidate,
    QueryMeta,
    cpp_available,
    cpp_version,
)
from .retriever import Retriever
from .errors import RetrievalErrorCode, RetrievalError

__all__ = [
    "RetrievalRequest",
    "CandidateList",
    "Candidate",
    "QueryMeta",
    "Retriever",
    "RetrievalErrorCode",
    "RetrievalError",
    "cpp_available",
    "cpp_version",
]
