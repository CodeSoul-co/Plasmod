# Replay

Replay 从 WAL 中读取 Event，再执行运行时物化和投影，用于重建派生状态或恢复中断后的处理。

## 前提

- 使用 `PLASMOD_STORAGE=disk`；
- `<dataDir>/wal.log` 可读且未被截断；
- schema 和 materializer 与记录兼容；
- 目标 canonical store 和 retrieval backend 可写；
- 对重放范围有明确 LSN 边界。

## 入口

管理路由 `/v1/admin/replay` 触发 replay。WAL 还提供内部 stream transport，用于节点间传输，不应当作
面向应用的事件订阅 API。

## 正确性检查

1. 记录 replay 前 `LatestLSN` 和目标对象状态；
2. 暂停冲突的业务写入或明确并发策略；
3. 重放指定范围；
4. 检查 object 数、Edge、ObjectVersion 和 trace；
5. 检查 consistency checkpoint；
6. 抽查 query 是否返回期望最新版本。

Replay 不能恢复从未写入 WAL 的直接 CRUD 历史，也不能恢复已物理清除且 WAL 不再保留的 payload。
