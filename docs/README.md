# 00. Plasmod 核心库工程文档

> Language: 中文 | [English](en/README.md)

本目录只描述 Plasmod 核心库的公开行为、实现边界、代码结构、构建、部署与维护方式。事实来源是当前仓库中的 Go/C++ 代码、Schema、配置、构建脚本和测试；规划接口与未接入实现会显式标记，不作为已完成能力声明。

## 00.1. 顺序阅读

| 顺序 | 文档 | 主要问题 |
|---|---|---|
| 01 | [项目总览、需求与系统边界](01-project-overview-and-requirements.md) | 系统为何存在，需求和边界是什么 |
| 02 | [整体系统架构与设计原则](02-system-architecture-and-design.md) | 系统由哪些层和模块构成 |
| 03 | [规范对象模型与记忆生命周期](03-canonical-data-model-and-memory-lifecycle.md) | Event 如何成为规范对象，Memory 如何演化 |
| 04 | [运行时、四条执行 Chain 与调度](04-runtime-chains-and-scheduling.md) | 请求如何经过四条 Chain 与调度组件 |
| 05 | [一致性、恢复与正确性模型](05-consistency-recovery-and-correctness.md) | 一致性阶段、失败窗口和恢复语义是什么 |
| 06 | [检索、查询与证据构建](06-retrieval-query-and-evidence.md) | 如何检索、hydrate 并构建证据 |
| 07 | [作用域、治理、协作与安全](07-governance-collaboration-and-security.md) | 作用域、共享、治理和安全如何工作 |
| 08 | [API、Schema、配置与 SDK 参考](08-api-schema-and-sdk-reference.md) | API、Schema、配置和 SDK 如何调用 |
| 09 | [安装、启动与用户操作手册](09-getting-started-and-user-guide.md) | 如何安装、启动和完成用户操作 |
| 10 | [代码库、接口实现与函数调用路径](10-codebase-interfaces-and-call-paths.md) | 接口、实现、字段和函数调用在哪里 |
| 11 | [依赖、构建、测试与开发流程](11-dependencies-build-and-development.md) | 依赖、构建、测试和开发流程是什么 |
| 12 | [部署、运维、恢复与故障排查](12-deployment-operations-and-troubleshooting.md) | 如何部署、运维、恢复和排障 |
| 13 | [扩展、兼容性与系统演进](13-extensibility-compatibility-and-evolution.md) | 如何扩展并保持兼容 |
| 14 | [实现状态、缺口与可声明边界](14-implementation-status-gaps-and-claim-boundaries.md) | 哪些能力完成，哪些仍有缺口 |
| 15 | [架构决策记录](15-architecture-decision-records.md) | 关键架构决策及其后果是什么 |

## 00.2. 按角色阅读

| 读者 | 最短路径 |
|---|---|
| 首次了解 Plasmod | 01 -> 02 -> 03 -> 04 |
| API/SDK 使用者 | 01 -> 08 -> 09 |
| 核心开发者 | 02 -> 03 -> 04 -> 10 -> 11 -> 14 |
| 部署与运维人员 | 09 -> 12 -> 05 |
| 架构评审者 | 02 -> 04 -> 05 -> 06 -> 07 -> 14 -> 15 |

## 00.3. 状态标签

| 标签 | 定义 |
|---|---|
| Implemented | 当前 bootstrap 或主调用链可达，并有代码或测试支撑 |
| Partial | 存在实现，但覆盖面、持久化、隔离、接口或恢复语义不完整 |
| Experimental | 核心代码可用，但接口、配置或兼容性尚未承诺稳定 |
| Defined-not-wired | 类型和实现存在，但当前 composition root 不调用 |
| Contract-only | 只有接口或 Schema，核心库没有 active production implementation |
| Planned | 目标设计，当前代码尚未实现 |

## 00.4. 事实来源优先级

1. src/cmd/server/main.go 与 src/internal/app/bootstrap.go 的真实启动链。
2. src/internal/access/gateway.go、gateway_rpc.go 与 gRPC server 的真实接口。
3. src/internal/worker/runtime.go、consistency controller 与 Chain 的真实调用顺序。
4. src/internal/schemas/、src/internal/storage/contracts.go 与 event backbone contract。
5. Makefile、Compose、.env.example、配置加载器及对应测试。

历史注释或设计名称若与可执行路径冲突，以代码和测试为准；差异统一记录在第 14 章。
