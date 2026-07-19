# ADR-0004: Hot/Warm/Cold 分层

- Status: Accepted
- Context: agent runtime 同时需要低延迟活跃对象、完整在线对象和低成本归档。
- Decision: HotObjectCache 保存高活跃对象，warm ObjectStore/segment 保存在线事实，cold store 保存显式归档对象。
- Consequences: promotion/archive/delete 必须跨 tier 协调；cold query 只在显式 `include_cold` 时执行。
- Alternatives: 所有对象常驻内存或每次写入同步 S3，因容量或写放大被拒绝。
- Invariant: cold copy 不替代 canonical/WAL backup，archive 不等于 hard delete。
