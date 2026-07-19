# Config Index

| Reader | Active inputs |
|---|---|
| `app/ports.go` | HTTP/gRPC address and size envs |
| `storage/factory.go` | storage mode, data dir, WAL/checkpoint, S3 envs |
| `worker/consistency` | mode, queue, workers, retry, timeout, checkpoint envs |
| `dataplane` | embedder/retrieval envs |
| memory provider loader | `configs/memory_tiering.yaml`, `configs/algorithm_*.yaml` |
| access middleware | APP_MODE, admin key |

`configs/app.yaml`、`storage.yaml`、`retrieval.yaml`、`graph.yaml` 当前不构成 BuildServer 的完整配置源。
