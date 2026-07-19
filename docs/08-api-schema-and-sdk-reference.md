# 08. API、Schema、配置与 SDK 参考

> Language: 中文 | [English](en/08-api-schema-and-sdk-reference.md)

---

集中定义 HTTP、gRPC、内部 API、SDK、错误、幂等、配置和核心 Schema。

---

## 08.1. Admin API

### 08.1.1. 认证

设置：

```bash
export PLASMOD_ADMIN_API_KEY='replace-with-a-secret'
```

请求使用 `X-Admin-Key` 或 `Authorization: Bearer <key>`。兼容环境变量
`ANDB_ADMIN_API_KEY` 仍可读取，但新部署应使用 Plasmod 名称。

如果密钥未设置，当前代码记录警告并放行 admin route。生产安全检查必须把“密钥为空”视为部署失败。

### 08.1.2. 只读接口

- topology、storage、config/effective；
- metrics；
- consistency/governance/runtime/provider mode 的 GET；
- provider health；
- purge task status。

### 08.1.3. 变更接口

- S3 export/snapshot/cold purge；
- warm prebuild、embedding reindex；
- dataset/source delete 和 purge；
- data wipe、rollback、replay；
- consistency/governance/runtime/provider mode 的 POST。

### 08.1.4. 高风险操作

`data/wipe`、purge、cold-purge、rollback 和 replay 会改变大范围数据或派生状态。调用前必须：

1. 验证 effective config 和目标实例；
2. 备份；
3. 限制并发写入；
4. 保存请求参数和返回 task ID；
5. 通过 query、trace、storage key 和 metrics 验证结果。

Admin API 没有内建多人审批、细粒度 RBAC 或审计归档服务，这些应由控制面网关补齐。

---

## 08.2. API Overview

### 08.2.1. HTTP 面

Plasmod 使用 `net/http` 注册三类接口：

- 数据/应用接口：Event ingest、Query、canonical object、Trace；
- 内部 runtime 接口：memory algorithm、task、plan、MAS、transport；
- 管理接口：配置、存储、replay、删除、模式和 metrics。

统一模式把管理和数据路由放在 `127.0.0.1:8080`；拆分模式默认管理 `9091`、数据 `19530`。

### 08.2.2. Transport 面

`src/internal/transport/server.go` 提供内部 HTTP RPC 和 WAL stream。gRPC server 默认监听 `19531`，当前
能力范围小于 HTTP API，不应假定每个 HTTP route 都有 gRPC 等价物。

### 08.2.3. Content Type

请求和成功响应主要使用 `application/json`。部分错误由 `http.Error` 返回纯文本。客户端应先检查 HTTP
状态，再根据 `Content-Type` 解码，不能对所有失败直接调用 JSON parser。

### 08.2.4. 稳定性标签

- **Implemented**：当前启动链路注册且有实现；
- **Experimental**：已实现，但命名或 payload 可能变化；
- **Partial**：能力存在，但契约覆盖不完整；
- **Not Confirmed**：代码中没有足够证据，不应依赖。

`v1` 是路由名称，不构成完整的兼容性承诺；参见 [`api-versioning.md`](08-api-schema-and-sdk-reference.md)。

---

## 08.3. API Versioning

### 08.3.1. 当前版本边界

- HTTP 路径使用 `/v1`；
- Event schema 使用 `plasmod.dynamic_event.v0.4`；
- canonical Go structs 通过 JSON tag 定义 wire shape；
- internal/transport API 与仓库 commit 更紧密绑定。

`/v1` 不意味着所有字段已冻结。新增 optional 字段通常向后兼容；重命名、类型变化、默认一致性变化和删除字段
都需要迁移说明。

### 08.3.2. 兼容规则

1. 新客户端输出 canonical v0.4 嵌套字段；
2. 服务端可读取 legacy alias，但不应在新响应中继续扩散；
3. 未知 optional 字段应通过 extensions/payload 承载；
4. internal API 只保证同版本组件互通；
5. 持久化 schema 变化必须同时考虑 replay 旧 WAL；
6. SDK release 必须标明对应服务 commit/tag。

