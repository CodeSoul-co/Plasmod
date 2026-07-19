# ADR-0006: Use Knowhere-style Native ANN Behind An Adapter

## Status

Accepted, build-dependent.

## Context

Plasmod 需要 HNSW、IVF 和 DiskANN 等物理索引能力，但不应在 Go runtime 中重复实现成熟 ANN engine，也不能
把第三方内部对象模型暴露为 Agent database contract。

## Decision

在 `cpp/vendor` 保留 source-level Knowhere-style engine，通过 `cpp/retrieval` 组合 C ABI，再由
`dataplane/retrievalplane` CGO bridge 调用。Go 层保留 object ID mapping、scope、policy、tiering、fusion 和
Evidence。

## Consequences

- 获得多 index backend 与 native performance；
- 增加 CMake、CGO、ABI、license 和平台维护成本；
- build feature 决定某 index 是否可用；
- pure Go stub 仍可运行 canonical/lexical 路径；
- 不得把 Knowhere 内部实现宣称为 Plasmod 自有算法。
