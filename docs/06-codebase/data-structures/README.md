# Data Structures

## Logical records

- Dynamic Event：嵌套 v0.4 wire model；
- canonical objects：Agent、Session、Memory、AgentState、Artifact 等；
- Edge/ObjectVersion/PolicyRecord：证据和演进；
- QueryRequest/QueryResponse：查询与结构化 evidence；
- RetrievalSegment：物理 segment 元数据。

## Runtime structures

- WAL `Record`：LSN + Event；
- consistency tracker：committed/projected/visible progress；
- controller queue item：Event、mode、deadline/retry metadata；
- HotObjectCache：有界进程内对象缓存；
- native segment handle：CGO 管理的物理索引引用。

## ID invariants

- Event ID 由调用者提供且稳定；
- 默认 Memory ID 为 `mem_<event_id>`；
- ingest checkpoint State ID 为 `state_<session_id>_<event_id>`；
- keyed AgentState ID 为 `state_<agent_id>_<state_key>`；
- Artifact 可使用显式 object ID；
- Edge ID 必须稳定连接 source/destination/type；
- Version 关联 mutation event ID。

ID 规则是 replay 和持久化兼容的一部分，不能作为普通格式化细节修改。
