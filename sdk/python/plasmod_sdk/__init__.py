from .client import PlasmodClient

# Note: the previous `RetrievalLib` ctypes wrapper that called the old
# `andb_dense_search` C entry-point has been removed.  All retrieval
# algorithms (HNSW, IVF_FLAT, DiskANN) now live exclusively in C++ and
# are exposed to Go through CGO at:
#   src/internal/dataplane/retrievalplane/bridge.go
#     - NewRetriever / NewRetrieverWithIndexType   (HNSW + dispatch)
#     - NewIVFRetriever                            (IVF_FLAT + tuning)
#     - NewDiskANNRetriever                        (on-disk DiskANN)
# Python clients should drive retrieval through the HTTP API via
# PlasmodClient (above) instead of opening libplasmod_retrieval.so
# directly.

__all__ = ["PlasmodClient"]
