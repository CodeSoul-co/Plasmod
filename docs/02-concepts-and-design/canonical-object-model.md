# Canonical Object Model

权威结构定义在 `src/internal/schemas/canonical.go`。

| Object | Primary ID | 关键关系 | Versionable | Indexable |
|---|---|---|---:|---:|
| Agent | `agent_id` | tenant/workspace/policy/capability | Yes | No |
| Session | `session_id` | agent/parent session/task | Yes | No |
| Event | `identity.event_id` | actor/causality/access/materialization | Yes | Yes |
| Memory | `memory_id` | source events/scope/provenance/lifecycle | Yes | Yes |
| AgentState | `state_id` | agent/session/state key/value | Yes | No |
| Artifact | `artifact_id` | session/owner/producer event | Yes | Yes |
| Edge | `edge_id` | source/type/target/provenance | No | No |
| ObjectVersion | object ID + version | mutation event/valid interval | N/A | No |
| PolicyRecord | `policy_id` | object/decision/visibility | No | No |
| ShareContract | `contract_id` | ACL/consistency/merge/audit policy | No | No |
| RetrievalSegment | `segment_id` | namespace/index/storage/tier | No | Physical metadata |

`semantic.ObjectModelRegistry` 保存 type metadata。Go 中 `AgentState` 是 `State` 的 alias；新 object type 名称应使用 `agent_state`，`state` 只用于兼容。

## ID 与版本

Event 未提供 ID 时 Gateway 生成 `evt_*`。Memory 通常使用 `mem_ + event_id`；State materializer 使用 `state_ + agent_id + state_key`；Artifact 可由 Event object 指定或按默认规则生成。确定性 ID 让 replay 可以覆盖同一 canonical key，但不能自动保证所有外部副作用 exactly once。

## 原子 projection

`storage.CanonicalProjection` 可包含 Memory、State、Artifact、Versions、Edges 和 base-edge flags。`storage.factory` 强制 objects、edges、versions 使用同一种 backend，Badger 实现才能在共享事务中提交这组变更。

## Direct CRUD 边界

`/v1/agents`、`/v1/sessions`、`/v1/memory`、`/v1/states`、`/v1/artifacts`、`/v1/edges` 等 POST 路由直接写 store/coordinator，主要用于管理和兼容。需要 audit/replay/consistency 的业务写入应使用 Event ingest。
