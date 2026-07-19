# Add A Storage Backend

新 backend 必须实现所需 `RuntimeStorage` 子接口，并说明：

- object/edge/version transaction；
- key/order/list semantics；
- durability 和 fsync；
- concurrent readers/writers；
- backup/restore；
- delete/purge；
- error classes；
- shutdown；
- schema migration。

在 `storage/factory.go` 添加显式 mode 和 config snapshot。不要根据 DSN 内容静默猜 backend。

Contract tests 应同时对 memory、Badger 和新 backend 运行，确保空列表、not found、覆盖和事务行为一致。