正式升级流程见 [第 13 章：扩展、兼容性与系统演进](13-extensibility-compatibility-and-evolution.md)。

---

## 08.4. Authentication

### 08.4.1. 内建能力

Admin middleware 只匹配 `/v1/admin/`：

- primary env：`PLASMOD_ADMIN_API_KEY`；
- compatibility env：`ANDB_ADMIN_API_KEY`；
- headers：`X-Admin-Key` 或 Bearer token；
- 比较使用常量时间/HMAC 方式降低时序泄露。

### 08.4.2. 未内建的能力

- 数据 API 的统一用户登录；
- `/v1/internal/*` 的认证；
- 细粒度 RBAC；
- TLS 终止；
- token 签发/轮换；
- request quota。

### 08.4.3. 生产部署

在 Plasmod 前放置可信 gateway/service mesh：

1. TLS；
2. 验证用户或 workload identity；
3. 把身份绑定到 tenant/workspace，拒绝客户端任意越权覆盖；
4. 隔离 admin、internal、transport 端口；
5. 设置 body size、rate limit 和审计日志；
6. 将 admin key 放入 secret manager，并定期轮换。

Canonical User、Policy 和 ShareContract 是数据/治理模型，不等于 HTTP authentication 已完成。

---

## 08.5. Binary And gRPC Transport

### 08.5.1. gRPC Server

gRPC 默认启用并监听 `0.0.0.0:19531`。可通过 `PLASMOD_GRPC_ENABLED=0` 关闭。默认最大消息尺寸为
512 MiB，具体解析位于 `src/internal/app/ports.go`。

当前 gRPC 面主要服务高吞吐或内部数据传输，能力不与所有 HTTP route 一一对应。部署前应从已注册 service
定义确认客户端方法，不要仅根据端口开放判断协议完整。

当前 active Gateway/transport 未注册 SSE endpoint；WAL stream 是内部 HTTP stream，不是浏览器 EventSource
契约。所谓 binary transport 主要指 protobuf/gRPC 和 row-major float vector payload 的组件协议。

### 08.5.2. Internal HTTP RPC

`src/internal/transport/server.go` 提供：

- batch ingest；
- unload segment；
- warm query 及其 batch/raw 变体；
- warm segment register；
- WAL stream。

这些接口面向 Plasmod 组件，不是 Agent 应用的首选入口。

Warm batch vector 使用 row-major layout：二维 HTTP vectors 在服务端展平为 `nq * dim`，每行维度必须一致；
internal flat request 还显式携带 `nq`、`dim`、`top_k`。连接复用由 HTTP client/gRPC channel 管理，服务端不为
每个 Agent 建立独立持久 session connection。

### 08.5.3. Message Size

大向量 batch 需要同时考虑：

- gRPC max receive/send size；
- HTTP server/client body 和 timeout；
- gateway write concurrency semaphore；
- native segment 内存；
- Badger transaction size。

提高消息上限不会自动提高吞吐，反而可能放大内存峰值。

---

## 08.6. Configuration Reference

### 08.6.1. 核心启动配置

| Variable | Default | Purpose |
|---|---|---|
| `PLASMOD_STORAGE` | `disk` | `disk` 或 `memory` |
| `PLASMOD_DATA_DIR` | `.andb_data` | Badger、WAL、checkpoint 根目录 |
| `PLASMOD_EMBEDDER` | 由配置解析 | `tfidf`、ONNX/其他 provider |
| `PLASMOD_GRPC_ENABLED` | enabled | 是否启动 gRPC |
| `PLASMOD_ADMIN_API_KEY` | empty | admin route key |
| `APP_MODE` | 非 prod | visibility/debug 行为 |
| `PLASMOD_SKIP_VECTOR_INDEX` | false | 全局跳过向量投影 |

### 08.6.2. 端口

统一模式默认 HTTP `127.0.0.1:8080`；拆分默认 management `0.0.0.0:9091`、API
`0.0.0.0:19530`；gRPC 默认 `0.0.0.0:19531`。精确变量与解析逻辑见
`src/internal/app/ports.go`。

### 08.6.3. Consistency

