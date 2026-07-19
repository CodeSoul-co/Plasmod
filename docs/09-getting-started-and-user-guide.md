# 09. 安装、启动与用户操作手册

> Language: 中文 | [English](en/09-getting-started-and-user-guide.md)

---

提供环境准备、构建启动、首次写入查询以及全部用户操作的完整闭环。

---

## 09.1. First Event And Query

以下示例使用 Dynamic Event v0.4，并以 Event 作为 WAL 和物化链路的入口。

### 09.1.1. 写入 Event

```bash
curl -fsS -X POST http://127.0.0.1:8080/v1/ingest/events \
  -H 'Content-Type: application/json' \
  -d '{
    "schema_version": "plasmod.dynamic_event.v0.4",
    "identity": {
      "event_id": "evt_quickstart_001",
      "tenant_id": "tenant-quickstart",
      "workspace_id": "workspace-quickstart"
    },
    "actor": {
      "agent_id": "agent-quickstart",
      "session_id": "session-quickstart"
    },
    "time": {
      "event_time": 1767225600000,
      "logical_ts": 1
    },
    "event": {
      "event_type": "user_message",
      "importance": 0.8
    },
    "object": {
      "object_type": "memory"
    },
    "access": {
      "consistency": "strict",
      "visibility": "workspace"
    },
    "materialization": {
      "enabled": true,
      "targets": ["memory", "object_version"]
    },
    "retrieval": {
      "index_text": "The user prefers dark mode",
      "has_embedding": false
    },
    "payload": {
      "text": "The user prefers dark mode."
    }
  }'
```

当前默认 memory materializer 使用 `mem_` 加 `event_id` 生成 Memory ID，因此本例的对象 ID 是
`mem_evt_quickstart_001`。不要假设 `object.object_id` 会覆盖这一规则；显式对象 ID 在 Artifact 路径上
才由 `ArtifactIDOrDefault` 处理。

`has_embedding=false` 且提供 `index_text` 会跳过向量索引，但对象仍进入 canonical storage，且可以走
词法查询。这适合不依赖外部 embedding 的安装验证。

### 09.1.2. 精确查询对象

```bash
curl -fsS -X POST http://127.0.0.1:8080/v1/query \
  -H 'Content-Type: application/json' \
  -d '{
    "query_text": "dark mode preference",
    "tenant_id": "tenant-quickstart",
    "workspace_id": "workspace-quickstart",
    "session_id": "session-quickstart",
    "agent_id": "agent-quickstart",
    "target_object_ids": ["mem_evt_quickstart_001"],
    "object_types": ["memory"],
    "top_k": 10,
    "response_mode": "structured_evidence"
  }'
```

响应的主要部分包括：

- `objects`：命中的 canonical objects；
- `edges`、`versions`、`provenance`：可恢复的关联证据；
- `proof_trace`：证据组装步骤；
- `retrieval_summary`：实际使用的检索层和过滤信息；
- `query_status`：查询是否完成或降级。

### 09.1.3. 查询追踪

```bash
curl -fsS http://127.0.0.1:8080/v1/traces/mem_evt_quickstart_001
```

Trace API 从 object、edge、version、policy 等 canonical records 组装结果，不等同于原始应用日志。

---

## 09.2. Install From Source

### 09.2.1. 获取依赖

在仓库根目录执行：

```bash
go mod download
```

Python SDK 是独立可编辑包：

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e ./sdk/python
```

### 09.2.2. 最小启动

```bash
PLASMOD_STORAGE=disk \
PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf \
PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

该命令的实际语义：

- `disk` 选择 Badger 持久化和文件 WAL；
- `.andb_data` 保存 canonical objects、edges、versions、WAL 和一致性 checkpoint；
- `tfidf` 避免依赖外部 embedding 服务；
- 关闭 gRPC，只开放统一 HTTP `127.0.0.1:8080`。

不要依赖 `configs/storage.yaml` 决定启动后端；当前 `app.BuildServer` 的存储选择以环境变量为准。

### 09.2.3. 启用原生检索

```bash
make cpp
make build
./bin/plasmod
```

`make cpp` 当前会请求 FAISS 支持。构建失败时，先查看
[第 11 章的原生检索依赖说明](11-dependencies-build-and-development.md)，不要把
CGO 失败误判为 Go 对象存储失败。

### 09.2.4. 使用开发脚本

```bash
cp .env.example .env
make dev
```

