# Pagination And Batching

## Pagination

当前 canonical collection handlers 没有统一的 `page_token`/`cursor` 契约。不同 GET route 使用自身 query
parameters，调用者不能假设大列表具有稳定快照分页。

大范围导出应使用管理 export/snapshot 能力或直接扩展带稳定 cursor 的 API，而不是循环读取无排序列表。

## Query Batch

`POST /v1/query/batch` 接收 `VectorWarmBatchQueryRequest`，不是多个通用 QueryRequest。它要求
`warm_segment_id`、`agent_mode` 和二维 `vectors`，并可用 `source_ids`/`row_lineage` 将每行结果分发给
single-agent 或 multi-agent 来源。Batch 降低 native search 调用开销，但：

- 不构成跨查询事务；
- 该 route 不执行通用 QueryRequest 的 tenant/scope/evidence 流程；
- 需要逐行检查 `rows`，并读取 `by_source`；
- batch size 受 HTTP body、内存、embedding 和 native search 限制。

## Vector Batch

`/v1/ingest/vectors` 接受二维 vectors 和可选 object IDs。要求：

- 每个向量维度一致；
- object ID 数量与 vectors 一致；
- index 参数在整个 segment 内一致；
- batch 失败时不要假设部分提交可自动回滚。
