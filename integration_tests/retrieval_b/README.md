# Member B - Retrieval Module Integration Tests

This folder contains integration tests for the retrieval module (Member B's scope).

## Test Files

| File | Description |
|---|---|
| `test_hybrid_retrieval.py` | Tests hybrid retrieval: lexical search, semantic search, RRF fusion, filters |

## Running Tests

```bash
# Make sure the server is running first
cd /Users/lixin/Downloads/CogDB
go run ./cmd/server &

# Run retrieval tests
python integration_tests/retrieval_b/test_hybrid_retrieval.py
```

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `ANDB_BASE_URL` | `http://127.0.0.1:8080` | Server base URL |
| `ANDB_HTTP_TIMEOUT` | `10` | HTTP request timeout in seconds |

## Test Cases

### 1. Ingest Test Data
Ingests 5 sample memories for testing:
- User preference (dark mode)
- Python tutorial completion
- Database configuration
- Machine learning discussion
- API rate limit info

### 2. Lexical Search - Keyword Match
Query with specific keyword ("Python programming") should return matching memories.

### 3. Lexical Search - No Match
Query with non-existent keyword should return empty results.

### 4. Semantic Search
Query with semantic meaning ("deep learning and AI optimization") should return semantically relevant memories.

### 5. Filter by Memory Type
Query with `memory_types=["semantic"]` filter should only return semantic memories.

### 6. Top-K Limit
Query with `top_k=2` should return at most 2 results.

### 7. Query Response Structure
Verify response contains required fields: `objects`, `edges`, `provenance`, `versions`, `applied_filters`, `proof_trace`.

### 8. RRF Fusion - Multiple Signals
Query that matches both lexically and semantically should rank relevant results higher.

## Expected Output

```
==============================================================
Member B - Hybrid Retrieval Integration Tests
==============================================================
Target: http://127.0.0.1:8080

=== Test: Ingest Test Data ===
  [OK] Ingested: test_mem_001
  [OK] Ingested: test_mem_002
  ...
  [OK] All test data ingested

=== Test: Lexical Search - Keyword Match ===
  Query: 'Python programming'
  Results: 2 objects returned
  [OK] Lexical search returned results

...

==============================================================
Test Summary
==============================================================
  [PASS] Ingest Test Data
  [PASS] Lexical Search - Keyword Match
  [PASS] Lexical Search - No Match
  [PASS] Semantic Search
  [PASS] Filter by Memory Type
  [PASS] Top-K Limit
  [PASS] Query Response Structure
  [PASS] RRF Fusion - Multiple Signals

Total: 8/8 passed

[SUCCESS] All tests passed
```

## Verification Criteria (Week 2)

| Criteria | Test |
|---|---|
| Lexical search works | `test_lexical_search_keyword_match` |
| Semantic search works | `test_semantic_search` |
| RRF fusion combines results | `test_rrf_fusion_multiple_signals` |
| Filters work | `test_filter_by_memory_type` |
| Top-K limit respected | `test_top_k_limit` |
| Response structure correct | `test_query_response_structure` |