`make dev` 调用 `scripts/dev_up.sh`，会读取 `.env` 并根据本地原生库是否存在决定是否添加
`retrieval` tag。`.env.example` 只是模板，最终有效配置仍应通过启动日志和管理配置接口确认。

---

## 09.3. Prerequisites

### 09.3.1. 仅使用 Go 数据路径

- Go `1.25.x`，版本以 `go.mod` 为准；
- Git；
- macOS 或 Linux；
- 可写的数据目录。

这种方式使用 Go stub retrieval，不要求先编译 C++，适合验证对象、WAL、查询和证据链。

### 09.3.2. 启用原生检索

还需要：

- CMake `3.20+`；
- 支持 C++17 的编译器；
- OpenMP；
- FAISS 及其传递依赖，或关闭相应构建选项；
- CGO 可用。

`cpp/CMakeLists.txt` 定义原生库，`src/internal/dataplane/retrievalplane/bridge.go` 通过
`retrieval` build tag 启用 CGO 桥接。若 `cpp/build/libplasmod_retrieval.dylib` 或对应 `.so` 存在，`make build`
会自动添加该 tag。

### 09.3.3. Docker 路径

- Docker Desktop 或兼容 Docker Engine；
- Docker Compose v2；
- 至少为镜像构建和 Badger/MinIO 数据卷预留足够磁盘空间。

### 09.3.4. 启动前检查

```bash
go version
docker version
docker compose version
cmake --version
```

不使用 Docker 或原生检索时，可忽略对应检查。

---

## 09.4. Python SDK Quickstart

### 09.4.1. 安装

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -e ./sdk/python
```

当前包名为 `plasmod-sdk`，导入模块为 `plasmod_sdk`，客户端类为 `PlasmodClient`。

### 09.4.2. 写入和查询

```python
from plasmod_sdk import PlasmodClient

client = PlasmodClient(base_url="http://127.0.0.1:8080")

client.ingest_event(
    event_id="evt_python_001",
    agent_id="agent-python",
    session_id="session-python",
    event_type="user_message",
    payload={"text": "The user prefers concise answers."},
    tenant_id="tenant-quickstart",
    workspace_id="workspace-quickstart",
    access={"consistency": "strict", "visibility": "workspace"},
)

result = client.query(
    query_text="answer style preference",
    tenant_id="tenant-quickstart",
    workspace_id="workspace-quickstart",
    session_id="session-python",
    agent_id="agent-python",
    top_k=10,
)
print(result)
```

SDK 是 HTTP 客户端，不会在本地启动 Plasmod，也不会替服务端生成持久化目录。更完整的方法签名见
[第 8 章的 SDK Reference](08-api-schema-and-sdk-reference.md)。

---

## 09.5. Quickstart

### 09.5.1. 启动

```bash
PLASMOD_STORAGE=disk PLASMOD_DATA_DIR=.andb_data \
PLASMOD_EMBEDDER=tfidf PLASMOD_GRPC_ENABLED=0 \
go run ./src/cmd/server
```

### 09.5.2. 检查健康状态

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

### 09.5.3. 写入和查询

按 [`first-event-and-query.md`](09-getting-started-and-user-guide.md) 写入 `evt_quickstart_001`，随后查询
`mem_evt_quickstart_001`。严格一致性写入返回成功时，该对象已通过当前一致性 gate。

### 09.5.4. 查看追踪

```bash
curl -fsS http://127.0.0.1:8080/v1/traces/mem_evt_quickstart_001
```

完整验证项见 [`verify-installation.md`](09-getting-started-and-user-guide.md)。

---

## 09.6. Run With Docker

### 09.6.1. 拆分端口模式

```bash
docker compose up -d --build
docker compose ps
```

默认服务入口：

- 数据 API：`http://127.0.0.1:19530`；
- 管理 API：`http://127.0.0.1:9091`；
- gRPC：`127.0.0.1:19531`；
- MinIO API：`http://127.0.0.1:9000`；
- MinIO Console：`http://127.0.0.1:9001`。

健康检查：

```bash
curl -fsS http://127.0.0.1:9091/healthz
```

### 09.6.2. 统一 HTTP 模式

```bash
docker compose -f docker-compose.unified.yml up -d --build
curl -fsS http://127.0.0.1:8080/healthz
```

统一模式在一个 HTTP 监听器上注册管理和数据路由，gRPC 仍使用独立端口。

