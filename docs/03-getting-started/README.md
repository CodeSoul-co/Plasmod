# Getting Started

本目录提供从源码或 Docker 启动 Plasmod，并完成一次 Event 写入、物化、查询和追踪的最短闭环。

## 推荐路径

1. [`prerequisites.md`](prerequisites.md)
2. [`install-from-source.md`](install-from-source.md) 或 [`run-with-docker.md`](run-with-docker.md)
3. [`first-event-and-query.md`](first-event-and-query.md)
4. [`verify-installation.md`](verify-installation.md)
5. [`stop-reset-and-cleanup.md`](stop-reset-and-cleanup.md)

需要从 Python 调用时，继续阅读 [`python-sdk-quickstart.md`](python-sdk-quickstart.md)。

## 默认端口

| 模式 | HTTP | gRPC | MinIO API | MinIO Console |
|---|---:|---:|---:|---:|
| 本地统一模式 | `8080` | `19531` | 按需配置 | 按需配置 |
| Docker 拆分模式 | 管理 `9091`，数据 `19530` | `19531` | `9000` | `9001` |

端口来源是 `src/internal/app/ports.go` 和仓库根目录 Compose 文件。
