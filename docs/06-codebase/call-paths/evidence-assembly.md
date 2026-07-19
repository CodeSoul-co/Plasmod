# Evidence Assembly

```text
ranked object IDs
  -> load canonical objects
  -> GraphEdgeStore traversal
  -> ObjectVersion lookup
  -> PolicyRecord/ShareContract filters
  -> derivation/provenance lookup
  -> GraphNode + ProofStep
  -> QueryResponse
```

Production visibility filtering happens after response construction and may remove chain/debug fields。Evidence assembler
不得把被 policy 拒绝的对象通过 Edge 侧漏。
