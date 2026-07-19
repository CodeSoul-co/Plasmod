# 并发模型

| 区域 | 机制 | 目的 |
|---|---|---|
| Gateway writes | bounded channel semaphore | 限制并发写，过载立即 503 |
| WAL | implementation mutex/file append | 分配单调 LSN，保护记录 |
| Consistency admission | RW gates + append mutex | 模式切换与 append 顺序 |
| Projection | worker-sharded queues + global slots | 有界并发与 backpressure |
| Bounded mode | per-shard reservation | 防止同 shard SLA 过度排队 |
| Tracker/checkpoint | mutex + buffered checkpoint | 连续 watermark 与合并持久化 |
| EventSubscriber | polling goroutine + drain mutex | 按 visible LSN 串行 drain |
| Node manager | RWMutex | worker 注册与 dispatch |
| Orchestrator | 四级 priority channels | 有界 worker pool 调度 |
| Hot cache | RWMutex | cache get/put/eviction |
| Badger | transactions | concurrent KV read/write |
| Segment/index | package-level locks | build/search/unload 协调 |
| Shutdown | context cancellation + WaitGroup | 停止 admission、drain worker、关闭资源 |

## 关键不变量

- 不在持有模式切换 write gate 时执行无界外部请求。
- bounded deadline 从 WAL accepted time 计算，而不是从排队前计算。
- projection worker 失败不能推进 Tracker。
- subscriber 不能读取高于 controller visible watermark 的 entry。
- panic 必须被隔离并进入 dead-letter 路径，不能杀死 drain loop。

## Background goroutine 所有权

Gateway 拥有 hard-delete manager；Runtime 拥有 flush loop、subscriber、consistency lifecycle 与部分 outbox；ServerBundle.Shutdown 负责按依赖顺序停止它们并关闭 Badger/WAL/derivation store。新增 goroutine 必须明确 owner、cancel source、drain policy 和 shutdown timeout。
