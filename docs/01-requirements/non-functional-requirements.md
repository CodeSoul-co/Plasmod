# 非功能需求

本文定义工程要求，不给出基准结果。

| ID | 类别 | 要求 | 当前证据/边界 |
|---|---|---|---|
| NFR-PERF-001 | Performance | 写入必须有有界 admission；retrieval index build 不应在每次写入做全量同步重建。 | Gateway semaphore、consistency queue、background flush。 |
| NFR-FRESH-001 | Freshness | 系统必须区分 WAL accepted、object visible 和 retrieval visible，并暴露 watermark/lag。 | consistency tracker/controller。 |
| NFR-COR-001 | Correctness | canonical object、edge、version 的共享持久 backend 必须支持原子 projection。 | factory 强制 objects/edges/versions 同 backend；Badger transaction。 |
| NFR-DUR-001 | Durability | disk mode 的 WAL 与 canonical store 必须可在进程重启后读取。 | FileWAL + Badger；需要外部备份策略。 |
| NFR-AVL-001 | Availability | 可恢复错误应返回明确状态；shutdown 必须停止 admission、drain worker 并关闭存储。 | controller lifecycle 与 ServerBundle.Shutdown。 |
| NFR-SCL-001 | Scalability | queue、worker、write semaphore 和 batch 接口必须可配置；单机实现不得被描述为已验证分布式集群。 | env config；当前核心启动为单进程。 |
| NFR-SEC-001 | Security | admin route 在部署时必须启用 shared key 或由反向代理保护；生产响应不得暴露 debug/raw 字段。 | admin auth + visibility middleware。 |
| NFR-ISO-001 | Tenant isolation | tenant/workspace/session 标识必须贯穿 Event、Query 和对象；服务端必须明确哪些路径实际强制过滤。 | schema 完整，enforcement 为 Partial。 |
| NFR-OBS-001 | Observability | 必须提供 health、admin metrics、topology、storage/config 状态和可辨认错误。 | HTTP management routes 与 runtime stats。 |
| NFR-MNT-001 | Maintainability | source of truth、projection、third-party ownership 和 extension registration 必须文档化。 | 本文档体系与 package contracts。 |
| NFR-COMP-001 | Compatibility | API、schema、WAL、storage key、embedding family 和 native ABI 变更必须有迁移/回滚说明。 | evolution docs；当前版本化能力不完整。 |
| NFR-PORT-001 | Portability | pure Go/lexical 路径应在无 native bridge 时可启动；native/GPU 能力按平台声明。 | retrieval stub、build tags、CMake options。 |

## 验证原则

功能测试证明行为，不替代生产容量、故障注入、安全审计或平台认证。任何性能、可用性或扩展性结论都必须在独立验证中产生，不能写进核心功能文档作为既定事实。
