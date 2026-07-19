# 约束与非目标

## 一致性与事务

- Event ingest 有 WAL 和 projection 一致性控制，但 canonical direct CRUD 路由不全部经过 WAL。
- 不提供跨 Badger、S3、external embedder 和 native index 的全局 ACID 事务。
- 不保证所有异步 materializer 和外部副作用 exactly once；实现以 LSN、checkpoint、deterministic ID 和 retry 降低重复影响。

## 系统边界

- Plasmod 不是 agent framework、LLM gateway、tool executor 或 workflow scheduler。
- Plasmod 不实现 S3、Badger、Knowhere、FAISS、DiskANN、ONNX Runtime 或 TensorRT 的内部算法。
- `coordinator/controlplane`、`eventbackbone/streamplane`、`platformpkg` 中的大量上游兼容代码不等于当前 BuildServer 已启用完整分布式控制面。

## API 与安全

- 当前没有统一用户认证、OAuth/OIDC、细粒度 RBAC 或 TLS termination。
- `PLASMOD_ADMIN_API_KEY` 只保护 `/v1/admin/*`；公共与 internal data routes 仍需网络边界保护。
- 列表型 CRUD 缺少统一 pagination/cursor contract。
- `/v1/internal/*` 不是稳定公开 API。

## 存储与部署

- 默认启动为单进程；disk mode 默认目录为 `.andb_data`。
- S3 cold tier 是可选归档 backend，不替代 canonical Badger/WAL 的所有职责。
- index、WAL 和 Badger format 的跨版本迁移工具仍不完整。
- Docker Compose 使用开发凭据，不能直接视为生产安全配置。

## SDK 与可选能力

- Python SDK 仅覆盖部分 HTTP；Node SDK 当前主要覆盖 consistency mode。
- gRPC 只覆盖 health、event/vector ingest 和 query/batch query。
- ANN、GPU、ONNX、GGUF、TensorRT 和部分 index type 依赖 build tag/native library/platform。
- MemoryBank/Zep profile 的存在不代表所有论文算法或外部产品行为已完整复现。
