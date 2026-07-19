# Schema And Payload Extension

优先级：

1. 业务非索引字段放 `payload`；
2. 可选扩展标签/字段放 `extensions`；
3. 只有跨功能稳定语义才提升为 Event/canonical 顶层字段；
4. 新 canonical object 必须有持久化和生命周期。

新增字段应 optional、有明确默认值，并验证旧 WAL JSON 能读取。不要复用旧字段表达不同语义。

若字段参与 scope、policy、ID、排序或持久化 key，则不能只作为自由 payload；需要 typed schema 和全链路测试。