默认 strict，相关默认值包括 queue 4096、worker 4、retry 8、bounded lag 1s、query/shutdown timeout
30s 和 checkpoint flush 50ms。覆盖项由 `worker/consistency` 的环境解析定义，管理 API 可在运行时修改模式。

### 08.6.4. S3/MinIO

配置包括 endpoint、bucket、access key、secret key、TLS、region/prefix。敏感值不应出现在日志、文档示例或
版本控制中。

### 08.6.5. YAML 的真实状态

当前启动代码会读取 `configs/memory_tiering.yaml` 和 `configs/algorithm_*.yaml`。仓库中的
`configs/app.yaml`、`storage.yaml`、`retrieval.yaml`、`graph.yaml` 不能单独作为运行时配置真值；在
`app.BuildServer` 未接入前，它们应视为参考/兼容配置。

使用 `/v1/admin/config/effective` 和启动日志确认最终值。

---

## 08.7. Error Model

### 08.7.1. HTTP 状态

| Status | 当前含义 |
|---:|---|
| `200`/其他 2xx | handler 完成；仍需检查 query status/task status |
| `400` | JSON、字段或普通运行时校验错误 |
| `401` | admin key 缺失或错误 |
| `405` | Method 不支持 |
| `408` | 请求 context canceled |
| `503` | backpressure、paused、accepted-not-visible、projection failure 或 runtime unavailable |
| `504` | consistency/query 等待 deadline exceeded |

状态映射位于 `src/internal/access/gateway.go`。不同 handler 有些使用 JSON，有些使用纯文本错误。

### 08.7.2. 重试分类

- 400/405：不重试，修正调用；
- 401：修正凭证；
- 503 backpressure：指数退避；
- 503 accepted-not-visible：先用 event/object ID 查询状态，避免盲目重复写；
- 503 projection failure：检查 embedding/native backend，再按恢复策略 replay；
- 504：结果可能稍后可见，先读后重试；
- 网络断开：按未知提交结果处理。

### 08.7.3. Query 的逻辑状态

HTTP 2xx 下 `query_status` 仍可能是 `no_retrieval_hits` 或
`no_retrieval_hits_supplemented`。后者表示检索没有 seed，但 canonical listing 补充了对象。

---

## 08.8. Idempotency

### 08.8.1. 当前结论

公共 API 没有统一 `Idempotency-Key` header、幂等记录表或 exactly-once 提交承诺。

### 08.8.2. Event 写入

`identity.event_id` 应由调用者稳定生成，是追踪和应用层去重的主键。网络超时后：

1. 查询预期 canonical object 或 trace；
2. 检查服务日志/metrics 和 WAL 状态；
3. 只有确认未处理或 materialization 可安全重入时才重发；
4. 重发使用同一 event ID，不生成新的逻辑事件。

WAL append、projection 和直接 CRUD 的重复行为不同，不能从一个 route 的幂等性外推到所有 route。

### 08.8.3. 管理操作

reindex、export、purge 和 replay 可能产生任务或重复扫描。调用者应保存 task ID、范围和 checkpoint，并在
重试前查询状态。

### 08.8.4. 扩展要求

新增写接口应明确：幂等键、重复检测范围、结果缓存时间、并发重复的处理方式以及 WAL/事务边界。

---

## 08.9. Internal API

Internal API 连接 Agent SDK、算法 provider 和 runtime 内部组件。它们已在当前 Gateway 注册，但不具备稳定
公共契约。

### 08.9.1. Memory Algorithm Bridge

- `POST /v1/internal/memory/recall`
- `POST /v1/internal/memory/ingest`
- `POST /v1/internal/memory/compress`
- `POST /v1/internal/memory/summarize`
- `POST /v1/internal/memory/decay`
- `POST /v1/internal/memory/share`
- `POST /v1/internal/memory/conflict/resolve`
- `POST /v1/internal/memory/stale`
- `POST /v1/internal/memory/conflict/inject`

请求由 `src/internal/access/gateway.go` 解码并分派到 `agent-sdk`/semantic/coordinator 服务。算法 profile 可以
来自 baseline、MemoryBank 或 Zep 配置，但 canonical schema 不随 provider 改变。

### 08.9.2. Task And Plan

