# Schema Evolution

## Event

Dynamic Event 有独立 `schema_version`。新增字段应 optional；旧 flat aliases 可读取但不作为新输出。重大语义变化
需要新 schema version 和 replay adapter。

## Canonical objects

Go JSON struct、Badger encoded bytes、S3 JSON 和 SDK 都可能持有 schema。新增字段需定义 zero value；删除/重命名
需双读和 migration。

## Constants

Event/object/edge/memory type 字符串会进入 WAL 和持久化数据。改名采用 alias + canonical normalization，不能只改常量。

## Validation

升级测试必须覆盖旧 Event JSON、旧 Badger object、旧 S3 object 和混合版本 replay。
