# S3 And MinIO Integration

S3/MinIO 是 ColdObjectStore，可保存归档 Memory、Embedding、AgentState、Artifact、Edge 和相应索引。

## Required configuration

- endpoint、bucket；
- access/secret key；
- region、TLS；
- key prefix。

Docker Compose 启动 MinIO API `9000` 和 console `9001`。Plasmod 连接的是 API 端口，不是 console。

## Consistency

S3 写入与 Badger canonical transaction 分离。归档流程应先确认对象成功写 Cold，再根据策略清理 Warm。
Cold purge 也需要验证对象 key 和 edge index key 都被处理。

## Security

使用专用 bucket/credential、最小权限、TLS、server-side encryption 和 bucket lifecycle。不要把 compose 默认凭证
用于正式环境。