- task：start、complete、tokens、claim、stage；
- plan：step、repair；
- session：context；
- tool：tool-state；
- agent：handoff；
- MAS：answer-consistency、aggregate。

这些接口假设调用者是受信 runtime。当前 admin auth middleware 不覆盖 `/v1/internal/*`。

### 08.9.3. Transport RPC

`src/internal/transport/server.go` 另行注册 ingest batch、unload segment、warm query 和 register warm 等
`/v1/internal/rpc/*` 路由。它们是节点组件协议，payload 与 Gateway API 不同。

### 08.9.4. 使用约束

1. 只在私有网络开放；
2. 客户端和服务端按同一 commit 部署；
3. 升级时先对照 handler request struct；
4. 不把 internal response 保存为长期外部契约；
5. 需要业务稳定性时封装在自有 adapter 后面。

---

## 08.10. Pagination And Batching

### 08.10.1. Pagination

当前 canonical collection handlers 没有统一的 `page_token`/`cursor` 契约。不同 GET route 使用自身 query
parameters，调用者不能假设大列表具有稳定快照分页。

大范围导出应使用管理 export/snapshot 能力或直接扩展带稳定 cursor 的 API，而不是循环读取无排序列表。

### 08.10.2. Query Batch

`POST /v1/query/batch` 接收 `VectorWarmBatchQueryRequest`，不是多个通用 QueryRequest。它要求
`warm_segment_id`、`agent_mode` 和二维 `vectors`，并可用 `source_ids`/`row_lineage` 将每行结果分发给
single-agent 或 multi-agent 来源。Batch 降低 native search 调用开销，但：

- 不构成跨查询事务；
- 该 route 不执行通用 QueryRequest 的 tenant/scope/evidence 流程；
- 需要逐行检查 `rows`，并读取 `by_source`；
- batch size 受 HTTP body、内存、embedding 和 native search 限制。

### 08.10.3. Vector Batch

`/v1/ingest/vectors` 接受二维 vectors 和可选 object IDs。要求：

- 每个向量维度一致；
- object ID 数量与 vectors 一致；
- index 参数在整个 segment 内一致；
- batch 失败时不要假设部分提交可自动回滚。

---

## 08.11. Public HTTP API

### 08.11.1. Event Ingest

```text
POST /v1/ingest/events
```

Body 为 [`schema-reference/event.md`](08-api-schema-and-sdk-reference.md) 所述 Event。成功响应包含写入/可见状态和
LSN 相关信息；失败状态见 [`error-model.md`](08-api-schema-and-sdk-reference.md)。这是需要 WAL、replay 和 materialization
语义时的首选写入口。

### 08.11.2. Vector Ingest

```text
POST /v1/ingest/vectors
```

Body 主要字段：`vectors`、`object_ids`、`segment_id`、`index_type`，以及 IVF 参数。支持的 index type 为
`HNSW`、`IVF_FLAT`、`IVF_PQ`、`IVF_SQ8`、`DISKANN`，但是否可用取决于原生构建。

该入口写物理检索 segment，不替代 Event/canonical object ingest。只写向量会缺少完整 Agent 对象语义。

### 08.11.3. Query

```text
POST /v1/query
POST /v1/query/batch
```

`/v1/query` 的请求和响应见 [`schema-reference/query.md`](08-api-schema-and-sdk-reference.md)。`/v1/query/batch` 并非
多个通用 QueryRequest；它接收 `VectorWarmBatchQueryRequest`，字段为 `agent_mode`、`warm_segment_id`、
`top_k`、二维 `vectors`、可选 `source_ids`/`row_lineage` 和 `search_raw`，直接执行已注册 Warm Segment 的
批量 ANN。

### 08.11.4. Canonical Collections

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

### 08.11.5. Trace

```text
GET /v1/traces/{object_id}
```

`object_id` 是路径后缀，必须 URL encode。响应由已有 object、edge、version、policy 和 provenance 组装。

---

## 08.12. Route Index

### 08.12.1. Management