### 09.6.3. 数据持久化

Compose 将 Plasmod 数据目录挂载到 `/data`，MinIO 使用独立 volume。删除容器不会自动删除 volume；
只有显式执行 `docker compose down -v` 才会清除持久化数据。

### 09.6.4. 管理接口认证

正式环境必须设置 `PLASMOD_ADMIN_API_KEY`。请求可使用：

```bash
curl -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:9091/v1/admin/config/effective
```

未设置管理密钥时，当前实现只记录警告，不会自动拒绝管理请求，因此不能把默认 Compose 当作安全配置。

---

## 09.7. Stop, Reset And Cleanup

### 09.7.1. 源码启动

在前台终端按 `Ctrl-C`。`app.RunServers` 会触发 HTTP/gRPC shutdown，并等待运行时关闭。

确认进程已停止：

```bash
pgrep -af 'plasmod|src/cmd/server'
```

需要清空本地数据时，先停止服务，再删除你显式设置的目录：

```bash
rm -rf .andb_data
```

该操作会删除 WAL、canonical records、versions 和 checkpoint，不可恢复。

### 09.7.2. Docker

停止但保留数据：

```bash
docker compose down
```

停止并删除 Compose volumes：

```bash
docker compose down -v
```

不要在服务仍写入时手工删除 Badger 文件。对于生产数据，优先执行备份、快照或受控 purge；参见
[第 12 章的备份与恢复说明](12-deployment-operations-and-troubleshooting.md)。

---

## 09.8. Verify Installation

按以下顺序检查，能区分进程、持久化、物化和检索问题。

### 09.8.1. 服务存活

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

Docker 拆分模式把地址改为 `http://127.0.0.1:9091/healthz`。

### 09.8.2. Event 写入成功

执行 [`first-event-and-query.md`](09-getting-started-and-user-guide.md) 的写入命令。HTTP 非 2xx 时先查看响应体；
严格一致性超时、投影失败和普通校验错误使用不同状态码。

### 09.8.3. 对象可查询

查询响应应至少包含 ID 为 `mem_evt_quickstart_001` 的对象。只得到空向量结果时，确认请求包含
`target_object_ids`，或确认词法索引文本已写入。

### 09.8.4. WAL 和数据目录存在

```bash
test -f .andb_data/wal.log
test -d .andb_data
```

磁盘模式下还会保存一致性 checkpoint 和 Badger 数据。内存模式不会留下可恢复数据。

### 09.8.5. 有效配置

设置管理密钥后：

```bash
curl -fsS -H "X-Admin-Key: $PLASMOD_ADMIN_API_KEY" \
  http://127.0.0.1:8080/v1/admin/config/effective
```

该接口比静态 YAML 更接近当前进程实际配置。

### 09.8.6. 重启后仍可读取

保持 `PLASMOD_DATA_DIR` 不变，正常停止服务并重新执行同一启动命令，然后再次运行 Query 和 Trace。对象、Edge
和 Version 应仍存在，`wal.log` 的 LatestLSN 不应回到 0。若改成 `PLASMOD_STORAGE=memory`，重启后数据消失
是预期行为。

---

## 09.9. Deletion And Purge

删除必须先确认目标是隐藏、逻辑删除、索引移除还是物理擦除。

### 09.9.1. 管理入口

管理 API 提供：

- dataset delete/purge 和 purge task；
- memory 按 source delete/purge；
- 全数据 wipe；
- S3 cold purge；
- rollback 相关操作。

这些路由位于 `/v1/admin/*`，应使用 `PLASMOD_ADMIN_API_KEY` 保护。

### 09.9.2. 语义区别

- Delete：通常使对象不再参与正常查询，但可能保留审计记录；
- Purge：物理移除 canonical、索引或 cold records，范围取决于具体处理器；
- Wipe：开发/灾难恢复操作，清除大范围数据；
- Rollback：回退投影或状态，不等同于删除原始 Event。

### 09.9.3. 操作顺序

1. 先查询目标 scope 和数量；
2. 备份或快照；
3. 设置管理认证并记录请求；
4. 执行 delete；
5. 验证查询不可见；
6. 只有法规或容量要求明确时执行 purge；
7. 验证 object、edge、version、index 和 cold key 的处理结果。

当前没有跨 Badger、S3 和所有检索 backend 的单一分布式事务。大范围 purge 需要按任务状态验证完成度。

---

