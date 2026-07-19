# Badger Integration

Badger 是 `PLASMOD_STORAGE=disk` 的默认 canonical backend。

## Stored records

Agent、Session、Event、Memory、State、Artifact、User、Edge、Version、Policy、ShareContract、segment/index metadata
及部分 algorithm/audit records。

## Transaction boundary

同一 DB 中的 canonical projection 可以原子写 object、edge、version。Native index、FileWAL 和 S3 不在 Badger
transaction 内，由 runtime/consistency controller 协调。

## Operational rules

- 一个数据目录只由一个兼容 Plasmod 进程写；
- 不手工编辑 `.sst`/value log；
- 备份前协调写入或使用受支持 snapshot；
- 关注磁盘空间和 value log GC；
- schema/key prefix 升级前备份并验证迁移。