| Method | Route | Purpose | Status | Auth |
|---|---|---|---|---|
| GET | `/healthz` | 进程健康 | Implemented | 无内置认证 |
| GET | `/v1/system/mode` | APP_MODE/调试状态 | Implemented | 无内置认证 |
| GET | `/v1/admin/topology` | 拓扑摘要 | Implemented | Admin key |
| GET | `/v1/admin/storage` | 存储配置摘要 | Implemented | Admin key |
| GET | `/v1/admin/config/effective` | 有效配置 | Implemented | Admin key |
| POST | `/v1/admin/s3/export` | 导出到 cold store | Implemented | Admin key |
| POST | `/v1/admin/s3/snapshot-export` | 快照导出 | Implemented | Admin key |
| POST | `/v1/admin/s3/cold-purge` | 清理 cold records | Implemented | Admin key |
| POST | `/v1/admin/warm/prebuild` | 预构建 warm segment | Implemented | Admin key |
| POST | `/v1/admin/embeddings/reindex` | 重建 embedding/index | Implemented | Admin key |
| POST | `/v1/admin/dataset/delete` | 数据集逻辑删除 | Implemented | Admin key |
| POST | `/v1/admin/dataset/purge` | 数据集物理清理 | Implemented | Admin key |
| GET/POST | `/v1/admin/dataset/purge/task` | purge task 状态/控制 | Implemented | Admin key |
| POST | `/v1/admin/memory/delete-by-source` | 按来源删除 | Implemented | Admin key |
| POST | `/v1/admin/memory/purge-by-source` | 按来源清理 | Implemented | Admin key |
| POST | `/v1/admin/data/wipe` | 清空数据 | Implemented | Admin key |
| POST | `/v1/admin/rollback` | 管理回退 | Partial | Admin key |
| GET/POST | `/v1/admin/consistency-mode` | 查看/切换一致性 | Implemented | Admin key |
| POST | `/v1/admin/replay` | WAL replay | Implemented | Admin key |
| GET | `/v1/admin/metrics` | 运行指标 | Implemented | Admin key |
| GET/POST | `/v1/admin/governance-mode` | 治理模式 | Implemented | Admin key |
| GET/POST | `/v1/admin/runtime-mode` | runtime 模式 | Implemented | Admin key |
| GET/POST | `/v1/admin/memory/providers/mode` | 算法 provider 模式 | Experimental | Admin key |
| GET | `/v1/admin/memory/providers/health` | provider 健康 | Experimental | Admin key |

### 08.12.2. Application Data API

| Method | Route | Purpose | Status |
|---|---|---|---|
| POST | `/v1/ingest/events` | Dynamic Event ingest | Implemented |
| POST | `/v1/ingest/vectors` | 预计算向量 segment ingest | Implemented |
| POST | `/v1/ingest/document` | 长文档分段写入 | Experimental |
| POST | `/v1/query` | 单查询 | Implemented |
| POST | `/v1/query/batch` | Warm Segment 向量批查询 | Implemented |
| GET/POST | `/v1/agents` | Agent canonical records | Implemented |
| GET/POST | `/v1/sessions` | Session canonical records | Implemented |
| GET/POST | `/v1/memory` | Memory canonical records | Implemented |
| GET/POST | `/v1/states` | AgentState canonical records | Implemented |
| GET/POST | `/v1/artifacts` | Artifact canonical records | Implemented |
| GET/POST | `/v1/edges` | Edge canonical records | Implemented |
| GET/POST | `/v1/policies` | Policy records | Implemented |
| GET/POST | `/v1/share-contracts` | Share contracts | Implemented |
| GET | `/v1/traces/{object_id}` | Evidence/provenance trace | Implemented |
| GET | `/v1/agent/list` | 按角色/范围列 Agent | Experimental |

`net/http.ServeMux` 只按路径注册；每个 handler 自己检查 Method。表中方法来自当前 handler 行为，调用不支持的
Method 通常得到 `405`。

### 08.12.3. Internal Runtime API