## 09.10. Ingest Events

### 09.10.1. 目的

`POST /v1/ingest/events` 接收 Dynamic Event v0.4，把一次 Agent runtime 变化记录为可回放事实，
再派生 Memory、State、Artifact、Edge 和 ObjectVersion。

### 09.10.2. 最小输入

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

### 09.10.3. 写入阶段

1. Gateway 解码并校验 Event。
2. Runtime 将 Event 追加到 WAL，得到 LSN。
3. Consistency controller 按 strict、bounded 或 eventual 调度。
4. Materializer 写 canonical objects、edges 和 versions。
5. Retrieval projection 更新可查询表示；可按事件配置跳过向量投影。
6. Tracker 更新 committed/projected/visible 状态并返回结果。

### 09.10.4. 一致性选择

- `strict`：请求等待本次写入达到当前严格可见门槛；
- `bounded`：在 `freshness_sla_ms` 约束内异步推进；
- `eventual`：接受后异步物化和投影。

Event 的 `access.consistency` 优先于服务默认模式。strict 的成功响应不代表下游外部系统也完成，只代表
Plasmod 当前实现定义的 gate 已满足。

### 09.10.5. Embedding 选择

- 已有向量：提供 precomputed embedding 和维度；
- 服务端生成：`retrieval.index_text` 加 `has_embedding=true`；
- 仅 canonical/词法：`index_text` 加 `has_embedding=false`；
- 全局跳过：`PLASMOD_SKIP_VECTOR_INDEX=1`。

预计算向量必须与已配置的检索空间维度和语义一致。Plasmod 不会证明不同模型产生的同维向量可比较。

### 09.10.6. 错误处理

- 校验失败：修正字段，不应无条件重试；
- `503`：写入已接受但不可见，或投影失败；应查询状态并使用相同 `event_id` 谨慎恢复；
- `504`：一致性等待超时；先查对象/trace 再决定是否重放；
- `408`：客户端上下文取消；
- 其他 5xx：检查 WAL、Badger、embedding 和 native retrieval 日志。

当前接口没有通用 Idempotency-Key 协议。`event_id` 是业务幂等和追踪的首要标识，但重复提交是否需要
去重必须由调用方结合 trace/WAL 状态判断。

---

## 09.11. Manage Agent State

### 09.11.1. 数据模型

Canonical 类型为 `schemas.AgentState`，兼容别名为 `State`；对象类型常量是 `agent_state`，`state` 仅作
旧名称兼容。State 由 Agent、state key、value、version 和时间等字段描述。

### 09.11.2. Event 驱动更新

状态变化应写成 `state_update`、`state_change` 或相关 tool result Event，并提供 state key/value。专用 State
materializer 使用：

```text
state_<agent_id>_<state_key>
```

作为稳定 ID，并在进程内跟踪 key 的递增版本。

此外，通用 `materialization.Service` 会为每次 ingest 创建 `ingest_checkpoint` State，ID 为
`state_<session_id>_<event_id>`，值指向本次默认 Memory。`materialization.targets` 当前不是阻止这一默认
checkpoint 的硬开关。

### 09.11.3. 直接状态接口

`/v1/states` 提供 canonical 状态访问。直接 POST 不等同于 Event 驱动更新：它不会自动补齐原始 Event、
WAL 因果链和所有派生关系。

### 09.11.4. 查询最新状态

查询时至少约束 tenant/workspace/agent 和 state key，对返回版本取最大值。不要仅依赖相似度排序。

### 09.11.5. 当前限制

- state version 的部分递增状态保存在 worker 进程内；重启恢复语义应通过持久化 ObjectVersion 和 replay 验证；
- 没有跨进程全局事务锁来保证任意直接 CRUD 更新顺序；
- `state` 与 `agent_state` 名称并存，新增客户端应使用 canonical `agent_state`。

---

## 09.12. Manage Agents And Sessions

### 09.12.1. Agent

Canonical `schemas.Agent` 保存 Agent ID、名称、类型、能力、运行状态、租户和工作空间信息。

HTTP 集合入口为 `/v1/agents`。GET 用于读取或列举，POST 用于直接保存 canonical record。直接 POST
适合注册目录数据；若 Agent 创建本身需要成为可回放业务事实，应同时或优先写入 Event。

### 09.12.2. Session

`schemas.Session` 把一组 Event、Memory、State 和 Artifact 绑定到会话范围。入口为 `/v1/sessions`。

