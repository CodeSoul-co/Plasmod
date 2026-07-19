# Idempotency

## 当前结论

公共 API 没有统一 `Idempotency-Key` header、幂等记录表或 exactly-once 提交承诺。

## Event 写入

`identity.event_id` 应由调用者稳定生成，是追踪和应用层去重的主键。网络超时后：

1. 查询预期 canonical object 或 trace；
2. 检查服务日志/metrics 和 WAL 状态；
3. 只有确认未处理或 materialization 可安全重入时才重发；
4. 重发使用同一 event ID，不生成新的逻辑事件。

WAL append、projection 和直接 CRUD 的重复行为不同，不能从一个 route 的幂等性外推到所有 route。

## 管理操作

reindex、export、purge 和 replay 可能产生任务或重复扫描。调用者应保存 task ID、范围和 checkpoint，并在
重试前查询状态。

## 扩展要求

新增写接口应明确：幂等键、重复检测范围、结果缓存时间、并发重复的处理方式以及 WAL/事务边界。