| Routes | Purpose | Status |
|---|---|---|
| `/v1/internal/memory/*` | recall/ingest/compress/summarize/decay/share/conflict/stale | Experimental |
| `/v1/internal/task/*` | start/complete/tokens/claim/stage | Experimental |
| `/v1/internal/plan/*` | step/repair | Experimental |
| `/v1/internal/mas/*` | answer consistency/aggregate | Experimental |
| `/v1/internal/tool-state` | stateful tool query | Experimental |
| `/v1/internal/agent/handoff` | Agent handoff | Experimental |
| `/v1/internal/session/context` | Session context aggregation | Experimental |
| `/v1/internal/warm-segment/register` | 注册原生 warm segment | Experimental |

最后一组路由虽存在于核心 Gateway，但并非稳定公共 API；生产部署应阻止外网直接访问。

---

## 08.13. Canonical Objects

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

---

## 08.14. Dynamic Event v0.4

Canonical schema 为 `schemas.Event` 和 `schemas.DynamicEvent` 相关嵌套类型。

| Group | Responsibility |
|---|---|
| `identity` | event、tenant、workspace 标识 |
| `actor` | agent、session 和角色 |
| `time` | event/ingest/visible time、logical timestamp |
| `event` | event type、importance 等描述 |
| `object` | 目标对象类型和可选 ID |
| `causality` | parent、causal refs、dependencies |
| `access` | visibility、consistency、ACL/policy references |
| `materialization` | enabled、targets、状态提示 |
| `retrieval` | namespace、index text、embedding、query hints |
| `payload`/`data` | 业务内容和结构化数据 |
| `runtime` | 写入/可见状态 |
| `extensions` | 扩展 labels/fields |

文本抽取顺序优先 `retrieval.index_text`，然后 payload 中 text/content。Namespace 优先 retrieval namespace，
再 workspace、session，最后 default。

旧平铺字段可被输入兼容层吸收，但 canonical JSON 输出隐藏对应 `json:"-"` alias。

---

## 08.15. Materialization Schema

Event 的 `materialization` group 描述期望物化状态和目标类型。常见 target：

- `memory`；
- `agent_state`；
- `artifact`；
- `relation`/`edge`；
- `object_version`；
- retrieval projection。

当前 active materializer 尚未把 `enabled`/`targets` 作为通用硬 gate。每次 Event ingest 默认生成 Memory、
Memory ObjectVersion 和 ingest checkpoint State；Artifact 与专用 AgentState 再由 event/object/payload 和
state key 条件触发。设置 target 不保证任意未知类型能被自动创建，设置 `enabled=false` 也不保证跳过上述
默认 canonical projection。

Memory ID 默认 `mem_<event_id>`；ingest checkpoint State ID 为 `state_<session_id>_<event_id>`；专用 keyed
State ID 为 `state_<agent_id>_<state_key>`；Artifact 可优先采用 `object.object_id`。这些规则属于持久化
兼容面，修改时需要迁移与 replay 验证。

Runtime status 区分接受、物化、投影和可见阶段；一致性 controller 的 tracker/checkpoint 保存推进位置。

---

## 08.16. Memory Algorithm Schema

Memory algorithm 通过稳定 canonical Memory 字段和独立 algorithm state 工作，而不是定义新的数据库主对象。

相关字段包括：

- `memory_type`、`content`、`summary`；
- `confidence`、`importance`、`freshness_score`；
- `ttl`、`valid_from`、`valid_to`；
- `lifecycle_state`、`is_active`；
- `policy_tags`、`algorithm_state_ref`；
- source event IDs 和 provenance ref。

Provider profile 配置位于 `configs/algorithm_baseline.yaml`、`configs/algorithm_memorybank.yaml` 和
`configs/algorithm_zep.yaml`。算法可以影响召回、衰减、压缩和冲突处理，但不能绕过 tenant/
workspace scope、canonical persistence 或治理过滤。

---

## 08.17. Query Schema

`schemas.QueryRequest` 的关键字段：

- 文本：`query_text`、`query_scope`、`top_k`；
- scope：`tenant_id`、`workspace_id`、`agent_id`、`session_id`；
- 类型：`object_types`、`memory_types`、`edge_types`；
- 精确对象：`target_object_ids`；
- 时间：`time_window.from/to`；
- 关系：`relation_constraints`；
- 返回：`response_mode`；
- 数据来源：dataset/source/import batch selectors；
- 访问/物化/runtime filter；
- `warm_segment_id`、`include_cold`、`embedding_vector`。

