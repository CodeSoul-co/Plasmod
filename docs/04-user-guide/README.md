# User Guide

本目录按 Agent runtime 的工作流说明 Plasmod，而不是按 Go package 罗列函数。

## 数据写入

- [`ingest-events.md`](ingest-events.md)
- [`manage-agents-and-sessions.md`](manage-agents-and-sessions.md)
- [`manage-agent-state.md`](manage-agent-state.md)
- [`manage-artifacts.md`](manage-artifacts.md)

## 查询与解释

- [`query-memories.md`](query-memories.md)
- [`query-relations.md`](query-relations.md)
- [`trace-and-provenance.md`](trace-and-provenance.md)

## 生命周期与治理

- [`memory-lifecycle.md`](memory-lifecycle.md)
- [`sharing-and-visibility.md`](sharing-and-visibility.md)
- [`deletion-and-purge.md`](deletion-and-purge.md)
- [`replay.md`](replay.md)
- [`tiered-storage.md`](tiered-storage.md)
- [`runtime-modes.md`](runtime-modes.md)

除非明确需要直接维护 canonical record，业务写入优先使用 `/v1/ingest/events`。该入口会经过
WAL、consistency controller、materialization 和 retrieval projection；直接 CRUD 路由不自动获得完整的
Event replay 语义。
