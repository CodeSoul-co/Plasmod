# Trace And Provenance

## Trace API

```text
GET /v1/traces/{object_id}
```

该接口围绕目标对象收集 canonical object、Edge、ObjectVersion、PolicyRecord、provenance 和 proof steps。

## Provenance 与应用日志的区别

- Provenance：对象如何由 Event、父对象和派生关系产生；
- Proof trace：本次 evidence 组装经过哪些可解释步骤；
- Chain trace：查询链路中的可选内部跟踪；
- 应用日志：进程运行事件，不是 canonical database record。

生产模式由 visibility middleware 删除 `debug`、`raw`、`log`、`chain_traces` 等内部字段。不能把测试模式
响应格式直接作为生产客户端契约。

## 完整追踪的前提

1. 写入从 Event 入口经过 WAL；
2. Event 携带稳定 ID 和父依赖；
3. materialization 创建 ObjectVersion 和 Edge；
4. 删除策略保留必要 tombstone/provenance；
5. scope 允许当前查询者看到关系两端。

直接保存孤立 canonical object 时，Trace API 只能返回已有记录，无法推断不存在的历史。
