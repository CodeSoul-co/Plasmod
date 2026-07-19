# Query Schema

`schemas.QueryRequest` 的关键字段：

- 文本：`query_text`、`query_scope`、`top_k`；
- scope：`tenant_id`、`workspace_id`、`agent_id`、`session_id`；
- 类型：`object_types`、`memory_types`、`edge_types`；
- 精确对象：`target_object_ids`；
- 时间：`time_window.from/to`；
- 关系：`relation_constraints`；
- 返回：`response_mode`；
- 数据来源：dataset/source/import batch selectors；
- 访问/物化/runtime filter；
- `warm_segment_id`、`include_cold`、`embedding_vector`。

响应包含 objects/nodes、edges、provenance、versions、applied filters、proof trace、chain traces、evidence
cache、retrieval summary、query status 和 hint。

定义中的标准 response mode 是 `structured_evidence` 和 `objects_only`。未知 mode 的行为应视为不稳定。

`POST /v1/query/batch` 使用 `schemas.VectorWarmBatchQueryRequest`，不是本页 QueryRequest 的数组；其输出为
`VectorWarmBatchQueryResponse`，用于 Warm Segment native batch ANN。
