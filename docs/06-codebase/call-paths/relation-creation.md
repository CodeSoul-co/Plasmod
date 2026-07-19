# Relation Creation

```text
Event causality/parents/relation descriptor
  -> validate source/destination/type
  -> deterministic Edge
  -> GraphEdgeStore
  -> source and destination edge indexes
  -> evidence traversal/proof trace
```

Edge 写入必须保留 scope 和 provenance。直接创建 Edge 时，Gateway 不会自动证明两端对象存在或语义正确。
