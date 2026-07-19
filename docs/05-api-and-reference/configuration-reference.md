# Configuration Reference

## 核心启动配置

| Variable | Default | Purpose |
|---|---|---|
| `PLASMOD_STORAGE` | `disk` | `disk` 或 `memory` |
| `PLASMOD_DATA_DIR` | `.andb_data` | Badger、WAL、checkpoint 根目录 |
| `PLASMOD_EMBEDDER` | 由配置解析 | `tfidf`、ONNX/其他 provider |
| `PLASMOD_GRPC_ENABLED` | enabled | 是否启动 gRPC |
| `PLASMOD_ADMIN_API_KEY` | empty | admin route key |
| `APP_MODE` | 非 prod | visibility/debug 行为 |
| `PLASMOD_SKIP_VECTOR_INDEX` | false | 全局跳过向量投影 |

## 端口

统一模式默认 HTTP `127.0.0.1:8080`；拆分默认 management `0.0.0.0:9091`、API
`0.0.0.0:19530`；gRPC 默认 `0.0.0.0:19531`。精确变量与解析逻辑见
`src/internal/app/ports.go`。

## Consistency

默认 strict，相关默认值包括 queue 4096、worker 4、retry 8、bounded lag 1s、query/shutdown timeout
30s 和 checkpoint flush 50ms。覆盖项由 `worker/consistency` 的环境解析定义，管理 API 可在运行时修改模式。

## S3/MinIO

配置包括 endpoint、bucket、access key、secret key、TLS、region/prefix。敏感值不应出现在日志、文档示例或
版本控制中。

## YAML 的真实状态

当前启动代码会读取 `configs/memory_tiering.yaml` 和 `configs/algorithm_*.yaml`。仓库中的
`configs/app.yaml`、`storage.yaml`、`retrieval.yaml`、`graph.yaml` 不能单独作为运行时配置真值；在
`app.BuildServer` 未接入前，它们应视为参考/兼容配置。

使用 `/v1/admin/config/effective` 和启动日志确认最终值。
