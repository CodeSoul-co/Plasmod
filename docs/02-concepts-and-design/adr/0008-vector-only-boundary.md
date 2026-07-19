# ADR-0008: Keep Vector-only Ingest As A Physical Projection Interface

## Status

Accepted.

## Context

预计算 embedding 和批量 segment 构建需要绕过 Event text embedding，但纯向量记录无法表达 Session、State、
Artifact、Edge、Version、Policy 和 provenance。

## Decision

保留 `/v1/ingest/vectors` 和 Warm Segment register/query 作为物理 retrieval projection 接口。Agent-native
业务写入仍使用 Event/canonical path；vector-only interface 不升级为 canonical source of truth。

## Consequences

- 可复用外部预计算向量并减少重复 embedding；
- 调用方必须提供稳定 object ID mapping 和 embedding compatibility tuple；
- 只写向量无法通过 WAL replay 恢复完整 Agent 对象；
- Query 使用这些候选后仍需 Go 层 canonical/evidence 处理。
