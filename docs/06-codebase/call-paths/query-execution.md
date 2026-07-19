# Query Execution

```text
POST /v1/query
  -> Gateway.handleQuery
  -> schemas.QueryRequest validation/defaults
  -> semantic QueryPlanner/operators
  -> DataPlane query
     -> Hot lexical/canonical candidates
     -> Warm lexical/vector segment candidates
     -> optional Cold candidates
  -> merge/filter/rank
  -> evidence assembler
  -> visibility middleware
  -> QueryResponse
```

`target_object_ids` 可能从 canonical store 补充对象；因此必须通过 `query_status` 区分 native retrieval hit 与
supplemented result。
