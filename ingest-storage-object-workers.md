# 与 Ingest / Storage / Object 相关的 Worker 与代码位置

> 本文档描述《系统分层架构图》中与 **摄入（Ingest）**、**存储（Storage）**、**规范对象（Object）** 相关的 Worker 角色、当前仓库中的**具体文件路径**，以及**缺口**与**解耦**建议。  
> 说明：本文件已列入 `.gitignore`，默认不进入版本库；若需组内共享可移出 ignore 后提交。

---

## 1. 范围说明

- **Ingest**：从 HTTP 接入到 WAL、物化、写入规范存储与检索投影的整条写路径。  
- **Storage**：`RuntimeStorage` 及各子存储接口的实现（内存 / Badger 等）。  
- **Object**：`Memory` / `State` / `Artifact` 等规范对象及其 `ObjectVersion`、相关 `Edge` 的持久化。

「Worker」在代码里分两类：

1. **`nodes.Manager` 注册的节点**：`DataNode` / `IndexNode` / `QueryNode` 及若干 `*Worker` 接口实现（见 `worker/nodes`）。  
2. **架构文档中的角色、但当前以内联服务存在**：如物化逻辑在 `materialization.Service`，未注册为独立 Worker 类型。

---

## 2. 架构图角色 ↔ 代码（与 Ingest / Storage / Object 相关）

| 架构图角色（执行平面） | 与 I/S/O 的关系 | 代码落点 | 注册 / 调度 |
|------------------------|-----------------|----------|-------------|
| **Ingest Worker** | Ingest 入口、校验、入 WAL | `src/internal/access/gateway.go`（路由与调用 `Runtime.SubmitIngest`）<br>`src/internal/worker/runtime.go` → `SubmitIngest` 前半（WAL） | 无独立 `Ingest` 节点；逻辑内联在 Runtime + Gateway |
| **Object Materialization Worker** | Event → Memory/State/Artifact/Version/Edges | `src/internal/materialization/service.go` → `MaterializeEvent`<br>`src/internal/materialization/pre_compute.go` → `PreComputeService`（证据预计算，依赖物化结果） | 未注册为 `nodes` Worker；由 `SubmitIngest` 直接调用 |
| **Memory Extraction Worker** | 从事件抽取 Memory（文档） | `src/internal/worker/nodes/governance.go` → `InMemoryMemoryExtractionWorker`<br>接口：`src/internal/worker/nodes/contracts.go` → `MemoryExtractionWorker` | `src/internal/app/bootstrap.go` 有 `RegisterMemoryExtraction`；**`SubmitIngest` 未调用** `DispatchMemoryExtraction` |
| **State Materialization Worker** | State 物化 | 同上，合并在 `materialization/service.go` 的 `MaterializeEvent` 内 | 无独立 Worker 类型 |
| **Tool Trace Worker** | 工具调用链追踪 | `src/internal/worker/nodes/contracts.go` 仅有 `NodeTypeToolTrace` 常量 | **无实现、无注册** |
| **Memory Consolidation Worker** | 多源记忆整合 | `src/internal/worker/nodes/governance.go` → `InMemoryMemoryConsolidationWorker` | 已注册；**ingest 主路径未调度** |
| **Graph / Relation Worker** | 边索引、关系写入（文档） | `src/internal/worker/nodes/governance.go` → `InMemoryGraphRelationWorker` → `IndexEdge` | 已注册；**ingest 主路径未走 `IndexEdge`**，边由物化直接 `storage.Edges().PutEdge` |
| **Index Build Worker** | 索引构建 | `src/internal/worker/nodes/inmemory.go` → `InMemoryIndexNode` → `BuildIndex` | `DispatchIngest` 会调用（元数据型占位） |
| **DataNode（数据面节点）** | Segment 元数据与摄入侧记录 | `src/internal/worker/nodes/inmemory.go` → `InMemoryDataNode` → `HandleIngest` | `DispatchIngest` 会调用 |
| **Query Worker** | 查询（不直接写 Object，但读 Storage） | `src/internal/worker/nodes/inmemory.go` → `InMemoryQueryNode` | ingest 无关；`ExecuteQuery` 使用 |

与 **Object** 强相关、但**不是** `nodes` Worker 的协调层：

| 组件 | 路径 | 说明 |
|------|------|------|
| ObjectCoordinator | `src/internal/coordinator/object_coordinator.go` | 对象 CRUD 协调（管理 API 等路径） |
| 规范对象定义 | `src/internal/schemas/canonical.go` | `Memory` / `State` / `Artifact` / `Edge` / `ObjectVersion` 等 |

---

## 3. Ingest 主路径上的调用顺序（便于对照文件）

以下顺序在 `src/internal/worker/runtime.go` 的 `SubmitIngest` 中**同步**执行：

