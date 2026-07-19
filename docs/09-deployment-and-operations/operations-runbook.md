# Operations Runbook

## Start

1. 启动 S3/MinIO（若启用）；
2. 检查 data dir 和 secret；
3. 启动 Plasmod；
4. 检查 health/effective config/storage/provider；
5. 执行受控 write/query/trace；
6. 开放流量。

## Stop

1. 从 load balancer 摘除；
2. 停止新写；
3. 等待 queue/visible LSN；
4. 发送 SIGTERM；
5. 等待 checkpoint/shutdown；
6. 确认端口和进程退出。

## Daily checks

- health、restart count；
- disk/WAL/Badger size；
- visible lag/queue/retry；
- S3/provider health；
- failed admin task；
- backup freshness。

## Incident evidence

保存版本、effective config（脱敏）、LSN/checkpoint、相关 event/object ID、错误首发时间、日志和资源状态，再进行
repair。不要先删除 data dir 或重建所有索引。
