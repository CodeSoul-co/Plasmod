# Canonical Objects

| Object | Primary ID | Purpose |
|---|---|---|
| Agent | `agent_id` | runtime actor 和能力目录 |
| Session | `session_id` | 一段任务/交互范围 |
| Event | `identity.event_id` | 不可替代的因果输入 |
| Memory | `memory_id` | 可召回 Agent memory |
| AgentState | `state_id` | key/version 状态 |
| Artifact | `artifact_id` | 外部或生成产物 |
| Edge | `edge_id` | 对象关系 |
| ObjectVersion | object ID + version | 变更历史 |
| User | `user_id` | 最小身份数据对象 |
| Embedding | `vector_id` | 向量引用和模型信息 |
| PolicyRecord | policy/object | 对象治理决策 |
| ShareContract | `contract_id` | 显式共享协议 |
| RetrievalSegment | `segment_id` | 物理检索单元 |

完整字段在 `src/internal/schemas/canonical.go`。对象类型字符串在 `schemas/constants.go`；新增对象时必须同步
storage interface、Badger prefix、Gateway、materializer、query/evidence 和文档。
