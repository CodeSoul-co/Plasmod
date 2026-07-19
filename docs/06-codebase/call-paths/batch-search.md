# Batch Search

Gateway 的 `/v1/query/batch` 解码 `VectorWarmBatchQueryRequest`，校验每行维度和 lineage，再调用已注册
Warm Segment 的 native batch search。内部 transport 还提供 warm batch、serial batch 和 raw batch 变体。

原生 batch search 只负责向量候选；Go 层仍需逐 query 应用 scope、policy、canonical supplement 和 evidence。
因此该 route 返回 `VectorWarmBatchQueryResponse`，不是带 canonical Evidence 的通用 QueryResponse。

批量优化必须保持单请求等价语义，并对每项返回独立错误/状态。
