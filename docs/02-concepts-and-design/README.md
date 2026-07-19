# 概念与设计

本板块解释 Plasmod 的核心不变量和可执行设计。[System Design Reference](system-design/README.md) 是面向代码核对的详细真值入口，严格区分 Architecture、Chain、Perspective、Mechanism 和 Engine。

推荐顺序：

1. [Design Overview](design-overview.md)
2. [System Architecture](system-architecture.md)
3. [30 项 System Design Reference](system-design/README.md)
4. [Source of Truth](source-of-truth-model.md)
5. [Write Path](write-path-design.md) 与 [Query Path](query-path-design.md)
6. [Consistency](consistency-model.md)、[Concurrency](concurrency-model.md)、[Failure](failure-model.md)
7. [ADR Index](adr/README.md)

快速概念页解释主要不变量；System Design Reference 提供每项的 package、file、constructor、public method、typed I/O、状态、副作用、失败恢复、成熟度和不可过度声明边界。
