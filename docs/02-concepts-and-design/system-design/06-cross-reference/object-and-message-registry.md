# Object and Message Registry

本页集中定义 Engine 之间传递的对象和字段。各 Engine 页面不重复复制完整 schema，而是引用本注册表并说明本 Engine 实际读写的字段。

## Dynamic Event v0.4

代码：`src/internal/schemas/dynamic_event.go`, `src/internal/schemas/canonical.go`。

| Group | 字段 | 主要消费者 |
|---|---|---|
| `schema_version` | `schema_version` | normalize/validation |
| `identity` | `trace_id`, `event_id`, `tenant_id`, `workspace_id`, `source`, `dataset`, `import_batch_id`, `ingest_mode`, `file_name`, `replay_order` | WAL、scope、dataset 管理、replay |
| `actor` | `session_id`, `agent_id`, `role_profile`, `team_id`, `parent_agent_id`, `agent_generation`, `agent_type` | materialization、governance、collaboration |
| `time` | `event_time`, `logical_ts`, `wal_lsn`, `ingest_time`, `visible_time` | WAL、version、consistency、time filter |
| `event` | `event_type`, `event_subtype`, `action`, `importance`, `confidence` | materializer、worker routing、salience |
| `object` | `object_id`, `object_type`, `object_subtype`, `version`, `lifecycle_state`, `state_type`, `state_key`, `artifact_name`, `artifact_uri`, `uri`, `mime_type` | object derivation |
| `causality` | `parent_event_id`, `causal_refs`, `provenance_refs`, `call_event_id`, source/target object IDs, `edge_kind`, `edge_weight`, `reason`, `hooks` | Edge、Artifact、proof/provenance |
| `access` | `consistency`, `visibility`, visible agent/role lists, `ttl_ms`, `freshness_sla_ms`, `policy_tags`, `share_contract_id`, `hooks` | consistency、policy、scope |
| `materialization` | `enabled`, `targets`, `mode`, `planned_object_ids`, `status`, `materialized_at_ms`, `hooks` | projection metadata；当前不是通用硬 gate |
| `retrieval` | `index_text`, `has_embedding`, `embedding_dim`, `embedding_vector`, `embedding_ref`, `index_fields`, `retrieval_namespace`, `sparse_terms`, `hooks` | retrieval projection |
| `payload` | `map[string]any`；常用 `text/content/state_value/artifact` | materializer 和专用 worker |
| `data` | `payload_size_bytes`, `record_size_bytes`, `payload_hash`, `canonicalization`, `schema_name`, `schema_ref` | validation/metadata |
| `runtime` | created/write/materialized/visible/query 时间、三类 latency、write/materialization/visibility status | 运行状态和观测；部分值来自输入或 consistency 更新 |
| `extensions` | `custom`, `labels`, `hooks` | extension hooks/filters |

旧平铺字段是 `json:"-"` 兼容 alias，经过 `NormalizeDynamicEventV04` 汇入嵌套模型，不是 canonical 输出字段。

## Canonical Objects

### Agent and Session

| Object | 字段 |
|---|---|
| `Agent` | `agent_id`, `tenant_id`, `workspace_id`, `agent_type`, `role_profile`, `policy_ref`, `capability_set`, `default_memory_policy`, `created_at`, `status` |
| `Session` | `session_id`, `agent_id`, `parent_session_id`, `task_type`, `goal`, `context_ref`, `start_ts`, `end_ts`, `status`, `budget_token`, `budget_time_ms` |

### Memory

| 字段组 | 字段 | 所有权 |
|---|---|---|
| Identity/type | `memory_id`, `memory_type`, `agent_id`, `session_id`, `owner_type`, `scope`, `level` | canonical Memory |
| Content | `content`, `summary` | canonical Memory |
| Provenance | `source_event_ids`, `provenance_ref` | canonical Memory；详细关系在 Edge/derivation log |
| Quality | `confidence`, `importance`, `freshness_score` | canonical Memory |
| Validity | `ttl`, `valid_from`, `valid_to`, `version`, `is_active`, `lifecycle_state` | canonical Memory |
| External references | `embedding_ref`, `algorithm_state_ref` | 指向 Embedding/MemoryAlgorithmState；当前 embedding object 持久化接线有限 |
| Governance | `policy_tags`, `scope` | Memory + PolicyRecord/ShareContract |
| Ingest lineage | `dataset_name`, `source_file_name`, `import_batch_id` | delete/query selectors |

### State, Artifact, Relation and Version

| Object | 字段 |
|---|---|
| `State`/`AgentState` | `state_id`, `agent_id`, `session_id`, `state_type`, `state_key`, `state_value`, `derived_from_event_id`, `checkpoint_ts`, `version` |
| `Artifact` | `artifact_id`, `session_id`, `owner_agent_id`, `artifact_type`, `uri`, `content_ref`, `mime_type`, `metadata`, `hash`, `produced_by_event_id`, `version` |
| `Edge` | `edge_id`, `src_object_id`, `src_type`, `edge_type`, `dst_object_id`, `dst_type`, `weight`, `provenance_ref`, `created_ts`, `properties`, `expires_at` |
| `ObjectVersion` | `object_id`, `object_type`, `version`, `mutation_event_id`, `valid_from`, `valid_to`, `snapshot_tag` |

