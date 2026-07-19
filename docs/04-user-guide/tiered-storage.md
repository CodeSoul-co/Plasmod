# Tiered Storage

## 三层职责

| 层 | 当前实现 | 用途 |
|---|---|---|
| Hot | `HotObjectCache` | 最近对象的进程内访问，默认容量 2000 |
| Warm | `storage.ObjectStore`、检索索引 | 常规持久化与查询 |
| Cold | S3/MinIO 或内存 cold store | 显式归档和历史读取 |

## 写入行为

常规 Event 写入进入 Warm，并可能更新 Hot；不会默认同步归档到 Cold。Cold 归档和 snapshot export 由管理
操作触发。

## 查询行为

Query 默认查 Hot/Warm。只有 `include_cold=true` 时读取 Cold，因此需要历史完整性的业务必须显式设置。

## S3 Key 空间

Cold backend 在配置 prefix 下区分 memories、embeddings、agents、states、artifacts、edges 和 edge indexes。
不要由外部程序随意改写 key；对象 JSON 和索引 key 的一致性由 Plasmod 维护。

## 配置

MinIO/S3 endpoint、bucket、credentials、TLS 和 prefix 由环境变量及 storage factory 读取。`configs/storage.yaml`
不能单独证明当前进程已使用 S3，需查看有效配置和日志。
