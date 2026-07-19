# API to Engine Matrix

## Public/Application Routes

| Method/route | Handler/Runtime entry | Chain | Primary Engines | 关键边界 |
|---|---|---|---|---|
| `POST /v1/ingest/events` | `handleIngest` -> `SubmitIngestContext` | Ingest | Execution Coordination, Object Derivation, Canonical Graph, Retrieval, Consistency | 完整 WAL/canonical/visibility 入口 |
| `POST /v1/ingest/vectors` | `handleIngestVectors` -> warm ingest methods | none | Adaptive Retrieval | 只写 retrieval segment，无 Event/canonical 语义 |
| `POST /v1/ingest/document` | document assembler -> repeated `SubmitIngest` | Ingest | Object Derivation, Retrieval | internal document adapter；稳定性不等同 Event API |
| `POST /v1/query` | `handleQuery` -> `ServiceQueryContext` -> `ExecuteQueryContext` | Query | Retrieval, Evidence, Canonical Graph, Governance | structured evidence 主入口 |
| `POST /v1/query/batch` | `ServiceQueryBatch` | none | Adaptive Retrieval | `VectorWarmBatchQueryRequest`，不是 QueryRequest 数组 |
| `GET/POST /v1/agents` | canonical handler | none | Canonical Graph | 直接 store CRUD，绕过 WAL/Chain |
| `GET/POST /v1/sessions` | canonical handler | none | Canonical Graph | 同上 |
| `GET/POST /v1/memory` | canonical handler | none | Canonical Graph | 同上；不会自动写 projection/version |
| `GET/POST /v1/states` | canonical handler | none | Canonical Graph | 同上 |
| `GET/POST /v1/artifacts` | canonical handler | none | Canonical Graph | 同上 |
| `GET/POST /v1/edges` | canonical handler | none | Canonical Graph | 不验证两端对象语义完整性 |
| `GET/POST /v1/policies` | canonical handler | none | Governance, Canonical Graph | record storage；不是通用 policy language |
| `GET/POST /v1/share-contracts` | canonical handler | none | Governance, Canonical Graph | contract storage；执行覆盖有限 |
| `GET /v1/traces/{id}` | `handleTraces` | Query-like | Evidence, Canonical Graph | 按对象组装已有 evidence/provenance |
| `GET /v1/agent/list` | internal agent listing | Collaboration | Canonical Graph/Governance | experimental |

## Internal Runtime Routes

| Route group | Runtime entry | Engine | State mutation |
|---|---|---|---|
| `/v1/internal/memory/recall` | `DispatchRecall` | Memory Evolution + Governance | 通常只读；plugin recall 不持久化 state |
| `/v1/internal/memory/ingest` | `DispatchAlgorithm("ingest")` | Memory Evolution | algorithm state, Memory ref, audit |
| `/v1/internal/memory/compress` | algorithm dispatch | Memory Evolution + Canonical Graph | derived Memory, state/audit |
| `/v1/internal/memory/summarize` | algorithm dispatch | Memory Evolution + Canonical Graph | summary Memory, audit |
| `/v1/internal/memory/decay` | algorithm dispatch | Memory Evolution | lifecycle/algorithm state/audit |
| `/v1/internal/memory/share` | `DispatchShare` | Collaboration + Governance | shared Memory copy；contract enforcement 有限 |
| `/v1/internal/memory/conflict/resolve` | `DispatchConflictResolve` | Collaboration | winner/loser mutation, conflict edge/audit |
| `/v1/internal/memory/stale` | handler direct store mutation | Memory Evolution | `stale`, inactive, audit/metric path |
| `/v1/internal/memory/conflict/inject` | handler direct canonical setup | Collaboration | controlled conflict records；internal only |
| `/v1/internal/task/start\|complete\|tokens\|claim\|stage` | metric/state handlers | Execution Coordination | session metrics/canonical events depending handler |
| `/v1/internal/plan/step\|repair` | plan handlers | Execution Coordination/Evidence | plan state/metrics |
| `/v1/internal/mas/answer-consistency\|aggregate` | MAS handlers | Collaboration | metrics/aggregate response |
| `/v1/internal/tool-state` | tool state handler -> ingest/query | Ingest + Query | State/Event/projection |
| `/v1/internal/agent/handoff` | handoff handler -> ingest/share | Collaboration + Ingest | Event/shared object |
| `/v1/internal/session/context` | session context query | Query/Evidence | read only |
| `/v1/internal/warm-segment/register` | Runtime register | Adaptive Retrieval | in-memory/native segment mapping |
| `/v1/internal/eval/ground-truth` | internal handler | outside stable core contract | 不作为核心系统能力声明 |

Internal routes 未被 admin middleware 自动保护，部署层必须限制网络访问。

## Management Routes

| Route | Primary Engine/Manager | 行为 |
|---|---|---|
| `/healthz`, `/v1/system/mode` | app/access | process/mode status |
| `/v1/admin/topology` | Execution Coordination | coordinator registry + NodeManager topology |
| `/v1/admin/storage`, `/config/effective` | Tiered Storage/app | effective runtime config |
| `/v1/admin/s3/export`, `/snapshot-export`, `/cold-purge` | Tiered Storage | archive/export/purge |
| `/v1/admin/warm/prebuild` | Adaptive Retrieval | build/load default warm segment |
| `/v1/admin/embeddings/reindex` | Retrieval + Reconciliation fragments | scan canonical memories and rebuild retrieval projection |
| `/v1/admin/dataset/delete`, `/memory/delete-by-source` | Governance + Canonical Graph | logical deletion |
| `/v1/admin/dataset/purge`, `/memory/purge-by-source`, `/dataset/purge/task` | Reconciliation fragments + Storage | staged hard deletion; no distributed transaction |
| `/v1/admin/data/wipe` | Runtime/Storage/Consistency | pause/reset stores, WAL/checkpoint/index |
| `/v1/admin/rollback` | Version/Storage | 部分实现；不是全系统 point-in-time rollback |
| `/v1/admin/consistency-mode` | Consistency Controller | inspect/change default mode |
| `/v1/admin/replay` | Consistency/Runtime | preview or apply WAL replay |
| `/v1/admin/metrics` | metrics collector | in-process counters/histograms/status |
| `/v1/admin/governance-mode`, `/runtime-mode` | Runtime/Governance | toggle in-memory runtime flags |
| `/v1/admin/memory/providers/mode\|health` | Memory Evolution | provider router mode/health |

## Internal Transport Routes

| Route | Contract | Engine | Evidence/canonical semantics |
|---|---|---|---|
| `POST /v1/internal/rpc/ingest_batch` | binary vector batch | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/unload_segment` | segment ID | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm` | binary query vector | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm_batch` | binary NQ batch | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm_serial_batch` | serial reference | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/query_warm_batch_raw` | raw native path | Adaptive Retrieval | 无 |
| `POST /v1/internal/rpc/register_warm` | JSON mapping | Adaptive Retrieval | 无 |
| `GET /v1/wal/stream` | SSE | Event backbone | WAL observation only |

## API Design Rules

1. 需要 durability、replay、canonical object 和 visibility guarantee 时使用 Event ingest；
2. direct canonical POST 是管理/迁移接口，不应被描述为等价 Ingest Chain；
3. native warm routes只返回 ANN 候选，不能宣称返回 evidence-bearing response；
4. internal route 的 request struct 和稳定性由当前 commit 决定；
5. 新接口必须在本表明确其 Chain、Engine、事务边界和绕过的机制。
