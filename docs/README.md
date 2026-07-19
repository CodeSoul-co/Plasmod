# Plasmod 工程文档

本目录描述 Plasmod 核心库的公开行为、实现边界和维护方式。内容以当前仓库中的 Go、C++、配置、构建脚本、SDK 和测试为事实来源。

## 阅读入口

| 目标 | 建议路径 |
|---|---|
| 先理解 Plasmod | [项目总览](00-overview/project-overview.md) -> [能力地图](00-overview/capability-map.md) -> [系统架构](02-concepts-and-design/system-architecture.md) |
| 启动并完成首次写入 | [前置条件](03-getting-started/prerequisites.md) -> [Quickstart](03-getting-started/quickstart.md) -> [首次 Event 与 Query](03-getting-started/first-event-and-query.md) |
| 集成 API 或 SDK | [API 总览](05-api-and-reference/api-overview.md) -> [HTTP API](05-api-and-reference/public-http-api.md) -> [SDK Reference](05-api-and-reference/sdk-reference.md) |
| 阅读核心代码 | [仓库总览](06-codebase/repository-overview.md) -> [架构到代码映射](06-codebase/architecture-to-code-map.md) -> [调用链](06-codebase/call-paths/) |
| 修改或扩展实现 | [本地开发](08-development/local-development.md) -> [常见代码修改](08-development/common-code-changes.md) -> [扩展模型](10-extensibility/extension-overview.md) |
| 部署和排障 | [部署模式](09-deployment-and-operations/deployment-modes.md) -> [运维 Runbook](09-deployment-and-operations/operations-runbook.md) -> [故障排查](09-deployment-and-operations/troubleshooting.md) |

完整导航见 [Documentation Map](00-overview/documentation-map.md)。

## 状态标签

- **Implemented**：启动路径可达，并有代码或测试支撑。
- **Partial**：存在实现，但覆盖面、持久化、隔离、接口或恢复语义不完整。
- **Experimental**：代码可用但接口、配置或兼容性尚未承诺稳定。
- **Not Confirmed**：仓库中存在声明或结构，但未发现当前启动路径或测试证明。
- **Deprecated**：保留用于兼容，不应作为新集成入口。

## 事实来源优先级

1. `src/cmd/server/main.go` 与 `src/internal/app/bootstrap.go` 的真实启动链。
2. `src/internal/access/gateway.go`、`gateway_rpc.go` 和 gRPC proto 的真实接口。
3. `src/internal/schemas/`、`storage/contracts.go` 和 `eventbackbone/contracts.go` 的类型与不变量。
4. `Makefile`、Docker Compose、`.env.example` 和实际配置加载器。
5. 与上述路径对应的单元和集成测试。

注释、旧配置文件和 README 若与可执行路径冲突，以代码和测试为准；差异记录在 [Known Limitations](11-evolution-and-status/known-limitations.md)。

## 核心边界

文档只覆盖核心库。仓库中的 `src/internal/platformpkg/`、`coordinator/controlplane/`、`eventbackbone/streamplane/` 与 `cpp/vendor/` 需要按上游/兼容代码处理，不能作为 Plasmod 自研实现宣称；边界见 [Component Ownership](06-codebase/component-ownership.md)。
