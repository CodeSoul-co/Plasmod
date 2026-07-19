# 需求追踪矩阵

| Requirement | Design | Module/API | Test | Status |
|---|---|---|---|---|
| FR-ING-001 | Event-first, write path | `schemas/dynamic_event.go`; `POST /v1/ingest/events` | dynamic event/gateway tests | Implemented |
| FR-ORD-001 | Source of truth, failure model | `eventbackbone/*wal*`; admin replay | WAL tests | Implemented |
| FR-MAT-001 | Event-to-object, canonical projection | materialization + storage projection | materialization/projection tests | Implemented |
| FR-STA-001 | Canonical object/version model | state materializer; `/v1/states` | state/runtime tests | Implemented/Partial |
| FR-RET-001 | Query path, evidence | semantic/dataplane/evidence; `/v1/query` | query/evidence/tiered tests | Implemented |
| FR-CON-001 | Consistency model | worker/consistency; admin mode | controller/tracker tests | Implemented |
| FR-GOV-001 | Security/policy model | semantic policy + policy/contract/audit stores | governance tests | Partial |
| FR-OPS-001 | Failure/recovery/lifecycle | admin delete/purge/wipe/replay | access/storage/runtime tests | Implemented/Partial |
| FR-SDK-001 | Transport model | HTTP/gRPC/binary + SDK | gRPC/framing/SDK tests | Partial |
| NFR-FRESH-001 | Consistency/watermark | tracker/controller | consistency tests | Implemented |
| NFR-COR-001 | Canonical atomicity | storage factory/projection | Badger projection tests | Implemented for shared Badger backend |
| NFR-SEC-001 | Security model | admin auth/visibility | auth/visibility tests | Partial |
| NFR-PORT-001 | Dependency/build model | retrieval stub/build tags | standard and tagged builds | Partial |

## 维护规则

新增或修改功能时，应同步更新 requirement、设计文档、route/schema reference、代码映射和至少一项测试。只有接口存在但启动链不可达时，状态必须写为 Not Confirmed，而不是 Implemented。
