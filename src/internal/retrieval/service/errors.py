"""
Error codes and exceptions for retrieval module.
Aligned with week-3 design doc section 4.2.
"""

from enum import IntEnum
from dataclasses import dataclass
from typing import Optional


class RetrievalErrorCode(IntEnum):
    """
    Error codes for retrieval module.
    
    Usage by Query Layer (member D):
    - 200: Normal response, process candidates
    - 400: Check request format, fix missing fields
    - 404: Valid but empty result, return empty to agent
    - 500: Log error, return degraded result
    - 503: Log timeout, may retry
    """
    OK = 200                      # Normal response
    BAD_REQUEST = 400             # Missing required fields in RetrievalRequest
    NOT_FOUND = 404               # No candidates found (valid, not an error)
    INTERNAL_ERROR = 500          # Internal error (Milvus connection failed, etc.)
    SERVICE_UNAVAILABLE = 503     # Timeout or service unavailable


@dataclass
class RetrievalError(Exception):
    """Retrieval error with code and message"""
    code: RetrievalErrorCode
    message: str
    details: Optional[str] = None
    
    def __str__(self) -> str:
        return f"[{self.code.value}] {self.message}"


class BadRequestError(RetrievalError):
    """Missing required fields in RetrievalRequest"""
    def __init__(self, message: str, details: Optional[str] = None):
        super().__init__(
            code=RetrievalErrorCode.BAD_REQUEST,
            message=message,
            details=details,
        )


class InternalError(RetrievalError):
    """Internal error (Milvus connection failed, etc.)"""
    def __init__(self, message: str, details: Optional[str] = None):
        super().__init__(
            code=RetrievalErrorCode.INTERNAL_ERROR,
            message=message,
            details=details,
        )


class TimeoutError(RetrievalError):
    """Retrieval timeout"""
    def __init__(self, message: str, details: Optional[str] = None):
        super().__init__(
            code=RetrievalErrorCode.SERVICE_UNAVAILABLE,
            message=message,
            details=details,
        )


def validate_request(request) -> None:
    """
    Validate RetrievalRequest required fields.
    Raises BadRequestError if validation fails.
    """
    errors = []
    
    if not request.query_id:
        errors.append("query_id is required")
    if not request.query_text and not request.query_vector:
        errors.append("query_text or query_vector is required")
    if not request.tenant_id:
        errors.append("tenant_id is required")
    if not request.workspace_id:
        errors.append("workspace_id is required")
    if request.top_k <= 0:
        errors.append("top_k must be positive")
    
    if errors:
        raise BadRequestError(
            message="Invalid RetrievalRequest",
            details="; ".join(errors),
        )
