# Ingest Events

## 目的

`POST /v1/ingest/events` 接收 Dynamic Event v0.4，把一次 Agent runtime 变化记录为可回放事实，
再派生 Memory、State、Artifact、Edge 和 ObjectVersion。

## 最小输入

推荐至少提供：

- `schema_version`；
- `identity.event_id`、`tenant_id`、`workspace_id`；
- `actor.agent_id`、`session_id`；
- `time.event_time`、`logical_ts`；
- `event.event_type`；
- `access.consistency`、`visibility`；
- `materialization.enabled`、`targets`；
- `payload` 或 `data`。

字段定义来自 `src/internal/schemas/dynamic_event.go`。兼容层可接收部分旧式平铺字段，但新客户端应输出
嵌套 v0.4，避免别名解析差异。

## 写入阶段

1. Gateway 解码并校验 Event。
2. Runtime 将 Event 追加到 WAL，得到 LSN。
3. Consistency controller 按 strict、bounded 或 eventual 调度。
4. Materializer 写 canonical objects、edges 和 versions。
5. Retrieval projection 更新可查询表示；可按事件配置跳过向量投影。
6. Tracker 更新 committed/projected/visible 状态并返回结果。

## 一致性选择

- `strict`：请求等待本次写入达到当前严格可见门槛；
- `bounded`：在 `freshness_sla_ms` 约束内异步推进；
- `eventual`：接受后异步物化和投影。

Event 的 `access.consistency` 优先于服务默认模式。strict 的成功响应不代表下游外部系统也完成，只代表
Plasmod 当前实现定义的 gate 已满足。

## Embedding 选择

- 已有向量：提供 precomputed embedding 和维度；
- 服务端生成：`retrieval.index_text` 加 `has_embedding=true`；
- 仅 canonical/词法：`index_text` 加 `has_embedding=false`；
- 全局跳过：`PLASMOD_SKIP_VECTOR_INDEX=1`。

预计算向量必须与已配置的检索空间维度和语义一致。Plasmod 不会证明不同模型产生的同维向量可比较。

## 错误处理

- 校验失败：修正字段，不应无条件重试；
- `503`：写入已接受但不可见，或投影失败；应查询状态并使用相同 `event_id` 谨慎恢复；
- `504`：一致性等待超时；先查对象/trace 再决定是否重放；
- `408`：客户端上下文取消；
- 其他 5xx：检查 WAL、Badger、embedding 和 native retrieval 日志。

当前接口没有通用 Idempotency-Key 协议。`event_id` 是业务幂等和追踪的首要标识，但重复提交是否需要
去重必须由调用方结合 trace/WAL 状态判断。
