# Query Model

Query execution stages:
1. Planner builds retrieval constraints from scope, time window, and relation limits.
2. Retrieval executes dense/sparse/filter paths.
3. Merge combines and ranks candidates.
4. Graph/tensor expansion builds structured evidence context.
5. Response builder emits explainable proof trace and provenance.
