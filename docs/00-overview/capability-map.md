# 能力地图

状态基于当前启动链和测试，不代表长期兼容承诺。

| 能力 | 用户入口 | 核心对象 | 主要实现 | 状态 |
|---|---|---|---|---|
| 写入 Agent Event | `POST /v1/ingest/events`, gRPC | Event | `access.Gateway`, `worker.Runtime`, WAL | Implemented |
| WAL 持久化与扫描 | Runtime/Admin | Event, LSN | `eventbackbone.FileWAL` / `InMemoryWAL` | Implemented |
| Event 物化 | Event ingest | Memory/State/Artifact/Edge/Version | `materialization.Service`, worker materializers | Implemented |
| 查询 Memory 与对象 | `POST /v1/query` | Memory 等 | planner, TieredDataPlane, Assembler | Implemented |
| 查询最新 State | `/v1/states` 或 query selector | AgentState | ObjectStore, state materializer | Partial |
| Artifact 管理 | `/v1/artifacts`, Event ingest | Artifact | object materializer, ObjectStore | Implemented |
| Relation/Edge | `/v1/edges`, query/trace | Edge | GraphEdgeStore, evidence assembler | Implemented |
| Trace 与 provenance | `GET /v1/traces/{id}` | Edge/Version/derivation | Gateway, Evidence, logs | Implemented |
| Strict/Bounded/Eventual | Event/query + admin mode | LSN/visibility | consistency.Controller/Tracker | Implemented |
| Replay | `/v1/admin/replay` | Event/Object | Runtime + WAL scan | Implemented |
| Rollback | `/v1/admin/rollback` | Version/Object | Gateway/Storage | Partial |
| Hot/Warm/Cold memory | config/query | Memory | TieredObjectStore/TieredDataPlane | Implemented |
| S3/MinIO cold tier | env + compose/admin | archived objects | S3ColdStore | Conditional |
| Hybrid retrieval | query | retrieval projection | lexical + CGO ANN + RRF | Conditional |
| Warm vector batch | HTTP internal/binary/gRPC | RetrievalSegment | transport + retrievalplane | Experimental |
| Governance policy | `/v1/policies`, query trace | PolicyRecord | PolicyEngine/PolicyStore | Partial |
| Share contract | `/v1/share-contracts` | ShareContract | ContractStore/PolicyEngine | Partial |
| Memory lifecycle | internal memory routes | Memory/AlgorithmState | cognitive workers | Experimental |
| Python SDK | `sdk/python` | HTTP objects | `PlasmodClient` | Partial |
| Node SDK | `sdk/nodejs` | consistency mode | `AndbClient` | Partial |
| gRPC | `:19531` | core ingest/query/vector | `PlasmodAPIService` | Implemented, limited surface |

## 状态解释

“Partial” 常见原因包括：直接 CRUD 绕过 Event/WAL；缺少分页或完整 ACL；只覆盖一部分对象类型；或实现存在但没有稳定 SDK 映射。“Conditional” 表示需要编译标签、native library、外部服务或额外配置。
