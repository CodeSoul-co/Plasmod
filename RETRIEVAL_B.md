# Retrieval Module (Member B)

## Overview

The Retrieval Module is responsible for multi-signal candidate retrieval over canonical cognitive objects. It implements a three-path parallel retrieval architecture with RRF (Reciprocal Rank Fusion) merging, safety filtering, and reranking.

**Architecture**: Python thin wrapper + C++ core (via pybind11)

**Location**: 
- `src/internal/retrieval/` - Python thin wrapper (parameter conversion only)
- `cpp/` - C++ core implementation (all retrieval logic)

**Schema Documentation**: `docs/schema/retrieval-schema.md`

---

## Architecture

All retrieval logic is implemented in C++. Python layer only does parameter conversion.

```
┌─────────────────────────────────────────────────────────────┐
│                    Python Layer                              │
│  src/internal/retrieval/                                     │
│  - main.py (entry point, --dev flag)                        │
│  - service/retriever.py (thin wrapper, calls C++)           │
│  - service/types.py (type definitions)                      │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ pybind11
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                      C++ Layer                               │
│  cpp/                                                        │
│  ├── include/andb/                                          │
│  │   ├── types.h         (Candidate, SearchResult, etc.)    │
│  │   ├── dense.h         (DenseRetriever - HNSW)            │
│  │   ├── sparse.h        (SparseRetriever - SPARSE_INDEX)   │
│  │   ├── filter.h        (FilterBitset - BitsetView)        │
│  │   ├── merger.h        (RRF merge + reranking)            │
│  │   └── retrieval.h     (Unified Retriever + C API)        │
│  ├── retrieval/                                             │
│  │   ├── dense.cpp       (Knowhere HNSW integration)        │
│  │   ├── sparse.cpp      (Knowhere SPARSE_INVERTED_INDEX)   │
│  │   ├── filter.cpp      (BitsetView mechanism)             │
│  │   ├── merger.cpp      (RRF k=60, reranking formula)      │
│  │   └── retrieval.cpp   (Unified implementation)           │
│  ├── python/bindings.cpp (pybind11 bindings)                │
│  └── CMakeLists.txt      (FetchContent Knowhere, pybind11)  │
└─────────────────────────────────────────────────────────────┘
```

---

## Current Features

### 1. Three-Path Parallel Retrieval (all in C++)

| Path | C++ Implementation | Description |
|------|-------------------|-------------|
| **Dense** | `cpp/retrieval/dense.cpp` | Knowhere HNSW index, Search with BitsetView |
| **Sparse** | `cpp/retrieval/sparse.cpp` | Knowhere SPARSE_INVERTED_INDEX |
| **Filter** | `cpp/retrieval/filter.cpp` | BitsetView mechanism passed to Search |

### 2. RRF Fusion (C++)

**Implementation**: `cpp/retrieval/merger.cpp`

```
RRF_score(d) = Σ 1/(k + rank_i(d))
```

- `k = 60` (configurable via MergeConfig)
- Accumulates scores across all three channels
- Per-channel scores tracked: `dense_score`, `sparse_score`

### 3. Reranking Formula (C++)

```cpp
final_score = rrf_score * max(importance, 0.01f) * max(freshness_score, 0.01f) * max(confidence, 0.01f)
```

### 4. Seed Marking (C++)

- Candidates with `final_score >= seed_threshold` (default 0.7) are marked as seeds
- Sets `is_seed = true` and `seed_score = final_score`
- Graph Expansion Layer uses these seeds for 1-hop traversal

### 5. Filter Mechanism (C++)

Uses Knowhere's BitsetView:
- Bit = 1 means filtered out (excluded from search)
- Applied during Search call, not as separate index
- Supports safety rules: quarantine, TTL, visibility, is_active

---

## Module Structure

