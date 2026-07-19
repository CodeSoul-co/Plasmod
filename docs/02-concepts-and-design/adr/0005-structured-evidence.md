# ADR-0005: Query 返回 Structured Evidence

- Status: Accepted
- Context: agent 决策需要知道来源、版本和关系，而不只是 top-k IDs。
- Decision: `QueryResponse` 包含 objects、edges、versions、provenance、proof trace、filters 和 retrieval/cache summary。
- Consequences: query stage 需要 canonical lookups 与 graph expansion；prod middleware 可隐藏 debug traces。
- Alternatives: 只返回相似度列表，被保留为 warm/internal/objects-only 条件路径，不作为完整语义。
- Invariant: proof/provenance 只能来自已存事实或明确的 planner/retrieval step，不伪造外部证据。
