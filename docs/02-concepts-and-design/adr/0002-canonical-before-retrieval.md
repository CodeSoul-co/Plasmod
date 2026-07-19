# ADR-0002: Canonical object 优先于 Retrieval

- Status: Accepted
- Context: ANN/lexical index 适合查找，但无法完整表达对象版本、关系和治理。
- Decision: ObjectStore/EdgeStore/VersionStore 保存权威对象；retrieval index 是可重建 projection。
- Consequences: embedding family 变化可 reindex；查询需要将 retrieval IDs 与 canonical/evidence 合并。
- Alternatives: 向量行作为唯一事实，被拒绝，因为 replay、state 和 provenance 语义不足。
- Invariant: index 命中不得覆盖或创造 canonical object 内容。