建议：

- Session ID 在上游 runtime 中生成并保持稳定；
- 每次 Event 都携带同一 tenant/workspace/agent/session scope；
- 不要用 Session ID 代替租户隔离；
- 会话结束可以更新状态，但不要立即物理删除其证据链。

### 09.12.3. 查询作用域

`QueryRequest` 可同时使用 `tenant_id`、`workspace_id`、`agent_id` 和 `session_id`。过滤应从最强隔离
边界开始，再缩小到 Agent 和 Session。仅传自然语言 query 不能替代 scope filter。

### 09.12.4. 生命周期边界

Agent/Session canonical CRUD 当前没有统一 ETag、乐观锁或通用分页协议。并发管理者应在应用层维护版本，
或通过 Event + ObjectVersion 建立可审计更新链。

---

## 09.13. Manage Artifacts

Artifact 表示计划、报告、代码修改、工具产物或其他可独立寻址的 Agent 输出，类型定义在
`src/internal/schemas/canonical.go`。

### 09.13.1. 创建

推荐通过 Event 创建：

1. `event.event_type` 使用 artifact/tool result 相关类型；
2. `object.object_type` 设为 `artifact`；
3. `object.object_id` 可提供稳定业务 ID；
4. `materialization.targets` 可声明 `artifact` 和 `object_version` 作为期望元数据；
5. 在 causality 中提供 parents/dependencies；
6. payload 保存内容或外部位置描述。

当前是否创建 Artifact 由 artifact-like object/event、tool event 或 URI/body 等 payload 条件决定，而不是仅由
targets 决定。Artifact materializer 的 `ArtifactIDOrDefault` 会优先使用有效的显式对象 ID；这与默认
Memory ID 规则不同。

### 09.13.2. 直接访问

`/v1/artifacts` 提供 canonical CRUD。适合迁移或管理已有 Artifact，但要由调用方补充 Edge、Version 和
Event 记录，否则 trace 可能只看到孤立对象。

### 09.13.3. 更新

不要原地覆盖后丢失来源。推荐为更新生成新 Event 和 ObjectVersion，并用 `updates`、`derived_from` 或
`supersedes` 语义的 Edge 连接前后对象。

### 09.13.4. 内容存储

Plasmod 可保存结构化 payload 和对象元数据；大文件通常应保存在对象存储中，Artifact 保存 URI、hash、
media type 和 provenance。上传文件生命周期不应只依赖检索索引。

---

## 09.14. Memory Lifecycle

Plasmod 把 Memory 生命周期拆成写入、召回、压缩/总结、衰减、共享、冲突处理、分层和删除。

### 09.14.1. 创建与分类

Event materializer 默认创建 `mem_<event_id>`。Memory type 可表示 episodic、semantic、procedural、social、
reflective、factual、profile、affective state 或 preference/constraint。

### 09.14.2. 算法操作

内部 runtime 路由包括 recall、ingest、compress、summarize、decay、share、conflict resolve 和 stale 标记。
这些 `/v1/internal/memory/*` 接口用于受控集成，当前稳定性为 Experimental，不应直接暴露到不可信网络。

算法配置由 `configs/memory_tiering.yaml` 以及 `configs/algorithm_*.yaml` 读取。算法改变 score、重要性或
生命周期决策，但 canonical Memory/Edge/Event 仍是数据库记录。

### 09.14.3. Tier 转换

- Hot：进程内有限缓存；
- Warm：主要 canonical store 和检索层；
- Cold：显式归档到 S3/MinIO 或内存 cold store。

对象不是写入后自动复制到所有层。归档、warm prebuild 和 purge 都是显式操作。

### 09.14.4. 删除

逻辑删除、按 source/dataset 删除和物理 purge 语义不同。需要审计或 replay 时，应先明确保留 WAL、Edge、
Version 和冷存档的范围。

---

## 09.15. Query Memories

### 09.15.1. 查询入口

- 单请求：`POST /v1/query`；
- Warm Segment 预计算向量批请求：`POST /v1/query/batch`；
- 直接 Memory 访问：`/v1/memory`；
- runtime 内部 recall：`/v1/internal/memory/recall`，稳定性为 Experimental。

### 09.15.2. 主要过滤维度

`schemas.QueryRequest` 支持：

