# Deletion And Purge

删除必须先确认目标是隐藏、逻辑删除、索引移除还是物理擦除。

## 管理入口

管理 API 提供：

- dataset delete/purge 和 purge task；
- memory 按 source delete/purge；
- 全数据 wipe；
- S3 cold purge；
- rollback 相关操作。

这些路由位于 `/v1/admin/*`，应使用 `PLASMOD_ADMIN_API_KEY` 保护。

## 语义区别

- Delete：通常使对象不再参与正常查询，但可能保留审计记录；
- Purge：物理移除 canonical、索引或 cold records，范围取决于具体处理器；
- Wipe：开发/灾难恢复操作，清除大范围数据；
- Rollback：回退投影或状态，不等同于删除原始 Event。

## 操作顺序

1. 先查询目标 scope 和数量；
2. 备份或快照；
3. 设置管理认证并记录请求；
4. 执行 delete；
5. 验证查询不可见；
6. 只有法规或容量要求明确时执行 purge；
7. 验证 object、edge、version、index 和 cold key 的处理结果。

当前没有跨 Badger、S3 和所有检索 backend 的单一分布式事务。大范围 purge 需要按任务状态验证完成度。
