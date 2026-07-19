# 文档地图

| 板块 | 内容 | 适合读者 |
|---|---|---|
| [00 Overview](./) | 定位、边界、能力与术语 | 所有人 |
| [01 Requirements](../01-requirements/) | 问题、用例、功能/非功能要求和追踪 | 产品、架构、核心开发 |
| [02 Concepts and Design](../02-concepts-and-design/) | source of truth、写/读路径、一致性、失败模型和 [30 项系统设计核对](../02-concepts-and-design/system-design/README.md) | 架构与核心开发 |
| [03 Getting Started](../03-getting-started/) | 安装、启动、首次请求、验证、清理 | 新用户 |
| [04 User Guide](../04-user-guide/) | 按用户任务使用每项核心能力 | 应用开发者 |
| [05 API and Reference](../05-api-and-reference/) | HTTP/gRPC/binary、schema、配置与错误 | 集成开发者 |
| [06 Codebase](../06-codebase/) | 仓库、package、interface、调用链、存储 key | 核心开发者 |
| [07 Dependencies](../07-dependencies/) | native stack、Badger、S3、embedder 与升级边界 | 构建与维护人员 |
| [08 Development](../08-development/) | 构建、调试、测试、贡献和常见修改 | 贡献者 |
| [09 Operations](../09-deployment-and-operations/) | 部署、安全、监控、恢复与排障 | 运维人员 |
| [10 Extensibility](../10-extensibility/) | 新 schema、materializer、operator、backend | 扩展开发者 |
| [11 Evolution](../11-evolution-and-status/) | 成熟度、兼容、迁移和限制 | 发布与维护人员 |

## 代码事实入口

- 启动：`src/cmd/server/main.go`, `src/internal/app/bootstrap.go`
- HTTP：`src/internal/access/gateway.go`
- gRPC：`src/internal/api/grpc/proto/plasmod/v1/plasmod.proto`
- Event schema：`src/internal/schemas/dynamic_event.go`
- canonical schema：`src/internal/schemas/canonical.go`
- runtime：`src/internal/worker/runtime.go`, `runtime_consistency.go`
- storage：`src/internal/storage/contracts.go`, `factory.go`
- retrieval：`src/internal/dataplane/`, `src/internal/dataplane/retrievalplane/`, `cpp/`
- consistency：`src/internal/worker/consistency/`

## 详细设计入口

| 需要核对的内容 | 文档 |
|---|---|
| 3 Architecture + 4 Chains + 5 Perspectives + 8 Mechanisms + 10 Engines | [System Design Reference](../02-concepts-and-design/system-design/README.md) |
| canonical/Event/Query/Worker 字段与 typed I/O | [Object and Message Registry](../02-concepts-and-design/system-design/06-cross-reference/object-and-message-registry.md) |
| interface、实现、constructor、接线状态 | [Interface Implementation Registry](../02-concepts-and-design/system-design/06-cross-reference/interface-implementation-registry.md) |
| route 到 Chain/Engine 的真实映射 | [API to Engine Matrix](../02-concepts-and-design/system-design/06-cross-reference/api-to-engine-matrix.md) |
| 同步/异步、阶段、失败窗口和恢复 | [Execution State and Failure Matrix](../02-concepts-and-design/system-design/06-cross-reference/execution-state-and-failure-matrix.md) |
| 可以声明与不可过度声明的能力 | [Claim and Test Boundary](../02-concepts-and-design/system-design/06-cross-reference/claim-and-test-boundary.md) |