```
cpp/                              # C++ core (all retrieval logic)
├── include/andb/
│   ├── types.h                   # Candidate, SearchResult, IndexConfig, MergeConfig
│   ├── dense.h                   # DenseRetriever interface
│   ├── sparse.h                  # SparseRetriever interface
│   ├── filter.h                  # FilterBitset, FilterBuilder
│   ├── merger.h                  # Merger (RRF + reranking)
│   └── retrieval.h               # Unified Retriever + C API
├── retrieval/
│   ├── dense.cpp                 # Knowhere HNSW wrapper
│   ├── sparse.cpp                # Knowhere SPARSE_INVERTED_INDEX wrapper
│   ├── filter.cpp                # BitsetView implementation
│   ├── merger.cpp                # RRF merge + reranking
│   └── retrieval.cpp             # Unified implementation
├── python/
│   └── bindings.cpp              # pybind11 bindings
└── CMakeLists.txt                # Build configuration

src/internal/retrieval/           # Python thin wrapper
├── README.md                     # This file
├── main.py                       # Entry point (--dev for debug mode)
├── requirements.txt              # Python dependencies
├── proto/
│   └── retrieval.proto           # gRPC service definition
└── service/
    ├── __init__.py
    ├── types.py                  # Type definitions + C++ availability check
    ├── retriever.py              # Thin wrapper calling C++ module
    └── errors.py                 # Error codes
```

---

## Building the C++ Module

### Prerequisites

- CMake 3.14+
- C++17 compiler (GCC 9+, Clang 10+, MSVC 2019+)
- Python 3.8+ with development headers

### Build Steps

```bash
cd cpp
mkdir build && cd build
cmake .. -DANDB_WITH_PYBIND=ON
make -j$(nproc)
```

### CMake Options

| Option | Default | Description |
|--------|---------|-------------|
| `ANDB_WITH_PYBIND` | ON | Build pybind11 Python bindings |
| `ANDB_WITH_GPU` | OFF | Enable GPU support via Knowhere RAFT |

### Cross-Platform Support

- Ubuntu 20.04 x86_64
- Ubuntu 20.04 Aarch64
- macOS x86_64
- macOS Apple Silicon (arm64)

---

## Running the Module

### Development Mode

```bash
python -m src.internal.retrieval.main --dev
```

### Test Mode

```bash
python -m src.internal.retrieval.main --test
python -m src.internal.retrieval.main --test --dev  # with debug output
```

### Command Line Options

| Option | Default | Description |
|--------|---------|-------------|
| `--dev` | false | Enable dev mode (verbose logging) |
| `--test` | false | Run basic test |
| `--index-type` | HNSW | Index type (HNSW, IVF_FLAT) |
| `--metric-type` | IP | Metric type (IP, L2, COSINE) |
| `--dim` | 128 | Vector dimension |
| `--rrf-k` | 60 | RRF smoothing parameter |
| `--seed-threshold` | 0.7 | Seed marking threshold |

---

## Error Codes

Defined in `service/errors.py`:

| Code | Name | Description |
|------|------|-------------|
| 200 | OK | Normal response |
| 400 | BAD_REQUEST | Missing required fields |
| 404 | NOT_FOUND | No candidates found (valid) |
| 500 | INTERNAL_ERROR | Internal error |
| 503 | SERVICE_UNAVAILABLE | Timeout |

---

## Changelog

### 2026-03-20 (Evening)

- **Migrated to C++ architecture**: All retrieval logic now in `cpp/`, Python layer is thin wrapper only
- **Knowhere integration**: Dense (HNSW), Sparse (SPARSE_INVERTED_INDEX), Filter (BitsetView)
- **pybind11 bindings**: Exposes all C++ interfaces to Python
- **CMake build system**: FetchContent for Knowhere/pybind11, cross-platform support
- **Removed old Milvus-based Python implementation**: `dense.py`, `sparse.py`, `filter.py`, `merger.py` deleted

### 2026-03-20 (Morning)

- **Added `benchmark_retrieve()` interface**: Returns ALL candidates without truncation
- **Added `rrf_score` field to Candidate**: For benchmark analysis
- **Added error codes module** (`service/errors.py`)
- **Enhanced `--dev` mode logging**

### 2026-03-19

- **Fixed safety filter bug**: All 24 fields now read from storage
- **Rewrote retrieval-schema.md**: Exhaustive field documentation

---

## TODO / Future Work

- [x] C++ retrieval layer with Knowhere integration (completed 2026-03-20)
- [x] pybind11 bindings (completed 2026-03-20)
- [x] Python thin wrapper (completed 2026-03-20)
- [ ] Replace stub implementations with actual Knowhere calls
- [ ] GPU support via Knowhere RAFT
- [ ] Distributed retrieval with sharding
- [ ] Caching layer for hot queries
