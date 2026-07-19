# Public HTTP API

## Event Ingest

```text
POST /v1/ingest/events
```

Body 为 [`schema-reference/event.md`](schema-reference/event.md) 所述 Event。成功响应包含写入/可见状态和
LSN 相关信息；失败状态见 [`error-model.md`](error-model.md)。这是需要 WAL、replay 和 materialization
语义时的首选写入口。

## Vector Ingest

```text
POST /v1/ingest/vectors
```

Body 主要字段：`vectors`、`object_ids`、`segment_id`、`index_type`，以及 IVF 参数。支持的 index type 为
`HNSW`、`IVF_FLAT`、`IVF_PQ`、`IVF_SQ8`、`DISKANN`，但是否可用取决于原生构建。

该入口写物理检索 segment，不替代 Event/canonical object ingest。只写向量会缺少完整 Agent 对象语义。

## Query

```text
POST /v1/query
POST /v1/query/batch
```

`/v1/query` 的请求和响应见 [`schema-reference/query.md`](schema-reference/query.md)。`/v1/query/batch` 并非
多个通用 QueryRequest；它接收 `VectorWarmBatchQueryRequest`，字段为 `agent_mode`、`warm_segment_id`、
`top_k`、二维 `vectors`、可选 `source_ids`/`row_lineage` 和 `search_raw`，直接执行已注册 Warm Segment 的
批量 ANN。

## Canonical Collections

```text
/v1/agents
/v1/sessions
/v1/memory
/v1/states
/v1/artifacts
/v1/edges
/v1/policies
/v1/share-contracts
```

GET 使用 query parameters 过滤，POST 接受对应 canonical schema。它们是管理/迁移入口，不自动补写完整
Event/WAL/Edge/Version 链。

## Trace

```text
GET /v1/traces/{object_id}
```

`object_id` 是路径后缀，必须 URL encode。响应由已有 object、edge、version、policy 和 provenance 组装。
