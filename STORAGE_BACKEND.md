# 存储后端方案（Memory / Badger / Hybrid）

本文档描述 **CogDB / ANDB** 运行时存储的设计决议与落地计划，与代码实现同步演进。  
**状态**：已确认方向；实现进行中。

---

## 1. 目标

| 项目 | 决议 |
|------|------|
| 内存实现 | 保留现有 `MemoryRuntimeStorage`，作为默认与测试基线 |
| 磁盘实现 | **Badger**（嵌入式 KV，单进程内持久化） |
| 切换粒度 | **进程级**：由启动时环境变量决定；业务 HTTP 路径不区分「写内存 / 写盘」 |
| 混合模式名 | **`hybrid`**：同一进程内可按**子存储**分别选择 memory 或 disk |
| WAL | **第一版不做** 落盘；不保证崩溃前未物化事件的恢复 |
| 可观测 | 新增 **`GET /v1/admin/storage`**（不新增业务资源路由），返回当前解析后的存储配置 |
| 测试 | **双 backend** 单测：`memory`、Badger 临时目录、以及 `hybrid` 组合场景 |

---

## 2. 架构：`RuntimeStorage` + 组合式后端

对外仍只暴露一个 **`storage.RuntimeStorage`** 接口（`bootstrap`、worker、gateway 注入方式不变）。

对内采用 **Composite（组合）RuntimeStorage**：

- `Segments()`、`Indexes()`、`Objects()`、`Edges()`、`Versions()`、`Policies()`、`Contracts()` 各自返回 **Memory*** 或 **Badger*** 实现之一。
- 由**配置**决定每个子接口使用哪类后端，**不在代码里写死**「谁必须上盘」。

### 2.1 七类子存储（须全部提供 Badger 实现）

与 `contracts.go` / `memory.go` 中的划分一致，以下 **7 类**均需提供可插拔实现（Memory 已有，Badger 待实现）：

| 序号 | `RuntimeStorage` 方法 | 接口类型 | 说明 |
|------|------------------------|----------|------|
| 1 | `Segments()` | `SegmentStore` | 检索 segment 元数据 |
| 2 | `Indexes()` | `IndexStore` | 索引元数据 |
| 3 | `Objects()` | `ObjectStore` | Agent / Session / Memory / State / Artifact / User 等规范对象 |
| 4 | `Edges()` | `GraphEdgeStore` | 对象间边 |
| 5 | `Versions()` | `SnapshotVersionStore` | 对象版本快照 |
| 6 | `Policies()` | `PolicyStore` | 治理策略（追加型） |
| 7 | `Contracts()` | `ShareContractStore` | 共享合约 |

### 2.2 `HotCache`（不参与「七类」二选一）

- `HotCache()` 返回 `*HotObjectCache`，语义为**热路径内存缓存**。
- **始终为进程内内存**；不参与 `memory`/`disk`/`hybrid` 的 Badger 路由（若未来有特殊需求再单独扩展）。

---

## 3. 环境变量

### 3.0 可选：纯内存 Badger（测试 / 磁盘受限环境）

| 变量 | 说明 |
|------|------|
| `ANDB_BADGER_INMEMORY` | 设为 `true` 时，若需要打开 Badger，则使用 **内存表**（无磁盘 mmap），`data_dir` 在快照中记为 `:memory:` |

用于 CI、沙箱或本地磁盘空间不足时；生产环境一般不设。

### 3.1 全局模式：`ANDB_STORAGE`

| 取值 | 行为 |
|------|------|
| `memory`（默认） | 全部 7 类子存储使用内存实现 |
| `disk` | 在 `ANDB_DATA_DIR` 打开 Badger，全部 7 类子存储使用 Badger（`HotCache` 仍为内存） |
| `hybrid` | 按 **3.2** 逐项覆盖；未指定的项继承全局默认（建议默认规则见下） |

若未设置 `ANDB_STORAGE`，视为 **`memory`**。

### 3.2 数据目录：`ANDB_DATA_DIR`

