# storage

`storage/factory.go` 选择 disk 或 memory runtime bundle。Disk 模式组合 Badger canonical stores、FileWAL、
checkpoint 和可选 S3 cold store。

`contracts.go` 定义 RuntimeStorage；`badger_stores.go` 实现 prefix/key codec；`tiered.go` 组合 Hot/Warm/Cold；
`s3store.go` 管理 cold objects 和 edge indexes。

Object、Edge、Version 使用同一 Badger backend 时可通过 canonical projection transaction 原子提交。跨 S3 或
native index 不在同一事务中。