1. **`eventbackbone.WAL.Append`** — WAL 实现见 `src/internal/eventbackbone/`（如 `wal.go`）。  
2. **`materialization.Service.MaterializeEvent`** — `src/internal/materialization/service.go`。  
3. **规范对象与版本写入 `RuntimeStorage`** — 通过 `r.storage.Objects()` / `Versions()` / `Edges()`，接口定义见 `src/internal/storage/contracts.go`。  
4. **`PreComputeService.Compute`** — `src/internal/materialization/pre_compute.go`；高显著度写入 `HotCache()`。  
5. **`nodes.Manager.DispatchIngest`** — `src/internal/worker/nodes/manager.go`：依次 `DataNode.HandleIngest`、`IndexNode.BuildIndex`。  
6. **`dataplane.DataPlane.Ingest`** — 默认 `src/internal/dataplane/tiered_adapter.go` → `TieredDataPlane.Ingest`；亦可对照 `segment_adapter.go`。

存储构建入口：**`src/internal/app/bootstrap.go`** → `storage.BuildRuntimeFromEnv()`（`src/internal/storage/factory.go`），实现分布在 `memory.go`、`badger_stores.go`、`tiered.go` 等。

---

## 4. 可能缺少或偏薄的部分

| 项 | 说明 |
|----|------|
| **独立 Ingest Worker** | 文档中的 Ingest Worker 与代码中 Gateway+Runtime 内联不对齐；缺少可单独伸缩、重试、背压的摄入进程边界。 |
| **Tool Trace Worker** | 工具类 `event` 的可追溯写入无专门 Worker/存储叙事与实现。 |
| **异步 WAL 消费链** | 文档中的「WAL Stream → 多组 Worker」在 v1 中多为同步单路径，缺少消费者与队列语义。 |
| **Graph Worker 与物化双写** | 边既可由物化直写 `Edges()`，又可由 `GraphRelationWorker.IndexEdge` 写，**缺少唯一真理源**，易产生拓扑与行为不一致。 |
| **Memory Extraction 与物化** | 物化已写 `Memory` 时，若再调度 `MemoryExtractionWorker`，易**重复或 ID 冲突**；职责未在文档与代码层钉死。 |
| **事务/一致性边界** | WAL 成功但后续存储或 `plane.Ingest` 失败时的补偿、幂等、部分成功状态未显式建模。 |
| **SegmentSeal / Compaction / Replay** | 与长期存储生命周期相关，见 `arch-design.zh-CN.md` §7；当前未接入主 ingest 链路。 |

---

## 5. 解耦建议（更安全、更易改）

1. **单一写边策略**  
   - 方案 A：物化只输出「边意图」结构体，**仅**由 `GraphRelationWorker`（或单一 `EdgeWriter` 模块）调用 `PutEdge`。  
   - 方案 B：取消 ingest 路径上的 Graph Worker 注册，文档明确「边由物化服务写入」。  
   二者择一，避免双路径。

2. **拆分 `SubmitIngest` 为可测试阶段**  
   将「WAL」「物化+持久化」「预计算」「数据面 Ingest」「节点通知」拆为显式步骤或小接口（即使仍同步调用），便于单测与后续改为异步 Worker。

3. **明确 Memory 主写入者**  
   在 `MaterializeEvent` 与 `MemoryExtractionWorker` 之间只保留一条**主写路径**；另一条改为异步增强或删除未使用注册。

4. **接入 `WorkerScheduler`（可选）**  
   `src/internal/coordinator/worker_scheduler.go` 已有按类型的 `Dispatch`/`Stats`，可在关键 ingest 步骤打点，与架构图「Scheduler → 执行平面」一致，并便于观测。

5. **存储访问收口**  
   物化与 Worker 仅通过 `RuntimeStorage`（或更窄的 `IngestWritePort` 接口）写库，避免包之间直接依赖具体 Badger 实现，改动后端时影响面更小。

6. **失败策略**  
   为 `plane.Ingest` 与 `storage.Put*` 定义：是否回滚 WAL 指针、是否标记事件为 `needs_retry`、是否仅 ACK 已持久化事件等，减少「半写入」不可见状态。

---

## 6. 快速路径索引（文件列表）

| 主题 | 路径 |
|------|------|
| HTTP Ingest | `src/internal/access/gateway.go` |
| 运行时编排 | `src/internal/worker/runtime.go` |
| 物化 | `src/internal/materialization/service.go` |
| 预计算 / HotCache | `src/internal/materialization/pre_compute.go` |
| Worker 接口与类型常量 | `src/internal/worker/nodes/contracts.go` |
| 调度与拓扑 | `src/internal/worker/nodes/manager.go` |
| Data / Index / Query 内存实现 | `src/internal/worker/nodes/inmemory.go` |
| Memory / Graph / Proof 等 | `src/internal/worker/nodes/governance.go` |
| 组装与注册 | `src/internal/app/bootstrap.go` |
| 存储接口 | `src/internal/storage/contracts.go` |
| 存储工厂 / 后端 | `src/internal/storage/factory.go`、`memory.go`、`badger_stores.go`、`tiered.go` |
| 数据面 Ingest | `src/internal/dataplane/tiered_adapter.go`、`segment_adapter.go` |
| 对象协调 | `src/internal/coordinator/object_coordinator.go` |
| Worker 类型统计（协调器侧） | `src/internal/coordinator/worker_scheduler.go` |

---

*文档生成说明：与根目录 `系统分层架构图.md` 及当前 `main` 实现对照整理；实现变更后请同步更新本文。*