### Governance and Retrieval Records

| Object | 字段 |
|---|---|
| `User` | `user_id`, `user_name`, `user_tenant_id`, `user_workspace_id`, `default_visibility` |
| `Embedding` | `vector_id`, `vector_context`, `original_text`, `embedding_type`, `dim`, `model_id`, `vector_ref`, `created_ts` |
| `Policy` | `policy_id`, `policy_version`, start/end time, publisher type/id, policy type |
| `PolicyRecord` | policy ID/version/context, target object/type, salience/TTL/decay/confidence, verified/quarantine/visibility, reason/source/event ID |
| `ShareContract` | `contract_id`, `scope`, read/write/derive ACL, TTL/consistency/merge/quarantine/audit policy |
| `RetrievalSegment` | segment/object/namespace/time bucket, embedding family, storage/index refs, row count, min/max TS, tier |
| `AuditRecord` | record/target/operation/actor/policy snapshot/decision/reason/time/downstream request |
| `MemoryAlgorithmState` | memory/algorithm ID, strength, recall time/count, retention, portrait state, summary refs, suggested lifecycle, update time |

## Retrieval Plane Messages

| Type | 输入/输出字段 |
|---|---|
| `dataplane.IngestRecord` | object ID, text, namespace, attributes, event Unix TS, embedding family/dim/vector, skip-vector flag |
| `dataplane.SearchInput` | query text/vector, TopK, namespace, constraints, time range, growing/cold flags, object/memory types |
| `dataplane.SearchOutput` | object IDs, scanned/planned segments, tier, cold mode/IDs/candidate count/request/fallback |
| `semantic.QueryPlan` | normalized TopK/namespace/time/types/tier plus access/materialization/runtime/hook descriptors |

## Query API Messages

| Type | 字段 |
|---|---|
| `QueryRequest` | query/scope/session/agent/tenant/workspace, TopK/time, object/target/memory/edge/relation filters, response mode, dataset lineage, access/policy/share/materialization/runtime/extension filters, query hooks, warm segment, cold flag, query vector |
| `QueryResponse` | object IDs, graph nodes/edges, provenance, versions, filters, proof trace, four chain trace slots, cache stats, retrieval summary, query status/hint |
| `GraphExpandRequest` | seeds/types, session/agent, hops/time/edges, node/edge limits, props/provenance/response mode |
| `GraphExpandResponse` | `EvidenceSubgraph`, applied filters |

`QueryResponse.Objects` 是 ID 列表，不是 hydrated object payload。Canonical hydration 当前用于类型判断、Node/Edge/Version/Provenance 组装；完整对象需通过 canonical API 或后续 adapter 读取。

## Worker Typed I/O

代码：`src/internal/schemas/worker_params.go`。

| Worker | Input | Output | 关键副作用 |
|---|---|---|---|
| Ingest | `IngestInput{Event}` | `IngestOutput{Valid,Error}` | 无持久化 |
| Object materialization | `ObjectMaterializationInput{Event}` | object ID/type/materialized | object store/edge/version |
| State apply/checkpoint | Event 或 agent/session | state ID/version/checkpoint | state/version store |
| Tool trace | Event | artifact ID/traced | artifact/derivation |
| Memory extraction | event/agent/session/content | memory ID/extracted | memory/derivation |
| Consolidation | agent/session | produced IDs/count | derived Memory |
| Summarization | agent/session/max level | produced IDs/count | summary Memory |
| Reflection policy | object ID/type | policy applied | Memory/Policy/Audit/tier |
| Index build | object ID/type/namespace/text | segment/count | segment/index/dataplane |
| Graph relation | source/destination/type/weight | edge ID | Edge store |
| Subgraph expand | request + prefetched nodes/edges | graph response | 无持久化 |
| Conflict merge | two IDs/object type | winner/loser/resolved | Memory/Edge/Audit |
| Proof trace | object IDs/depth | proof steps/hops | 无持久化 |
| Microbatch | query ID/opaque payload | items/count | in-memory queue |
| Algorithm dispatch | operation, IDs, query/time/signals/scope | updated/produced/scored refs | Memory/algorithm state/audit |
| Communication | source/target agent/memory ID | shared memory ID | copied Memory |

## ID and Version Invariants

| Record | 默认规则 |
|---|---|
| Memory | `mem_<event_id>` |
| Ingest checkpoint State | `state_<session_id>_<event_id>` |
| Keyed State worker | `state_<agent_id>_<state_key>` |
| Artifact | explicit `object.object_id` 优先，否则 deterministic default |
| Shared Memory | `shared_<memory_id>_to_<agent_id>` |
| Edge | implementation-specific deterministic source/type/destination composition |
| Version | 由 logical TS 或 state worker 当前版本递增；必须关联 mutation event |

这些规则直接影响 replay 幂等和存储兼容，修改时必须提供迁移与重放测试。
