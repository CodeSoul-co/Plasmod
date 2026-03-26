# Member TODO Distribution

## Overview

本文档将 Worker 框架中未完成的任务分发给各成员。
所有 Worker 实现均已完成，剩余工作为集成改进和文档。

---

## member-D: API / Worker / Integration

### ✅ DONE — Dead-letter channel in EventSubscriber

**文件:** `src/internal/worker/subscriber.go`

- `DeadLetterEntry` struct (`entry`, `panicValue`, `timestamp`)
- `deadLetter chan DeadLetterEntry` field on `EventSubscriber`
- `DeadLetterChannel() <-chan DeadLetterEntry` public method
- `DLQStats()` method returning `{PanicCount int, TotalProcessed int64}`
- `TestEventSubscriber_DeadLetter_RecoveredPanic` ✅
- `TestEventSubscriber_DeadLetter_DLQFull_DoesNotBlock` ✅

---

### 📋 MicroBatch 持久化 drain target

**文件:** `src/internal/worker/coordination/microbatch.go`

**问题:** `Flush()` 当前只清空内存队列，payload 未持久化。生产环境需要将 flushed payload 转发到 coordinator 或 DLQ。

**建议方案:**
- 选项 A: 添加 `SetDrainTarget(func(payload map[string]any))` 接口，将每次 flush 的 payload 转发到指定 handler
- 选项 B: 将 MicroBatch 与 WAL 日志联动，flush 时写一条 `microbatch_flush` 事件到 DerivationLog

**验证:**
```bash
go test ./src/internal/worker/coordination -v -run "MicroBatch" -timeout 30s
```

---

## member-E: Testing / Algorithm Verification

### ✅ DONE — 4 个 Chain 完整测试

**文件:** `src/internal/worker/chain/chain_test.go`

新增 18 个测试用例覆盖 4 个 chain（MainChain 5个、MemoryPipelineChain 5个、QueryChain 5个、CollaborationChain 5个）。

**验证:**
```bash
go test ./src/internal/worker/chain -v -timeout 60s
```

---

### ✅ DONE — StateCheckpoint 流程测试

**文件:** `src/internal/worker/runtime_test.go`

新增 `TestRuntime_StateCheckpoint_Flow`:
1. ingest 两个 `state_update` events
2. 调用 `DispatchStateCheckpoint`
3. 验证 `Versions().GetVersions(stateID)` 包含 `SnapshotTag` 前缀为 `checkpoint_`

**验证:**
```bash
go test ./src/internal/worker -v -run "TestRuntime_StateCheckpoint" -timeout 30s
```

---

### ✅ DONE — CommunicationWorker E2E 测试

**文件:** `src/internal/worker/coordination/coordination_test.go`

新增 `TestCommunicationWorker_Broadcast_EndToEnd`:
- 通过 `mgr.DispatchCommunication` 而非直接调用 `Broadcast`
- 验证 `agentB` 空间出现带 `ProvenanceRef` 引用 `agentA` 的共享 memory

**验证:**
```bash
go test ./src/internal/worker/coordination -v -run "TestCommunicationWorker_Broadcast_EndToEnd" -timeout 30s
```

---

## member-A: Event / Object Materialization

### 📋 文档: ObjectMaterialization 路由表

**新建文件:** `devdocs/object-materialization-routing.md`

**内容要求:**
- 每个 `EventType` →  canonical object 的映射表
- 路由路径: ObjectMaterializationWorker vs ToolTraceWorker 的区别
- 示例 payload 格式

**模板:**
```markdown
# ObjectMaterialization Routing Table

| EventType | Target Object | Worker | Payload Keys |
|-----------|--------------|--------|-------------|
| agent_thought | Memory (Level-0) | ObjectMaterializationWorker | text |
| user_message | Memory (Level-0) | ObjectMaterializationWorker | text |
| state_update | State | StateMaterializationWorker | state_key, state_value |
| state_change | State | StateMaterializationWorker | state_key, state_value |
| checkpoint | State (snapshot) | StateMaterializationWorker | — |
| tool_call | Artifact | ObjectMaterializationWorker + ToolTraceWorker | tool_name, tool_args |
| tool_result | Artifact | ToolTraceWorker | tool_name, tool_result |
```

---

## member-B: Retrieval / Indexing

### 📋 文档: IndexBuildWorker 集成状态

**新建文件:** `devdocs/index-build-worker-status.md`

**内容要求:**
- `IndexBuildWorker.BuildIndex` 在 `Runtime.SubmitIngest` 中被调用
- 当前 in-memory segment index 未接入 `ExecuteQuery` 检索路径（查询走 `TieredDataPlane`）
- 说明 `IndexBuildWorker` 是作为 segment 元数据 tracker 还是独立二级索引
- 明确是否需要接入检索路径

---

## member-C: Graph / Relation

### 📋 文档: GraphRelationWorker dual-write 澄清

**新建文件:** `devdocs/graph-relation-dual-write.md`

**问题:** `ingest-storage-object-workers.md` 提出 edges 存在双重写入：
1. `materialization.Service` 通过 `storage.Edges().PutEdge`
2. `GraphRelationWorker.IndexEdge` 在 MainChain 中调用

**建议方案（选一）:**
- 方案 A: Materialization 只发"edge intent"事件，由 GraphRelationWorker 消费并写入
- 方案 B: 移除 GraphRelationWorker 从 ingest 路径，edges 单一来源为 Materialization

---

## All Members: Integration Test Scripts

### ✅ DONE — Go HTTP E2E 集成测试

**新增文件:**
- `integration_tests/chain_main_test.go` — MainChain HTTP 端到端（5 个测试）
- `integration_tests/chain_query_test.go` — QueryChain HTTP 端到端（4 个测试）
- `integration_tests/chain_collab_test.go` — CollaborationChain HTTP 端到端（4 个测试）

**验证:**
```bash
go test ./integration_tests/... -v -run "Chain" -timeout 60s
```

### ✅ DONE — Python SDK 集成测试

**新增文件:**
- `integration_tests/python/test_chain_main.py`
- `integration_tests/python/test_chain_query.py`
- `integration_tests/python/test_chain_collab.py`

**验证:**
```bash
python integration_tests/python/run_all.py
```

---

## Quick Reference: Run All Tests

```bash
# Worker 单元测试
go test ./src/internal/worker/... -count=1 -timeout 60s

# HTTP 集成测试（需要 server 运行: make dev）
make integration-test

# Python SDK 测试
python integration_tests/python/run_all.py
```
