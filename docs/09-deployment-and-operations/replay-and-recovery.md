# Replay And Recovery

## Failure classes

- 进程中断：从持久 store/checkpoint/WAL 继续；
- canonical projection 不完整：replay Event range；
- retrieval index 丢失：从 canonical/embedding 重建；
- cold store 不可用：Hot/Warm 可继续，显式 cold query 降级/失败；
- WAL 损坏：停止自动恢复，使用备份并确定最后可靠 LSN。

## Recovery order

1. 阻止新写入；
2. 备份故障现场；
3. 检查 disk、Badger、WAL 和 checkpoint；
4. 启动依赖；
5. 以兼容 binary 打开 store；
6. replay 必需范围；
7. rebuild retrieval；
8. 验证 strict write、latest state、trace 和 cold query；
9. 恢复流量。

直接 CRUD 历史没有 Event 时只能依靠 canonical backup，不能通过 WAL replay 恢复。