- Badger 数据根路径。
- **默认**：`.andb_data`（相对进程工作目录）。
- 仅当至少有一个子存储选择 `disk` 时需要有效目录；应支持自动创建目录、单进程锁/单实例约定（实现时定义）。

### 3.3 混合模式逐项覆盖：`ANDB_STORE_*`

在 `ANDB_STORAGE=hybrid` 时使用；每项取值：`memory` | `disk`。

建议命名（实现时可与代码常量对齐）：

| 环境变量 | 对应子存储 |
|----------|------------|
| `ANDB_STORE_SEGMENTS` | `SegmentStore` |
| `ANDB_STORE_INDEXES` | `IndexStore` |
| `ANDB_STORE_OBJECTS` | `ObjectStore` |
| `ANDB_STORE_EDGES` | `GraphEdgeStore` |
| `ANDB_STORE_VERSIONS` | `SnapshotVersionStore` |
| `ANDB_STORE_POLICIES` | `PolicyStore` |
| `ANDB_STORE_CONTRACTS` | `ShareContractStore` |

**默认继承规则（建议）**：

- `hybrid` 下若某项未设置：可继承 `ANDB_STORAGE` 在切到 hybrid 前的隐含默认，或统一默认为 `memory`（以最终实现为准，并在 `/v1/admin/storage` 中体现**解析结果**）。

---

## 4. Badger 实现要点（计划）

- **依赖**：`go.mod` 增加 Badger 官方模块，版本与 Go 版本兼容。
- **键空间**：按子存储类型加前缀（如 `obj/`、`edge/`、`ver/`…），避免冲突。
- **值编码**：对 `schemas` 类型使用 JSON 或等价序列化，便于调试与演进；后续可加版本号/迁移策略。
- **生命周期**：进程退出时 `Close()`/`Sync()`；测试使用 `t.TempDir()`。
- **并发**：与现有 Memory 实现一样，遵守接口线程安全约定。

---

## 5. 管理端点：`GET /v1/admin/storage`

- **不新增**业务对象的 REST 路由；仅运维/调试用途。
- 响应建议为 JSON，包含例如：
  - `mode`：`memory` | `disk` | `hybrid`
  - `data_dir`：解析后的 Badger 路径（若适用）
  - `stores`：各子存储解析后的 `memory` / `disk`
  - `wal_persistence`：`false`（第一版固定说明）

文档同步更新：`docs/api/admin.md`。

---

## 6. 测试策略

- **Memory**：现有单测延续。
- **Badger**：临时目录建库，对 7 类 store 分别做 Put/Get/List 等与 Memory 行为一致的用例（按接口覆盖）。
- **Hybrid**：至少一例交叉组合（例如 `OBJECTS=disk` + `EDGES=memory`），验证路由独立。
- CI：`go test ./...`，无需真实磁盘路径。

---

## 7. 文档与仓库卫生

- 根目录本文档 **`STORAGE_BACKEND.md`** 为设计源；`README.md` 可添加简短链接。
- **`.gitignore`**：加入 `.andb_data/`（若尚未忽略），避免本地数据目录入库。

---

## 8. 明确不在第一版范围

- **WAL / binlog 落盘**与 **replay**。
- **按 HTTP 请求**动态选择 backend（仍可通过多进程多配置实现部署级隔离）。

---

## 9. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-03-19 | 初稿：Badger、进程级切换、`hybrid` 命名、7 类子存储全量实现、admin storage、双 backend 测试 |

---

## 10. 代码锚点（随实现更新）

| 组件 | 路径（当前） |
|------|----------------|
| 接口定义 | `src/internal/storage/contracts.go` |
| 内存实现 | `src/internal/storage/memory.go` |
| Badger 实现 | `src/internal/storage/badger_helpers.go`, `badger_stores.go` |
| 组合 + 工厂 | `src/internal/storage/composite.go`, `factory.go`, `config_snapshot.go` |
| 启动组装 | `src/internal/app/bootstrap.go`（`storage.BuildRuntimeFromEnv`） |
| 网关 / Admin | `src/internal/access/gateway.go`（含 `GET /v1/admin/storage`） |
