# 一致性模型

## 状态定义

- **Accepted**：Event 已 append 到 WAL，并获得 LSN。
- **Canonical visible**：该 LSN 的 object/edge/version projection 成功，Tracker 已标记 visible。
- **Retrieval visible**：对应对象已经能从目标 retrieval path 命中；background flush/index build 可能使它晚于 canonical visible。
- **Evidence visible**：查询所需 edge/version/cache fragment 已可组装。

## 模式

| Mode | 写入返回 | 读取行为 | 适用场景 |
|---|---|---|---|
| `strict_visible` / `strict` | 等待 projection 可见；失败返回 accepted-not-visible 或 projection error | 等待目标 visibility | 下一步决策必须读到本次写入 |
| `bounded_staleness` / `bounded` | 预留 shard slot，在 freshness SLA 内推进 | 最多等待配置的 query timeout | 容忍有界陈旧 |
| `eventual_visibility` / `eventual` | WAL 接受后异步 projection | 不等待最新 LSN | 吞吐优先、可重试读取 |

## 配置

默认模式为 strict。核心变量包括 `PLASMOD_CONSISTENCY_DEFAULT_MODE`、`BOUNDED_MAX_LAG`、`QUEUE_SIZE`、`WORKERS`、重试间隔、query/shutdown timeout、checkpoint path 与 flush interval。

## 并发与顺序

Controller 使用 admission gate、mode gate、append mutex、全局 slots 和按 worker shard queues。bounded 写对同一 shard 只允许一个 reservation，避免 deadline 队列过度订阅。Tracker 只按连续成功 LSN 推进 watermark。

## 模式切换

admin consistency mode 修改 runtime default，不重写已持久 Event 的 resolved mode。切换期间 controller 通过 gate/drain 防止不同模式的 admission 无序穿越。

## 恢复

disk mode 的 checkpoint 默认位于 data dir。首次持久化启动可将 checkpoint bootstrap 到当前 WAL latest，随后从 checkpoint + 1 扫描。已有 checkpoint 时必须重放后续 entry；scan/decode error 终止恢复。

## 限制

该模型约束 Event ingest 主路径；direct canonical CRUD 不自动获得相同 WAL/visibility 保证。跨 S3/native index 的 visibility 也不是全局事务提交点。
