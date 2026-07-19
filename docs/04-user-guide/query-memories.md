# Query Memories

## 查询入口

- 单请求：`POST /v1/query`；
- Warm Segment 预计算向量批请求：`POST /v1/query/batch`；
- 直接 Memory 访问：`/v1/memory`；
- runtime 内部 recall：`/v1/internal/memory/recall`，稳定性为 Experimental。

## 主要过滤维度

`schemas.QueryRequest` 支持：

- tenant、workspace、agent、session scope；
- `object_types`、`memory_types`、`edge_types`；
- 时间窗口；
- `target_object_ids`；
- relation constraints；
- dataset/source/batch selectors；
- access、materialization、runtime filters；
- `top_k`、`response_mode`；
- `include_cold`；
- 预计算 query embedding。

这些通用过滤字段只适用于 `/v1/query`。`/v1/query/batch` 使用独立的
`VectorWarmBatchQueryRequest`，直接查询 Warm Segment，不自动组装 canonical Evidence。

## 执行层次

1. Hot cache 候选；
2. Warm canonical/lexical/vector 候选；
3. 只有显式 `include_cold=true` 时才读取 Cold；
4. 合并、过滤和排序；
5. Evidence assembler 补充 edges、versions、policies、provenance 和 proof trace。

因此，Cold 数据不是每次 query 的隐式兜底。对历史归档有要求的调用方必须明确请求并接受额外 I/O。

## 响应模式

`QueryResponse` 可包含：

- `objects`、`nodes`；
- `edges`、`versions`、`provenance`；
- `filters`；
- `proof_trace`、`chain_traces`；
- `retrieval_summary`；
- `query_status` 和 `hint`。

客户端必须读取 `query_status`，不能只根据 HTTP 200 假定所有层均参与。生产模式还会删除 debug/raw/log
等内部字段。

## Latest Memory

“最新”需要确定 scope、memory type 和时间字段。推荐同时给出 Agent/Session、类型和时间窗口，再按返回对象
的版本/时间判断。向量相似度第一名不天然等于最新对象。