- tenant、workspace、agent、session scope；
- `object_types`、`memory_types`、`edge_types`；
- 时间窗口；
- `target_object_ids`；
- relation constraints；
- dataset/source/batch selectors；
- access、materialization、runtime filters；
- `top_k`、`response_mode`；
- `include_cold`；
- 预计算 query embedding。

这些通用过滤字段只适用于 `/v1/query`。`/v1/query/batch` 使用独立的
`VectorWarmBatchQueryRequest`，直接查询 Warm Segment，不自动组装 canonical Evidence。

### 09.15.3. 执行层次

1. Hot cache 候选；
2. Warm canonical/lexical/vector 候选；
3. 只有显式 `include_cold=true` 时才读取 Cold；
4. 合并、过滤和排序；
5. Evidence assembler 补充 edges、versions、policies、provenance 和 proof trace。

因此，Cold 数据不是每次 query 的隐式兜底。对历史归档有要求的调用方必须明确请求并接受额外 I/O。

### 09.15.4. 响应模式

`QueryResponse` 可包含：

- `objects`、`nodes`；
- `edges`、`versions`、`provenance`；
- `filters`；
- `proof_trace`、`chain_traces`；
- `retrieval_summary`；
- `query_status` 和 `hint`。

客户端必须读取 `query_status`，不能只根据 HTTP 200 假定所有层均参与。生产模式还会删除 debug/raw/log
等内部字段。

### 09.15.5. Latest Memory

“最新”需要确定 scope、memory type 和时间字段。推荐同时给出 Agent/Session、类型和时间窗口，再按返回对象
的版本/时间判断。向量相似度第一名不天然等于最新对象。

---

## 09.16. Query Relations

### 09.16.1. Edge 模型

`schemas.Edge` 是 canonical relation，至少连接 source 和 destination，并携带 edge type、scope、时间和
可选属性。常用类型包括：

- `caused_by`、`derived_from`；
- `supports`、`contradicts`；
- `summarizes`、`updates`；
- `uses_tool`、`tool_produces`；
- `belongs_to_session`、`owned_by_agent`、`shared_with`。

### 09.16.2. 写入

Event 的 causality/parents、relation 类型和 materialization 配置可以生成 Edge。`/v1/edges` 也支持直接
canonical 管理，但直接写 Edge 时调用方必须保证两端对象和 scope 合法。

### 09.16.3. 查询

可以：

- 通过 `/v1/edges` 读取已知关系；
- 在 `/v1/query` 中使用 `edge_types` 和 relation constraints；
- 使用 `/v1/traces/{id}` 请求围绕对象的 evidence graph。

### 09.16.4. 解释结果

关系命中不等于事实正确。`supports` 和 `contradicts` 是系统记录的 Agent runtime 关系，需要结合来源对象、
ObjectVersion、PolicyRecord 和 ProofStep 解释。Evidence assembler 负责结构化返回，不负责替代领域验证。

---

## 09.17. Replay

Replay 从 WAL 中读取 Event，再执行运行时物化和投影，用于重建派生状态或恢复中断后的处理。

### 09.17.1. 前提

- 使用 `PLASMOD_STORAGE=disk`；
- `<dataDir>/wal.log` 可读且未被截断；
- schema 和 materializer 与记录兼容；
- 目标 canonical store 和 retrieval backend 可写；
- 对重放范围有明确 LSN 边界。

### 09.17.2. 入口

管理路由 `/v1/admin/replay` 触发 replay。WAL 还提供内部 stream transport，用于节点间传输，不应当作
面向应用的事件订阅 API。

### 09.17.3. 正确性检查

1. 记录 replay 前 `LatestLSN` 和目标对象状态；
2. 暂停冲突的业务写入或明确并发策略；
3. 重放指定范围；
4. 检查 object 数、Edge、ObjectVersion 和 trace；
5. 检查 consistency checkpoint；
6. 抽查 query 是否返回期望最新版本。

Replay 不能恢复从未写入 WAL 的直接 CRUD 历史，也不能恢复已物理清除且 WAL 不再保留的 payload。

---

## 09.18. Runtime Modes

Plasmod 有多组彼此独立的模式，不能只用一个“生产模式”概括。

### 09.18.1. APP_MODE

- `test`：响应可包含 `_debug` 和内部字段；
- `prod`：visibility middleware 删除 debug、raw、log、chain traces 等字段。

它影响响应暴露面，不自动开启 TLS 或用户认证。

### 09.18.2. Storage Mode