响应包含 objects/nodes、edges、provenance、versions、applied filters、proof trace、chain traces、evidence
cache、retrieval summary、query status 和 hint。

定义中的标准 response mode 是 `structured_evidence` 和 `objects_only`。未知 mode 的行为应视为不稳定。

`POST /v1/query/batch` 使用 `schemas.VectorWarmBatchQueryRequest`，不是本页 QueryRequest 的数组；其输出为
`VectorWarmBatchQueryResponse`，用于 Warm Segment native batch ANN。

---

## 08.18. Retrieval Schema

### 08.18.1. Event Retrieval

Event 的 retrieval group 描述：namespace、index text、embedding presence/dimension/vector，以及 materialized
retrieval 状态。它是 canonical Event 的投影指令，不是 canonical object 本身。

### 08.18.2. RetrievalSegment

字段包括 segment ID、object type、namespace、time bucket、embedding family、storage/index ref、row count、
min/max timestamp 和 tier。

### 08.18.3. 物理索引

原生层接受 HNSW、IVF_FLAT、IVF_PQ、IVF_SQ8、DISKANN。Embedding family、dimension 和 model ID 必须
作为 segment 兼容边界；同维度不代表同 embedding 空间。

### 08.18.4. Query Projection

Query 可以提供 `embedding_vector` 绕过 embedder。若为空，数据面可调用 configured embedder。结果还会与
lexical/canonical 候选和 Evidence 组装合并，因此原生 ANN hit 不是最终 QueryResponse 的全部语义。

---

## 08.19. SDK Reference

### 08.19.1. Python

路径：`sdk/python`；包：`plasmod-sdk`；模块：`plasmod_sdk`；类：`PlasmodClient`。

主要方法：

```python
PlasmodClient(base_url=None, timeout=None)
ingest_event(event_id, agent_id, session_id, event_type, payload, **extra)
ingest_vectors(vectors, segment_id="", object_ids=None, index_type="", ...)
query(query_text, query_scope="global", top_k=10, **filters)
get_consistency_mode()
set_consistency_mode(mode)
get(path)
post(path, body)
```

默认地址优先读取 `PLASMOD_URI`、`PLASMOD_BASE_URL`，否则使用 `http://127.0.0.1:19530`。超时读取
`PLASMOD_HTTP_TIMEOUT`。SDK 使用 `requests.raise_for_status()`，错误响应会抛出异常。

`ingest_event` 当前构造兼容平铺 Event；需要 v0.4 全部嵌套字段时可使用通用 `post()`，或扩展 SDK。

### 08.19.2. Node.js

路径：`sdk/nodejs`；包名当前为 `andb-sdk-node`，标记为 private；类为 `AndbClient`。目前公开能力主要是
consistency mode 操作，命名仍处于兼容迁移状态，稳定性为 Partial。

### 08.19.3. SDK 兼容原则

SDK 不应自行改变 ID、scope 和 consistency 语义。新增方法必须从 Go schema JSON tag 生成请求，并增加
服务端契约测试；不能仅更新 SDK 示例。

---

## 08.20. WAL Stream

### 08.20.1. 接口

Transport server 注册 `/v1/wal/stream`，用于从指定位置传输 WAL records。核心接口定义在
`src/internal/eventbackbone`：

```go
type WAL interface {
    Append(Event) (LSN, error)
    Scan(from LSN, fn func(Record) bool)
    LatestLSN() LSN
}
```

支持错误传播的实现还实现 `ErrorAwareWAL`。

### 08.20.2. 语义

- LSN 表示日志顺序，不等于 wall-clock 时间；
- stream record 是 Event 事实，不是已经物化的 object snapshot；
- 消费者必须处理断线、重复和从 checkpoint 恢复；
- scan 完成不代表 retrieval projection 已完成；
- FileWAL 损坏必须显式报错，不能把尾部缺失当作正常 EOF。

### 08.20.3. 安全边界

WAL 可能包含原始 payload 和跨 scope 事件。该 route 应只对受信节点开放，并通过网络身份、TLS 和最小
权限控制。它不是公共变更数据捕获 API。
