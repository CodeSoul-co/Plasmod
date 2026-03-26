# Worker 类型总览（14 类说明与对照）

仓库里「14 个 worker」出现在**不同文档/代码**时，含义略有重叠但不完全相同。本文汇总三处来源，便于对照。

---

## 一、《系统分层架构图》— 执行平面 14 个「Worker 角色」

来源：`系统分层架构图.md` 第三节「执行平面」。

按功能组分共 **14** 个命名角色（设计叙事用）：

### 3.1 摄入与物化组（5）

| # | 角色 |
|---|------|
| 1 | Ingest Worker（摄入工作器） |
| 2 | Object Materialization Worker（对象物化工作器） |
| 3 | Memory Extraction Worker（内存提取工作器） |
| 4 | State Materialization Worker（状态物化工作器） |
| 5 | Tool Trace Worker（工具追踪工作器） |

### 3.2 内存与治理组（4）

| # | 角色 |
|---|------|
| 6 | Memory Consolidation Worker（内存整合工作器） |
| 7 | Reflection / Policy Worker（反思/策略工作器） |
| 8 | Communication Worker（通信工作器） |
| 9 | Conflict & Merge Worker（冲突与合并工作器） |

### 3.3 检索与推理组（5）

| # | 角色 |
|---|------|
| 10 | Index Build Worker（索引构建工作器） |
| 11 | Graph / Relation Worker（图/关系工作器） |
| 12 | Proof Trace Worker（证明追踪工作器） |
| 13 | Query Worker（查询工作器） |
| 14 | Micro-batch Scheduler（微批调度器） |

---

## 二、规格文档 `docs/arch-design.md` §7 — 14 类 **Worker Node Contracts**

来源：`docs/arch-design.md`「## 7. Worker Node Contracts (14 types)」，注明 *Per spec section 16.4*。

这是**与实现/接口表对齐**的一套编号（ID 1–14）：

| ID | Type | 职责摘要 |
|----|------|----------|
| 1 | **DataNode** | ingest, flush, metadata |
| 2 | **IndexNode** | build, update, metadata |
| 3 | **QueryNode** | search, metadata |
| 4 | **MemoryExtractionWorker** | 从事件中抽取 memory |
| 5 | **MemoryConsolidationWorker** | 多层级 memory 合并/蒸馏 |
| 6 | **ReflectionPolicyWorker** | 反思策略 |
| 7 | **ConflictMergeWorker** | 多智能体冲突合并 |
| 8 | **MaterializationWorker** | 事件 → ingest 记录/物化 |
| 9 | **EmbeddingWorker** | 向量嵌入计算 |
| 10 | **GraphRelationWorker** | 边提取与持久化 |
| 11 | **ProofTraceWorker** | 证明轨迹组装 |
| 12 | **SegmentSealWorker** | growing shard 封段 |
| 13 | **CompactionWorker** | 冷层 compaction |
| 14 | **ReplayWorker** | 自某 LSN 起 WAL 回放 |

> 说明：这一套与「架构图」里的 14 个**名字不完全一一对应**（例如规格里单独列出 Data/Index/Query、Embedding、SegmentSeal、Compaction、Replay，而架构图用「Ingest / Index Build / Query / Micro-batch」等叙事名）。

---

## 三、代码中的 `NodeType` 常量

来源：`src/internal/worker/nodes/contracts.go`

这里用字符串区分**节点类型**（用于 `NodeInfo.Type` 等），共 **3 个数据面节点类型 + 13 个 worker 风格类型**（与 §7 的 14 行表**不是同一套枚举**，有命名差异）：

**数据面（3）**

- `data_node` / `index_node` / `query_node`

**Worker 风格（13）**

| 常量名 | 字符串值 |
|--------|-----------|
| `NodeTypeIngest` | `ingest_worker` |
| `NodeTypeObjectMaterialization` | `object_materialization_worker` |
| `NodeTypeMemoryExtraction` | `memory_extraction_worker` |
| `NodeTypeStateMaterialization` | `state_materialization_worker` |
| `NodeTypeToolTrace` | `tool_trace_worker` |
| `NodeTypeMemoryConsolidation` | `memory_consolidation_worker` |
| `NodeTypeReflectionPolicy` | `reflection_policy_worker` |
| `NodeTypeCommunication` | `communication_worker` |
| `NodeTypeConflictMerge` | `conflict_merge_worker` |
| `NodeTypeIndexBuild` | `index_build_worker` |
| `NodeTypeGraphRelation` | `graph_relation_worker` |
| `NodeTypeProofTrace` | `proof_trace_worker` |
| `NodeTypeMicroBatch` | `micro_batch_scheduler` |

接口实现见同目录 `contracts.go`（`DataNode` / `IndexNode` / `QueryNode` 与各 `*Worker` interface）。

---

## 四、与当前运行时的大致对应（便于自查）

| 规格 §7 类型 | 当前 v1 原型中的常见落点 |
|--------------|-------------------------|
| DataNode / IndexNode / QueryNode | `InMemoryDataNode` / `InMemoryIndexNode` / `InMemoryQueryNode`，在 `app/bootstrap.go` 注册到 `nodes.Manager` |
| MaterializationWorker | 未单独 Worker；逻辑在 `materialization.Service.MaterializeEvent`，由 `worker.Runtime.SubmitIngest` 直接调用 |
| MemoryExtraction / Consolidation / GraphRelation / ProofTrace | 有 `InMemory*` 实现类并可注册；与 ingest 主路径的**完整调度**仍在演进中 |
| Embedding / SegmentSeal / Compaction / Replay 等 | 多为占位或后续分布式形态 |

更细的实现状态以代码与 `README.md` 为准。

---

## 五、相关文件索引

| 说明 | 路径 |
|------|------|
| 架构图 14 角色 | `系统分层架构图.md` §3 |
| 规格 14 类表 | `docs/arch-design.md` §7 |
| NodeType 与接口 | `src/internal/worker/nodes/contracts.go` |
| 节点注册与拓扑 | `src/internal/worker/nodes/manager.go` |
| 组装入口 | `src/internal/app/bootstrap.go` |

---

*文档用于组内对齐；若与飞书最新规格冲突，以飞书与 `arch-design.md` 更新为准。*