- `disk`：默认，Badger + FileWAL，可恢复；
- `memory`：进程内 store + InMemoryWAL，仅适合测试和临时运行。

### 09.18.3. Consistency Mode

- `strict_visible`/`strict`；
- `bounded_staleness`/`bounded`；
- `eventual_visibility`/`eventual`。

服务默认值可由管理 API 修改，单个 Event 也可覆盖。

### 09.18.4. Governance 与 Memory Provider Mode

管理 API 可以调整 governance mode、runtime mode、memory provider mode，并查询 provider health。这些切换
改变协调和策略行为，不会迁移已有数据格式。

### 09.18.5. Unified 与 Split Server

统一模式共享一个 HTTP 监听器；拆分模式把管理面和数据面分开。二者使用相同核心 runtime，不代表两个
独立数据库实例。

---

## 09.19. Sharing And Visibility

### 09.19.1. Scope

Event 和 Query 都可以携带 tenant、workspace、agent、session 范围。`access.visibility` 表达对象可见级别，
ShareContract 和 PolicyRecord 表达更明确的共享/治理决策。

### 09.19.2. 推荐规则

1. tenant 是最高隔离边界；
2. workspace 表示协作范围；
3. agent/session 进一步缩小上下文；
4. private memory 不应只靠命名约定隔离；
5. shared memory 应创建 ShareContract 或可审计 Edge；
6. 查询必须带 scope，不依赖检索后过滤敏感结果。

### 09.19.3. ShareContract

`schemas.ShareContract` 保存提供者、接收者、对象范围、授权条件和生命周期。入口为
`/v1/share-contracts`。直接写 contract 只建立 canonical contract；应用仍需在查询入口执行相应 policy。

### 09.19.4. 当前安全边界

Plasmod 有 scope filter、policy records 和 admin key，但不是完整 IAM 产品。公开 HTTP 数据路由没有统一
用户认证中间件。生产部署必须由可信网关完成 TLS、身份认证、租户绑定和速率限制。

---

## 09.20. Tiered Storage

### 09.20.1. 三层职责

| 层 | 当前实现 | 用途 |
|---|---|---|
| Hot | `HotObjectCache` | 最近对象的进程内访问，默认容量 2000 |
| Warm | `storage.ObjectStore`、检索索引 | 常规持久化与查询 |
| Cold | S3/MinIO 或内存 cold store | 显式归档和历史读取 |

### 09.20.2. 写入行为

常规 Event 写入进入 Warm，并可能更新 Hot；不会默认同步归档到 Cold。Cold 归档和 snapshot export 由管理
操作触发。

### 09.20.3. 查询行为

Query 默认查 Hot/Warm。只有 `include_cold=true` 时读取 Cold，因此需要历史完整性的业务必须显式设置。

### 09.20.4. S3 Key 空间

Cold backend 在配置 prefix 下区分 memories、embeddings、agents、states、artifacts、edges 和 edge indexes。
不要由外部程序随意改写 key；对象 JSON 和索引 key 的一致性由 Plasmod 维护。

### 09.20.5. 配置

MinIO/S3 endpoint、bucket、credentials、TLS 和 prefix 由环境变量及 storage factory 读取。`configs/storage.yaml`
不能单独证明当前进程已使用 S3，需查看有效配置和日志。

---

## 09.21. Trace And Provenance

### 09.21.1. Trace API

```text
GET /v1/traces/{object_id}
```

该接口围绕目标对象收集 canonical object、Edge、ObjectVersion、PolicyRecord、provenance 和 proof steps。

### 09.21.2. Provenance 与应用日志的区别

- Provenance：对象如何由 Event、父对象和派生关系产生；
- Proof trace：本次 evidence 组装经过哪些可解释步骤；
- Chain trace：查询链路中的可选内部跟踪；
- 应用日志：进程运行事件，不是 canonical database record。

生产模式由 visibility middleware 删除 `debug`、`raw`、`log`、`chain_traces` 等内部字段。不能把测试模式
响应格式直接作为生产客户端契约。

### 09.21.3. 完整追踪的前提

1. 写入从 Event 入口经过 WAL；
2. Event 携带稳定 ID 和父依赖；
3. materialization 创建 ObjectVersion 和 Edge；
4. 删除策略保留必要 tombstone/provenance；
5. scope 允许当前查询者看到关系两端。

直接保存孤立 canonical object 时，Trace API 只能返回已有记录，无法推断不存在的历史。
